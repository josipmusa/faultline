package main

import (
	"github.com/spf13/cobra"
)

// newSessionCmd is the session's CLI face: reading what an instance that is
// already up has observed, and putting it back to the start of a measurement.
// `faultline run` reports the session it owns; these two work on the one that
// is already running.
func newSessionCmd() *cobra.Command {
	var flags apiFlags

	cmd := &cobra.Command{
		Use:   "session",
		Short: "Read and clear the current session",
		Long: "A session is what Faultline has observed since it started, or since the last\n" +
			"reset. `session report` prints the same document a finished run prints, for an\n" +
			"instance that is already up. `session reset` puts it back to the start of a\n" +
			"measurement: what was observed is cleared and every rule's behavior state is\n" +
			"re-armed, so a spent first_n applies again, while the rules themselves and the\n" +
			"active scenario are left exactly as they are.",
	}
	flags.register(cmd)
	cmd.AddCommand(newSessionReportCmd(&flags), newSessionResetCmd(&flags))
	return cmd
}

func newSessionReportCmd(flags *apiFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "report",
		Short: "Print the current session's report",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := flags.client()
			if err != nil {
				return err
			}
			report, err := c.Report(cmd.Context())
			if err != nil {
				return err
			}
			// A warning is a diagnostic, so it goes to stderr whichever mode
			// this is in: visible to a person, out of a piped payload. The
			// JSON carries them as well, for a script that wants them.
			if err := writeReportWarnings(cmd.ErrOrStderr(), report); err != nil {
				return err
			}
			if flags.json {
				return printJSON(cmd.OutOrStdout(), report)
			}
			return writeReportTable(cmd.OutOrStdout(), report)
		},
	}
}

// sessionResetResult is what --json answers. The endpoint has nothing to
// return but its success, and it is the same shape the reset_session tool
// answers an agent with.
type sessionResetResult struct {
	Reset bool `json:"reset"`
}

func newSessionResetCmd(flags *apiFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Clear the recorded calls and re-arm every rule",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := flags.client()
			if err != nil {
				return err
			}
			if err := c.ResetSession(cmd.Context()); err != nil {
				return err
			}
			if flags.json {
				return printJSON(cmd.OutOrStdout(), sessionResetResult{Reset: true})
			}
			return printf(cmd.OutOrStdout(), "session reset: observed calls cleared, rules re-armed\n")
		},
	}
}
