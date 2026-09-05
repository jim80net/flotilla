package deliver

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func writeProcFixture(t *testing.T, root string, pid, ppid int, start uint64, cmdline, cgroup string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(filepath.Join(dir, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "S"
	fields[1] = strconv.Itoa(ppid)
	fields[19] = strconv.FormatUint(start, 10)
	stat := fmt.Sprintf("%d (fixture) %s\n", pid, strings.Join(fields, " "))
	for name, body := range map[string]string{
		"stat": stat, "cmdline": cmdline, "cgroup": cgroup,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func linkProcFD(t *testing.T, root string, pid int, fd, target string, flags uint64) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(filepath.Join(dir, "fdinfo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "fd", fd)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fdinfo", fd), []byte(fmt.Sprintf("flags:\t%o\n", flags)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotPaneReapSetSelectsPipeWritersAndDescendants(t *testing.T) {
	root := t.TempDir()
	const panePID = 100
	paneCgroup := "0::/user.slice/tmux.service\n"
	writeProcFixture(t, root, panePID, 1, 10, "grok\x00", paneCgroup)
	linkProcFD(t, root, panePID, "3", "pipe:[42]", 0)

	writeProcFixture(t, root, 200, 1, 20, "python3\x00monitor.py\x00", paneCgroup)
	linkProcFD(t, root, 200, "1", "pipe:[42]", 1)
	writeProcFixture(t, root, 201, panePID, 21, "python3\x00helper.py\x00", paneCgroup)
	linkProcFD(t, root, 201, "1", "/dev/null", 1)

	writeProcFixture(t, root, 202, 1, 22, "python3\x00other.py\x00", paneCgroup)
	linkProcFD(t, root, 202, "1", "pipe:[99]", 1)
	writeProcFixture(t, root, 203, 1, 23, "/usr/bin/flotilla\x00watch\x00", paneCgroup)
	linkProcFD(t, root, 203, "1", "pipe:[42]", 1)
	writeProcFixture(t, root, 204, 1, 24, "python3\x00service.py\x00", "0::/user.slice/backend.service\n")
	linkProcFD(t, root, 204, "1", "pipe:[42]", 1)

	got, err := snapshotPaneReapSet(root, panePID)
	if err != nil {
		t.Fatal(err)
	}
	want := []ProcessRef{{PID: 200, StartTime: 20}, {PID: 201, StartTime: 21}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot = %+v, want pipe writer + descendant %+v", got, want)
	}
}
