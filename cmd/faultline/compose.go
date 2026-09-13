package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/josipmusa/faultline/internal/compose"
)

func newComposeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compose",
		Short: "Attach Faultline to a Docker Compose project",
	}
	cmd.AddCommand(newComposeInjectCmd())
	return cmd
}

func newComposeInjectCmd() *cobra.Command {
	var (
		source   string
		output   string
		image    string
		services []string
		force    bool
	)

	cmd := &cobra.Command{
		Use:   "inject",
		Short: "Write a Compose override attaching services to a Faultline companion",
		Long: "Inject reads the project's Compose file and writes an override that runs\n" +
			"Faultline beside it, points the named services at it, and gives them the CA\n" +
			"it signs with. The project's own file is never changed, and neither is any\n" +
			"application image: the two are brought up together.\n\n" +
			"  docker compose -f compose.yaml -f " + compose.DefaultOutput + " up",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := source
			if path == "" {
				dir, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("looking for a Compose file: %w", err)
				}
				if path, err = compose.Discover(dir); err != nil {
					return err
				}
				path = filepath.Base(path)
			}

			data, err := os.ReadFile(path) //nolint:gosec // the path is the operator's own Compose file
			if err != nil {
				return fmt.Errorf("reading %s: %w", path, err)
			}
			src, err := compose.ParseSource(path, data)
			if err != nil {
				return err
			}

			result, err := compose.Inject(src, compose.Options{
				Image:      image,
				Services:   services,
				SourcePath: path,
				OutputPath: output,
			})
			if err != nil {
				return err
			}
			if err := writeOverride(output, result.Text, force); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for _, warning := range result.Warnings {
				if err := printf(cmd.ErrOrStderr(), "warning: %s\n", warning); err != nil {
					return err
				}
			}
			return printf(out,
				"wrote %s\n\n"+
					"Bring the project up with both files, and the calls appear at http://localhost:9000:\n\n"+
					"  docker compose -f %s -f %s up\n\n"+
					"A JVM reads neither the proxy variables nor a PEM certificate, so a Java service\n"+
					"needs `faultline trust java` in front of its start command in its own image to\n"+
					"see faults on HTTPS. Every other runtime is attached by the override alone.\n"+
					"See docs/trust.md.\n",
				output, path, output)
		},
	}

	cmd.Flags().StringVarP(&source, "file", "f", "",
		"the project's Compose file (default: the one Compose itself would read)")
	cmd.Flags().StringVarP(&output, "output", "o", compose.DefaultOutput, "the override to write")
	cmd.Flags().StringVar(&image, "image", compose.DefaultImage, "the Faultline image to run")
	cmd.Flags().StringSliceVar(&services, "service", nil,
		"a service to attach to Faultline; repeat it for more than one")
	cmd.Flags().BoolVar(&force, "force", false, "replace the override when one is already there")

	return cmd
}

func writeOverride(path, text string, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	// O_EXCL rather than a stat and a write: the refusal is the file system's,
	// so nothing can appear between the two.
	file, err := os.OpenFile(path, flags, starterMode) //nolint:gosec // a Compose override is meant to be committed
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s is already there, and inject will not overwrite it; "+
			"write another file with -o <file>, or replace this one with --force", path)
	}
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	if _, err := file.WriteString(text); err != nil {
		_ = file.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
