//go:build linux

package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeSessionRequiresMatchingIdentity(t *testing.T) {
	const sessionID = "df80c404-a298-48b9-a7df-63e880bfe5a8"
	valid := `{"pid":123,"procStart":"456","sessionId":"` + sessionID + `","name":"actionlint nix","cwd":"/projects/actionlint","entrypoint":"cli"}`
	for _, tt := range []struct {
		name, metadata string
		pid            int
		start          uint64
		want           bool
	}{
		{"verified", valid, 123, 456, true},
		{"wrong PID", strings.Replace(valid, `"pid":123`, `"pid":999`, 1), 123, 456, false},
		{"reused PID", valid, 123, 457, false},
		{"missing birth time", strings.Replace(valid, `"procStart":"456",`, "", 1), 123, 456, false},
		{"unknown process identity", valid, 123, 0, false},
		{"invalid session ID", strings.Replace(valid, sessionID, "not-a-uuid", 1), 123, 456, false},
		{"partial write", valid[:len(valid)-1], 123, 456, false},
		{"oversized", strings.Replace(valid, "actionlint nix", strings.Repeat("x", 65536), 1), 123, 456, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "123.json"), []byte(tt.metadata), 0o600); err != nil {
				t.Fatal(err)
			}
			session := readClaudeSession(dir, tt.pid, tt.start)
			if (session != nil) != tt.want {
				t.Fatalf("got session=%v, want available=%v", session, tt.want)
			}
			if session != nil && (session.ID != sessionID || session.Name != "actionlint nix" || session.ProjectDir != "/projects/actionlint" || session.Entrypoint != "cli") {
				t.Fatalf("wrong metadata: %+v", session)
			}
		})
	}
}

func TestClaudeSessionMissingAndRenamed(t *testing.T) {
	dir := t.TempDir()
	if readClaudeSession(dir, 123, 456) != nil {
		t.Fatal("missing file must remain unavailable")
	}
	const template = `{"pid":123,"procStart":"456","sessionId":"df80c404-a298-48b9-a7df-63e880bfe5a8","name":"NAME"}`
	for _, name := range []string{"first name", "renamed session"} {
		if err := os.WriteFile(filepath.Join(dir, "123.json"), []byte(strings.Replace(template, "NAME", name, 1)), 0o600); err != nil {
			t.Fatal(err)
		}
		session := readClaudeSession(dir, 123, 456)
		if session == nil || session.Name != name {
			t.Fatalf("name did not follow metadata refresh: %+v", session)
		}
	}
}
