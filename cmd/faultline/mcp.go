package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"syscall"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/admin"
	faultmcp "github.com/josipmusa/faultline/internal/mcp"
	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/runner"
	"github.com/josipmusa/faultline/internal/tlsmitm"
)

func newMCPCmd() *cobra.Command {
	flags := &apiFlags{}
	var configPath string
	var proxyPort int

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the MCP interface for coding agents",
		Long: "Serve Faultline's tools to a coding agent over stdio.\n\n" +
			"Add it to an agent that speaks MCP, such as:\n" +
			"  claude mcp add faultline -- faultline mcp\n\n" +
			"If a Faultline is already running at --admin, the agent joins it, so its rules\n" +
			"and the ones in the UI are the same rules. If none is, one is started here and\n" +
			"stopped again when the agent disconnects: an agent needs no separate step, and\n" +
			"the UI is on the admin port for as long as the session lasts.\n\n" +
			"The instance it starts reads faultline.yaml from the working directory and\n" +
			"creates the interception CA if there is none, the same way `faultline run`\n" +
			"does. Everything Faultline says goes to stderr; stdout is the protocol.\n\n" +
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
			return serveAgent(cmd.Context(), cmd.ErrOrStderr(), &sdk.StdioTransport{}, c,
				configPath, admin.DefaultPort, proxyPort)
		},
	}

	// --json means nothing here: the protocol decides the shape, not the user.
	cmd.PersistentFlags().StringVar(&flags.admin, "admin", client.DefaultAddr, adminFlagHelp)
	cmd.Flags().StringVar(&configPath, "config", "", configFlagHelp)
	cmd.Flags().IntVar(&proxyPort, "proxy-port", forward.DefaultPort,
		"port for the forward proxy of the instance this starts, if it starts one")

	return cmd
}

// serveAgent serves the tools to an agent on t. It attaches to the instance at
// c's address when one answers, and brings one up in this process when none
// does, so an agent is never the one that has to start Faultline first.
//
// In process rather than as a background child: the agent's session already
// owns this process, so the instance goes away with it and there is no pid
// file, no orphan, and no waiting for a port to come up.
func serveAgent(ctx context.Context, errOut io.Writer, t sdk.Transport, c *client.Client, configPath string, adminPort, proxyPort int) error {
	if running(ctx, c) {
		if err := printf(errOut, "agent: attached to the faultline at %s\n", c.Addr()); err != nil {
			return err
		}
		return faultmcp.Serve(ctx, c, version, t, faultmcp.Mirrors(errOut))
	}
	return serveAgentStandalone(ctx, errOut, t, configPath, adminPort, proxyPort)
}

// running reports whether something answers as Faultline at c's address. Only
// "nothing is listening" means no: an instance that is there but unwell is
// still the one the agent should be talking to, and starting a second one over
// the top of it would split the rules in two.
func running(ctx context.Context, c *client.Client) bool {
	_, err := c.Config(ctx)
	var unreachable *client.UnreachableError
	return !errors.As(err, &unreachable)
}

// serveAgentStandalone brings an instance up for this session and serves the
// tools over it, without going back out through the socket it is listening on.
func serveAgentStandalone(ctx context.Context, errOut io.Writer, t sdk.Transport, configPath string, adminPort, proxyPort int) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	routes, err := routesFor(cfg, nil, nil)
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
	// An agent that has to ask a human to run `faultline ca init` first has a
	// setup step after all, so the CA is created here the way `run` creates it.
	ca, err := ensureCA(caDir, errOut)
	if err != nil {
		return err
	}
	trustVars := runner.TrustVars(ca.CertPath, runner.JavaTrustStoreOrNone(ca.CertPath, errOut))

	s, err := start(cfg, adminPort, proxyPort, routes, ca, bypass, trustVars)
	if err != nil {
		// Two agents starting at once: the slower one lost the port, which
		// means the instance it wanted now exists, so it joins that one.
		if c, retry := attachAfterRace(err, adminPort); retry {
			return serveAgent(ctx, errOut, t, c, configPath, adminPort, proxyPort)
		}
		return err
	}

	if err := s.banner(errOut); err != nil {
		return err
	}
	s.watch(ctx)

	inProcess, err := faultmcp.InProcess(s.api)
	if err != nil {
		return stopAfter(ctx, s, err)
	}
	return stopAfter(ctx, s, faultmcp.Serve(ctx, inProcess, version, t, faultmcp.Mirrors(errOut)))
}

// attachAfterRace turns a lost race for the admin port into a client for
// whoever won it. Any other failure to start is the operator's to see.
func attachAfterRace(err error, adminPort int) (*client.Client, bool) {
	if !errors.Is(err, syscall.EADDRINUSE) {
		return nil, false
	}
	c, newErr := client.New(fmt.Sprintf("http://localhost:%d", adminPort))
	if newErr != nil {
		return nil, false
	}
	return c, true
}

// stopAfter takes the stack down once the session is over, and reports the
// session's own failure ahead of the shutdown's: the first is why the agent
// stopped, the second only how tidily.
func stopAfter(ctx context.Context, s *stack, err error) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	stopErr := s.stop(shutdownCtx)
	if err != nil {
		return err
	}
	return stopErr
}
