package main

import (
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
		newInitCmd(),
		newServeCmd(),
		newRunCmd(),
		newRuleCmd(),
		newScenarioCmd(),
		newSessionCmd(),
		newEventsCmd(),
		newUpstreamsCmd(),
		newCACmd(),
		newMCPCmd(),
		newVersionCmd(),
	)

	return root
}
