package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeWatchIdentityProc(t *testing.T, root, pid, command, exe string) {
	t.Helper()
	dir := filepath.Join(root, pid)
	if err := os.MkdirAll(filepath.Join(dir, "fd"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(strings.ReplaceAll(command, " ", "\x00")+"\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, filepath.Join(dir, "exe")); err != nil {
		t.Fatal(err)
	}
}

func TestCollectWatchIdentityListsWatchAndDeletedDashboardBind(t *testing.T) {
	root := t.TempDir()
	writeWatchIdentityProc(t, root, "101", "/opt/flotilla watch --interval 10m", "/opt/flotilla")
	writeWatchIdentityProc(t, root, "202", "/opt/flotilla-6e1c6649 dash --bind 127.0.0.1:8799", "/opt/flotilla-old (deleted)")
	if err := os.Symlink("socket:[77]", filepath.Join(root, "202", "fd", "9")); err != nil {
		t.Fatal(err)
	}
	netDir := filepath.Join(root, "202", "net")
	if err := os.MkdirAll(netDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tcp := "  sl  local_address rem_address st tx_queue tr tm->when retrnsmt uid timeout inode\n   0: 0100007F:225F 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 77\n"
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte(tcp), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := collectWatchIdentities(root, func(exePath, diskPath string) (string, string, string) {
		if strings.Contains(exePath, "/101/") {
			return "disk-watch", "exe-watch", "rev-watch"
		}
		if diskPath != "/opt/flotilla-old" {
			t.Fatalf("deleted disk path = %q", diskPath)
		}
		return "unavailable", "exe-old", "rev-old"
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("identities = %+v", got)
	}
	if got[0].Kind != "watch" || got[0].PID != 101 || got[0].DeletedInode {
		t.Fatalf("watch identity = %+v", got[0])
	}
	wantDash := watchProcessIdentity{
		Kind: "dash", PID: 202, DiskPath: "/opt/flotilla-old", DiskSHA256: "unavailable",
		ExeSHA256: "exe-old", Revision: "rev-old", DeletedInode: true, Leftover: true,
		ListenAddresses: []string{"127.0.0.1:8799"},
	}
	if !reflect.DeepEqual(got[1], wantDash) {
		t.Fatalf("dash identity = %+v, want %+v", got[1], wantDash)
	}
}

func TestCollectWatchIdentityIgnoresNonFlotillaAndNonListeningSockets(t *testing.T) {
	root := t.TempDir()
	writeWatchIdentityProc(t, root, "101", "/usr/bin/python worker.py", "/usr/bin/python")
	writeWatchIdentityProc(t, root, "202", "/opt/flotilla dash --bind 127.0.0.1:8787", "/opt/flotilla")
	writeWatchIdentityProc(t, root, "303", "/opt/flotillahelper dash --bind 127.0.0.1:8788", "/opt/flotillahelper")
	if err := os.Symlink("socket:[88]", filepath.Join(root, "202", "fd", "8")); err != nil {
		t.Fatal(err)
	}
	netDir := filepath.Join(root, "202", "net")
	if err := os.MkdirAll(netDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(netDir, "tcp"), []byte("  sl local_address rem_address st tx_queue tr tm->when retrnsmt uid timeout inode\n 0: 0100007F:2253 00000000:0000 01 00000000:00000000 00:00000000 00000000 1000 0 88\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := collectWatchIdentities(root, func(_, _ string) (string, string, string) {
		return "disk", "exe", "rev"
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PID != 202 || len(got[0].ListenAddresses) != 0 {
		t.Fatalf("identities = %+v", got)
	}
}
