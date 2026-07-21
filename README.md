# gt - A simple SSH connection manager

gt is a small UX layer over OpenSSH. It reads your existing SSH config to list and tab-complete host aliases, adds a colon shorthand for scp, and keeps a local audit log of every connection — then hands the alias straight to `ssh`/`scp`, so OpenSSH resolves the config and owns the connection.

## Features

- Direct connection to hosts from your SSH config
- Colorful, readable output
- List available SSH hosts with user and hostname info (resolved by `ssh -G`)
- Automatic handling of SSH config includes (including nested chains)
- Strict-mode permission check on the SSH config and every Include
- OpenSSH owns connection semantics: the alias is passed through unresolved, so ProxyJump, Match blocks, canonicalization, multiple IdentityFiles, and every other option behave exactly as with plain `ssh`
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

gt never resolves connection options itself. `gt myserver` execs `ssh -- myserver`, so OpenSSH matches Host blocks against the alias and applies the full config — including options gt has never heard of. gt only parses the config to enumerate aliases (for `gt list`, completions, and a friendly "host not found" error) and asks `ssh -G` when it needs resolved values for display, such as in `gt list` and the audit log. This also means defaults are OpenSSH's: with no `User` configured, you connect as your local user.

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

### Structuring your config: Include pitfalls

Because gt delegates to OpenSSH, it inherits ssh_config's sharp edges too. The
rules below are OpenSSH semantics, not gt inventions — gt just mirrors them
faithfully, so a config that misbehaves under plain `ssh` misbehaves
identically under gt. A layout that avoids all of them:

```ssh-config
# ~/.ssh/config
Include ~/.ssh/config.d/hosts   # 1. all Include lines first, before any Host/Match line

Host example                    # 2. the main file's own entries
    HostName example.com

Host *                          # 3. global defaults: a literal block, the LAST thing read
    ServerAliveInterval 60
```

The rules that make this the safe shape:

- **First value wins.** For every option, ssh uses the first value it reads,
  not the last. A `Host *` block read last provides defaults that per-host
  entries can override; read first, it silently overrides everything after it.
- **An `Include` after any `Host` or `Match` line is conditional.** Blocks only
  end where the next block begins, so an `Include` below a host entry belongs
  to that entry's block and its entire content — including its own `Host`
  lines — applies only when that block matches. Debug output (`ssh -Gv`) still
  shows the file as *read*, which makes this easy to misdiagnose. Keep
  includes above the first block.
- **Consequently, `Host *` defaults don't belong in an include** unless
  nothing follows the `Include` line in the main file: at the top they
  override later entries, at the bottom they're conditional. A literal
  `Host *` block at the end of the main config is the robust form.
- **Headerless lines in an included file** attach to whatever block context is
  active at the `Include` line. Start included files with a `Host` or `Match`
  header.
- **Relative `Include` paths resolve against `~/.ssh`**, never against the
  directory of the including file. Missing include files are skipped silently.
- **ssh lowercases the host argument before matching**, so aliases with
  uppercase letters in `Host` patterns never match. Keep aliases lowercase.
- **Some options accumulate instead of overriding** (`IdentityFile`,
  `CertificateFile`, `LocalForward`, `RemoteForward`): a later value is
  appended, not ignored — which can look like either, depending on when you
  ask.

For defaults that should only apply to *some* hosts, scope them with a
`Match host` or wildcard `Host` block instead of `Host *` — see
[ssh_config(5)](https://man.openbsd.org/ssh_config.5) for the full matching
and pattern semantics rather than relying on this summary.

When resolution surprises you, `ssh -Gv <alias> 2>&1 | grep -iE 'reading
configuration|applying options'` shows every file ssh opens and every block it
applies, in order — that plus first-value-wins explains nearly everything.

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
