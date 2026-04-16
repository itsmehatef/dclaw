package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/itsmehatef/dclaw/internal/client"
)

// PrintAgents renders a list of agents in the currently selected output
// format (table, json, yaml). Called by `dclaw agent list`.
func PrintAgents(w io.Writer, agents []client.Agent) error {
	switch outputFormat {
	case "json":
		return encodeJSON(w, agents)
	case "yaml":
		return encodeYAML(w, agents)
	default:
		return encodeAgentTable(w, agents)
	}
}

// PrintAgent renders a single agent.
func PrintAgent(w io.Writer, a client.Agent) error {
	switch outputFormat {
	case "json":
		return encodeJSON(w, a)
	case "yaml":
		return encodeYAML(w, a)
	default:
		return encodeAgentTable(w, []client.Agent{a})
	}
}

// PrintChannels renders a list of channels.
func PrintChannels(w io.Writer, channels []client.Channel) error {
	switch outputFormat {
	case "json":
		return encodeJSON(w, channels)
	case "yaml":
		return encodeYAML(w, channels)
	default:
		return encodeChannelTable(w, channels)
	}
}

// PrintStatus renders a fleet status summary (string from DaemonStatus for
// now; structured upgrade in beta.1).
func PrintStatus(w io.Writer, status string) error {
	switch outputFormat {
	case "json":
		return encodeJSON(w, map[string]string{"status": status})
	case "yaml":
		return encodeYAML(w, map[string]string{"status": status})
	default:
		_, err := fmt.Fprintln(w, status)
		return err
	}
}

// ----- format-specific helpers -----

func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func encodeYAML(w io.Writer, v any) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	defer enc.Close()
	return enc.Encode(v)
}

func encodeAgentTable(w io.Writer, agents []client.Agent) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	defer tw.Flush()
	fmt.Fprintln(tw, "NAME\tIMAGE\tSTATUS\tWORKSPACE")
	for _, a := range agents {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", a.Name, a.Image, a.Status, a.Workspace)
	}
	return nil
}

func encodeChannelTable(w io.Writer, channels []client.Channel) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	defer tw.Flush()
	fmt.Fprintln(tw, "NAME\tTYPE\tCONFIG")
	for _, c := range channels {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", c.Name, c.Type, c.Config)
	}
	return nil
}

// humanTime renders a unix-seconds timestamp as a relative duration when recent
// or a short ISO date when older. Used by the TUI.
func humanTime(unix int64) string {
	if unix == 0 {
		return "-"
	}
	t := time.Unix(unix, 0)
	age := time.Since(t)
	switch {
	case age < time.Minute:
		return fmt.Sprintf("%ds", int(age.Seconds()))
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	default:
		return t.Format("2006-01-02")
	}
}

// Kept explicit to silence the linter on unused helpers during staged dev.
var _ = os.Stdout
