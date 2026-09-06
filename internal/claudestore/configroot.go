package claudestore

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jim80net/flotilla/internal/deliver"
)

const claudeConfigDir = "CLAUDE_CONFIG_DIR"

type configDirObservationState uint8

const (
	configDirUnavailable configDirObservationState = iota
	configDirObservedAbsent
	configDirObservedValue
)

type configDirObservation struct {
	state configDirObservationState
	value string
}

var (
	panePID          = deliver.PanePID
	paneStartCommand = deliver.PaneStartCommand
	readProcFile     = os.ReadFile
)

func envValue(data []byte, key string) string {
	prefix := key + "="
	for _, field := range strings.Split(string(data), "\x00") {
		if strings.HasPrefix(field, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(field, prefix))
		}
	}
	return ""
}

func observedConfigDir(pid int) configDirObservation {
	seen := map[int]bool{}
	queue := []int{pid}
	observedEnvironment := false
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current <= 0 || seen[current] {
			continue
		}
		seen[current] = true
		if data, err := readProcFile(filepath.Join("/proc", strconv.Itoa(current), "environ")); err == nil {
			observedEnvironment = true
			if value := envValue(data, claudeConfigDir); value != "" {
				return configDirObservation{state: configDirObservedValue, value: value}
			}
		}
		if data, err := readProcFile(filepath.Join("/proc", strconv.Itoa(current), "task", strconv.Itoa(current), "children")); err == nil {
			for _, raw := range strings.Fields(string(data)) {
				if child, err := strconv.Atoi(raw); err == nil {
					queue = append(queue, child)
				}
			}
		}
	}
	if observedEnvironment {
		return configDirObservation{state: configDirObservedAbsent}
	}
	return configDirObservation{state: configDirUnavailable}
}

// configDirFromLaunch extracts the launch-recipe environment assignment recorded by tmux.
// It accepts the generated `export CLAUDE_CONFIG_DIR=value; ...` and `KEY=value command`
// shapes without executing or shell-expanding the command.
func configDirFromLaunch(launch string) string {
	for _, field := range strings.FieldsFunc(launch, func(r rune) bool {
		return r == ' ' || r == '\t' || r == ';'
	}) {
		field = strings.TrimPrefix(field, "export")
		if strings.HasPrefix(field, claudeConfigDir+"=") {
			return strings.Trim(strings.TrimPrefix(field, claudeConfigDir+"="), "'\"")
		}
	}
	return ""
}

func projectsRootForPane(pane string) (string, bool) {
	if pid, err := panePID(pane); err == nil {
		switch observed := observedConfigDir(pid); observed.state {
		case configDirObservedValue:
			return filepath.Join(observed.value, "projects"), true
		case configDirObservedAbsent:
			return projectsRoot()
		}
	}
	if launch, err := paneStartCommand(pane); err == nil {
		if dir := configDirFromLaunch(launch); dir != "" {
			return filepath.Join(dir, "projects"), true
		}
	}
	return projectsRoot()
}
