package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const example = `
name: example
services:
  app:
    image: app
    environment:
      FOO: bar
  db:
    image: postgres
volumes:
  data:
`

func TestSourceListsItsServicesInOrder(t *testing.T) {
	src, err := ParseSource("compose.yaml", []byte(example))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}
	if got, want := strings.Join(src.ServiceNames(), ","), "app,db"; got != want {
		t.Errorf("ServiceNames() = %q, want %q", got, want)
	}
}

func TestSourceRejectsYAMLItCannotRead(t *testing.T) {
	if _, err := ParseSource("compose.yaml", []byte("services: [")); err == nil {
		t.Fatal("ParseSource of broken YAML succeeded, want an error")
	}
}

func TestSourceRejectsAFileWithNoServices(t *testing.T) {
	_, err := ParseSource("compose.yaml", []byte("name: example\n"))
	if err == nil || !strings.Contains(err.Error(), "no services") {
		t.Fatalf("ParseSource of a file with no services: %v, want it to say there are none", err)
	}
}

func TestSourceKnowsWhichServiceSetsAProxyVariable(t *testing.T) {
	src, err := ParseSource("compose.yaml", []byte(`
services:
  mapping:
    environment:
      HTTP_PROXY: http://elsewhere:3128
  list:
    environment:
      - HTTPS_PROXY=http://elsewhere:3128
  clean:
    environment:
      FOO: bar
`))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}
	for _, name := range []string{"mapping", "list"} {
		if !src.SetsProxy(name) {
			t.Errorf("SetsProxy(%q) = false, want true", name)
		}
	}
	if src.SetsProxy("clean") {
		t.Error("SetsProxy(\"clean\") = true, want false")
	}
}

func TestDiscoverPrefersTheNameDockerPrefers(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("services:\n  app: {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		got, err := Discover(dir)
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if got != filepath.Join(dir, want) {
			t.Fatalf("Discover() = %q, want %q", got, want)
		}
		if err := os.Remove(got); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDiscoverSaysWhatItLookedFor(t *testing.T) {
	_, err := Discover(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "compose.yaml") {
		t.Fatalf("Discover in an empty directory: %v, want it to name what it looked for", err)
	}
}
