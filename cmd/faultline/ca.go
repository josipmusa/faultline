package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/josipmusa/faultline/internal/tlsmitm"
)

func newCACmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ca",
		Short: "Manage the TLS interception certificate authority",
		Long: "Faultline terminates HTTPS with certificates signed by a local certificate\n" +
			"authority, so it can see and degrade encrypted traffic. The CA lives in the\n" +
			"per-user config directory and is never trusted by the system without an\n" +
			"explicit confirmation.",
	}
	cmd.AddCommand(newCAInitCmd(), newCAPathCmd(), newCAInstallCmd())
	return cmd
}

func newCAInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the CA key and certificate",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := tlsmitm.DefaultDir()
			if err != nil {
				return err
			}
			ca, err := tlsmitm.Create(dir)
			if errors.Is(err, tlsmitm.ErrExists) {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "CA already exists in %s\n", dir)
				return err
			}
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(),
				"CA created: %s\nValid until %s. Run `faultline ca install` to trust it system-wide.\n",
				ca.CertPath, ca.Cert.NotAfter.Format("2006-01-02"))
			return err
		},
	}
}

func newCAPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the path of the CA certificate",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ca, err := loadCA()
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), ca.CertPath)
			return err
		},
	}
}

func newCAInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Explain how to trust the CA, and offer to do it",
		Long: "Install prints the steps for trusting the Faultline CA on this operating\n" +
			"system. On macOS and Linux it offers to run them, and only does so after\n" +
			"an explicit yes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ca, err := loadCA()
			if err != nil {
				return err
			}
			plan := tlsmitm.TrustPlan(runtime.GOOS, exec.LookPath, ca.CertPath)
			return tlsmitm.Install(cmd.Context(), plan, cmd.InOrStdin(), cmd.OutOrStdout(), runAttached)
		},
	}
}

// loadCA reads the CA from the default directory, pointing at `ca init` when
// there is none yet.
func loadCA() (*tlsmitm.CA, error) {
	dir, err := tlsmitm.DefaultDir()
	if err != nil {
		return nil, err
	}
	ca, err := tlsmitm.Load(dir)
	if errors.Is(err, tlsmitm.ErrNotFound) {
		return nil, fmt.Errorf("%w; run `faultline ca init` first", err)
	}
	return ca, err
}

// runAttached runs a trust command on the user's terminal so sudo can prompt.
func runAttached(ctx context.Context, argv []string) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // argv comes from the fixed trust plan
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
