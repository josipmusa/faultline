package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/config"
	"github.com/josipmusa/faultline/internal/rules"
)

// session is one observed run: the scenario it rehearses, if any, and where
// its report goes when it ends. Every `faultline run` is a session, because a
// report of what the application actually did is worth having whether or not
// a scenario was on.
type session struct {
	scenario string
	report   string
}

// reportFormats are the words someone reaches for when they read --report as a
// format rather than a destination. The report is always JSON, so the flag has
// nothing to choose and takes the file to write instead; a bare format word is
// almost certainly not the file anyone meant.
var reportFormats = []string{"json", "table", "text", "yaml", "ndjson"}

// newSession validates the run's session flags against the configuration the
// run is about to use. A scenario the file does not declare is refused here,
// before anything is started, rather than being noticed once the child has
// already finished.
func newSession(cfg *config.Config, scenario, report string) (session, error) {
	if err := checkScenario(cfg, scenario); err != nil {
		return session{}, err
	}
	if err := checkReportPath(report); err != nil {
		return session{}, err
	}
	return session{scenario: scenario, report: report}, nil
}

func checkScenario(cfg *config.Config, name string) error {
	switch {
	case name == "":
		return nil
	case cfg == nil:
		return fmt.Errorf("--scenario %q: scenarios are declared in a configuration file and there is none here; "+
			"write a %s or point --config at one", name, DefaultConfigFile)
	case slices.ContainsFunc(cfg.Scenarios, func(s rules.Scenario) bool { return s.Name == name }):
		return nil
	}
	return fmt.Errorf("--scenario %q: %s declares no scenario by that name%s", name, cfg.Path, declared(cfg.Scenarios))
}

func declared(list []rules.Scenario) string {
	if len(list) == 0 {
		return ", and none at all"
	}
	names := make([]string, 0, len(list))
	for _, s := range list {
		names = append(names, s.Name)
	}
	return "; it has " + strings.Join(names, ", ")
}

func checkReportPath(path string) error {
	if path == "" {
		return nil
	}
	if slices.Contains(reportFormats, strings.ToLower(path)) {
		return fmt.Errorf("--report %q: --report takes the file to write, not a format; "+
			"the report is written as JSON, so pass a path like --report report.json", path)
	}
	if dir := filepath.Dir(path); !isDir(dir) {
		return fmt.Errorf("--report %q: there is no directory %s to write it in", path, dir)
	}
	return nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// activate turns the session's scenario on before the child starts, so the
// child's very first call already meets it. Activating also starts the rules'
// behavior state over, which is what makes the report that follows a report of
// this run and not of everything since Faultline booted.
func (ss session) activate(ctx context.Context, c *client.Client, errOut io.Writer) error {
	if ss.scenario == "" {
		return nil
	}
	if _, err := c.SetScenarioActive(ctx, ss.scenario, true); err != nil {
		return fmt.Errorf("--scenario %q: %w", ss.scenario, err)
	}
	return printf(errOut, "scenario: %s active for this run\n", ss.scenario)
}

// finish turns the scenario off again and reports what the session saw. It
// runs whether the child succeeded, failed, or was interrupted. What it
// returns never decides the exit code: that belongs to the child.
func (ss session) finish(ctx context.Context, c *client.Client, errOut io.Writer) error {
	var errs []error

	if ss.scenario != "" {
		if _, err := c.SetScenarioActive(ctx, ss.scenario, false); err != nil {
			errs = append(errs, fmt.Errorf("turning the scenario %q off again: %w", ss.scenario, err))
		}
	}

	report, err := c.Report(ctx)
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("reading the session report: %w", err))...)
	}
	if err := writeReportTable(errOut, report); err != nil {
		errs = append(errs, err)
	}

	if ss.report != "" {
		if err := writeReportFile(ss.report, report); err != nil {
			errs = append(errs, err)
		} else if err := printf(errOut, "report: wrote %s\n", ss.report); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

var reportColumns = []string{"REQUESTS", "FAULTED", "RETRIES", "MAX RETRY WAIT", "ABANDONED"}

// writeReportTable prints the report for a person. It goes to stderr, because
// stdout belongs to the child.
func writeReportTable(w io.Writer, r client.ReportResult) error {
	if err := printf(w, "\nreport: this run\n"); err != nil {
		return err
	}
	// A warning here is the difference between "the application coped" and
	// "the fault never applied", so it belongs beside the counts it explains.
	for _, warning := range r.Warnings {
		if err := printf(w, "warning: %s\n", warning); err != nil {
			return err
		}
	}
	row := []string{
		strconv.Itoa(r.Total),
		strconv.Itoa(r.Faulted),
		strconv.Itoa(r.Retries),
		strconv.FormatInt(r.MaxRetryWaitMS, 10) + "ms",
		strconv.Itoa(r.Abandoned),
	}
	return printTable(w, reportColumns, [][]string{row})
}

// writeReportFile writes the report as the API's own JSON, reporting a failure
// to close it: a report nobody can read is not a report that happened.
func writeReportFile(path string, r client.ReportResult) (err error) {
	file, err := os.Create(path) //nolint:gosec // the path is the operator's own argument
	if err != nil {
		return fmt.Errorf("--report: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("--report: %w", closeErr)
		}
	}()

	return printJSON(file, r)
}
