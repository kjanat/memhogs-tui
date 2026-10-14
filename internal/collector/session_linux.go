//go:build linux

package collector

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func claudeSession(pid int, start uint64) *SessionInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return readClaudeSession(filepath.Join(home, ".claude", "sessions"), pid, start)
}

func readClaudeSession(directory string, pid int, start uint64) *SessionInfo {
	file, err := os.Open(filepath.Join(directory, strconv.Itoa(pid)+".json"))
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	// Decode only identity and display metadata. Large or malformed files are
	// ignored; transcripts, credentials, messaging keys, and prompts are not read.
	var metadata struct {
		PID        int    `json:"pid"`
		Start      string `json:"procStart"`
		ID         string `json:"sessionId"`
		Name       string `json:"name"`
		Cwd        string `json:"cwd"`
		Entrypoint string `json:"entrypoint"`
	}
	const metadataLimit = 64 * 1024
	if err := json.NewDecoder(io.LimitReader(file, metadataLimit)).Decode(&metadata); err != nil {
		return nil
	}
	if metadata.PID != pid || start == 0 || metadata.Start != strconv.FormatUint(start, 10) || !sessionUUID(metadata.ID) {
		return nil
	}
	return &SessionInfo{ID: metadata.ID, Name: metadata.Name, ProjectDir: metadata.Cwd, Entrypoint: metadata.Entrypoint}
}

func sessionUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}
