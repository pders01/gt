package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAuditLogPathResolution(t *testing.T) {
	t.Run("GT_LOG_DIR wins", func(t *testing.T) {
		t.Setenv("GT_LOG_DIR", "/tmp/forced")
		t.Setenv("XDG_STATE_HOME", "/tmp/xdg")
		got, err := auditLogPath()
		assert.NoError(t, err)
		assert.Equal(t, "/tmp/forced/connections.jsonl", got)
	})

	t.Run("XDG_STATE_HOME used when GT_LOG_DIR unset", func(t *testing.T) {
		t.Setenv("GT_LOG_DIR", "")
		t.Setenv("XDG_STATE_HOME", "/tmp/xdg")
		got, err := auditLogPath()
		assert.NoError(t, err)
		assert.Equal(t, "/tmp/xdg/gt/connections.jsonl", got)
	})

	t.Run("fallback to ~/.local/state when neither set", func(t *testing.T) {
		t.Setenv("GT_LOG_DIR", "")
		t.Setenv("XDG_STATE_HOME", "")
		got, err := auditLogPath()
		assert.NoError(t, err)
		home, _ := os.UserHomeDir()
		assert.Equal(t, filepath.Join(home, ".local", "state", "gt", "connections.jsonl"), got)
	})
}

func TestAppendAuditEntryRoundtrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GT_LOG_DIR", dir)

	entry := auditEntry{
		Start:      time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		End:        time.Date(2026, 1, 2, 3, 4, 7, 0, time.UTC),
		Alias:      "myhost",
		Address:    "me@host.example.com",
		Mode:       "ssh",
		ExitCode:   0,
		DurationMS: 2000,
	}

	if err := appendAuditEntry(entry); err != nil {
		t.Fatalf("append: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "connections.jsonl"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var got auditEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	assert.True(t, entry.Start.Equal(got.Start), "start time roundtrip")
	assert.True(t, entry.End.Equal(got.End), "end time roundtrip")
	assert.Equal(t, entry.Alias, got.Alias)
	assert.Equal(t, entry.Address, got.Address)
	assert.Equal(t, entry.Mode, got.Mode)
	assert.Equal(t, entry.ExitCode, got.ExitCode)
	assert.Equal(t, entry.DurationMS, got.DurationMS)
}

func TestAppendAuditEntryAppendsMultipleLines(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GT_LOG_DIR", dir)

	for i := 0; i < 3; i++ {
		err := appendAuditEntry(auditEntry{Alias: "h", Mode: "ssh"})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "connections.jsonl"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	assert.Len(t, lines, 3)
	for _, l := range lines {
		var e auditEntry
		assert.NoError(t, json.Unmarshal([]byte(l), &e))
	}
}

func TestRunCommandLoggedRespectsNoLog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GT_LOG_DIR", dir)

	origNoLog := noLog
	defer func() { noLog = origNoLog }()
	noLog = true

	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()
	execCommand = mockCmd.Command

	err := runCommandLogged(execCommand("ssh", "host"), "alias", "user@host", "ssh")
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, "connections.jsonl"))
	assert.True(t, os.IsNotExist(err), "log file must not be created when --no-log is set")
}

func TestRunCommandLoggedWritesEntry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GT_LOG_DIR", dir)

	origNoLog := noLog
	defer func() { noLog = origNoLog }()
	noLog = false

	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()
	execCommand = mockCmd.Command

	err := runCommandLogged(execCommand("ssh", "host"), "myalias", "me@host", "ssh")
	assert.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "connections.jsonl"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var e auditEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	assert.Equal(t, "myalias", e.Alias)
	assert.Equal(t, "me@host", e.Address)
	assert.Equal(t, "ssh", e.Mode)
	assert.Equal(t, 0, e.ExitCode)
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{0, "0ms"},
		{42, "42ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{1500, "1.5s"},
		{59_999, "60.0s"},
		{60_000, "1m00s"},
		{61_500, "1m01s"},
		{3_725_000, "62m05s"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, formatDuration(tt.ms), "ms=%d", tt.ms)
	}
}
