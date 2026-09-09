package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/josipmusa/faultline/internal/config"
)

// starterMode is the mode of a file meant to be committed and read by the
// whole team, which is the mode internal/config gives a file it writes itself.
const starterMode fs.FileMode = 0o644

func newInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init [file]",
		Short: "Write a starter faultline.yaml",
		Long: "Init writes a commented faultline.yaml into the working directory, holding one\n" +
			"example rule and one example scenario, both turned off. Edit the hosts to the\n" +
			"ones your application calls, then start it with `faultline run`.\n\n" +
			"A file that is already there is never overwritten: name another file, or say\n" +
			"--force to replace it.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := DefaultConfigFile
			if len(args) == 1 {
				path = args[0]
			}
			if err := writeStarter(path, force); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(),
				"wrote %s\n"+
					"Nothing in it is on yet. Edit the hosts to the ones your application calls,\n"+
					"then run it through Faultline:\n\n"+
					"  faultline run -- <the command you already use to start your app>\n",
				path)
			return err
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace the file when one is already there")

	return cmd
}

func writeStarter(path string, force bool) error {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return fmt.Errorf("%s is a directory; name the file to write, as in `faultline init %s`",
			path, filepath.Join(path, DefaultConfigFile))
	}

	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}

	// O_EXCL rather than a stat and a write: the refusal is the file system's,
	// so nothing can appear between the two.
	file, err := os.OpenFile(path, flags, starterMode) //nolint:gosec // a configuration file meant to be committed is not a secret
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s is already there, and init will not overwrite it; "+
			"write another file with `faultline init <file>`, or replace this one with --force", path)
	}
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}

	if _, err := file.WriteString(config.Starter); err != nil {
		_ = file.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
