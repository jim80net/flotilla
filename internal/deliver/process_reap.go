package deliver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	processReapTermWait = 2 * time.Second
	processReapKillWait = 1 * time.Second
	processReapPoll     = 25 * time.Millisecond
)

// ProcessRef binds a pid to its Linux process start time. Reaping checks both
// fields before every signal so a recycled pid can never target a reused pid.
type ProcessRef struct {
	PID       int
	StartTime uint64
}

type procStat struct {
	state     byte
	parentPID int
	startTime uint64
}

// SnapshotPaneReapSet returns the processes owned by a pane's transient session:
// descendants of panePID plus writers whose stdout pipe is held open for reading
// by panePID. The snapshot must be taken before tmux respawn-pane -k destroys the
// reader and reparents surviving descendants.
func SnapshotPaneReapSet(panePID int) ([]ProcessRef, error) {
	return snapshotPaneReapSet("/proc", panePID)
}

func snapshotPaneReapSet(procRoot string, panePID int) ([]ProcessRef, error) {
	if panePID <= 1 {
		return nil, fmt.Errorf("refusing unsafe pane pid %d", panePID)
	}
	if _, err := readProcStat(procRoot, panePID); err != nil {
		return nil, fmt.Errorf("read pane process %d: %w", panePID, err)
	}
	readPipes, err := paneReaderPipes(procRoot, panePID)
	if err != nil {
		return nil, err
	}
	paneCgroup, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(panePID), "cgroup"))
	if err != nil {
		return nil, fmt.Errorf("read pane %d cgroup: %w", panePID, err)
	}

	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, fmt.Errorf("read proc root: %w", err)
	}
	stats := make(map[int]procStat)
	for _, ent := range entries {
		pid, err := strconv.Atoi(ent.Name())
		if err != nil || pid == panePID {
			continue
		}
		stat, err := readProcStat(procRoot, pid)
		if err == nil {
			stats[pid] = stat
		}
	}

	descendant := make(map[int]bool)
	for changed := true; changed; {
		changed = false
		for pid, stat := range stats {
			if descendant[pid] {
				continue
			}
			if stat.parentPID == panePID || descendant[stat.parentPID] {
				descendant[pid] = true
				changed = true
			}
		}
	}

	refs := make([]ProcessRef, 0)
	for pid, stat := range stats {
		stdoutPipe := readLink(filepath.Join(procRoot, strconv.Itoa(pid), "fd", "1"))
		if !descendant[pid] {
			if _, ok := readPipes[stdoutPipe]; !ok {
				continue
			}
			flags, err := readFDFlags(filepath.Join(procRoot, strconv.Itoa(pid), "fdinfo", "1"))
			if err != nil {
				continue
			}
			mode := flags & syscall.O_ACCMODE
			if mode != syscall.O_WRONLY && mode != syscall.O_RDWR {
				continue
			}
		}
		protected, err := protectedRecycleProcess(procRoot, pid, string(paneCgroup))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("inspect recycle candidate %d: %w", pid, err)
		}
		if protected {
			continue
		}
		refs = append(refs, ProcessRef{PID: pid, StartTime: stat.startTime})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].PID < refs[j].PID })
	return refs, nil
}

func paneReaderPipes(procRoot string, panePID int) (map[string]struct{}, error) {
	fdRoot := filepath.Join(procRoot, strconv.Itoa(panePID), "fd")
	entries, err := os.ReadDir(fdRoot)
	if err != nil {
		return nil, fmt.Errorf("read pane %d file descriptors: %w", panePID, err)
	}
	pipes := make(map[string]struct{})
	for _, ent := range entries {
		linkPath := filepath.Join(fdRoot, ent.Name())
		target, err := os.Readlink(linkPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read pane %d fd %s: %w", panePID, ent.Name(), err)
		}
		if !strings.HasPrefix(target, "pipe:[") {
			continue
		}
		flags, err := readFDFlags(filepath.Join(procRoot, strconv.Itoa(panePID), "fdinfo", ent.Name()))
		if err != nil {
			return nil, fmt.Errorf("read pane %d fd %s flags: %w", panePID, ent.Name(), err)
		}
		if flags&syscall.O_ACCMODE != syscall.O_WRONLY {
			pipes[target] = struct{}{}
		}
	}
	return pipes, nil
}

func readFDFlags(path string) (uint64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && key == "flags" {
			return strconv.ParseUint(strings.TrimSpace(value), 8, 64)
		}
	}
	return 0, fmt.Errorf("flags field absent")
}

func readProcStat(procRoot string, pid int) (procStat, error) {
	raw, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "stat"))
	if err != nil {
		return procStat{}, err
	}
	closeParen := strings.LastIndexByte(string(raw), ')')
	if closeParen < 0 || closeParen+2 >= len(raw) {
		return procStat{}, fmt.Errorf("malformed stat")
	}
	fields := strings.Fields(string(raw[closeParen+2:]))
	if len(fields) <= 19 || len(fields[0]) != 1 {
		return procStat{}, fmt.Errorf("short stat")
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return procStat{}, fmt.Errorf("parse parent pid: %w", err)
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return procStat{}, fmt.Errorf("parse start time: %w", err)
	}
	return procStat{state: fields[0][0], parentPID: ppid, startTime: start}, nil
}

func readLink(path string) string {
	target, _ := os.Readlink(path)
	return target
}

func protectedRecycleProcess(procRoot string, pid int, paneCgroup string) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return false, err
	}
	argv := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	if len(argv) > 0 {
		base := filepath.Base(argv[0])
		if base == "flotilla-watch" || (base == "flotilla" && len(argv) > 1 && argv[1] == "watch") {
			return true, nil
		}
	}
	cgroup, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return false, err
	}
	// A process moved into another cgroup belongs to another service/scope. Even
	// if it inherited a pane pipe, its lifecycle is owned by that unit, not recycle.
	if string(cgroup) != paneCgroup {
		return true, nil
	}
	return false, nil
}

// ReapProcesses terminates a previously snapshotted set, escalating after a
// bounded wait. It returns success only when every original process has exited
// (a zombie counts as exited) or its pid has been reused by another process.
func ReapProcesses(refs []ProcessRef) error {
	if err := signalProcessRefs(refs, syscall.SIGTERM); err != nil {
		return err
	}
	left, err := waitProcessRefs(refs, processReapTermWait)
	if err != nil {
		return err
	}
	if len(left) == 0 {
		return nil
	}
	if err := signalProcessRefs(left, syscall.SIGKILL); err != nil {
		return err
	}
	left, err = waitProcessRefs(left, processReapKillWait)
	if err != nil {
		return err
	}
	if len(left) != 0 {
		pids := make([]string, 0, len(left))
		for _, ref := range left {
			pids = append(pids, strconv.Itoa(ref.PID))
		}
		return fmt.Errorf("recycle monitor processes still alive after SIGKILL: %s", strings.Join(pids, ","))
	}
	return nil
}

func signalProcessRefs(refs []ProcessRef, signal syscall.Signal) error {
	var result error
	for _, ref := range refs {
		alive, err := processRefAlive("/proc", ref)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("verify process %d: %w", ref.PID, err))
			continue
		}
		if !alive {
			continue
		}
		if err := syscall.Kill(ref.PID, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
			result = errors.Join(result, fmt.Errorf("signal process %d: %w", ref.PID, err))
		}
	}
	return result
}

func waitProcessRefs(refs []ProcessRef, timeout time.Duration) ([]ProcessRef, error) {
	deadline := time.Now().Add(timeout)
	for {
		left := make([]ProcessRef, 0, len(refs))
		for _, ref := range refs {
			alive, err := processRefAlive("/proc", ref)
			if err != nil {
				return nil, err
			}
			if alive {
				left = append(left, ref)
			}
		}
		if len(left) == 0 || !time.Now().Before(deadline) {
			return left, nil
		}
		time.Sleep(processReapPoll)
	}
}

func processRefAlive(procRoot string, ref ProcessRef) (bool, error) {
	if ref.PID <= 1 {
		return false, fmt.Errorf("unsafe process pid %d", ref.PID)
	}
	stat, err := readProcStat(procRoot, ref.PID)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return stat.startTime == ref.StartTime && stat.state != 'Z', nil
}
