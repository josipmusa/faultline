package main

import (
	"github.com/spf13/cobra"

	client "github.com/josipmusa/faultline/clients/go"
	faultmcp "github.com/josipmusa/faultline/internal/mcp"
)

func newMCPCmd() *cobra.Command {
	flags := &apiFlags{}

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the MCP interface for coding agents",
		Long: "Serve Faultline's tools to a coding agent over stdio, acting on the instance at --admin.\n\n" +
			"Add it to an agent that speaks MCP, such as:\n" +
			"  claude mcp add faultline -- faultline mcp\n\n" +
			"The same tools are served over HTTP at /mcp on the admin port, for an agent that\n" +
			"would rather connect to a Faultline that is already running.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := flags.client()
			if err != nil {
				return err
			}
			// The agent owns stdout: it is the transport. Anything Faultline
			// wants to say goes to stderr, which cobra already uses for errors.
			return faultmcp.ServeStdio(cmd.Context(), c, version)
		},
	}

	// --json means nothing here: the protocol decides the shape, not the user.
	cmd.PersistentFlags().StringVar(&flags.admin, "admin", client.DefaultAddr, adminFlagHelp)

	return cmd
}
