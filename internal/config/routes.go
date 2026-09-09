package config

import (
	"net/url"

	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/proxy/reverse"
)

var routeKeys = []string{"name", "upstream", "port"}

// decodeRoutes reads the explicit routes: a name, an upstream, and optionally
// the local port to listen on. Names and ports have to be unique, since a name
// is how everything else refers to a route and a port can only be listened on
// once.
func decodeRoutes(c cursor) ([]reverse.Route, error) {
	items, err := c.sequence("routes")
	if err != nil {
		return nil, err
	}

	out := make([]reverse.Route, 0, len(items))
	names := make(map[string]bool, len(items))
	ports := make(map[int]bool, len(items))

	for _, item := range items {
		m, mapErr := item.mapping("a route")
		if mapErr != nil {
			return nil, mapErr
		}
		if only := m.only("a route", routeKeys...); only != nil {
			return nil, only
		}

		route, routeErr := decodeRoute(m)
		if routeErr != nil {
			return nil, routeErr
		}
		if names[route.Name] {
			return nil, m.locate("name").errf("duplicate route name %q", route.Name)
		}
		names[route.Name] = true
		if route.Port != 0 {
			if ports[route.Port] {
				return nil, m.locate("port").errf("duplicate route port %d", route.Port)
			}
			ports[route.Port] = true
		}
		out = append(out, route)
	}
	return out, nil
}

func decodeRoute(m *mapping) (reverse.Route, error) {
	nameAt, err := m.require("name", "a route name")
	if err != nil {
		return reverse.Route{}, err
	}
	name, err := nameAt.text("a route name")
	if err != nil {
		return reverse.Route{}, err
	}
	if name == "" {
		return reverse.Route{}, nameAt.errf("a route name is required")
	}

	upstreamAt, err := m.require("upstream", "a route upstream")
	if err != nil {
		return reverse.Route{}, err
	}
	raw, err := upstreamAt.text("upstream")
	if err != nil {
		return reverse.Route{}, err
	}
	upstream, parseErr := url.Parse(raw)
	if parseErr != nil {
		return reverse.Route{}, upstreamAt.errf("upstream %q is not a URL: %v", raw, parseErr)
	}
	if upstream.Scheme != "http" && upstream.Scheme != "https" || upstream.Host == "" {
		return reverse.Route{}, upstreamAt.errf("upstream %q needs an http or https scheme and a host, like https://api.stripe.com", raw)
	}

	route := reverse.Route{Name: name, Upstream: upstream}
	if portAt, ok := m.value("port"); ok {
		port, portErr := portAt.number("a route port")
		if portErr != nil {
			return reverse.Route{}, portErr
		}
		if port < 1 || port > 65535 {
			return reverse.Route{}, portAt.errf("port %d is outside 1-65535", port)
		}
		route.Port = port
	}
	return route, nil
}

// decodeBypass reads the hosts to pass through untouched. The forward proxy
// owns what an entry may look like, so each one is handed to its parser and
// only the line is added here.
func decodeBypass(c cursor) ([]string, error) {
	items, err := c.sequence("bypass")
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(items))
	for _, item := range items {
		host, textErr := item.text("a bypass entry")
		if textErr != nil {
			return nil, textErr
		}
		if _, parseErr := forward.NewBypass([]string{host}); parseErr != nil {
			return nil, item.errf("%v", parseErr)
		}
		out = append(out, host)
	}
	return out, nil
}
