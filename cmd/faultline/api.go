package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	client "github.com/josipmusa/faultline/clients/go"
)

// apiFlags are the two flags every command that talks to a running instance
// has: where it is, and whether the answer is for a person or a program.
type apiFlags struct {
	admin string
	json  bool
}

const adminFlagHelp = "address of the running Faultline (--admin http://localhost:9000)"

const jsonFlagHelp = "print the API's own JSON instead of a table"

// register puts the flags on a command group, so every subcommand under it
// takes them and takes them the same way.
func (f *apiFlags) register(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&f.admin, "admin", client.DefaultAddr, adminFlagHelp)
	cmd.PersistentFlags().BoolVar(&f.json, "json", false, jsonFlagHelp)
}

// client returns a client for the address the flag names. A bad address is the
// operator's typo, so the error names the flag.
func (f *apiFlags) client() (*client.Client, error) {
	c, err := client.New(f.admin)
	if err != nil {
		return nil, fmt.Errorf("--admin: %w", err)
	}
	return c, nil
}

// printJSON writes a value the way the API wrote it, indented so a person can
// still read it and a program can still parse it.
func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("printing json: %w", err)
	}
	return nil
}

// printLine writes one line of JSON, the shape an export or a tail is read in.
func printLine(w io.Writer, v any) error {
	encoded, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("printing json: %w", err)
	}
	if _, err := fmt.Fprintf(w, "%s\n", encoded); err != nil {
		return fmt.Errorf("printing json: %w", err)
	}
	return nil
}

// printTable writes an aligned table. An empty body prints empty, so a caller
// with nothing to show says so itself rather than printing a bare header.
func printTable(w io.Writer, header []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	if _, err := fmt.Fprintln(tw, strings.Join(header, "\t")); err != nil {
		return fmt.Errorf("printing table: %w", err)
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(tw, strings.Join(row, "\t")); err != nil {
			return fmt.Errorf("printing table: %w", err)
		}
	}

	if err := tw.Flush(); err != nil {
		return fmt.Errorf("printing table: %w", err)
	}
	return nil
}

func printf(w io.Writer, format string, args ...any) error {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return fmt.Errorf("printing: %w", err)
	}
	return nil
}

// yesNo renders a flag the way a table column wants it.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// orDash keeps a column occupied when there is nothing to put in it.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
