package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/josipmusa/faultline/internal/admin"
	"github.com/josipmusa/faultline/internal/capture"
	"github.com/josipmusa/faultline/internal/config"
	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/faults"
	faultmcp "github.com/josipmusa/faultline/internal/mcp"
	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/proxy/reverse"
	"github.com/josipmusa/faultline/internal/rules"
	"github.com/josipmusa/faultline/internal/tlsmitm"
)

// shutdownTimeout is how long in-flight requests get to finish after Ctrl-C.
const shutdownTimeout = 10 * time.Second

func newServeCmd() *cobra.Command {
	var routeSpecs, portSpecs, bypassSpecs []string
	var configPath string
	var adminPort, proxyPort int
	var intercept bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the admin server and proxies",
		Long: "Serve starts the admin API, the forward proxy, and a listener for every\n" +
			"explicit route, so an application can send its traffic through Faultline\n" +
			"either by setting HTTP_PROXY or by pointing at a route's local port.\n\n" +
			"HTTPS is intercepted when the local CA exists (see faultline ca init), so\n" +
			"response faults reach encrypted traffic too. Pass --intercept=false to\n" +
			"tunnel HTTPS blindly instead; only connection faults apply then.\n\n" +
			"Hosts on the bypass list are passed through untouched: no rules, no events,\n" +
			"no interception. localhost is always on it, so Faultline never proxies itself.\n\n" +
			"A faultline.yaml in the working directory is read at startup and watched\n" +
			"while Faultline runs: edit a rule and save, and the change is in force\n" +
			"without a restart. Rules added or changed through the API are written back\n" +
			"to it. Routes and the bypass list are read once, so a change to those needs\n" +
			"a restart; Faultline says so rather than ignoring it.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(configPath)
			if err != nil {
				return err
			}
			routes, err := routesFor(cfg, routeSpecs, portSpecs)
			if err != nil {
				return err
			}
			bypass, err := bypassFor(cfg, bypassSpecs)
			if err != nil {
				return err
			}
			caDir, err := tlsmitm.DefaultDir()
			if err != nil {
				return err
			}
			ca, err := resolveInterception(caDir, cmd.Flags().Changed("intercept"), intercept)
			if err != nil {
				return err
			}
			return serve(cmd.Context(), cmd.OutOrStdout(), cfg, adminPort, proxyPort, routes, ca, bypass)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", configFlagHelp)
	cmd.Flags().StringArrayVar(&routeSpecs, "route", nil,
		"explicit route as name=url, repeatable (--route stripe=https://api.stripe.com)")
	cmd.Flags().StringArrayVar(&portSpecs, "route-port", nil,
		"local port for a route as name=port, repeatable (--route-port stripe=9100)")
	cmd.Flags().IntVar(&adminPort, "admin-port", admin.DefaultPort,
		"port for the admin API, the UI, and the MCP endpoint; 0 lets the operating system "+
			"choose one, which is how a test suite gets an instance of its own")
	cmd.Flags().IntVar(&proxyPort, "proxy-port", forward.DefaultPort,
		"port for the forward proxy, the one HTTP_PROXY points at")
	cmd.Flags().BoolVar(&intercept, "intercept", true,
		"terminate HTTPS with the local CA so response faults apply to it (default on when the CA exists)")
	cmd.Flags().StringSliceVar(&bypassSpecs, "bypass", nil,
		"host to pass through untouched, like NO_PROXY; repeatable or comma-separated, "+
			"*.internal covers subdomains, host:port limits it to a port (--bypass httpbin.org)")

	return cmd
}

// bypassList builds the forward proxy's bypass list: the defaults that keep
// Faultline from proxying itself, then whatever --bypass added.
// bypassList is the always-on entries plus the ones asked for. Only the ones
// asked for can be wrong here: the loader has already checked the file's.
func bypassList(specs []string) (*forward.Bypass, error) {
	bypass, err := forward.NewBypass(append(slices.Clone(forward.DefaultBypass), specs...))
	if err != nil {
		return nil, fmt.Errorf("--bypass: %w", err)
	}
	return bypass, nil
}

// resolveInterception decides whether HTTPS is intercepted. Left alone, the
// answer is yes exactly when the CA exists. Saying --intercept explicitly
// turns a missing CA into an error rather than a silent downgrade, and
// --intercept=false tunnels blindly even with a CA.
func resolveInterception(caDir string, flagSet, want bool) (*tlsmitm.CA, error) {
	if flagSet && !want {
		return nil, nil
	}
	ca, err := tlsmitm.Load(caDir)
	switch {
	case errors.Is(err, tlsmitm.ErrNotFound) && !flagSet:
		return nil, nil
	case errors.Is(err, tlsmitm.ErrNotFound):
		return nil, fmt.Errorf("--intercept: %w; run `faultline ca init` first, or pass --intercept=false", err)
	case err != nil:
		return nil, fmt.Errorf("--intercept: %w", err)
	}
	return ca, nil
}

// stack is everything a command brings up: the forward proxy, the admin
// server, and a listener for each explicit route. serve and run differ only
// in what they wait for while it is up.
type stack struct {
	routes   []reverse.Route
	server   *reverse.Server
	proxy    *forward.Server
	api      *admin.Server
	recorder *events.Recorder
	ca       *tlsmitm.CA
	bypass   *forward.Bypass
	config   *config.File
}

// start brings the whole stack up, or none of it. With a CA, CONNECT tunnels
// are intercepted; without one they are tunneled blindly. Hosts on the bypass
// list, which may be nil, skip the proxy's pipeline altogether. trustVars are
// the trust variables a wrapped child was given, empty for serve, which runs
// no child.
func start(cfg *config.Config, adminPort, proxyPort int, routes []reverse.Route, ca *tlsmitm.CA, bypass *forward.Bypass, trustVars []string) (*stack, error) {
	s := &stack{
		routes:   routes,
		recorder: events.NewRecorder(events.DefaultSize),
		ca:       ca,
		bypass:   bypass,
	}

	store := rules.New()
	scenarios := rules.NewScenarios(store)
	if cfg != nil {
		// The rules the file declares are in force before anything listens, so
		// the first request through cannot slip past them. Its scenarios come
		// with them: activating one is a change to rules that already exist.
		store.Replace(cfg.Rules)
		scenarios.Replace(cfg.Scenarios)
	}
	// One gate behind every pipeline: a rule that fails the first two requests
	// fails two altogether, not two per tier.
	gate := faults.NewGate()
	// One capture store behind every pipeline too, so the inspector reads an
	// exchange the same way whichever door it came through.
	captures := capture.NewStore(0)
	pipeline := faults.New(nil, store, s.recorder, events.TierPlain, gate)
	pipeline.CaptureTo(captures)

	fail := func(err error) (*stack, error) {
		_ = s.stop(context.Background())
		return nil, err
	}

	// Routes are optional now that the forward proxy is always there: setting
	// HTTP_PROXY is reason enough to run Faultline.
	if len(routes) > 0 {
		server, err := reverse.NewServer(routes, pipeline, nil)
		if err != nil {
			return fail(err)
		}
		s.server = server
		if err := server.Start(); err != nil {
			return fail(err)
		}
	}

	var interceptor *forward.Interceptor
	if ca != nil {
		issuer, err := tlsmitm.NewIssuer(ca)
		if err != nil {
			return fail(err)
		}
		intercepted := faults.New(nil, store, s.recorder, events.TierIntercepted, gate)
		intercepted.CaptureTo(captures)
		interceptor = forward.NewInterceptor(issuer, intercepted, s.recorder, nil)
	}

	s.proxy = forward.NewServer(pipeline, faults.NewDialer(store, s.recorder, gate), interceptor, bypass, nil)
	s.api = admin.NewServer(store, scenarios, s.recorder, bypass, trustVars, nil)
	s.api.CapturesFrom(captures)
	// The gate is the only thing holding behavior state, so resetting a session
	// has to go through it or a spent first_n stays spent.
	s.api.Rearms(gate)

	// The agent interface is a client of the API like the UI and the CLI, and
	// in process it reaches it without going back out through the socket the
	// admin server is answering on.
	inProcess, err := faultmcp.InProcess(s.api)
	if err != nil {
		return fail(err)
	}
	s.api.MountMCP(faultmcp.Handler(inProcess, version))

	if cfg != nil {
		s.config = config.Watch(cfg, newReloader(cfg, store, scenarios, bypass, nil), nil)
		s.api.Persist(s.config)
	}

	if err := s.proxy.Start(proxyPort); err != nil {
		return fail(err)
	}
	// The API can only say how to attach a child once the proxy has a port.
	s.api.ProxiesAt(s.proxyURL(), ca != nil)
	if err := s.api.Start(adminPort); err != nil {
		return fail(err)
	}

	return s, nil
}

// adminURL is the address to open, and the one every command prints first.
func (s *stack) adminURL() string { return "http://" + s.api.Addr() }

// proxyURL is what HTTP_PROXY points at.
func (s *stack) proxyURL() string { return "http://" + s.proxy.Addr() }

// banner says where everything is listening and what will happen to HTTPS.
func (s *stack) banner(out io.Writer) error {
	if _, err := fmt.Fprintf(out, "admin: %s\n", s.adminURL()); err != nil {
		return err
	}
	if s.config != nil {
		if _, err := fmt.Fprintf(out, "config: %s, watched for changes\n", s.config.Path()); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "proxy: %s\n", s.proxyURL()); err != nil {
		return err
	}
	tlsLine := "tls: passing HTTPS through, connection faults only (run faultline ca init to intercept)"
	if s.ca != nil {
		tlsLine = fmt.Sprintf("tls: intercepting HTTPS with CA %q", s.ca.CertPath)
	}
	if _, err := fmt.Fprintln(out, tlsLine); err != nil {
		return err
	}
	if patterns := s.bypass.Patterns(); len(patterns) > 0 {
		if _, err := fmt.Fprintf(out, "bypass: %s\n", strings.Join(patterns, ", ")); err != nil {
			return err
		}
	}
	for _, r := range s.routes {
		if _, err := fmt.Fprintf(out, "route %s: http://%s -> %s\n", r.Name, s.server.Addr(r.Name), r.Upstream); err != nil {
			return err
		}
	}
	return nil
}

// stop shuts every server down, giving in-flight requests until ctx expires.
// Proxies go first: the requests they are still serving produce the last
// events, and those should reach the streams before the admin server closes
// them. The recorder is closed last, once nothing can record any more.
// watch follows the configuration file until ctx is cancelled, if there is one.
func (s *stack) watch(ctx context.Context) {
	if s.config != nil {
		s.config.Start(ctx)
	}
}

func (s *stack) stop(ctx context.Context) error {
	var errs []error
	if s.config != nil {
		s.config.Stop()
	}
	if s.server != nil {
		errs = append(errs, s.server.Shutdown(ctx))
	}
	if s.proxy != nil {
		errs = append(errs, s.proxy.Shutdown(ctx))
	}
	if s.api != nil {
		errs = append(errs, s.api.Shutdown(ctx))
	}
	s.recorder.Close()
	return errors.Join(errs...)
}

// serve runs the stack until the process is interrupted.
func serve(ctx context.Context, out io.Writer, cfg *config.Config, adminPort, proxyPort int, routes []reverse.Route, ca *tlsmitm.CA, bypass *forward.Bypass) error {
	// Install the signal handler before anything is listening, so an interrupt
	// during startup shuts the parts that are already up down in order instead
	// of killing the process where it stands.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	s, err := start(cfg, adminPort, proxyPort, routes, ca, bypass, nil)
	if err != nil {
		return err
	}

	if err := s.banner(out); err != nil {
		return err
	}
	s.watch(ctx)

	<-ctx.Done()
	stop() // a second interrupt is the operator asking for the default, abrupt exit

	if _, err := fmt.Fprintln(out, "\nshutting down"); err != nil {
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	if err := s.stop(shutdownCtx); err != nil {
		return err
	}

	_, err = fmt.Fprintln(out, "shutdown complete")
	return err
}

// parseRoutes turns the --route and --route-port flags into routes. There may
// be none: the forward proxy runs either way. Routes with no port of their own
// are numbered from reverse.FirstPort, skipping any port already claimed, so
// ports stay predictable across runs.
// parseRouteSpecs reads the --route and --route-port flags. The ports nobody
// named are left at zero and handed out by assignPorts, once whatever the
// configuration file adds is known.
func parseRouteSpecs(routeSpecs, portSpecs []string) ([]reverse.Route, error) {
	routes := make([]reverse.Route, 0, len(routeSpecs))
	seen := make(map[string]bool, len(routeSpecs))

	for _, spec := range routeSpecs {
		name, raw, ok := strings.Cut(spec, "=")
		if !ok {
			return nil, fmt.Errorf("--route %q: want name=url", spec)
		}
		if name == "" {
			return nil, fmt.Errorf("--route %q: the name before = is empty", spec)
		}
		if raw == "" {
			return nil, fmt.Errorf("--route %q: the url after = is empty", spec)
		}
		if seen[name] {
			return nil, fmt.Errorf("--route %q: duplicate route name", spec)
		}
		seen[name] = true

		upstream, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("--route %q: %w", name, err)
		}
		if upstream.Scheme != "http" && upstream.Scheme != "https" {
			return nil, fmt.Errorf("--route %q: %q needs an http or https scheme", name, raw)
		}

		routes = append(routes, reverse.Route{Name: name, Upstream: upstream})
	}

	ports, err := parseRoutePorts(portSpecs, seen)
	if err != nil {
		return nil, err
	}
	for i, route := range routes {
		if port, ok := ports[route.Name]; ok {
			routes[i].Port = port
		}
	}

	return routes, nil
}

// parseRoutePorts reads the --route-port flags, which may only name routes that
// were actually declared.
func parseRoutePorts(portSpecs []string, known map[string]bool) (map[string]int, error) {
	ports := make(map[string]int, len(portSpecs))

	for _, spec := range portSpecs {
		name, raw, ok := strings.Cut(spec, "=")
		if !ok {
			return nil, fmt.Errorf("--route-port %q: want name=port", spec)
		}
		if !known[name] {
			return nil, fmt.Errorf("--route-port %q: there is no --route named %q", spec, name)
		}
		port, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("--route-port %q: port %q is not a number", spec, raw)
		}
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("--route-port %q: port %d is outside 1-65535", spec, port)
		}
		ports[name] = port
	}

	return ports, nil
}

// assignPorts gives every route its explicit port, then fills the gaps from
// reverse.FirstPort upwards without reusing one.
// assignPorts gives every route that named no port one from 9100 up, leaving
// the ports already spoken for alone.
func assignPorts(routes []reverse.Route) {
	taken := make(map[int]bool, len(routes))
	for _, r := range routes {
		if r.Port != 0 {
			taken[r.Port] = true
		}
	}

	next := reverse.FirstPort
	for i, r := range routes {
		if r.Port != 0 {
			continue
		}
		for taken[next] {
			next++
		}
		routes[i].Port = next
		taken[next] = true
	}
}
