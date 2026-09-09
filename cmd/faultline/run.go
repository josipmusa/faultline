package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/josipmusa/faultline/internal/admin"
	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/runner"
	"github.com/josipmusa/faultline/internal/tlsmitm"
)

// exitError carries a child's exit code out to main, which exits with it.
// It is never printed: the child has already said whatever it had to say.
type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run -- <command> [args...]",
		Short: "Start an application with Faultline in front of it",
		Long: "Run starts the admin server and the forward proxy, then runs your usual\n" +
			"start command as a child with the proxy variables set, so its outbound\n" +
			"HTTP and HTTPS calls pass through Faultline without changing anything in\n" +
			"the application.\n\n" +
			"HTTPS is intercepted, so the child is also told where the Faultline CA\n" +
			"is through the variables Go, OpenSSL, Python, curl, Node and git read.\n" +
			"The CA is created on the first run if there is none yet; trusting it\n" +
			"system-wide stays a separate, explicit `faultline ca install`.\n\n" +
			"The child owns stdin, stdout and stderr, interrupts are handed to it\n" +
			"rather than acted on here, and Faultline exits with the child's own exit\n" +
			"code. Everything Faultline itself prints goes to stderr.\n\n" +
			"Put the command after --, so its own flags are not read as Faultline's:\n" +
			"  faultline run -- npm run dev",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bypass, err := bypassList(nil)
			if err != nil {
				return err
			}
			caDir, err := tlsmitm.DefaultDir()
			if err != nil {
				return err
			}
			ca, err := ensureCA(caDir, cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			code, err := run(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin(),
				admin.DefaultPort, forward.DefaultPort, ca, bypass, args)
			if err != nil {
				return err
			}
			if code != 0 {
				return exitError(code)
			}
			return nil
		},
	}

	// Everything after the command name belongs to the child, flags included.
	cmd.Flags().SetInterspersed(false)

	return cmd
}

// run brings the stack up, runs args under it, and takes it down again once
// the child is gone. The returned code is the child's, so the caller can exit
// with it. Faultline's own output goes to errOut: stdout is the child's.
func run(ctx context.Context, out, errOut io.Writer, in io.Reader, adminPort, proxyPort int, ca *tlsmitm.CA, bypass *forward.Bypass, args []string) (int, error) {
	s, err := start(adminPort, proxyPort, nil, ca, bypass)
	if err != nil {
		return 1, err
	}

	if err := s.banner(errOut); err != nil {
		_ = s.stop(context.Background())
		return 1, err
	}

	caPath := ""
	if ca != nil {
		caPath = ca.CertPath
	}
	env := runner.Env(os.Environ(), s.proxyURL(), noProxy(bypass), caPath)
	code, runErr := runner.Run(ctx, args, env, in, out, errOut)

	// The child is gone, so nothing new will arrive; the timeout is only there
	// for requests it left in flight.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	stopErr := s.stop(shutdownCtx)

	if runErr != nil {
		return code, runErr
	}
	return code, stopErr
}

// ensureCA loads the interception CA, creating it the first time `run` is
// used so wrapping a command needs no setup step. The notice goes out only on
// the run that created it, on one line, because from then on it is not news.
func ensureCA(dir string, notice io.Writer) (*tlsmitm.CA, error) {
	ca, err := tlsmitm.Load(dir)
	if !errors.Is(err, tlsmitm.ErrNotFound) {
		return ca, err
	}

	ca, err = tlsmitm.Create(dir)
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(notice, "ca: created %s, valid until %s\n",
		ca.CertPath, ca.Cert.NotAfter.Format("2006-01-02")); err != nil {
		return nil, err
	}
	return ca, nil
}

// noProxy renders the bypass list the way NO_PROXY wants it, so the child
// skips the proxy for exactly the hosts the proxy would have passed through
// anyway, the admin port among them.
func noProxy(bypass *forward.Bypass) []string {
	patterns := bypass.Patterns()
	entries := make([]string, 0, len(patterns))
	for _, p := range patterns {
		// NO_PROXY spells a subdomain wildcard as a bare leading dot.
		entries = append(entries, strings.TrimPrefix(p, "*"))
	}
	return entries
}
