package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// ExitCodeNeedsDaemon is the BSD sysexits.h EX_UNAVAILABLE value, used to
// signal that a command requires the dclaw daemon, which is not yet shipped.
const ExitCodeNeedsDaemon = 69

// NextMilestone is the version at which the daemon-backed commands will start
// working. Keep this in one place so updates ship everywhere.
const NextMilestone = "v0.3.0-daemon"

// NotReadyPayload is the JSON envelope emitted by RequireDaemon when the user
// has selected -o json. Scripts can key off `error == "feature_not_ready"`.
type NotReadyPayload struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	ExitCode  int    `json:"exit_code"`
	Milestone string `json:"milestone"`
	Command   string `json:"command"`
}

// RequireDaemon writes the standardized "daemon required" message to either
// stdout (as structured JSON when -o json is set) or stderr (as human prose),
// then terminates the process with exit code 69.
//
// It calls os.Exit directly rather than returning an error because cobra's
// error-return path would produce its own "Error: ..." prefix; we want a
// clean, predictable message for scripting.
func RequireDaemon(cmd *cobra.Command, commandName string) error {
	msg := fmt.Sprintf(
		"%s requires the dclaw daemon, which is not yet implemented — see %s",
		commandName, NextMilestone,
	)

	if outputFormat == "json" {
		payload := NotReadyPayload{
			Error:     "feature_not_ready",
			Message:   msg,
			ExitCode:  ExitCodeNeedsDaemon,
			Milestone: NextMilestone,
			Command:   commandName,
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		_ = enc.Encode(payload)
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), msg)
	}

	os.Exit(ExitCodeNeedsDaemon)
	return nil // unreachable
}
