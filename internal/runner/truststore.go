package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrNoJDK says there is no JDK to build a trust store from. Most children
// are not JVMs, so it is not a failure the caller has to report: it means
// there is nothing to do.
var ErrNoJDK = errors.New("runner: no JDK found; set JAVA_HOME or put java on PATH")

// trustStoreAlias names the Faultline entry inside the trust store, so a
// rebuild replaces it rather than adding a second one.
const trustStoreAlias = "faultline"

// trustStoreName is the file Faultline builds beside the CA it was built for.
const trustStoreName = "java-truststore.p12"

// javaSettingsTimeout bounds the one `java -version` used to locate a JDK.
const javaSettingsTimeout = 10 * time.Second

// JavaTrustStore returns a JDK trust store that trusts the Faultline CA at
// caPath as well as everything the JDK already trusted, building it in dir
// the first time and whenever the CA or the JDK's own cacerts has changed
// since. The JVM cannot be handed an extra certificate the way Node and
// OpenSSL can, so the whole store is copied and added to rather than
// replaced: a child that trusts only Faultline would fail on every call to a
// dependency Faultline is passing through.
//
// caPath empty means Faultline is not intercepting, so the JDK's own store is
// still the right one and nothing is built. A machine without a JDK returns
// ErrNoJDK.
func JavaTrustStore(dir, caPath string) (string, error) {
	if caPath == "" {
		return "", nil
	}

	home, err := javaHome()
	if err != nil {
		return "", err
	}
	cacerts := filepath.Join(home, "lib", "security", "cacerts")
	store := filepath.Join(dir, trustStoreName)

	current, err := isCurrent(store, caPath, cacerts)
	if err != nil {
		return "", err
	}
	if current {
		return store, nil
	}
	if err := buildTrustStore(home, store, cacerts, caPath); err != nil {
		return "", err
	}
	return store, nil
}

// isCurrent reports whether an already built store still reflects both the
// sources it was built from.
func isCurrent(store string, sources ...string) (bool, error) {
	built, err := os.Stat(store)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat trust store %s: %w", store, err)
	}

	for _, source := range sources {
		info, err := os.Stat(source)
		if err != nil {
			return false, fmt.Errorf("stat %s: %w", source, err)
		}
		if info.ModTime().After(built.ModTime()) {
			return false, nil
		}
	}
	return true, nil
}

// buildTrustStore copies the JDK's cacerts and imports the Faultline CA into
// the copy with the JDK's own keytool, so no keystore format has to be
// written here. The copy is built under a temporary name and renamed into
// place, so a failure halfway through leaves no store a JVM would load.
func buildTrustStore(home, store, cacerts, caPath string) error {
	temp := store + ".tmp"
	defer func() { _ = os.Remove(temp) }()

	if err := copyFile(cacerts, temp); err != nil {
		return err
	}

	keytool := filepath.Join(home, "bin", executable("keytool"))
	out, err := exec.Command(keytool, // #nosec G204 -- the path is the located JDK's own keytool
		"-importcert", "-noprompt",
		"-alias", trustStoreAlias,
		"-file", caPath,
		"-keystore", temp,
		"-storepass", trustStorePassword,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("import %s into the trust store with keytool: %w: %s",
			caPath, err, strings.TrimSpace(string(out)))
	}

	if err := os.Rename(temp, store); err != nil {
		return fmt.Errorf("install trust store %s: %w", store, err)
	}
	return nil
}

func copyFile(from, to string) error {
	content, err := os.ReadFile(from) // #nosec G304 -- the JDK's own cacerts
	if err != nil {
		return fmt.Errorf("read %s: %w", from, err)
	}
	// #nosec G703 -- to is the trust store beside Faultline's own CA
	if err := os.WriteFile(to, content, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", to, err)
	}
	return nil
}

// javaHome locates a JDK: JAVA_HOME when it points at one, otherwise the java
// on PATH, asked where it lives. Asking rather than walking up from the
// binary is what makes this work on macOS, where /usr/bin/java is a stub that
// sits nowhere near the JDK it runs.
func javaHome() (string, error) {
	if home := os.Getenv("JAVA_HOME"); isJDK(home) {
		return home, nil
	}

	java, err := exec.LookPath("java")
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNoJDK, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), javaSettingsTimeout)
	defer cancel()
	// The settings go to stderr, alongside the version banner.
	// #nosec G204 -- java is what LookPath found on PATH
	out, err := exec.CommandContext(ctx, java, "-XshowSettings:properties", "-version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: ask %s where it lives: %w", ErrNoJDK, java, err)
	}
	if home := parseJavaHome(string(out)); isJDK(home) {
		return home, nil
	}
	return "", fmt.Errorf("%w: %s has no keytool or cacerts", ErrNoJDK, java)
}

// parseJavaHome picks java.home out of `java -XshowSettings:properties`.
func parseJavaHome(output string) string {
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "java.home" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// isJDK reports whether home holds both halves this needs: the trust store to
// copy and the tool that writes to it. A JRE has neither.
func isJDK(home string) bool {
	if home == "" {
		return false
	}
	for _, path := range []string{
		filepath.Join(home, "lib", "security", "cacerts"),
		filepath.Join(home, "bin", executable("keytool")),
	} {
		// #nosec G703 -- path is inside the JDK JAVA_HOME names
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}

func executable(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
