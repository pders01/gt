package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

// auditEntry is one line in the connections.jsonl log. Field order is fixed
// by JSON tags; new fields go at the end so older readers tolerate them.
type auditEntry struct {
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Alias      string    `json:"alias"`
	Address    string    `json:"address"`
	Mode       string    `json:"mode"` // "ssh" or "scp"
	ExitCode   int       `json:"exit_code"`
	DurationMS int64     `json:"duration_ms"`
}

// auditLogPath resolves the audit log location. GT_LOG_DIR wins (used by
// tests); then XDG_STATE_HOME per the XDG spec; then the conventional
// ~/.local/state fallback. Logs are state, not config or cache.
func auditLogPath() (string, error) {
	if dir := os.Getenv("GT_LOG_DIR"); dir != "" {
		return filepath.Join(dir, "connections.jsonl"), nil
	}
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "gt", "connections.jsonl"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "gt", "connections.jsonl"), nil
}

// appendAuditEntry serializes one entry as JSON and appends it as a single
// line. O_APPEND keeps concurrent writes from different gt invocations from
// interleaving as long as the line stays under PIPE_BUF (4 KiB on darwin
// and linux), which a metadata-only entry comfortably does.
func appendAuditEntry(e auditEntry) error {
	path, err := auditLogPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = f.Write(line)
	return err
}

// runCommandLogged wraps runCommand with timing and audit-log emission.
// Auditing is best-effort: if the log write fails (disk full, perms,
// missing parent) we surface a warning but do not fail the connection.
func runCommandLogged(cmd *exec.Cmd, alias, address, mode string) error {
	start := time.Now()
	err := runCommand(cmd)
	if noLog {
		return err
	}
	end := time.Now()

	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1 // command did not run cleanly (binary missing, etc.)
		}
	}

	if logErr := appendAuditEntry(auditEntry{
		Start:      start,
		End:        end,
		Alias:      alias,
		Address:    address,
		Mode:       mode,
		ExitCode:   exitCode,
		DurationMS: end.Sub(start).Milliseconds(),
	}); logErr != nil {
		warningColor.Fprintf(os.Stderr, "Could not write audit log: %v\n", logErr)
	}
	return err
}

var logLimit int

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Show recent connections from the audit log",
	Long: `Show recent connections from the local audit log at
$XDG_STATE_HOME/gt/connections.jsonl (or ~/.local/state/gt/connections.jsonl).
Each line is one connection: timestamp, alias, address, mode, duration, exit code.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := auditLogPath()
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				warningColor.Println("No audit log yet")
				return nil
			}
			return err
		}
		defer f.Close()

		var entries []auditEntry
		dec := json.NewDecoder(f)
		for dec.More() {
			var e auditEntry
			if err := dec.Decode(&e); err != nil {
				continue // skip malformed lines so a partial write does not poison the view
			}
			entries = append(entries, e)
		}
		if logLimit > 0 && len(entries) > logLimit {
			entries = entries[len(entries)-logLimit:]
		}
		for _, e := range entries {
			renderAuditEntry(e)
		}
		return nil
	},
}

func renderAuditEntry(e auditEntry) {
	symbolColor.Printf("%s  ", e.Start.Local().Format("2006-01-02 15:04:05"))
	aliasColor.Printf("%-16s ", e.Alias)
	userColor.Print(e.Address)
	symbolColor.Printf("  %s  %s  ", e.Mode, formatDuration(e.DurationMS))
	if e.ExitCode == 0 {
		userColor.Print("ok")
	} else {
		errorColor.Printf("exit %d", e.ExitCode)
	}
	fmt.Println()
}

func formatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	if ms < 60_000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	return fmt.Sprintf("%dm%02ds", ms/60_000, (ms%60_000)/1000)
}
