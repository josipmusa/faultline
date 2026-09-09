package main

import (
	"io"
	"strings"

	"github.com/spf13/cobra"

	client "github.com/josipmusa/faultline/clients/go"
)

func newScenarioCmd() *cobra.Command {
	var flags apiFlags

	cmd := &cobra.Command{
		Use:   "scenario",
		Short: "Turn scenarios on and off",
		Long: "A scenario is a named group of rules that describes a situation, declared\n" +
			"in faultline.yaml and committed with the repository. Turning one on enables\n" +
			"its rules and starts their behavior state over; only one is on at a time,\n" +
			"so turning one on turns off whichever was.",
	}
	flags.register(cmd)
	cmd.AddCommand(
		newScenarioListCmd(&flags),
		newScenarioSwitchCmd(&flags, "on", "Activate a scenario", true),
		newScenarioSwitchCmd(&flags, "off", "Deactivate a scenario", false),
	)
	return cmd
}

func newScenarioListCmd(flags *apiFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the scenarios the configuration declares",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := flags.client()
			if err != nil {
				return err
			}
			list, err := c.Scenarios(cmd.Context())
			if err != nil {
				return err
			}
			return writeScenarios(cmd.OutOrStdout(), flags.json, list)
		},
	}
}

func newScenarioSwitchCmd(flags *apiFlags, use, short string, active bool) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <name>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := flags.client()
			if err != nil {
				return err
			}
			scenario, err := c.SetScenarioActive(cmd.Context(), args[0], active)
			if err != nil {
				return err
			}
			return writeScenarios(cmd.OutOrStdout(), flags.json, []client.Scenario{scenario})
		},
	}
}

var scenarioColumns = []string{"NAME", "ACTIVE", "RULES"}

func writeScenarios(w io.Writer, asJSON bool, list []client.Scenario) error {
	if asJSON {
		if list == nil {
			list = []client.Scenario{}
		}
		return printJSON(w, list)
	}
	if len(list) == 0 {
		return printf(w, "no scenarios; declare them in faultline.yaml\n")
	}

	rows := make([][]string, 0, len(list))
	for _, s := range list {
		rows = append(rows, []string{s.Name, yesNo(s.Active), orDash(strings.Join(s.Rules, ", "))})
	}
	return printTable(w, scenarioColumns, rows)
}
