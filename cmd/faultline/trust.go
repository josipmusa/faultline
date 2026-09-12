package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/josipmusa/faultline/internal/runner"
	"github.com/josipmusa/faultline/internal/tlsmitm"
)

// javaToolOptions is the one variable a JVM reads without being asked. It is
// spelled here as well as in the runner because this command sets it for a
// process Faultline does not otherwise touch.
const javaToolOptions = "JAVA_TOOL_OPTIONS"

func newTrustCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Prepare a runtime that is not a child of Faultline to trust the CA",
		Long: "`faultline run` gives its child everything it needs. A process Faultline\n" +
			"did not start - a service in its own container, next to Faultline in its\n" +
			"own - gets the proxy address and the CA from outside instead, and trust\n" +
			"prepares whatever that runtime needs to accept them.",
	}
	cmd.AddCommand(newTrustJavaCmd())
	return cmd
}

func newTrustJavaCmd() *cobra.Command {
	var caPath, outDir string

	cmd := &cobra.Command{
		Use:   "java [-- <command> [args...]]",
		Short: "Build a JDK trust store holding the Faultline CA and point a JVM at it",
		Long: "A JVM reads neither the proxy variables nor a PEM certificate, so a\n" +
			"container that has both still sends its calls direct and rejects the\n" +
			"certificates Faultline presents. This command bridges that: it copies\n" +
			"the JDK's own cacerts, adds the Faultline CA to the copy so the service\n" +
			"still trusts everything it trusted before, and renders JAVA_TOOL_OPTIONS\n" +
			"with that trust store and with the proxy variables already in the\n" +
			"environment written as the system properties a JVM does read.\n\n" +
			"With a command after --, it is run with that variable set, which makes\n" +
			"this the entrypoint of an image that needs no other change:\n" +
			"  ENTRYPOINT [\"faultline\", \"trust\", \"java\", \"--\", \"java\", \"-jar\", \"/app.jar\"]\n" +
			"With no command it prints the assignment instead, for a shell to read.\n\n" +
			"The certificate is the only half of the CA this needs, so mounting the\n" +
			"CA directory read-only is enough, and the trust store is written\n" +
			"elsewhere: to the temporary directory unless --out says otherwise.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := caCertPath(caPath)
			if err != nil {
				return err
			}
			store, err := runner.JavaTrustStore(outDir, path)
			if err != nil {
				return err
			}

			value := runner.JavaEnv(os.Getenv(javaToolOptions), proxyFromEnv(), noProxyFromEnv(), store)
			if len(args) == 0 {
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s=%s\n", javaToolOptions, value)
				return err
			}

			env := append(withoutVar(os.Environ(), javaToolOptions), javaToolOptions+"="+value)
			code, err := runner.Cmd{
				Args:   args,
				Env:    env,
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			}.Run(cmd.Context())
			if err != nil {
				return err
			}
			if code != 0 {
				return exitError(code)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&caPath, "ca", "",
		"the Faultline CA certificate to trust; by default the one in this machine's "+
			"Faultline directory, which in a container is wherever the CA volume is mounted")
	cmd.Flags().StringVar(&outDir, "out", os.TempDir(),
		"directory the trust store is written to. Not the CA directory by default: that "+
			"is usually mounted read-only, and a service has no business writing to it")
	// Everything after the command name belongs to the child, flags included.
	cmd.Flags().SetInterspersed(false)

	return cmd
}

// caCertPath settles which certificate to trust, and says where it looked
// when there is none: in a container that is a missing mount, not a missing
// `ca init`, and the path is what tells the two apart.
func caCertPath(flag string) (string, error) {
	path := flag
	if path == "" {
		dir, err := tlsmitm.DefaultDir()
		if err != nil {
			return "", err
		}
		path = tlsmitm.CertPath(dir)
	}
	if _, err := os.Stat(path); err != nil { // #nosec G703 -- the path is the operator's own
		return "", fmt.Errorf("no Faultline CA certificate at %s: %w; mount Faultline's "+
			"CA directory here or pass --ca, and create the CA with `faultline ca init`", path, err)
	}
	return path, nil
}

// proxyFromEnv is the forward proxy as the environment already names it. Both
// spellings of both variables are read because the compose file, the shell or
// the image may have set any of them, and HTTPS wins: it is the one that
// carries the traffic interception is for.
func proxyFromEnv() string {
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

// noProxyFromEnv reads the bypass list in NO_PROXY's spelling; javaOptions
// rewrites it into Java's.
func noProxyFromEnv() []string {
	value := os.Getenv("NO_PROXY")
	if value == "" {
		value = os.Getenv("no_proxy")
	}
	if strings.TrimSpace(value) == "" {
		return nil
	}
	entries := strings.Split(value, ",")
	for i, entry := range entries {
		entries[i] = strings.TrimSpace(entry)
	}
	return entries
}

// withoutVar drops one variable from an environment, so the value this
// command computed replaces the image's rather than joining it twice.
func withoutVar(base []string, name string) []string {
	env := make([]string, 0, len(base))
	for _, kv := range base {
		if key, _, ok := strings.Cut(kv, "="); ok && key == name {
			continue
		}
		env = append(env, kv)
	}
	return env
}
