package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kevinburke/ssh_config"
	"github.com/stretchr/testify/assert"
)

// mockCommand replaces exec.Command for testing
type mockExecCommand struct {
	lastCommand string
	lastArgs    []string
}

var mockCmd = &mockExecCommand{}

func (m *mockExecCommand) Command(command string, args ...string) *exec.Cmd {
	m.lastCommand = command
	m.lastArgs = args
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
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
		// Just exit successfully for SSH
		os.Exit(0)
	case "scp":
		// For SCP, we could validate the arguments if needed
		os.Exit(0)
	default:
		os.Exit(1)
	}
}

func setupTestConfig(t *testing.T) {
	pattern, err := ssh_config.NewPattern("testserver")
	if err != nil {
		t.Fatalf("Failed to create pattern: %v", err)
	}

	// Create a temporary config for testing
	cfg = &ssh_config.Config{
		Hosts: []*ssh_config.Host{
			{
				Patterns: []*ssh_config.Pattern{pattern},
				Nodes: []ssh_config.Node{
					&ssh_config.KV{Key: "Hostname", Value: "test.example.com"},
					&ssh_config.KV{Key: "User", Value: "testuser"},
					&ssh_config.KV{Key: "Port", Value: "2222"},
					&ssh_config.KV{Key: "IdentityFile", Value: "~/.ssh/test_key"},
				},
			},
		},
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
	setupTestConfig(t)

	// Store the original execCommand
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	// Replace execCommand with our mock
	execCommand = mockCmd.Command

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
				"-P", "2222",
				"-i", "~/.ssh/test_key",
				"-p",
				"--",
				"local.txt",
				"testuser@test.example.com:remote/path",
			},
		},
		{
			name:  "download single file",
			files: []string{":remote.txt", "local/path"},
			wantArgs: []string{
				"-P", "2222",
				"-i", "~/.ssh/test_key",
				"-p",
				"--",
				"testuser@test.example.com:remote.txt",
				"local/path",
			},
		},
		{
			name:  "upload multiple files",
			files: []string{"local1.txt", "local2.txt", ":remote/path"},
			wantArgs: []string{
				"-P", "2222",
				"-i", "~/.ssh/test_key",
				"-p",
				"--",
				"local1.txt",
				"local2.txt",
				"testuser@test.example.com:remote/path",
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
			err := runSCP("testserver", "testuser@test.example.com", tt.files)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, "scp", mockCmd.lastCommand)
			if !tt.wantErr {
				assert.Equal(t, tt.wantArgs, mockCmd.lastArgs)
			}
		})
	}
}

func TestRunSCPOmitsEmptyPortAndIdentity(t *testing.T) {
	pattern, err := ssh_config.NewPattern("bare")
	if err != nil {
		t.Fatalf("Failed to create pattern: %v", err)
	}
	cfg = &ssh_config.Config{
		Hosts: []*ssh_config.Host{
			{
				Patterns: []*ssh_config.Pattern{pattern},
				Nodes: []ssh_config.Node{
					&ssh_config.KV{Key: "Hostname", Value: "bare.example.com"},
				},
			},
		},
	}

	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()
	execCommand = mockCmd.Command

	err = runSCP("bare", "user@bare.example.com", []string{"local.txt", ":remote/path"})
	assert.NoError(t, err)
	assert.Equal(t, "scp", mockCmd.lastCommand)
	assert.Equal(t, []string{
		"-p",
		"--",
		"local.txt",
		"user@bare.example.com:remote/path",
	}, mockCmd.lastArgs)
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
	setupTestConfig(t)

	// Store the original execCommand
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	// Replace execCommand with our mock
	execCommand = mockCmd.Command

	tests := []struct {
		name      string
		alias     string
		address   string
		remoteCmd []string
		wantArgs  []string
	}{
		{
			name:    "basic connection",
			alias:   "testserver",
			address: "testuser@test.example.com",
			wantArgs: []string{
				"-p", "2222",
				"-i", "~/.ssh/test_key",
				"--",
				"testuser@test.example.com",
			},
		},
		{
			name:      "remote command passthrough",
			alias:     "testserver",
			address:   "testuser@test.example.com",
			remoteCmd: []string{"ls", "/tmp"},
			wantArgs: []string{
				"-p", "2222",
				"-i", "~/.ssh/test_key",
				"--",
				"testuser@test.example.com",
				"ls", "/tmp",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runSSH(tt.alias, tt.address, tt.remoteCmd)
			assert.NoError(t, err)
			assert.Equal(t, "ssh", mockCmd.lastCommand)
			assert.Equal(t, tt.wantArgs, mockCmd.lastArgs)
		})
	}
}
