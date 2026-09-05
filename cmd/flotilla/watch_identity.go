package main

import (
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type watchProcessIdentity struct {
	Kind            string   `json:"kind"`
	PID             int      `json:"pid"`
	DiskPath        string   `json:"disk_path"`
	DiskSHA256      string   `json:"disk_sha256"`
	ExeSHA256       string   `json:"exe_sha256"`
	Revision        string   `json:"vcs_revision"`
	DeletedInode    bool     `json:"deleted_inode,omitempty"`
	Leftover        bool     `json:"leftover,omitempty"`
	ListenAddresses []string `json:"listen_addresses,omitempty"`
	Warning         string   `json:"warning,omitempty"`
}

type watchBinaryInspector func(exePath, diskPath string) (diskSHA, exeSHA, revision string)

func cmdWatchIdentity(args []string) error {
	asJSON := false
	for _, arg := range args {
		if arg == "--json" {
			asJSON = true
			continue
		}
		return fmt.Errorf("usage: flotilla watch identity [--json]")
	}
	identities, err := collectWatchIdentities("/proc", inspectWatchIdentityBinary)
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(identities)
	}
	if len(identities) == 0 {
		fmt.Println("no running flotilla watch or dash processes")
		return nil
	}
	for _, identity := range identities {
		fmt.Printf("%s pid=%d disk=%s disk_sha256=%s exe_sha256=%s vcs.revision=%s",
			identity.Kind, identity.PID, identity.DiskPath, identity.DiskSHA256,
			identity.ExeSHA256, identity.Revision)
		if identity.Warning == "" {
			fmt.Printf(" deleted_inode=%t", identity.DeletedInode)
		}
		if identity.Kind == "dash" {
			binds := identity.ListenAddresses
			if len(binds) == 0 {
				binds = []string{"none"}
			}
			fmt.Printf(" listen=%s", strings.Join(binds, ","))
			if identity.Warning == "" || identity.Leftover {
				fmt.Printf(" leftover=%t", identity.Leftover)
			}
		}
		if identity.Warning != "" {
			fmt.Printf(" warning=%q", identity.Warning)
		}
		fmt.Println()
	}
	return nil
}

func inspectWatchIdentityBinary(exePath, diskPath string) (string, string, string) {
	diskSHA, err := fileSHA256(diskPath)
	if err != nil {
		diskSHA = "unavailable"
	}
	exeSHA, err := fileSHA256(exePath)
	if err != nil {
		exeSHA = "unavailable"
	}
	revision := "unavailable"
	if info, err := buildinfo.ReadFile(exePath); err == nil {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				revision = setting.Value
				break
			}
		}
	}
	return diskSHA, exeSHA, revision
}

func collectWatchIdentities(procRoot string, inspect watchBinaryInspector) ([]watchProcessIdentity, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, fmt.Errorf("watch identity: read process table: %w", err)
	}
	identities := make([]watchProcessIdentity, 0)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || !entry.IsDir() {
			continue
		}
		pidRoot := filepath.Join(procRoot, entry.Name())
		cmdline, err := os.ReadFile(filepath.Join(pidRoot, "cmdline"))
		if err != nil {
			continue
		}
		kind, versioned := watchIdentityCommand(cmdline)
		if kind == "" {
			continue
		}
		identity := watchProcessIdentity{
			Kind: kind, PID: pid, DiskPath: "unavailable", DiskSHA256: "unavailable",
			ExeSHA256: "unavailable", Revision: "unavailable",
		}
		exeLink, err := os.Readlink(filepath.Join(pidRoot, "exe"))
		if err != nil {
			identity.Warning = fmt.Sprintf("executable identity unavailable: %v", err)
			if kind == "dash" {
				identity.Leftover = versioned
				identity.ListenAddresses = processListenAddresses(pidRoot)
			}
			identities = append(identities, identity)
			continue
		}
		deleted := strings.HasSuffix(exeLink, " (deleted)")
		diskPath := strings.TrimSuffix(exeLink, " (deleted)")
		diskSHA, exeSHA, revision := inspect(filepath.Join(pidRoot, "exe"), diskPath)
		identity.DiskPath = diskPath
		identity.DiskSHA256 = diskSHA
		identity.ExeSHA256 = exeSHA
		identity.Revision = revision
		identity.DeletedInode = deleted
		if kind == "dash" {
			identity.Leftover = deleted || versioned
			identity.ListenAddresses = processListenAddresses(pidRoot)
		}
		identities = append(identities, identity)
	}
	sort.Slice(identities, func(i, j int) bool { return identities[i].PID < identities[j].PID })
	return identities, nil
}

func watchIdentityCommand(raw []byte) (string, bool) {
	parts := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	if len(parts) < 2 {
		return "", false
	}
	base := filepath.Base(parts[0])
	versioned := strings.HasPrefix(base, "flotilla-")
	if base != "flotilla" && !versioned {
		return "", false
	}
	switch parts[1] {
	case "watch":
		if len(parts) > 2 && parts[2] == "identity" {
			return "", false
		}
		return "watch", versioned
	case "dash":
		if len(parts) > 2 && parts[2] == "deploy" {
			return "", false
		}
		return "dash", versioned
	default:
		return "", false
	}
}

func processListenAddresses(pidRoot string) []string {
	sockets := make(map[string]bool)
	entries, err := os.ReadDir(filepath.Join(pidRoot, "fd"))
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		link, err := os.Readlink(filepath.Join(pidRoot, "fd", entry.Name()))
		if err == nil && strings.HasPrefix(link, "socket:[") && strings.HasSuffix(link, "]") {
			sockets[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] = true
		}
	}
	addresses := make([]string, 0)
	for _, table := range []string{"tcp", "tcp6"} {
		raw, err := os.ReadFile(filepath.Join(pidRoot, "net", table))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 10 || fields[3] != "0A" || !sockets[fields[9]] {
				continue
			}
			if address, ok := decodeProcListenAddress(fields[1], table == "tcp6"); ok {
				addresses = append(addresses, address)
			}
		}
	}
	sort.Strings(addresses)
	return addresses
}

func decodeProcListenAddress(raw string, ipv6 bool) (string, bool) {
	hostHex, portHex, ok := strings.Cut(raw, ":")
	if !ok {
		return "", false
	}
	port, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return "", false
	}
	bytes, err := hex.DecodeString(hostHex)
	if err != nil || (!ipv6 && len(bytes) != net.IPv4len) || (ipv6 && len(bytes) != net.IPv6len) {
		return "", false
	}
	for start := 0; start < len(bytes); start += 4 {
		bytes[start], bytes[start+3] = bytes[start+3], bytes[start]
		bytes[start+1], bytes[start+2] = bytes[start+2], bytes[start+1]
	}
	return net.JoinHostPort(net.IP(bytes).String(), strconv.FormatUint(port, 10)), true
}
