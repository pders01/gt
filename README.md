# gt - A simple SSH connection manager

gt is a command-line tool that simplifies SSH connections by leveraging your existing SSH configuration. It provides a colorful, user-friendly interface to connect to your SSH hosts and supports all standard SSH config features including config includes.

## Features

- 🚀 Direct connection to hosts from your SSH config
- 🎨 Colorful, readable output
- 📋 List available SSH hosts with user and hostname info
- 🔄 Automatic handling of SSH config includes
- 🔑 Support for all SSH options (Port, IdentityFile, etc.)
- 👤 User override capability
- 📦 SCP support
- ✨ Shell completion for hosts

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
# Upload files to remote host
gt -s myserver local1.txt local2.txt remote/path/   # Upload local files to remote path

# Download files from remote host
gt -s myserver :remote/file1.txt local/path/        # Download remote file to local path
gt -s myserver :remote/dir/* local/path/            # Download all files in remote dir

# Note: Remote paths must be prefixed with ':' when downloading
```

### Options

```bash
gt -u root <host>       # Connect as root user
gt -s <host>            # Use SCP instead of SSH
gt --config ~/.ssh/custom_config <host>  # Use custom config file
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
- `-c, --config`: Specify custom SSH config file path
- `--help`: Show help message

## License

MIT

---
Created by [pders01](https://github.com/pders01) with ❤️  
Special thanks [Cascade (Codeium AI)](https://codeium.com) for assistance.
