// Package statereconcile compares host-private authorized state with read-only
// observations. It detects drift; it neither repairs state nor attributes actors.
package statereconcile

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const Schema = "flotilla.authorized_state/v1"

const (
	KindExecutableVersion = "executable-version"
	KindFileSHA256        = "file-sha256"
	KindSystemdUser       = "systemd-user-service"
)

type Manifest struct {
	Schema       string    `json:"schema"`
	AuthorizedAt time.Time `json:"authorized_at"`
	Checks       []Check   `json:"checks"`
}

type Check struct {
	ID                       string `json:"id"`
	Kind                     string `json:"kind"`
	InstructionRef           string `json:"instruction_ref"`
	Path                     string `json:"path,omitempty"`
	Unit                     string `json:"unit,omitempty"`
	ExpectedVersion          string `json:"expected_version,omitempty"`
	ExpectedSHA256           string `json:"expected_sha256,omitempty"`
	ExpectedActive           string `json:"expected_active,omitempty"`
	ExpectedExecutable       string `json:"expected_executable,omitempty"`
	ExpectedExecutableSHA256 string `json:"expected_executable_sha256,omitempty"`
}

type ServiceObservation struct {
	Active           string
	Executable       string
	ExecutableSHA256 string
}

// Observer is deliberately typed: a manifest cannot supply an arbitrary shell
// command. Production uses DefaultObserver; tests inject observations.
type Observer interface {
	ExecutableVersion(context.Context, string) (string, error)
	FileSHA256(string) (string, error)
	SystemdUserService(context.Context, string, bool) (ServiceObservation, error)
}

type Result struct {
	ID             string            `json:"id"`
	Kind           string            `json:"kind"`
	InstructionRef string            `json:"instruction_ref"`
	Status         string            `json:"status"`
	Expected       map[string]string `json:"expected"`
	Observed       map[string]string `json:"observed,omitempty"`
	Error          string            `json:"error,omitempty"`
}

type Report struct {
	Schema    string    `json:"schema"`
	CheckedAt time.Time `json:"checked_at"`
	Status    string    `json:"status"`
	Checks    []Result  `json:"checks"`
}

func (r Report) ExitCode() int {
	if r.Status == "error" {
		return 2
	}
	if r.Status == "drift" {
		return 1
	}
	return 0
}

func Load(path string) (Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 1<<20))
	dec.DisallowUnknownFields()
	var manifest Manifest
	if err := dec.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := ensureJSONEOF(dec); err != nil {
		return Manifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func ensureJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode manifest trailer: %w", err)
	}
	return errors.New("manifest contains more than one JSON value")
}

func (m Manifest) Validate() error {
	if m.Schema != Schema {
		return fmt.Errorf("manifest schema %q, want %q", m.Schema, Schema)
	}
	if m.AuthorizedAt.IsZero() {
		return errors.New("manifest authorized_at is required")
	}
	if len(m.Checks) == 0 {
		return errors.New("manifest checks must not be empty")
	}
	seen := make(map[string]bool, len(m.Checks))
	for i, check := range m.Checks {
		if err := validateCheck(check); err != nil {
			return fmt.Errorf("check %d: %w", i, err)
		}
		if seen[check.ID] {
			return fmt.Errorf("check %d: duplicate id %q", i, check.ID)
		}
		seen[check.ID] = true
	}
	return nil
}

func validateCheck(c Check) error {
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.InstructionRef) == "" {
		return errors.New("id and instruction_ref are required")
	}
	switch c.Kind {
	case KindExecutableVersion:
		if !filepath.IsAbs(c.Path) || strings.TrimSpace(c.ExpectedVersion) == "" {
			return errors.New("executable-version requires absolute path and expected_version")
		}
	case KindFileSHA256:
		if !filepath.IsAbs(c.Path) || !validDigest(c.ExpectedSHA256) {
			return errors.New("file-sha256 requires absolute path and sha256:<64 lowercase hex> expected_sha256")
		}
	case KindSystemdUser:
		if !validUnit(c.Unit) || strings.TrimSpace(c.ExpectedActive) == "" {
			return errors.New("systemd-user-service requires unit and expected_active")
		}
		if c.ExpectedExecutable != "" && !filepath.IsAbs(c.ExpectedExecutable) {
			return errors.New("expected_executable must be absolute")
		}
		if c.ExpectedExecutableSHA256 != "" && !validDigest(c.ExpectedExecutableSHA256) {
			return errors.New("expected_executable_sha256 must be sha256:<64 lowercase hex>")
		}
	default:
		return fmt.Errorf("unknown kind %q", c.Kind)
	}
	return nil
}

func validUnit(unit string) bool {
	if unit == "" || strings.HasPrefix(unit, "-") {
		return false
	}
	for _, r := range unit {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("_.@:-", r) {
			continue
		}
		return false
	}
	return true
}

func validDigest(v string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(v, prefix) || len(v) != len(prefix)+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(v, prefix))
	return err == nil && strings.ToLower(v) == v
}

func Run(ctx context.Context, manifest Manifest, observer Observer, now time.Time) Report {
	report := Report{Schema: Schema, CheckedAt: now.UTC(), Status: "clean"}
	for _, check := range manifest.Checks {
		result := observe(ctx, check, observer)
		report.Checks = append(report.Checks, result)
		if result.Status == "error" {
			report.Status = "error"
		} else if result.Status == "drift" && report.Status == "clean" {
			report.Status = "drift"
		}
	}
	return report
}

func observe(ctx context.Context, check Check, observer Observer) Result {
	result := Result{ID: check.ID, Kind: check.Kind, InstructionRef: check.InstructionRef, Status: "clean", Expected: map[string]string{}, Observed: map[string]string{}}
	var err error
	switch check.Kind {
	case KindExecutableVersion:
		result.Expected["version"] = check.ExpectedVersion
		result.Observed["version"], err = observer.ExecutableVersion(ctx, check.Path)
	case KindFileSHA256:
		result.Expected["sha256"] = check.ExpectedSHA256
		result.Observed["sha256"], err = observer.FileSHA256(check.Path)
	case KindSystemdUser:
		result.Expected["active"] = check.ExpectedActive
		if check.ExpectedExecutable != "" {
			result.Expected["executable"] = check.ExpectedExecutable
		}
		if check.ExpectedExecutableSHA256 != "" {
			result.Expected["executable_sha256"] = check.ExpectedExecutableSHA256
		}
		var got ServiceObservation
		inspectExecutable := check.ExpectedExecutable != "" || check.ExpectedExecutableSHA256 != ""
		got, err = observer.SystemdUserService(ctx, check.Unit, inspectExecutable)
		result.Observed["active"] = got.Active
		if got.Executable != "" {
			result.Observed["executable"] = got.Executable
		}
		if got.ExecutableSHA256 != "" {
			result.Observed["executable_sha256"] = got.ExecutableSHA256
		}
	}
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	for key, want := range result.Expected {
		if result.Observed[key] != want {
			result.Status = "drift"
			break
		}
	}
	return result
}

type DefaultObserver struct {
	ProbeTimeout time.Duration
}

func (o DefaultObserver) timeout() time.Duration {
	if o.ProbeTimeout <= 0 {
		return 5 * time.Second
	}
	return o.ProbeTimeout
}

func (o DefaultObserver) ExecutableVersion(ctx context.Context, path string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, o.timeout())
	defer cancel()
	out, err := exec.CommandContext(probeCtx, path, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("observe executable version %s: %w", path, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (DefaultObserver) FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("observe file %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash file %s: %w", path, err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func (o DefaultObserver) SystemdUserService(ctx context.Context, unit string, inspectExecutable bool) (ServiceObservation, error) {
	active, err := o.systemctl(ctx, unit, "ActiveState")
	if err != nil {
		return ServiceObservation{}, err
	}
	pidText, err := o.systemctl(ctx, unit, "MainPID")
	if err != nil {
		return ServiceObservation{}, err
	}
	result := ServiceObservation{Active: active}
	pid, err := strconv.Atoi(pidText)
	if err != nil {
		return result, fmt.Errorf("observe service %s: invalid MainPID %q", unit, pidText)
	}
	if pid <= 0 {
		if active == "active" {
			return result, fmt.Errorf("observe service %s: active with no MainPID", unit)
		}
		return result, nil
	}
	if !inspectExecutable {
		return result, nil
	}
	executable, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil {
		return result, fmt.Errorf("observe service %s executable: %w", unit, err)
	}
	result.Executable = strings.TrimSuffix(executable, " (deleted)")
	result.ExecutableSHA256, err = (DefaultObserver{}).FileSHA256(result.Executable)
	if err != nil {
		return result, err
	}
	return result, nil
}

func (o DefaultObserver) systemctl(ctx context.Context, unit, property string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, o.timeout())
	defer cancel()
	out, err := exec.CommandContext(probeCtx, "systemctl", "--user", "show", unit, "--property="+property, "--value").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("observe service %s %s: %w", unit, property, err)
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return "", fmt.Errorf("observe service %s %s: empty value", unit, property)
	}
	return value, nil
}

func WriteHuman(w io.Writer, report Report) {
	fmt.Fprintf(w, "authorized-state: %s (%d checks)\n", report.Status, len(report.Checks))
	for _, check := range report.Checks {
		fmt.Fprintf(w, "%s %-5s %s instruction=%s", strings.ToUpper(check.Status), check.Kind, check.ID, check.InstructionRef)
		if check.Error != "" {
			fmt.Fprintf(w, " error=%q", check.Error)
		} else if check.Status == "drift" {
			fmt.Fprintf(w, " expected=%s observed=%s", formatValues(check.Expected), formatValues(check.Observed))
		}
		fmt.Fprintln(w)
	}
}

func formatValues(values map[string]string) string {
	var b strings.Builder
	w := bufio.NewWriter(&b)
	first := true
	for _, key := range []string{"version", "sha256", "active", "executable", "executable_sha256"} {
		value, ok := values[key]
		if !ok {
			continue
		}
		if !first {
			fmt.Fprint(w, ",")
		}
		fmt.Fprintf(w, "%s=%q", key, value)
		first = false
	}
	w.Flush()
	return b.String()
}
