package cmd

import (
	"os"
	"os/exec"
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

func TestRunSSH(t *testing.T) {
	setupTestConfig(t)

	// Store the original execCommand
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()
	
	// Replace execCommand with our mock
	execCommand = mockCmd.Command

	tests := []struct {
		name     string
		alias    string
		address  string
		wantArgs []string
	}{
		{
			name:    "basic connection",
			alias:   "testserver",
			address: "testuser@test.example.com",
			wantArgs: []string{
				"-p", "2222",
				"-i", "~/.ssh/test_key",
				"testuser@test.example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runSSH(tt.alias, tt.address)
			assert.NoError(t, err)
			assert.Equal(t, "ssh", mockCmd.lastCommand)
			assert.Equal(t, tt.wantArgs, mockCmd.lastArgs)
		})
	}
}
