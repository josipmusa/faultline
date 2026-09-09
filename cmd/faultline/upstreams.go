package main

import (
	"io"
	"strconv"

	"github.com/spf13/cobra"

	client "github.com/josipmusa/faultline/clients/go"
)

func newUpstreamsCmd() *cobra.Command {
	var flags apiFlags

	cmd := &cobra.Command{
		Use:   "upstreams",
		Short: "List the upstreams Faultline has seen",
		Long: "Upstreams is what the application actually called: one line per host,\n" +
			"how many of its requests were faulted, and the tier Faultline could see\n" +
			"it at. An encrypted host takes connection faults only; the note column\n" +
			"says when something is in the way.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := flags.client()
			if err != nil {
				return err
			}
			list, err := c.Upstreams(cmd.Context())
			if err != nil {
				return err
			}
			return writeUpstreams(cmd.OutOrStdout(), flags.json, list)
		},
	}

	cmd.Flags().StringVar(&flags.admin, "admin", client.DefaultAddr, adminFlagHelp)
	cmd.Flags().BoolVar(&flags.json, "json", false, jsonFlagHelp)
	return cmd
}

var upstreamColumns = []string{"HOST", "TIER", "REQUESTS", "FAULTED", "LAST SEEN", "NOTE"}

func writeUpstreams(w io.Writer, asJSON bool, list []client.Upstream) error {
	if asJSON {
		if list == nil {
			list = []client.Upstream{}
		}
		return printJSON(w, list)
	}
	if len(list) == 0 {
		return printf(w, "no upstreams seen yet\n")
	}

	rows := make([][]string, 0, len(list))
	for _, u := range list {
		rows = append(rows, []string{
			u.Host,
			orDash(string(u.Tier)),
			strconv.Itoa(u.Requests),
			strconv.Itoa(u.Faulted),
			u.LastSeen.Local().Format(clockFormat),
			orDash(upstreamNote(u)),
		})
	}
	return printTable(w, upstreamColumns, rows)
}

func upstreamNote(u client.Upstream) string {
	if u.Bypassed {
		return "bypassed"
	}
	return u.Hint
}
