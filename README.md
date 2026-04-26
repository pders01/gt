# gt - A simple SSH connection manager

gt is a command-line tool that simplifies SSH connections by leveraging your existing SSH configuration. It provides a colorful, user-friendly interface to connect to your SSH hosts and supports all standard SSH config features including config includes.

## Features

- Direct connection to hosts from your SSH config
- Colorful, readable output
- List available SSH hosts with user and hostname info
- Automatic handling of SSH config includes (including nested chains)
- Strict-mode permission check on the SSH config and every Include
- Support for all SSH options (Port, IdentityFile, etc.)
- User override capability
- SCP support
- Shell completion for hosts
- Local audit log of every connection with a `gt log` viewer

## Installation

### From Source

```bash
go install
```

### From Release

1. Download the appropriate version for your system from the [releases page](https://github.com/pders01/gt/releases)
2. For macOS users:
   - The binary is ad-hoc signed for basic security verification
   - When you first run it, you'll see a security warning
   - You can approve it by:
     - `xattr -d com.apple.quarantine ./gt`
3. Add the binary to your PATH

## Usage

### Basic Connection

```bash
gt <host>                 # Connect to a host
gt <host> <command>       # Run command on host
```

### List Available Hosts

```bash
gt list                   # List all available hosts
```

### File Transfer (SCP)

```bash
# Upload files to remote host (remote path must start with ':')
gt -s myserver file1.txt file2.txt :remote/path/   # Upload local files to remote path

# Download files from remote host (remote paths must start with ':')
gt -s myserver :remote/file1.txt local/path/       # Download remote file to local path
gt -s myserver :remote/dir/* local/path/           # Download all files in remote dir

# The ':' prefix is mandatory:
# - For uploads: destination must start with ':'
# - For downloads: all source paths must start with ':'
# This helps prevent accidental uploads/downloads

# File modes and timestamps are preserved (-p flag)
```

### Audit Log

Every connection is recorded as a single JSON line in
`$XDG_STATE_HOME/gt/connections.jsonl` (or `~/.local/state/gt/connections.jsonl`
when `XDG_STATE_HOME` is unset). Each entry captures the start and end
timestamps, alias, address, mode (`ssh`/`scp`), exit code, and duration in
milliseconds — metadata only, never file paths or remote command text.

```bash
gt log                  # Show the 20 most recent entries
gt log -n 100           # Show the 100 most recent entries
gt log -n 0             # Show all entries
```

The log lives entirely on your machine and never leaves it. Failed connections
are logged too — that is usually when you most want the record. Pass `--no-log`
on any invocation to skip writing that one entry, or pipe `gt log` output
through `jq` directly against the JSONL file for richer queries.

### Options

```bash
gt -u root <host>       # Connect as root user
gt -s <host>            # Use SCP instead of SSH
gt --config ~/.ssh/custom_config <host>  # Use custom config file
gt --no-log <host>      # Skip the audit log for this connection
```

## Configuration

gt uses your existing SSH configuration (`~/.ssh/config` by default) and supports all standard SSH config features. No additional configuration is needed.

Example SSH config:

```ssh-config
# In ~/.ssh/config
Include ~/.ssh/config.d/*

Host dev
    HostName dev.example.com
    User developer
    Port 2222

Host prod
    HostName prod.example.com
    User admin
    IdentityFile ~/.ssh/prod_key
```

## Options

- `-u, --user`: Override SSH config user
- `-s, --scp`: Use SCP instead of SSH
- `--config`: Specify custom SSH config file path
- `--no-log`: Skip the audit log for this connection
- `--help`: Show help message

## License

MIT

---
Created by [pders01](https://github.com/pders01).
