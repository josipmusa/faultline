package main

import (
	"fmt"
	"log/slog"
	"os"
	"slices"

	"github.com/josipmusa/faultline/internal/config"
	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/proxy/reverse"
	"github.com/josipmusa/faultline/internal/rules"
)

// DefaultConfigFile is the file Faultline picks up on its own when it is in the
// working directory.
const DefaultConfigFile = "faultline.yaml"

const configFlagHelp = "the configuration file to read and write; " +
	"left out, " + DefaultConfigFile + " in this directory is used when there is one (--config ../faultline.yaml)"

// loadConfig reads the configuration a command runs with. Running with no file
// at all is ordinary, so a missing faultline.yaml means "no file, keep the
// rules in memory"; being told to use a file that is not there is an error,
// because the person meant that file.
func loadConfig(path string) (*config.Config, error) {
	if path == "" {
		if !isFile(DefaultConfigFile) {
			return nil, nil
		}
		path = DefaultConfigFile
	}
	return config.Load(path)
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// routesFor merges the routes the file declares with the ones the flags do. A
// --route with the same name as a route in the file wins, because a flag is
// what the person typed just now. Ports are handed out over the whole set, so
// an automatic port never lands on one the file asked for.
func routesFor(cfg *config.Config, routeSpecs, portSpecs []string) ([]reverse.Route, error) {
	fromFlags, err := parseRouteSpecs(routeSpecs, portSpecs)
	if err != nil {
		return nil, err
	}

	var merged []reverse.Route
	if cfg != nil {
		merged = slices.Clone(cfg.Routes)
	}
	for _, route := range fromFlags {
		if i := slices.IndexFunc(merged, func(r reverse.Route) bool { return r.Name == route.Name }); i >= 0 {
			merged[i] = route
			continue
		}
		merged = append(merged, route)
	}

	assignPorts(merged)
	return merged, nil
}

// bypassFor is everything to leave alone: the entries Faultline always keeps,
// then the file's, then --bypass. A bypass list is a union, so nothing here has
// to win over anything else.
func bypassFor(cfg *config.Config, specs []string) (*forward.Bypass, error) {
	if cfg != nil {
		specs = append(slices.Clone(cfg.Bypass), specs...)
	}
	return bypassList(specs)
}

// reloader is the running Faultline seen from the configuration file: what an
// edit to the file is applied to, and where the rules written back to it come
// from.
//
// Rules and the bypass list are swapped whole. Routes are read once at
// startup, because a route is a listener and Faultline does not open or close
// those while it runs, so a change to them is reported and waits for a restart.
type reloader struct {
	store     *rules.Store
	scenarios *rules.Scenarios
	log       *slog.Logger

	routes []reverse.Route
	bypass *forward.Bypass
}

func newReloader(cfg *config.Config, store *rules.Store, scenarios *rules.Scenarios, bypass *forward.Bypass, log *slog.Logger) *reloader {
	if log == nil {
		log = slog.Default()
	}
	return &reloader{store: store, scenarios: scenarios, log: log, routes: cfg.Routes, bypass: bypass}
}

func (r *reloader) Apply(cfg *config.Config) error {
	if !sameRoutes(r.routes, cfg.Routes) {
		r.routes = cfg.Routes
		r.log.Warn("config: the routes changed, and a route is a listener: restart faultline to open it. " +
			"The rules in this save are in force already.")
	}
	// The bypass list is applied here, not reported: unlike a route it opens
	// nothing, so an edit can simply take effect. A list that does not parse
	// refuses the whole save, which leaves the rules alone as well and is the
	// point: the file is applied atomically or not at all.
	if r.bypass != nil && !slices.Equal(r.bypass.Configured(), cfg.Bypass) {
		if err := r.bypass.Replace(append(slices.Clone(forward.DefaultBypass), cfg.Bypass...)); err != nil {
			return fmt.Errorf("config: the bypass list: %w", err)
		}
		r.log.Info("config: the bypass list changed and is in force", "bypass", r.bypass.Configured())
	}
	r.store.Replace(cfg.Rules)

	// The scenarios come back from the file as well, so one added or renamed
	// there can be activated without a restart. The rules a scenario named are
	// left as the file just set them: it is the file's turn to say what is on.
	if dropped := r.scenarios.Replace(cfg.Scenarios); dropped != "" {
		r.log.Warn("config: the active scenario is no longer in the file, so nothing is active now. "+
			"The rules it turned on are whatever this save says they are.", "scenario", dropped)
	}
	return nil
}

func (r *reloader) Rules() []rules.Rule { return r.store.List() }

// Bypass is what the file should say the bypass list is: the entries somebody
// configured, without the defaults Faultline keeps for itself.
func (r *reloader) Bypass() []string { return r.bypass.Configured() }

func sameRoutes(a, b []reverse.Route) bool {
	return slices.EqualFunc(a, b, func(x, y reverse.Route) bool {
		return x.Name == y.Name && x.Port == y.Port && x.Upstream.String() == y.Upstream.String()
	})
}
