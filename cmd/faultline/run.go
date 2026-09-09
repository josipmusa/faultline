package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/josipmusa/faultline/internal/admin"
	"github.com/josipmusa/faultline/internal/config"
	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/proxy/reverse"
	"github.com/josipmusa/faultline/internal/runner"
	"github.com/josipmusa/faultline/internal/tlsmitm"
)

// exitError carries a child's exit code out to main, which exits with it.
// It is never printed: the child has already said whatever it had to say.
type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func newRunCmd() *cobra.Command {
	var routeSpecs []string
	var configPath string

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
			"Node's native fetch ignores the proxy variables on its own, so\n" +
			"NODE_USE_ENV_PROXY is set for it; a Node too old to know that variable\n" +
			"sends fetch calls direct and they will not appear in Faultline.\n\n" +
			"A JVM reads none of those variables, so JAVA_TOOL_OPTIONS carries the\n" +
			"same settings as system properties, along with a trust store copied\n" +
			"from the JDK's own cacerts with the Faultline CA added, so the child\n" +
			"still trusts everything it trusted before. Reactor Netty, which Spring's\n" +
			"WebClient is built on, only reads those properties when the client is\n" +
			"built with proxyWithSystemProperties(); everything on the JDK's own HTTP\n" +
			"stack, RestClient included, needs no change.\n\n" +
			"The child owns stdin, stdout and stderr, interrupts are handed to it\n" +
			"rather than acted on here, and Faultline exits with the child's own exit\n" +
			"code. Everything Faultline itself prints goes to stderr.\n\n" +
			"Browser traffic is not the child's traffic, so a wrapped dev server's\n" +
			"page is not covered by any of this. Give the dependency an explicit\n" +
			"route with --route and point the dev server's own proxy at the local\n" +
			"port it prints (see docs/frontends.md).\n\n" +
			"Put the command after --, so its own flags are not read as Faultline's:\n" +
			"  faultline run -- npm run dev",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(configPath)
			if err != nil {
				return err
			}
			routes, err := routesFor(cfg, routeSpecs, nil)
			if err != nil {
				return err
			}
			bypass, err := bypassFor(cfg, nil)
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
				cfg, admin.DefaultPort, forward.DefaultPort, routes, ca, bypass, args)
			if err != nil {
				return err
			}
			if code != 0 {
				return exitError(code)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", configFlagHelp)
	cmd.Flags().StringArrayVar(&routeSpecs, "route", nil,
		"explicit route as name=url, repeatable; point a dev server's own proxy "+
			"at the port it prints (--route api=https://api.stripe.com)")

	// Everything after the command name belongs to the child, flags included.
	cmd.Flags().SetInterspersed(false)

	return cmd
}

// run brings the stack up, runs args under it, and takes it down again once
// the child is gone. The returned code is the child's, so the caller can exit
// with it. Faultline's own output goes to errOut: stdout is the child's.
func run(ctx context.Context, out, errOut io.Writer, in io.Reader, cfg *config.Config, adminPort, proxyPort int, routes []reverse.Route, ca *tlsmitm.CA, bypass *forward.Bypass, args []string) (int, error) {
	caPath := ""
	if ca != nil {
		caPath = ca.CertPath
	}
	javaStore := javaTrustStore(caPath, errOut)
	trustVars := runner.TrustVars(caPath, javaStore)

	s, err := start(cfg, adminPort, proxyPort, routes, ca, bypass, trustVars)
	if err != nil {
		return 1, err
	}

	if err := s.banner(errOut); err != nil {
		_ = s.stop(context.Background())
		return 1, err
	}

	// A child that does not trust the CA is the most common way a wrapped run
	// goes wrong, and it looks like a network failure from the child's side, so
	// the run says so as it happens rather than only in /api/upstreams.
	s.watch(ctx)

	stopWatching := watchDistrust(s.recorder, errOut, trustVars)

	env := runner.Env(os.Environ(), s.proxyURL(), noProxy(bypass), caPath, javaStore)
	code, runErr := runner.Run(ctx, args, env, in, out, errOut)
	stopWatching()

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

// javaTrustStore builds the trust store a JVM child needs, beside the CA it
// trusts, and returns where it is. A JVM cannot be handed a certificate to
// add to its own roots, so this is the only way it can trust Faultline and
// its real dependencies at once.
//
// A machine with no JDK is silent: most children are not JVMs and there is
// nothing to do. A JDK that fails gets one line, because a JVM child will
// then fail every HTTPS call for a reason that is not visible from the
// failure, and the rest of the run is unaffected either way.
func javaTrustStore(caPath string, notice io.Writer) string {
	store, err := runner.JavaTrustStore(filepath.Dir(caPath), caPath)
	switch {
	case errors.Is(err, runner.ErrNoJDK):
		return ""
	case err != nil:
		_, _ = fmt.Fprintf(notice, "java: no trust store, JVM children will not trust Faultline: %v\n", err)
		return ""
	}
	return store
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
