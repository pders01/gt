package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/fatih/color"
	"github.com/kevinburke/ssh_config"
	"github.com/spf13/cobra"
)

var (
	cfgFile     string
	cfg         *ssh_config.Config
	user        string
	useScp      bool
	noLog       bool
	execCommand = exec.Command
	// Color outputs using conventional terminal colors
	aliasColor     = color.New(color.FgBlue, color.Bold) // for the host alias (like ls directories)
	userColor      = color.New(color.FgGreen)            // for username (conventional user color)
	domainColor    = color.New(color.FgYellow)           // for domain part (conventional hostname color)
	subdomainColor = color.New(color.FgCyan)             // for subdomains
	portColor      = color.New(color.FgMagenta)          // for port numbers
	errorColor     = color.New(color.FgRed)              // for errors
	warningColor   = color.New(color.FgYellow)           // for warnings
	symbolColor    = color.New(color.FgWhite)            // for symbols like @ and :
)

func init() {
	// Respect NO_COLOR environment variable
	if os.Getenv("NO_COLOR") != "" {
		color.NoColor = true
	}

	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "SSH config file (default ~/.ssh/config)")
	rootCmd.PersistentFlags().StringVarP(&user, "user", "u", "", "override SSH config user")
	rootCmd.PersistentFlags().BoolVarP(&useScp, "scp", "s", false, "use SCP instead of SSH")
	rootCmd.PersistentFlags().BoolVar(&noLog, "no-log", false, "skip writing this connection to the audit log")

	logCmd.Flags().IntVarP(&logLimit, "limit", "n", 20, "show at most N most-recent entries (0 = all)")

	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(logCmd)
}

func getHosts() []string {
	var hosts []string
	seen := map[string]struct{}{}
	for _, host := range cfg.Hosts {
		// A single Host block can declare several aliases ("Host foo bar baz");
		// emit each one and dedupe in case the same alias appears in multiple
		// blocks across the merged config.
		for _, p := range host.Patterns {
			pattern := p.String()
			if strings.ContainsAny(pattern, "*?") {
				continue // skip wildcard match patterns
			}
			// Pattern.String() strips a leading "!", so exclusions are not
			// detectable from the text. Ask the block instead: a negated
			// pattern never matches its own alias, which filters entries
			// like the "!backup" in "Host web !backup".
			if !host.Matches(pattern) {
				continue
			}
			if _, ok := seen[pattern]; ok {
				continue
			}
			seen[pattern] = struct{}{}
			hosts = append(hosts, pattern)
		}
	}
	sort.Strings(hosts)
	return hosts
}

// resolvedHost holds the values OpenSSH reports for an alias via ssh -G.
type resolvedHost struct {
	user     string
	hostname string
	port     string
}

// resolveHost asks OpenSSH what an alias resolves to instead of
// reimplementing config resolution. ssh -G prints the fully resolved
// client configuration as "key value" lines without connecting, so
// Match blocks, canonicalization, and future options all behave exactly
// as they would for a real connection.
func resolveHost(alias string) (resolvedHost, error) {
	args := append(sshBaseArgs(), "-G", "--", alias)
	out, err := execCommand("ssh", args...).Output()
	if err != nil {
		return resolvedHost{}, fmt.Errorf("ssh -G %s: %w", alias, err)
	}
	var r resolvedHost
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		key, value, _ := strings.Cut(sc.Text(), " ")
		switch key {
		case "user":
			r.user = value
		case "hostname":
			r.hostname = value
		case "port":
			r.port = value
		}
	}
	return r, nil
}

type listRow struct {
	alias string
	resolvedHost
	err error
}

// resolveListRows queries ssh -G for every alias. Each query is a
// subprocess, so run a handful at a time rather than either one ssh per
// host all at once or a serial crawl through a large config.
func resolveListRows(hosts []string) []listRow {
	rows := make([]listRow, len(hosts))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, alias := range hosts {
		wg.Add(1)
		go func(i int, alias string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			resolved, err := resolveHost(alias)
			rows[i] = listRow{alias: alias, resolvedHost: resolved, err: err}
		}(i, alias)
	}
	wg.Wait()
	return rows
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all hosts from SSH config",
	Long: `List all hosts defined in your SSH config file.
Includes entries from included config files.
Resolved values (user, hostname, port) come from ssh -G.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		hosts := getHosts()
		if len(hosts) == 0 {
			warningColor.Println("No SSH hosts found")
			return nil
		}

		rows := resolveListRows(hosts)

		aliasWidth := 0
		for _, r := range rows {
			if len(r.alias) > aliasWidth {
				aliasWidth = len(r.alias)
			}
		}
		aliasWidth++ // single-space gutter after the longest alias

		for _, r := range rows {
			// Format: alias    user@host.subdomain.domain:port
			aliasColor.Printf("%-*s", aliasWidth, r.alias)
			if r.err != nil {
				warningColor.Println("(could not resolve)")
				continue
			}
			userColor.Print(r.user)
			symbolColor.Print("@")

			// Split hostname into parts and color each differently
			parts := strings.Split(r.hostname, ".")
			for i, part := range parts {
				if i > 0 {
					symbolColor.Print(".")
				}
				if i == len(parts)-1 {
					// Last part is the top-level domain
					domainColor.Print(part)
				} else if i == len(parts)-2 && len(parts) > 2 {
					// Second to last is usually the domain name
					domainColor.Print(part)
				} else {
					// Earlier parts are subdomains
					subdomainColor.Print(part)
				}
			}

			// Add port if specified and not default
			if r.port != "" && r.port != "22" {
				symbolColor.Print(":")
				portColor.Print(r.port)
			}

			fmt.Println() // New line
		}
		return nil
	},
}

var rootCmd = &cobra.Command{
	Use:   "gt [alias] [file...]",
	Short: "gt is a small UX layer over OpenSSH",
	Long: `gt is a small UX layer over OpenSSH. It lists and tab-completes the
Host aliases in ~/.ssh/config, adds a colon shorthand for scp, and keeps a
local audit log — the alias itself is handed to ssh/scp, so OpenSSH resolves
the config and owns the connection.

Examples:
  # Connect to a host defined in ~/.ssh/config
  gt myserver

  # Connect with a different user
  gt myserver -u admin

  # Run a one-shot command on the remote host
  gt myserver uptime

  # Upload files to remote host (remote path must start with ':')
  gt myserver -s file1.txt file2.txt :remote/path/

  # Download files from remote host (remote paths must start with ':')
  gt myserver -s :remote/file1.txt :remote/file2.txt local/path/`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completeHosts,
	RunE: func(cmd *cobra.Command, args []string) error {
		alias := args[0]

		if !knownHost(alias) {
			return fmt.Errorf("host '%s' not found in SSH config", alias)
		}
		if user != "" {
			if err := validateNoFlagPrefix("user", user); err != nil {
				return err
			}
		}

		if useScp {
			return runSCP(alias, args[1:])
		}
		return runSSH(alias, args[1:])
	},
}

// knownHost reports whether alias is addressed by a Host block in the
// config, so a typo fails with a clear error instead of a DNS lookup on
// the raw alias. Blocks whose only patterns are the catch-all "*" are
// ignored: those hold global defaults and would make every alias look
// valid. Wildcard blocks like "Host web-*" still count, and OpenSSH
// resolves the actual options at exec time.
func knownHost(alias string) bool {
	for _, host := range cfg.Hosts {
		if hasSpecificPattern(host) && host.Matches(alias) {
			return true
		}
	}
	return false
}

// hasSpecificPattern reports whether the block names anything beyond the
// catch-all "*". Pattern.String() strips negation, so a non-"*" pattern
// counts only if the block would actually apply to it — this keeps a pure
// exclusion block like "Host * !secret" classified as a catch-all.
func hasSpecificPattern(host *ssh_config.Host) bool {
	for _, p := range host.Patterns {
		if s := p.String(); s != "*" && host.Matches(s) {
			return true
		}
	}
	return false
}

// sshBaseArgs returns the flags shared by every ssh/scp/ssh -G
// invocation gt makes: the alternate config file and the user override.
// Everything else is deliberately left to OpenSSH, which resolves the
// alias against the config itself.
func sshBaseArgs() []string {
	var args []string
	if cfgFile != "" {
		args = append(args, "-F", cfgFile)
	}
	if user != "" {
		args = append(args, "-o", "User="+user)
	}
	return args
}

func completeHosts(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return getHosts(), cobra.ShellCompDirectiveNoFileComp
}

func runSCP(alias string, files []string) error {
	if err := validateSCPPaths(files); err != nil {
		return err
	}

	// scp reads ssh_config itself, so passing alias:path leaves port,
	// identity, ProxyJump, and everything else to OpenSSH.
	args := sshBaseArgs()
	args = append(args, "-p", "--") // -p preserves attributes; -- ends option parsing

	dest := files[len(files)-1]
	if strings.HasPrefix(dest, ":") {
		// Upload: Add all source files then the remote destination
		args = append(args, files[:len(files)-1]...)
		args = append(args, alias+dest)
	} else {
		// Download: Add remote sources then local destination
		for _, src := range files[:len(files)-1] {
			args = append(args, alias+src)
		}
		args = append(args, dest)
	}

	return runCommandLogged(execCommand("scp", args...), alias, "scp")
}

func runSSH(alias string, remoteCmd []string) error {
	// After --, ssh treats the next arg as the destination and everything
	// after as the remote command, forwarded to the remote shell verbatim.
	// The alias goes through unresolved so ssh matches Host blocks against
	// it, exactly as a plain `ssh alias` would.
	sshArgs := sshBaseArgs()
	sshArgs = append(sshArgs, "--", alias)
	sshArgs = append(sshArgs, remoteCmd...)
	return runCommandLogged(execCommand("ssh", sshArgs...), alias, "ssh")
}

func runCommand(cmd *exec.Cmd) error {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func validateNoFlagPrefix(name, value string) error {
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("%s must not start with '-' (got %q)", name, value)
	}
	return nil
}

func validateSCPPaths(files []string) error {
	if len(files) < 2 {
		return fmt.Errorf("SCP requires at least a source and destination")
	}

	// Determine if this is a download based on the first file
	isDownload := strings.HasPrefix(files[0], ":")

	if isDownload {
		// For downloads, all source paths must start with :
		for i := 0; i < len(files)-1; i++ {
			if !strings.HasPrefix(files[i], ":") {
				return fmt.Errorf("download paths must start with ':' (got %s)", files[i])
			}
		}
		local := files[len(files)-1]
		// The last path (destination) must not start with :
		if strings.HasPrefix(local, ":") {
			return fmt.Errorf("local destination path must not start with ':' (got %s)", local)
		}
		if strings.HasPrefix(local, "-") {
			return fmt.Errorf("local path must not start with '-' (got %s); prefix it with './'", local)
		}
	} else {
		// For uploads, all source paths must not start with :
		for i := 0; i < len(files)-1; i++ {
			src := files[i]
			if strings.HasPrefix(src, ":") {
				return fmt.Errorf("local source paths should not contain ':' (got %s)", src)
			}
			if strings.HasPrefix(src, "-") {
				return fmt.Errorf("local path must not start with '-' (got %s); prefix it with './'", src)
			}
		}
		// The last path (destination) must start with :
		if !strings.HasPrefix(files[len(files)-1], ":") {
			return fmt.Errorf("remote destination path must start with ':' (got %s)", files[len(files)-1])
		}
	}

	return nil
}

func Execute() error {
	return rootCmd.Execute()
}

func initConfig() {
	if cfgFile != "" {
		loadConfig(cfgFile)
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		errorColor.Fprintf(os.Stderr, "Error getting home directory: %v\n", err)
		os.Exit(1)
	}

	loadConfig(filepath.Join(home, ".ssh", "config"))
}

func loadConfig(path string) {
	f, err := os.Open(path)
	if err != nil {
		errorColor.Fprintf(os.Stderr, "Could not open SSH config at %s: %v\n", path, err)
		os.Exit(1)
	}
	defer f.Close()

	if err := validateOpenConfigPerms(path, f); err != nil {
		errorColor.Fprintf(os.Stderr, "Refusing to load SSH config: %v\n", err)
		os.Exit(1)
	}

	decoded, err := decodeConfig(f)
	if err != nil {
		errorColor.Fprintf(os.Stderr, "Error parsing SSH config: %v\n", err)
		os.Exit(1)
	}

	seen := map[string]struct{}{}
	if abs, err := filepath.Abs(path); err == nil {
		seen[abs] = struct{}{}
	}
	cfg = &ssh_config.Config{Hosts: resolveIncludes(decoded.Hosts, seen)}
}

// decodeConfig parses an SSH config stream, first dropping Match blocks,
// which the ssh_config library rejects outright ("Match directive parsing
// is unsupported") even though OpenSSH accepts them. gt only needs Host
// patterns for alias enumeration and a Match block cannot declare aliases,
// so skipping the block is faithful. Its body — including any conditional
// Includes, whose criteria gt could not evaluate anyway — is dropped;
// OpenSSH still applies all of it at connection time.
func decodeConfig(r io.Reader) (*ssh_config.Config, error) {
	var filtered bytes.Buffer
	sc := bufio.NewScanner(r)
	skipping := false
	for sc.Scan() {
		line := sc.Text()
		switch configKeyword(line) {
		case "match":
			skipping = true
			continue
		case "host":
			skipping = false
		}
		if skipping {
			continue
		}
		filtered.WriteString(line)
		filtered.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return ssh_config.Decode(&filtered)
}

// configKeyword returns the lowercased leading keyword of a config line,
// or "" for blanks and comments. Keywords may be separated from their
// arguments by whitespace or '='.
func configKeyword(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return ""
	}
	if i := strings.IndexAny(trimmed, " \t="); i >= 0 {
		trimmed = trimmed[:i]
	}
	return strings.ToLower(trimmed)
}

// resolveIncludes walks the host list and replaces every Include node with
// the hosts it resolves to, recursively. Includes inside included files are
// expanded the same way, so chains like main -> ~/.ssh/config.d/* -> shared
// load fully. An Include that sits inside a Host block is conditional in
// OpenSSH — its content only applies when the enclosing block matches the
// queried host — so hosts expanded from one are filtered through the
// enclosing block's patterns rather than merged wholesale. seen holds
// absolute paths of files already merged so a cycle terminates instead of
// looping forever. Note: the underlying library has its own depth-5 guard
// inside Decode, which catches absolute-path cycles before this layer ever
// sees them; our seen set covers cycles it resolves differently than gt.
func resolveIncludes(hosts []*ssh_config.Host, seen map[string]struct{}) []*ssh_config.Host {
	out := make([]*ssh_config.Host, 0, len(hosts))
	for _, host := range hosts {
		out = append(out, host)
		for _, node := range host.Nodes {
			include, ok := node.(*ssh_config.Include)
			if !ok {
				continue
			}
			out = append(out, filterConditional(host, expandInclude(include, seen))...)
		}
	}
	return out
}

// filterConditional applies OpenSSH's conditional-include semantics to
// hosts expanded from an Include node found inside the enclosing block.
// The catch-all block (the library's implicit top-of-file "Host *", or an
// explicit one) matches every query, so its includes pass through intact.
// Anything else only takes effect when the enclosing block matches, so an
// alias is kept only if the enclosing block would match it too.
func filterConditional(enclosing *ssh_config.Host, hosts []*ssh_config.Host) []*ssh_config.Host {
	if len(enclosing.Patterns) == 1 && enclosing.Patterns[0].String() == "*" {
		return hosts
	}
	var out []*ssh_config.Host
	for _, h := range hosts {
		var kept []*ssh_config.Pattern
		for _, p := range h.Patterns {
			if enclosing.Matches(p.String()) {
				kept = append(kept, p)
			}
		}
		if len(kept) > 0 {
			out = append(out, &ssh_config.Host{Patterns: kept, Nodes: h.Nodes})
		}
	}
	return out
}

// includeDirectives extracts the path arguments from an Include node.
// The node's String() renders the whole config line — leading indentation,
// optional "=", trailing comment — so strip the decoration down to the
// space-separated paths. An Include line may name several.
func includeDirectives(include *ssh_config.Include) []string {
	line := strings.TrimSpace(include.String())
	if i := strings.Index(line, "#"); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "Include"))
	line = strings.TrimPrefix(line, "=")
	return strings.Fields(line)
}

func expandInclude(include *ssh_config.Include, seen map[string]struct{}) []*ssh_config.Host {
	var matches []string
	for _, directive := range includeDirectives(include) {
		expanded, err := filepath.Glob(resolveIncludePath(directive))
		if err != nil {
			continue
		}
		matches = append(matches, expanded...)
	}
	var hosts []*ssh_config.Host
	for _, match := range matches {
		abs, err := filepath.Abs(match)
		if err != nil {
			abs = match
		}
		if _, dup := seen[abs]; dup {
			continue // already loaded somewhere up the chain
		}
		f, err := os.Open(match)
		if err != nil {
			continue
		}
		if err := validateOpenConfigPerms(match, f); err != nil {
			warningColor.Fprintf(os.Stderr, "Skipping include: %v\n", err)
			f.Close()
			continue
		}
		decoded, err := decodeConfig(f)
		f.Close()
		if err != nil {
			continue
		}
		// Mark before recursing so a self-referential include terminates.
		seen[abs] = struct{}{}
		hosts = append(hosts, resolveIncludes(decoded.Hosts, seen)...)
	}
	return hosts
}

// validateOpenConfigPerms refuses to parse a config file that another local
// user could have tampered with. Mirrors OpenSSH's StrictModes-style check on
// client config files: must be owned by the running user (or root) and must
// not be group/world writable. Stat is taken from the open fd so the result
// describes the same inode we will read, closing the TOCTOU window between
// the check and the parse.
func validateOpenConfigPerms(path string, f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil // Non-unix filesystem; mode/uid semantics differ.
	}
	return checkConfigOwnerAndMode(path, stat.Uid, info.Mode().Perm(), uint32(os.Getuid()))
}

func checkConfigOwnerAndMode(path string, fileUID uint32, mode os.FileMode, runningUID uint32) error {
	if fileUID != runningUID && fileUID != 0 {
		return fmt.Errorf("%s: bad ownership (uid %d; expected %d or root)", path, fileUID, runningUID)
	}
	if mode&0o022 != 0 {
		return fmt.Errorf("%s: bad permissions %#o (group/world writable)", path, mode)
	}
	return nil
}

// resolveIncludePath mirrors OpenSSH: "~" expands to the home directory,
// and relative paths resolve against ~/.ssh — never against the directory
// of the including file, no matter where that file lives.
func resolveIncludePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, "~") {
		return filepath.Join(home, path[1:])
	}
	if !filepath.IsAbs(path) {
		return filepath.Join(home, ".ssh", path)
	}
	return path
}
