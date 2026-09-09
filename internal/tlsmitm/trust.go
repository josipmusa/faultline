package tlsmitm

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

// Plan describes how to make the operating system trust the CA: instructions
// a person can follow, and the exact commands Faultline can run on their
// behalf once they confirm. Commands is nil when Faultline cannot do it for
// them on this system.
type Plan struct {
	Instructions string
	Commands     [][]string
}

// Runner executes one command with the given argv, attached to the user's
// terminal so tools like sudo can prompt.
type Runner func(ctx context.Context, argv []string) error

// TrustPlan builds the plan for the given OS. lookPath is used to find the
// Linux trust tooling, since every distribution family ships a different one.
func TrustPlan(goos string, lookPath func(string) (string, error), certPath string) Plan {
	shown := shellQuote(certPath)
	switch goos {
	case "darwin":
		return Plan{
			Instructions: "macOS keeps trusted roots in the System Keychain. Adding the Faultline CA there\n" +
				"makes Safari, curl, Go, Python and most other clients trust intercepted hosts:\n\n" +
				"    sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain " + shown + "\n\n" +
				"Firefox, Java and Node keep their own trust stores; `faultline run` handles those per runtime.",
			Commands: [][]string{{"sudo", "security", "add-trusted-cert", "-d", "-r", "trustRoot",
				"-k", "/Library/Keychains/System.keychain", certPath}},
		}
	case "linux":
		return linuxPlan(lookPath, certPath)
	case "windows":
		return Plan{
			Instructions: "Windows keeps trusted roots in the certificate store. From an elevated prompt run:\n\n" +
				"    certutil -addstore -f ROOT " + shown + "\n\n" +
				"Firefox, Java and Node keep their own trust stores; `faultline run` handles those per runtime.",
		}
	default:
		return Plan{
			Instructions: "Faultline does not know how to trust a CA on " + goos + ".\n" +
				"Add this certificate to the system trust store by hand:\n\n    " + shown,
		}
	}
}

// linuxPlan picks the trust tooling that is actually installed.
func linuxPlan(lookPath func(string) (string, error), certPath string) Plan {
	shown := shellQuote(certPath)
	footer := "\n\nFirefox, Java and Node keep their own trust stores; `faultline run` handles those per runtime."

	if _, err := lookPath("update-ca-certificates"); err == nil {
		dest := "/usr/local/share/ca-certificates/faultline.crt"
		return Plan{
			Instructions: "This system uses update-ca-certificates (Debian, Ubuntu and relatives):\n\n" +
				"    sudo cp " + shown + " " + dest + "\n" +
				"    sudo update-ca-certificates" + footer,
			Commands: [][]string{{"sudo", "cp", certPath, dest}, {"sudo", "update-ca-certificates"}},
		}
	}
	if _, err := lookPath("update-ca-trust"); err == nil {
		dest := "/etc/pki/ca-trust/source/anchors/faultline.crt"
		return Plan{
			Instructions: "This system uses update-ca-trust (Fedora, RHEL and relatives):\n\n" +
				"    sudo cp " + shown + " " + dest + "\n" +
				"    sudo update-ca-trust" + footer,
			Commands: [][]string{{"sudo", "cp", certPath, dest}, {"sudo", "update-ca-trust"}},
		}
	}
	if _, err := lookPath("trust"); err == nil {
		return Plan{
			Instructions: "This system uses p11-kit's trust tool (Arch and relatives):\n\n" +
				"    sudo trust anchor --store " + shown + footer,
			Commands: [][]string{{"sudo", "trust", "anchor", "--store", certPath}},
		}
	}
	return Plan{
		Instructions: "No known trust tooling (update-ca-certificates, update-ca-trust, trust) is on the PATH.\n" +
			"Consult your distribution's documentation for adding a CA certificate to the\n" +
			"system trust store. The certificate is at:\n\n    " + shown + footer,
	}
}

// Install prints the plan's instructions and, when the plan has commands,
// asks for confirmation on in before running them with run. Anything but an
// explicit yes, including a closed stdin, leaves the trust store untouched.
func Install(ctx context.Context, plan Plan, in io.Reader, out io.Writer, run Runner) error {
	if _, err := fmt.Fprintln(out, plan.Instructions); err != nil {
		return err
	}
	if plan.Commands == nil {
		return nil
	}

	if _, err := fmt.Fprint(out, "\nRun the command(s) above now? They change the system trust store. [y/N] "); err != nil {
		return err
	}
	if !confirmed(in) {
		_, err := fmt.Fprintln(out, "Left the trust store untouched.")
		return err
	}

	for _, argv := range plan.Commands {
		if err := run(ctx, argv); err != nil {
			return fmt.Errorf("running %s: %w", strings.Join(argv, " "), err)
		}
	}
	_, err := fmt.Fprintln(out, "The Faultline CA is now trusted by the system.")
	return err
}

// confirmed reads one line and reports whether it is an explicit yes.
func confirmed(in io.Reader) bool {
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// shellQuote wraps a path in double quotes when it contains whitespace, so the
// printed instructions can be pasted into a shell as they are. macOS puts the
// config dir under "Application Support", which makes this the common case.
func shellQuote(path string) string {
	if strings.ContainsAny(path, " \t") {
		return `"` + path + `"`
	}
	return path
}
