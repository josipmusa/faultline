package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version is the build version, overridden at link time with
// -ldflags "-X main.version=<v>".
var version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the Faultline version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "faultline %s\n", version)
			return err
		},
	}
}
