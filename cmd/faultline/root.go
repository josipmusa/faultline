package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "faultline",
		Short: "See what your app calls, and make it misbehave on purpose",
		Long: "Faultline is an HTTP and HTTPS fault-injection proxy for development and testing.\n" +
			"It sits between an application and its dependencies, shows every outbound call,\n" +
			"and can degrade those calls on purpose.",
		SilenceUsage:  true,
		SilenceErrors: true, // main prints the error once, prefixed with the program name
	}

	root.AddCommand(
		newServeCmd(),
		placeholder("run", "Start an application with Faultline in front of it"),
		placeholder("rule", "Manage fault rules"),
		placeholder("scenario", "Manage scenarios"),
		newCACmd(),
		placeholder("mcp", "Serve the MCP interface for coding agents"),
		newVersionCmd(),
	)

	return root
}

// placeholder returns a subcommand that is declared but not built yet.
func placeholder(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "not implemented")
			return err
		},
	}
}
