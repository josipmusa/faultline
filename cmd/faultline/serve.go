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
	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/faults"
	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/proxy/reverse"
	"github.com/josipmusa/faultline/internal/rules"
	"github.com/josipmusa/faultline/internal/tlsmitm"
)

// shutdownTimeout is how long in-flight requests get to finish after Ctrl-C.
const shutdownTimeout = 10 * time.Second

func newServeCmd() *cobra.Command {
	var routeSpecs, portSpecs, bypassSpecs []string
	var proxyPort int
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
			"no interception. localhost is always on it, so Faultline never proxies itself.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			routes, err := parseRoutes(routeSpecs, portSpecs)
			if err != nil {
				return err
			}
			bypass, err := bypassList(bypassSpecs)
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
			return serve(cmd.Context(), cmd.OutOrStdout(), admin.DefaultPort, proxyPort, routes, ca, bypass)
		},
	}

	cmd.Flags().StringArrayVar(&routeSpecs, "route", nil,
		"explicit route as name=url, repeatable (--route stripe=https://api.stripe.com)")
	cmd.Flags().StringArrayVar(&portSpecs, "route-port", nil,
		"local port for a route as name=port, repeatable (--route-port stripe=9100)")
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

// serve runs the forward proxy and every route until the process is interrupted.
// With a CA, CONNECT tunnels are intercepted; without one they are tunneled
// blindly. Hosts on the bypass list, which may be nil, skip the proxy's
// pipeline altogether.
func serve(ctx context.Context, out io.Writer, adminPort, proxyPort int, routes []reverse.Route, ca *tlsmitm.CA, bypass *forward.Bypass) error {
	// Install the signal handler before anything is listening, so an interrupt
	// during startup shuts the parts that are already up down in order instead
	// of killing the process where it stands.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	store := rules.New()
	recorder := events.NewRecorder(events.DefaultSize)
	defer recorder.Close()

	pipeline := faults.New(nil, store, recorder, events.TierPlain)

	// Routes are optional now that the forward proxy is always there: setting
	// HTTP_PROXY is reason enough to run Faultline.
	var server *reverse.Server
	if len(routes) > 0 {
		var err error
		server, err = reverse.NewServer(routes, pipeline, nil)
		if err != nil {
			return err
		}
		if err := server.Start(); err != nil {
			return err
		}
	}

	var interceptor *forward.Interceptor
	if ca != nil {
		issuer, err := tlsmitm.NewIssuer(ca)
		if err != nil {
			return err
		}
		interceptor = forward.NewInterceptor(issuer, faults.New(nil, store, recorder, events.TierIntercepted), recorder, nil)
	}

	proxy := forward.NewServer(pipeline, faults.NewDialer(store, recorder), interceptor, bypass, nil)
	api := admin.NewServer(store, recorder, bypass, nil)

	stopAll := func() {
		if server != nil {
			_ = server.Shutdown(context.Background())
		}
		_ = proxy.Shutdown(context.Background())
		_ = api.Shutdown(context.Background())
	}

	if err := proxy.Start(proxyPort); err != nil {
		stopAll()
		return err
	}
	if err := api.Start(adminPort); err != nil {
		stopAll()
		return err
	}

	if _, err := fmt.Fprintf(out, "admin: http://%s\n", api.Addr()); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "proxy: http://%s\n", proxy.Addr()); err != nil {
		return err
	}
	tlsLine := "tls: passing HTTPS through, connection faults only (run faultline ca init to intercept)"
	if ca != nil {
		tlsLine = fmt.Sprintf("tls: intercepting HTTPS with CA %q", ca.CertPath)
	}
	if _, err := fmt.Fprintln(out, tlsLine); err != nil {
		return err
	}
	if patterns := bypass.Patterns(); len(patterns) > 0 {
		if _, err := fmt.Fprintf(out, "bypass: %s\n", strings.Join(patterns, ", ")); err != nil {
			return err
		}
	}
	for _, r := range routes {
		if _, err := fmt.Fprintf(out, "route %s: http://%s -> %s\n", r.Name, server.Addr(r.Name), r.Upstream); err != nil {
			return err
		}
	}

	// Signal handling belongs to the serve command for now; the graceful
	// shutdown task takes it over once there is more than one server to stop.
	<-ctx.Done()
	stop() // a second interrupt is the operator asking for the default, abrupt exit

	if _, err := fmt.Fprintln(out, "\nshutting down"); err != nil {
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	// Proxies first: the requests they are still serving produce the last
	// events, and those should reach the streams before the admin server
	// closes them.
	var routeErr error
	if server != nil {
		routeErr = server.Shutdown(shutdownCtx)
	}
	if err := errors.Join(routeErr, proxy.Shutdown(shutdownCtx), api.Shutdown(shutdownCtx)); err != nil {
		return err
	}

	_, err := fmt.Fprintln(out, "shutdown complete")
	return err
}

// parseRoutes turns the --route and --route-port flags into routes. There may
// be none: the forward proxy runs either way. Routes with no port of their own
// are numbered from reverse.FirstPort, skipping any port already claimed, so
// ports stay predictable across runs.
func parseRoutes(routeSpecs, portSpecs []string) ([]reverse.Route, error) {
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
	assignPorts(routes, ports)

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
func assignPorts(routes []reverse.Route, explicit map[string]int) {
	taken := make(map[int]bool, len(routes))
	for _, port := range explicit {
		taken[port] = true
	}

	next := reverse.FirstPort
	for i, r := range routes {
		if port, ok := explicit[r.Name]; ok {
			routes[i].Port = port
			continue
		}
		for taken[next] {
			next++
		}
		routes[i].Port = next
		taken[next] = true
	}
}
