//go:build linux

package collector

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestParseStartTime(t *testing.T) {
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "S"
	fields[19] = "12345"
	for _, name := range []string{"normal", "name with spaces", "name ) with ( parentheses)"} {
		got, err := parseStartTime("42 (" + name + ") " + strings.Join(fields, " "))
		if err != nil || got != 12345 {
			t.Fatalf("%q: %d, %v", name, got, err)
		}
	}
	for _, stat := range []string{"", "42 missing", "42 (name) S", "42 (name) " + strings.Repeat("0 ", 19) + "bad"} {
		if _, err := parseStartTime(stat); err == nil {
			t.Fatalf("accepted malformed stat %q", stat)
		}
	}
}

func TestProcMemAvailability(t *testing.T) {
	dir := t.TempDir()
	for _, tt := range []struct {
		status string
		known  bool
		swap   int64
	}{
		{"VmRSS:\t4096 kB\nVmSwap:\t0 kB\n", true, 0},
		{"VmRSS:\t4096 kB\nVmSwap:\t1024 kB\n", true, 1024},
		{"VmRSS:\t4096 kB\n", false, 0},
		{"VmRSS:\t4096 kB\nVmSwap:\tbad kB\n", false, 0},
	} {
		if err := os.WriteFile(filepath.Join(dir, "status"), []byte(tt.status), 0o600); err != nil {
			t.Fatal(err)
		}
		rss, swap, known := procMem(dir)
		if rss != 4096 || swap != tt.swap || known != tt.known {
			t.Fatalf("%q: got %d %d %v", tt.status, rss, swap, known)
		}
	}
}

func TestProcStatusEffectiveAccount(t *testing.T) {
	for _, tt := range []struct{ line, want string }{
		{"Uid:\t1000\t0\t1000\t1000", "0"},
		{"Uid: 0 1000 0 0", "1000"},
		{"Uid: 0 bad 0 0", ""},
		{"Uid: 0 -1 0 0", ""},
		{"Uid: 0 4294967296 0 0", ""},
		{"Uid: 1000", ""},
		{"", ""},
	} {
		t.Run(tt.line, func(t *testing.T) {
			dir := t.TempDir()
			status := tt.line + "\nVmRSS: 4096 kB\nVmSwap: 128 kB\n"
			if err := os.WriteFile(filepath.Join(dir, "status"), []byte(status), 0o600); err != nil {
				t.Fatal(err)
			}
			rss, swap, known, uid := procStatus(dir)
			if uid != tt.want || rss != 4096 || swap != 128 || !known {
				t.Fatalf("got uid=%q rss=%d swap=%d known=%v", uid, rss, swap, known)
			}
		})
	}
}

func TestProcessStartTimeMatchesCurrentProcess(t *testing.T) {
	start, err := ProcessStartTime(os.Getpid())
	if err != nil || start == 0 {
		t.Fatalf("start=%d err=%v", start, err)
	}
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(os.Getpid()), "stat"))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := parseStartTime(string(raw))
	if err != nil || start != expected {
		t.Fatalf("creation time mismatch: %d %d %v", start, expected, err)
	}
}

func TestReadProcessFixture(t *testing.T) {
	dir := t.TempDir()
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "S"
	fields[19] = "100"
	fixtures := map[string]string{
		"stat":    "123 (worker ) name) " + strings.Join(fields, " "),
		"comm":    "MainThread\n",
		"cmdline": "/usr/bin/node\x00/projects/myapp/server.js\x00--worker\x00",
		"status":  "Uid: 0 1000 0 0\nVmRSS: 4096 kB\nVmSwap: 512 kB\n",
	}
	for name, value := range fixtures {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	name, p, ok := readProcess(dir)
	want := ProcDetail{Args: []string{"/usr/bin/node", "/projects/myapp/server.js", "--worker"}, UID: "1000", StartTime: 100, Cmdline: "node /projects/myapp/server.js --worker", RSSKB: 4096, SwapKB: 512, SwapKnown: true}
	requireCPU(t, !p.CPUAt.IsZero() && p.CPUTime == 0, "missing idle CPU observation")
	want.CPUAt = p.CPUAt
	if !ok || name != "node server.js" || !reflect.DeepEqual(p, want) {
		t.Fatalf("bad fixture: %q %+v %v", name, p, ok)
	}
	if err := os.Remove(filepath.Join(dir, "stat")); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := readProcess(dir); ok {
		t.Fatal("accepted vanished process")
	}
}

func TestCollectIncludesCurrentProcess(t *testing.T) {
	snapshot, err := Collect()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.At.IsZero() || snapshot.System.MemTotalKB <= 0 || snapshot.System.MemAvailableKB < 0 {
		t.Fatalf("bad live system snapshot: %+v", snapshot.System)
	}
	for _, a := range snapshot.Apps {
		for _, p := range a.Children {
			if p.PID == os.Getpid() {
				if p.StartTime == 0 || p.RSSKB <= 0 {
					t.Fatalf("bad live process: %+v", p)
				}
				return
			}
		}
	}
	t.Fatal("live collection did not include its own process")
}

func TestCmdlineDisplayPreservesRewrittenArgv(t *testing.T) {
	dir := t.TempDir()
	// Chromium may place its entire launch command in argv[0]. A path in a
	// later argument must not make filepath.Base discard the earlier flags.
	raw := "/opt/google/chrome/chrome --type=renderer --user-data-dir=/tmp/GitHub9061 --ozone-platform=wayland\x00"
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	got := cmdlineDisplay(dir)
	if !strings.Contains(got, "--type=renderer") || !strings.HasPrefix(got, "/opt/google/chrome/chrome") {
		t.Fatalf("rewritten argv was corrupted: %q", got)
	}
}

func TestReadProcessExcludesZombie(t *testing.T) {
	dir := t.TempDir()
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "Z"
	fields[19] = "100"
	for name, value := range map[string]string{"stat": "123 (zsh) " + strings.Join(fields, " "), "comm": "zsh\n", "status": "State: Z (zombie)\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, ok := readProcess(dir); ok {
		t.Fatal("zombie counted as a memory-holding process")
	}
}

func TestProcessContextUsesDirectoryAndTerminal(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "fd"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/projects/memhogs-tui", filepath.Join(dir, "cwd")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/pts/17", filepath.Join(dir, "fd", "0")); err != nil {
		t.Fatal(err)
	}
	cwd, terminal := processContext(dir)
	if cwd != "/projects/memhogs-tui" || terminal != "pts/17" {
		t.Fatalf("got cwd=%q terminal=%q", cwd, terminal)
	}
}
