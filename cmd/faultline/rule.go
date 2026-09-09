package main

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	client "github.com/josipmusa/faultline/clients/go"
)

func newRuleCmd() *cobra.Command {
	var flags apiFlags

	cmd := &cobra.Command{
		Use:   "rule",
		Short: "Manage fault rules",
		Long: "Rule adds, removes, and turns fault rules on and off in a running\n" +
			"Faultline, through the same HTTP API the web UI uses. Start one with\n" +
			"`faultline serve` or `faultline run` first.",
	}
	flags.register(cmd)
	cmd.AddCommand(
		newRuleListCmd(&flags),
		newRuleAddCmd(&flags),
		newRuleRmCmd(&flags),
		newRuleEnabledCmd(&flags, "enable", "Turn a rule on", true),
		newRuleEnabledCmd(&flags, "disable", "Turn a rule off", false),
	)
	return cmd
}

func newRuleListCmd(flags *apiFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the rules the instance holds",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := flags.client()
			if err != nil {
				return err
			}
			list, err := c.Rules(cmd.Context())
			if err != nil {
				return err
			}
			return writeRules(cmd.OutOrStdout(), flags.json, list)
		},
	}
}

func newRuleAddCmd(flags *apiFlags) *cobra.Command {
	var spec ruleSpec

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a rule",
		Long: "Add writes one rule. A rule is a match, a fault, and optionally a\n" +
			"behavior that says when the fault applies:\n\n" +
			"  faultline rule add --host api.stripe.com --fault delay --set ms=2000\n" +
			"  faultline rule add --host api.stripe.com --path '/v1/charges/*' \\\n" +
			"      --fault status --set code=503 --behavior first_n --behavior-set n=2\n\n" +
			"Every fault and behavior in the catalogue is reached the same way, with\n" +
			"--set and --behavior-set, so nothing here has to know their parameters.\n" +
			"Faultline validates them and names the field it refused.\n\n" +
			"A whole rule can be read as JSON instead, from a file or standard input:\n\n" +
			"  faultline rule add --from rule.json\n" +
			"  echo '{\"name\":\"...\"}' | faultline rule add --from -",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := flags.client()
			if err != nil {
				return err
			}
			rule, err := spec.rule(cmd)
			if err != nil {
				return err
			}
			added, err := c.AddRule(cmd.Context(), rule)
			if err != nil {
				return err
			}
			return writeRules(cmd.OutOrStdout(), flags.json, []client.Rule{added})
		},
	}
	spec.register(cmd)
	return cmd
}

func newRuleRmCmd(flags *apiFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>...",
		Short: "Remove one or more rules",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := flags.client()
			if err != nil {
				return err
			}
			for _, id := range args {
				if err := c.DeleteRule(cmd.Context(), id); err != nil {
					return err
				}
				// The API answers a delete with no content, so there is nothing
				// to print as JSON; the line is for the person either way.
				if err := printf(cmd.OutOrStdout(), "removed rule %q\n", id); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func newRuleEnabledCmd(flags *apiFlags, use, short string, enabled bool) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <id>",
		Short: short,
		Long:  short + ". Either way the rule's behavior state starts over, so a first_n rule fails its first N requests again.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := flags.client()
			if err != nil {
				return err
			}
			rule, err := c.SetRuleEnabled(cmd.Context(), args[0], enabled)
			if err != nil {
				return err
			}
			return writeRules(cmd.OutOrStdout(), flags.json, []client.Rule{rule})
		},
	}
}

var ruleColumns = []string{"ID", "ENABLED", "MATCH", "FAULT", "BEHAVIOR", "NAME"}

func writeRules(w io.Writer, asJSON bool, list []client.Rule) error {
	if asJSON {
		if list == nil {
			list = []client.Rule{}
		}
		return printJSON(w, list)
	}
	if len(list) == 0 {
		return printf(w, "no rules\n")
	}

	rows := make([][]string, 0, len(list))
	for _, r := range list {
		rows = append(rows, []string{
			r.ID,
			yesNo(r.Enabled),
			matchText(r.Match),
			faultText(r.Fault),
			behaviorText(r.Behavior),
			r.Name,
		})
	}
	return printTable(w, ruleColumns, rows)
}

// matchText renders a match the way it reads aloud: "POST api.stripe.com/v1/*",
// with the parts that were left out left out.
func matchText(m client.Match) string {
	host := m.Host
	if host == "" {
		host = "*"
	}

	text := host + m.Path
	if m.Method != "" {
		text = m.Method + " " + text
	}
	for _, name := range slices.Sorted(maps.Keys(m.Header)) {
		text += fmt.Sprintf(" +%s=%s", name, m.Header[name])
	}
	return text
}

func faultText(f client.Fault) string {
	return orDash(strings.TrimSpace(string(f.Type) + " " + paramsText(f.Params)))
}

func behaviorText(b *client.Behavior) string {
	if b == nil {
		return "-"
	}
	return orDash(strings.TrimSpace(string(b.Type) + " " + paramsText(b.Params)))
}

// paramsText renders a parameter bag in name order, so two rules with the same
// parameters read the same in the table.
func paramsText(params client.Params) string {
	parts := make([]string, 0, len(params))
	for _, name := range slices.Sorted(maps.Keys(params)) {
		parts = append(parts, fmt.Sprintf("%s=%v", name, valueText(params[name])))
	}
	return strings.Join(parts, " ")
}

// valueText keeps a whole number a whole number: JSON decoding makes every
// number a float, and "ms=2000" is what was written.
func valueText(value any) any {
	if number, ok := value.(float64); ok && number == float64(int64(number)) {
		return int64(number)
	}
	return value
}
