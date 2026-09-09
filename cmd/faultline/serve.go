package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
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
)

// shutdownTimeout is how long in-flight requests get to finish after Ctrl-C.
const shutdownTimeout = 10 * time.Second

func newServeCmd() *cobra.Command {
	var routeSpecs, portSpecs []string
	var proxyPort int

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the admin server and proxies",
		Long: "Serve starts the admin API, the forward proxy, and a listener for every\n" +
			"explicit route, so an application can send its traffic through Faultline\n" +
			"either by setting HTTP_PROXY or by pointing at a route's local port.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			routes, err := parseRoutes(routeSpecs, portSpecs)
			if err != nil {
				return err
			}
			return serve(cmd.Context(), cmd.OutOrStdout(), admin.DefaultPort, proxyPort, routes)
		},
	}

	cmd.Flags().StringArrayVar(&routeSpecs, "route", nil,
		"explicit route as name=url, repeatable (--route stripe=https://api.stripe.com)")
	cmd.Flags().StringArrayVar(&portSpecs, "route-port", nil,
		"local port for a route as name=port, repeatable (--route-port stripe=9100)")
	cmd.Flags().IntVar(&proxyPort, "proxy-port", forward.DefaultPort,
		"port for the forward proxy, the one HTTP_PROXY points at")

	return cmd
}

// serve runs the forward proxy and every route until the process is interrupted.
func serve(ctx context.Context, out io.Writer, adminPort, proxyPort int, routes []reverse.Route) error {
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

	proxy := forward.NewServer(pipeline, faults.NewDialer(store, recorder), nil)
	api := admin.NewServer(store, recorder, nil)

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
