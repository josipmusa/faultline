package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/config"
	"github.com/josipmusa/faultline/internal/rules"
)

func newSaveCmd() *cobra.Command {
	var admin string

	cmd := &cobra.Command{
		Use:   "save [file]",
		Short: "Write the running Faultline's rules and scenarios to a faultline.yaml",
		Long: "Save writes the rules, scenarios and bypass list of a Faultline that is\n" +
			"holding them in memory to a configuration file, so what was arranged in the\n" +
			"UI, on the command line or by an agent is still there tomorrow. The file is\n" +
			DefaultConfigFile + " in the working directory unless another is named.\n\n" +
			"The running instance keeps its rules in memory: it was started without the\n" +
			"file, so it does not start writing to it now. The next `faultline run` or\n" +
			"`faultline serve` in this directory reads the file, and from then on every\n" +
			"change is written back to it as it happens.\n\n" +
			"A file that is already there is never overwritten, and an instance that is\n" +
			"already writing to a file has nothing to save.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.New(admin)
			if err != nil {
				return fmt.Errorf("--admin: %w", err)
			}
			path := DefaultConfigFile
			if len(args) == 1 {
				path = args[0]
			}
			return saveRunning(cmd.Context(), cmd.OutOrStdout(), c, path)
		},
	}
	cmd.Flags().StringVar(&admin, "admin", client.DefaultAddr, adminFlagHelp)

	return cmd
}

// saveRunning writes what the instance at c holds to path, refusing to write
// over a file that is there or to save an instance that saves itself.
func saveRunning(ctx context.Context, out io.Writer, c *client.Client, path string) error {
	cfg, err := c.Config(ctx)
	if err != nil {
		return err
	}
	if cfg.Persisted {
		return fmt.Errorf("the Faultline at %s is already writing its changes to %s, so there is nothing to save",
			c.Addr(), cfg.Path)
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s is already there, and the running Faultline is not using it, so save will not "+
			"write over it; name another file with `faultline save <file>`, or start Faultline with the file "+
			"so changes are written to it as they happen", path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("checking %s: %w", path, err)
	}
	if dir := filepath.Dir(path); !isDir(dir) {
		return fmt.Errorf("there is no directory %s to write %s in", dir, filepath.Base(path))
	}

	rs, err := c.Rules(ctx)
	if err != nil {
		return err
	}
	listed, err := c.Scenarios(ctx)
	if err != nil {
		return err
	}
	scenarios := make([]rules.Scenario, 0, len(listed))
	for _, s := range listed {
		scenarios = append(scenarios, rules.Scenario{Name: s.Name, Rules: s.Rules})
	}
	if len(rs) == 0 && len(scenarios) == 0 && len(cfg.Bypass) == 0 {
		return fmt.Errorf("the Faultline at %s has no rules, no scenarios and no bypass list, so there is nothing to save; "+
			"`faultline init` writes a starter file", c.Addr())
	}

	if err := config.Save(path, rs, cfg.Bypass, scenarios); err != nil {
		return err
	}
	return printf(out, "wrote %s to %s\n"+
		"The running Faultline still holds them in memory. The next `faultline run` or `faultline serve`\n"+
		"in this directory reads the file and writes every change back to it.\n",
		counted(len(rs), "rule")+" and "+counted(len(scenarios), "scenario"), path)
}

func counted(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
