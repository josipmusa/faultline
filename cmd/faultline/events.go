package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/admin"
)

const clockFormat = "15:04:05"

func newEventsCmd() *cobra.Command {
	var flags apiFlags

	cmd := &cobra.Command{
		Use:   "events",
		Short: "Watch and export what Faultline saw",
		Long: "Events is the record of every call that went through Faultline: what was\n" +
			"called, how it ended, and which rule if any changed the outcome.",
	}
	flags.register(cmd)
	cmd.AddCommand(newEventsTailCmd(&flags), newEventsExportCmd(&flags))
	return cmd
}

func newEventsTailCmd(flags *apiFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "tail",
		Short: "Follow events as they happen",
		Long: "Tail follows the live event stream until you interrupt it. The stream is\n" +
			"read-only: nothing you type reaches Faultline, and changing rules goes\n" +
			"through the other commands.\n\n" +
			"With --json every message is printed as the API sends it, one JSON object\n" +
			"per line, including the notice that says a cached rule list is stale.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := flags.client()
			if err != nil {
				return err
			}
			return tailEvents(cmd, c, flags.json)
		},
	}
}

func tailEvents(cmd *cobra.Command, c *client.Client, asJSON bool) error {
	// A tail runs until it is interrupted, so it takes the signal itself
	// rather than leaving the socket to be cut off mid-frame.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The header waits for the first message, so a tail that never connects
	// prints its error and nothing else.
	out, header := cmd.OutOrStdout(), !asJSON

	err := c.Stream(ctx, func(m client.Message) error {
		if header {
			header = false
			if err := printf(out, "%s\n", eventHeader()); err != nil {
				return err
			}
		}
		return writeMessage(out, asJSON, m)
	})
	switch {
	case errors.Is(err, context.Canceled):
		return nil // the operator asked for the end of it
	case err != nil:
		return err
	}
	return printf(cmd.ErrOrStderr(), "faultline closed the stream\n")
}

func writeMessage(w io.Writer, asJSON bool, m client.Message) error {
	if asJSON {
		return printLine(w, m)
	}
	switch {
	case m.Type == admin.MessageEvent && m.Event != nil:
		return printf(w, "%s\n", eventLine(*m.Event))
	case m.Type == admin.MessageRulesChanged:
		return printf(w, "-- rules changed\n")
	default:
		return nil // a message kind this build does not know is not an error
	}
}

func newEventsExportCmd(flags *apiFlags) *cobra.Command {
	var query client.EventQuery
	var faulted bool
	var path string

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Write the events Faultline is holding",
		Long: "Export writes what the ring buffer holds, oldest first, as JSON Lines:\n" +
			"one event object per line. That is the shape another tool wants - it\n" +
			"streams, it appends, and jq, grep and wc all read it a line at a time -\n" +
			"and it is the same object `events tail` prints.\n\n" +
			"With --json the API's own array is written instead, for a reader that\n" +
			"wants one document.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := flags.client()
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("faulted") {
				query.Faulted = &faulted
			}
			list, err := c.Events(cmd.Context(), query)
			if err != nil {
				return err
			}
			return exportEvents(cmd, flags.json, path, list)
		},
	}

	cmd.Flags().StringVar(&query.Host, "host", "", "only events for this upstream (--host api.stripe.com)")
	cmd.Flags().IntVar(&query.Limit, "limit", 0, "keep only the most recent N events")
	cmd.Flags().BoolVar(&faulted, "faulted", false,
		"only faulted events, or only unfaulted ones with --faulted=false")
	cmd.Flags().StringVarP(&path, "out", "o", "", "write to this file instead of standard output")

	return cmd
}

func exportEvents(cmd *cobra.Command, asJSON bool, path string, list []client.Event) error {
	if path == "" {
		return writeEvents(cmd.OutOrStdout(), asJSON, list)
	}
	if err := writeEventFile(path, asJSON, list); err != nil {
		return err
	}
	return printf(cmd.OutOrStdout(), "wrote %d events to %s\n", len(list), path)
}

// writeEventFile writes the export to a file, reporting a failure to close it:
// an export nobody can read is not an export that happened.
func writeEventFile(path string, asJSON bool, list []client.Event) (err error) {
	file, err := os.Create(path) //nolint:gosec // the path is the operator's own argument
	if err != nil {
		return fmt.Errorf("--out: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("--out: %w", closeErr)
		}
	}()

	return writeEvents(file, asJSON, list)
}

func writeEvents(w io.Writer, asJSON bool, list []client.Event) error {
	if asJSON {
		if list == nil {
			list = []client.Event{}
		}
		return printJSON(w, list)
	}
	for _, e := range list {
		if err := printLine(w, e); err != nil {
			return err
		}
	}
	return nil
}

// eventHeader and eventLine share their widths, because a tail prints its
// header once and then a line at a time, with no chance to align them later.
func eventHeader() string {
	return fmt.Sprintf(eventFormat, "TIME", "METHOD", "HOST", "PATH", "STATUS", "MS", "TIER", "RULE")
}

const eventFormat = "%-8s  %-6s  %-24s  %-28s  %6s  %7s  %-11s  %s"

func eventLine(e client.Event) string {
	status := "-"
	if e.Status > 0 {
		status = strconv.Itoa(e.Status)
	}
	return fmt.Sprintf(eventFormat,
		e.Timestamp.Local().Format(clockFormat),
		e.Method,
		e.Host,
		e.Path,
		status,
		strconv.FormatInt(e.DurationMS, 10),
		e.Tier,
		eventNote(e),
	)
}

// eventNote is the last column: which rule changed this call, and what went
// wrong when something did.
func eventNote(e client.Event) string {
	switch {
	case e.RuleID != "" && e.Error != "":
		return fmt.Sprintf("%s (%s)", e.RuleID, e.Error)
	case e.RuleID != "":
		return e.RuleID
	case e.Error != "":
		return e.Error
	default:
		return "-"
	}
}
