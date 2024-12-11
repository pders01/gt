package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/kevinburke/ssh_config"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	cfg     *ssh_config.Config
	user    string
	useScp  bool
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

	rootCmd.AddCommand(listCmd)
}

func getHosts() []string {
	var hosts []string
	for _, host := range cfg.Hosts {
		pattern := host.Patterns[0].String()
		// Skip pattern entries (those with * or ?)
		if strings.ContainsAny(pattern, "*?") {
			continue
		}
		hosts = append(hosts, pattern)
	}
	sort.Strings(hosts)
	return hosts
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all hosts from SSH config",
	Long: `List all hosts defined in your SSH config file.
Includes entries from included config files.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		hosts := getHosts()
		if len(hosts) == 0 {
			warningColor.Println("No SSH hosts found")
			return nil
		}
		for _, host := range hosts {
			hostname, _ := cfg.Get(host, "Hostname")
			if hostname == "" {
				continue // Skip pattern entries without hostnames
			}
			user, _ := cfg.Get(host, "User")
			if user == "" {
				user = "root"
			}
			port, _ := cfg.Get(host, "Port")

			// Format: alias    user@host.subdomain.domain:port
			aliasColor.Printf("%-20s ", host)
			userColor.Print(user)
			symbolColor.Print("@")

			// Split hostname into parts and color each differently
			parts := strings.Split(hostname, ".")
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
			if port != "" && port != "22" {
				symbolColor.Print(":")
				portColor.Print(port)
			}

			fmt.Println() // New line
		}
		return nil
	},
}

var rootCmd = &cobra.Command{
	Use:   "gt [alias] [file...]",
	Short: "gt is a simple SSH connection manager",
	Long: `gt simplifies SSH connections by using your existing SSH config.
It reads Host definitions from ~/.ssh/config and provides a simpler interface
for connecting to your hosts.

Examples:
  # Connect to a host defined in ~/.ssh/config
  gt myserver

  # Connect with a different user
  gt myserver -u admin

  # Upload files to remote host
  gt myserver -s local1.txt local2.txt remote/path/

  # Download files from remote host
  gt myserver -s :remote/file1.txt :remote/file2.txt local/path/`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completeHosts,
	RunE: func(cmd *cobra.Command, args []string) error {
		alias := args[0]

		hostname, err := cfg.Get(alias, "Hostname")
		if err != nil || hostname == "" {
			return fmt.Errorf("host '%s' not found in SSH config", alias)
		}

		connectUser := user
		if connectUser == "" {
			connectUser, _ = cfg.Get(alias, "User")
		}
		if connectUser == "" {
			connectUser = "root"
		}

		address := fmt.Sprintf("%s@%s", connectUser, hostname)

		if useScp {
			return runSCP(alias, address, args[1:])
		}
		return runSSH(alias, address)
	},
}

func completeHosts(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return getHosts(), cobra.ShellCompDirectiveNoFileComp
}

func runSCP(alias string, address string, files []string) error {
	if len(files) == 0 {
		return fmt.Errorf("no files specified for SCP")
	}

	scpArgs := []string{}

	// Add SSH config options
	port, _ := cfg.Get(alias, "Port")
	if port != "" {
		scpArgs = append(scpArgs, "-P", port)
	}

	identity, _ := cfg.Get(alias, "IdentityFile")
	if identity != "" {
		scpArgs = append(scpArgs, "-i", identity)
	}

	// Determine if this is a download or upload
	// If the first argument contains a colon, it's a download
	isDownload := strings.Contains(files[0], ":")

	if isDownload {
		// For downloads, we need to prefix the remote files with the host address
		for i, file := range files {
			if strings.Contains(file, ":") {
				files[i] = fmt.Sprintf("%s:%s", address, strings.Split(file, ":")[1])
			}
		}
		scpArgs = append(scpArgs, files...)
	} else {
		// For uploads, only the destination needs the host address
		scpArgs = append(scpArgs, files[:len(files)-1]...)
		scpArgs = append(scpArgs, fmt.Sprintf("%s:%s", address, files[len(files)-1]))
	}

	execCmd := exec.Command("scp", scpArgs...)
	return runCommand(execCmd)
}

func runSSH(alias, address string) error {
	sshArgs := []string{}

	port, _ := cfg.Get(alias, "Port")
	if port != "" {
		sshArgs = append(sshArgs, "-p", port)
	}

	identity, _ := cfg.Get(alias, "IdentityFile")
	if identity != "" {
		sshArgs = append(sshArgs, "-i", identity)
	}

	sshArgs = append(sshArgs, address)
	return runCommand(exec.Command("ssh", sshArgs...))
}

func runCommand(cmd *exec.Cmd) error {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
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
		fmt.Fprintf(os.Stderr, "Error getting home directory: %v\n", err)
		os.Exit(1)
	}

	loadConfig(filepath.Join(home, ".ssh", "config"))
}

func loadConfig(path string) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not open SSH config at %s: %v\n", path, err)
		os.Exit(1)
	}
	defer f.Close()

	cfg, err = ssh_config.Decode(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing SSH config: %v\n", err)
		os.Exit(1)
	}

	// Handle includes manually since the library doesn't do it automatically
	for _, host := range cfg.Hosts {
		for _, node := range host.Nodes {
			if include, ok := node.(*ssh_config.Include); ok {
				handleInclude(include, filepath.Dir(path))
			}
		}
	}
}

func handleInclude(include *ssh_config.Include, baseDir string) {
	path := include.String()
	// Remove the "Include " prefix
	path = strings.TrimPrefix(path, "Include ")
	matches, err := filepath.Glob(resolveIncludePath(path, baseDir))
	if err != nil {
		return
	}
	for _, match := range matches {
		f, err := os.Open(match)
		if err != nil {
			continue
		}
		includedCfg, err := ssh_config.Decode(f)
		f.Close()
		if err != nil {
			continue
		}
		// Merge the included config
		cfg.Hosts = append(cfg.Hosts, includedCfg.Hosts...)
	}
}

func resolveIncludePath(path, baseDir string) string {
	// Handle ~ in path
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		path = filepath.Join(home, path[1:])
	}
	// Handle relative paths
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	return path
}
