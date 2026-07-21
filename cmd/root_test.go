package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kevinburke/ssh_config"
	"github.com/stretchr/testify/assert"
)

// mockCommand replaces exec.Command for testing. It records every
// invocation because a single gt run now execs more than once: the
// connection itself, then ssh -G for the audit-log address. resolveListRows
// also calls it from multiple goroutines, hence the mutex.
type mockExecCommand struct {
	mu       sync.Mutex
	commands []string
	argLists [][]string
}

var mockCmd = &mockExecCommand{}

func (m *mockExecCommand) Command(command string, args ...string) *exec.Cmd {
	m.mu.Lock()
	m.commands = append(m.commands, command)
	m.argLists = append(m.argLists, append([]string(nil), args...))
	m.mu.Unlock()
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

func (m *mockExecCommand) reset() {
	m.mu.Lock()
	m.commands, m.argLists = nil, nil
	m.mu.Unlock()
}

// useMockExec swaps execCommand for the recording mock for one test.
func useMockExec(t *testing.T) {
	t.Helper()
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = mockCmd.Command
	mockCmd.reset()
}

// TestHelperProcess isn't a real test. It's used to mock exec.Command
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	// Get the command and args after "--"
	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) == 0 {
		os.Exit(1)
	}

	// Mock different commands
	switch args[0] {
	case "ssh":
		for _, a := range args[1:] {
			if a == "-G" {
				// Emulate ssh -G's resolved key-value output.
				fmt.Println("user testuser")
				fmt.Println("hostname test.example.com")
				fmt.Println("port 2222")
				fmt.Println("identityfile ~/.ssh/test_key")
				break
			}
		}
		os.Exit(0)
	case "scp":
		// For SCP, we could validate the arguments if needed
		os.Exit(0)
	default:
		os.Exit(1)
	}
}

func TestValidateSCPPaths(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "no files",
			files:   []string{},
			wantErr: true,
			errMsg:  "SCP requires at least a source and destination",
		},
		{
			name:    "single file",
			files:   []string{"file.txt"},
			wantErr: true,
			errMsg:  "SCP requires at least a source and destination",
		},
		{
			name:    "valid upload",
			files:   []string{"local.txt", ":remote/path"},
			wantErr: false,
		},
		{
			name:    "valid download",
			files:   []string{":remote.txt", "local/path"},
			wantErr: false,
		},
		{
			name:    "invalid upload - destination without colon",
			files:   []string{"local.txt", "remote/path"},
			wantErr: true,
			errMsg:  "remote destination path must start with ':' (got remote/path)",
		},
		{
			name:    "invalid download - source without colon",
			files:   []string{"remote.txt", "local/path"},
			wantErr: true,
			errMsg:  "remote destination path must start with ':' (got local/path)",
		},
		{
			name:    "multiple file upload",
			files:   []string{"local1.txt", "local2.txt", ":remote/path"},
			wantErr: false,
		},
		{
			name:    "multiple file download",
			files:   []string{":remote1.txt", ":remote2.txt", "local/path"},
			wantErr: false,
		},
		{
			name:    "mixed upload paths",
			files:   []string{"local1.txt", ":remote1.txt", ":remote/path"},
			wantErr: true,
			errMsg:  "local source paths should not contain ':' (got :remote1.txt)",
		},
		{
			name:    "upload local path starting with -",
			files:   []string{"-oProxyCommand=evil", ":remote/path"},
			wantErr: true,
			errMsg:  "local path must not start with '-' (got -oProxyCommand=evil); prefix it with './'",
		},
		{
			name:    "download local destination starting with -",
			files:   []string{":remote.txt", "-oProxyCommand=evil"},
			wantErr: true,
			errMsg:  "local path must not start with '-' (got -oProxyCommand=evil); prefix it with './'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSCPPaths(tt.files)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Equal(t, tt.errMsg, err.Error())
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRunSCP(t *testing.T) {
	t.Setenv("GT_LOG_DIR", t.TempDir())
	useMockExec(t)

	tests := []struct {
		name     string
		files    []string
		wantArgs []string
		wantErr  bool
	}{
		{
			name:  "upload single file",
			files: []string{"local.txt", ":remote/path"},
			wantArgs: []string{
				"-p",
				"--",
				"local.txt",
				"testserver:remote/path",
			},
		},
		{
			name:  "download single file",
			files: []string{":remote.txt", "local/path"},
			wantArgs: []string{
				"-p",
				"--",
				"testserver:remote.txt",
				"local/path",
			},
		},
		{
			name:  "upload multiple files",
			files: []string{"local1.txt", "local2.txt", ":remote/path"},
			wantArgs: []string{
				"-p",
				"--",
				"local1.txt",
				"local2.txt",
				"testserver:remote/path",
			},
		},
		{
			name:    "invalid paths",
			files:   []string{"local.txt"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCmd.reset()
			err := runSCP("testserver", tt.files)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, "scp", mockCmd.commands[0])
			assert.Equal(t, tt.wantArgs, mockCmd.argLists[0])
		})
	}
}

func TestRunSCPWithOverrides(t *testing.T) {
	t.Setenv("GT_LOG_DIR", t.TempDir())
	useMockExec(t)

	origUser, origCfgFile := user, cfgFile
	defer func() { user, cfgFile = origUser, origCfgFile }()
	user = "admin"
	cfgFile = "/tmp/custom_config"

	err := runSCP("testserver", []string{"local.txt", ":remote/path"})
	assert.NoError(t, err)
	assert.Equal(t, "scp", mockCmd.commands[0])
	assert.Equal(t, []string{
		"-F", "/tmp/custom_config",
		"-o", "User=admin",
		"-p",
		"--",
		"local.txt",
		"testserver:remote/path",
	}, mockCmd.argLists[0])
}

func writeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoadConfigResolvesNestedIncludes(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "config")
	inc1 := filepath.Join(dir, "inc1")
	inc2 := filepath.Join(dir, "inc2")

	writeConfigFile(t, main, "Host alpha\n  Hostname alpha.example.com\nInclude "+inc1+"\n")
	writeConfigFile(t, inc1, "Host beta\n  Hostname beta.example.com\nInclude "+inc2+"\n")
	writeConfigFile(t, inc2, "Host gamma\n  Hostname gamma.example.com\n")

	loadConfig(main)

	got := getHosts()
	assert.Equal(t, []string{"alpha", "beta", "gamma"}, got)
}

func TestGetHostsMultiPatternAndDedup(t *testing.T) {
	mkPatterns := func(t *testing.T, names ...string) []*ssh_config.Pattern {
		out := make([]*ssh_config.Pattern, 0, len(names))
		for _, n := range names {
			p, err := ssh_config.NewPattern(n)
			if err != nil {
				t.Fatalf("NewPattern(%q): %v", n, err)
			}
			out = append(out, p)
		}
		return out
	}

	cfg = &ssh_config.Config{
		Hosts: []*ssh_config.Host{
			{Patterns: mkPatterns(t, "alpha", "beta", "gamma")},
			{Patterns: mkPatterns(t, "delta", "*.internal", "?ildcard")},
			{Patterns: mkPatterns(t, "alpha")}, // duplicate of the first block
		},
	}

	got := getHosts()
	want := []string{"alpha", "beta", "delta", "gamma"}
	assert.Equal(t, want, got)
}

func TestCheckConfigOwnerAndMode(t *testing.T) {
	const me uint32 = 1000
	const other uint32 = 1234
	const root uint32 = 0

	tests := []struct {
		name    string
		uid     uint32
		mode    os.FileMode
		wantErr bool
	}{
		{"owner 0600", me, 0o600, false},
		{"owner 0644 (group/world readable, not writable)", me, 0o644, false},
		{"owner 0660 group writable", me, 0o660, true},
		{"owner 0606 world writable", me, 0o606, true},
		{"owner 0700", me, 0o700, false},
		{"owner 0777", me, 0o777, true},
		{"root-owned 0600", root, 0o600, false},
		{"root-owned 0660 still rejected", root, 0o660, true},
		{"other user owned, strict mode", other, 0o600, true},
		{"other user owned, loose mode", other, 0o666, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkConfigOwnerAndMode("/fake/path", tt.uid, tt.mode, me)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRunSSH(t *testing.T) {
	t.Setenv("GT_LOG_DIR", t.TempDir())
	useMockExec(t)

	tests := []struct {
		name      string
		alias     string
		remoteCmd []string
		wantArgs  []string
	}{
		{
			name:  "basic connection preserves the alias",
			alias: "testserver",
			wantArgs: []string{
				"--",
				"testserver",
			},
		},
		{
			name:      "remote command passthrough",
			alias:     "testserver",
			remoteCmd: []string{"ls", "/tmp"},
			wantArgs: []string{
				"--",
				"testserver",
				"ls", "/tmp",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCmd.reset()
			err := runSSH(tt.alias, tt.remoteCmd)
			assert.NoError(t, err)
			assert.Equal(t, "ssh", mockCmd.commands[0])
			assert.Equal(t, tt.wantArgs, mockCmd.argLists[0])
			// The follow-up call resolves the audit-log address via ssh -G.
			assert.Equal(t, "ssh", mockCmd.commands[1])
			assert.Equal(t, []string{"-G", "--", tt.alias}, mockCmd.argLists[1])
		})
	}
}

func TestRunSSHWithOverrides(t *testing.T) {
	t.Setenv("GT_LOG_DIR", t.TempDir())
	useMockExec(t)

	origUser, origCfgFile := user, cfgFile
	defer func() { user, cfgFile = origUser, origCfgFile }()
	user = "admin"
	cfgFile = "/tmp/custom_config"

	err := runSSH("testserver", nil)
	assert.NoError(t, err)
	assert.Equal(t, "ssh", mockCmd.commands[0])
	assert.Equal(t, []string{
		"-F", "/tmp/custom_config",
		"-o", "User=admin",
		"--",
		"testserver",
	}, mockCmd.argLists[0])
	// The overrides flow into the audit-address resolution too, so the
	// logged user matches who ssh actually connected as.
	assert.Equal(t, []string{
		"-F", "/tmp/custom_config",
		"-o", "User=admin",
		"-G",
		"--",
		"testserver",
	}, mockCmd.argLists[1])
}

func TestKnownHost(t *testing.T) {
	decoded, err := ssh_config.Decode(strings.NewReader(`Host testserver
  Hostname test.example.com

Host web-* !web-3
  User deploy

Host *
  ServerAliveInterval 60
`))
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}
	cfg = decoded

	tests := []struct {
		alias string
		want  bool
	}{
		{"testserver", true},
		{"web-1", true},             // wildcard blocks still count
		{"web-3", false},            // negated within its own block
		{"nope", false},             // catch-all "Host *" must not vouch for typos
		{"test.example.com", false}, // hostnames are not aliases
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, knownHost(tt.alias), "alias=%q", tt.alias)
	}
}

func TestResolveHost(t *testing.T) {
	useMockExec(t)

	got, err := resolveHost("testserver")
	assert.NoError(t, err)
	assert.Equal(t, resolvedHost{
		user:     "testuser",
		hostname: "test.example.com",
		port:     "2222",
	}, got)
	assert.Equal(t, []string{"-G", "--", "testserver"}, mockCmd.argLists[0])
}

func TestResolveListRows(t *testing.T) {
	useMockExec(t)

	rows := resolveListRows([]string{"alpha", "beta"})
	assert.Len(t, rows, 2)
	assert.Equal(t, "alpha", rows[0].alias)
	assert.Equal(t, "beta", rows[1].alias)
	for _, r := range rows {
		assert.NoError(t, r.err)
		assert.Equal(t, "test.example.com", r.hostname)
		assert.Equal(t, "testuser", r.user)
		assert.Equal(t, "2222", r.port)
	}
}
