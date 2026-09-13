package compose

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func inject(t *testing.T, source string, opts Options) Result {
	t.Helper()
	src, err := ParseSource("compose.yaml", []byte(source))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}
	got, err := Inject(src, opts)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	return got
}

func TestTheOverrideAddsTheCompanionAndAttachesTheNamedService(t *testing.T) {
	got := inject(t, example, Options{Image: "faultline:test", Services: []string{"app"}})

	var file struct {
		Services map[string]struct {
			Image       string            `yaml:"image"`
			Command     []string          `yaml:"command"`
			Environment map[string]string `yaml:"environment"`
			Volumes     []string          `yaml:"volumes"`
			DependsOn   map[string]struct {
				Condition string `yaml:"condition"`
			} `yaml:"depends_on"`
		} `yaml:"services"`
		Volumes map[string]yaml.Node `yaml:"volumes"`
	}
	if err := yaml.Unmarshal([]byte(got.Text), &file); err != nil {
		t.Fatalf("the override is not YAML: %v\n%s", err, got.Text)
	}

	ca, ok := file.Services["faultline-ca"]
	if !ok {
		t.Fatal("the override has no faultline-ca service")
	}
	if ca.Image != "faultline:test" {
		t.Errorf("faultline-ca image = %q, want the image asked for", ca.Image)
	}
	if strings.Join(ca.Command, " ") != "ca init" {
		t.Errorf("faultline-ca command = %q, want `ca init`", ca.Command)
	}

	proxy := file.Services["faultline"]
	if strings.Join(proxy.Command, " ") != "serve --bind 0.0.0.0" {
		t.Errorf("faultline command = %q, want it to bind every interface", proxy.Command)
	}

	app, ok := file.Services["app"]
	if !ok {
		t.Fatal("the override does not mention the named service")
	}
	for key, want := range map[string]string{
		"HTTP_PROXY":          "http://faultline:9001",
		"HTTPS_PROXY":         "http://faultline:9001",
		"SSL_CERT_FILE":       CACertPath,
		"NODE_EXTRA_CA_CERTS": CACertPath,
		"REQUESTS_CA_BUNDLE":  CACertPath,
		"CURL_CA_BUNDLE":      CACertPath,
	} {
		if app.Environment[key] != want {
			t.Errorf("app %s = %q, want %q", key, app.Environment[key], want)
		}
	}
	if app.Environment["NO_PROXY"] == "" {
		t.Error("app has no NO_PROXY, want localhost left alone")
	}
	if len(app.Volumes) != 1 || !strings.HasSuffix(app.Volumes[0], ":ro") {
		t.Errorf("app volumes = %v, want the CA mounted read-only", app.Volumes)
	}
	if app.DependsOn["faultline-ca"].Condition != "service_completed_successfully" {
		t.Errorf("app depends_on faultline-ca = %+v, want it to wait for the CA",
			app.DependsOn["faultline-ca"])
	}
	if _, ok := app.DependsOn["faultline"]; !ok {
		t.Error("app does not depend on faultline")
	}
	if _, ok := file.Volumes["faultline-ca"]; !ok {
		t.Error("the override declares no faultline-ca volume")
	}
	if file.Services["db"].Image != "" {
		t.Error("the override mentions a service that was not named")
	}
}

func TestTheOverrideAttachesEveryNamedService(t *testing.T) {
	got := inject(t, example, Options{Image: "i", Services: []string{"app", "db"}})
	for _, name := range []string{"app", "db"} {
		if !strings.Contains(got.Text, "\n  "+name+":\n") {
			t.Errorf("the override does not attach %q:\n%s", name, got.Text)
		}
	}
}

func TestInjectWarnsWhenAServiceAlreadyHasAProxy(t *testing.T) {
	got := inject(t, `
services:
  app:
    environment:
      HTTP_PROXY: http://elsewhere:3128
`, Options{Image: "i", Services: []string{"app"}})

	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "HTTP_PROXY") {
		t.Errorf("warnings = %q, want one about the proxy it already sets", got.Warnings)
	}
}

func TestInjectRefusesWhatItCannotWrite(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		services []string
		want     string
	}{
		{
			name:     "a service that is not there",
			source:   example,
			services: []string{"web"},
			want:     `"web"`,
		},
		{
			name:     "no service named at all",
			source:   example,
			services: nil,
			want:     "--service",
		},
		{
			name:     "a service called faultline already",
			source:   "services:\n  app: {}\n  faultline: {}\n",
			services: []string{"app"},
			want:     "faultline",
		},
		{
			name:     "a volume called faultline-ca already",
			source:   "services:\n  app: {}\nvolumes:\n  faultline-ca: {}\n",
			services: []string{"app"},
			want:     "faultline-ca",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := ParseSource("compose.yaml", []byte(tt.source))
			if err != nil {
				t.Fatalf("ParseSource: %v", err)
			}
			_, err = Inject(src, Options{Image: "i", Services: tt.services})
			if err == nil {
				t.Fatal("Inject succeeded, want it to refuse")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestAnUnknownServiceIsToldWhatIsThere(t *testing.T) {
	src, err := ParseSource("compose.yaml", []byte(example))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}
	_, err = Inject(src, Options{Image: "i", Services: []string{"web"}})
	if err == nil || !strings.Contains(err.Error(), "app, db") {
		t.Errorf("error %v, want it to list the services that are there", err)
	}
}

func TestTheOverrideDefaultsToThePublishedImage(t *testing.T) {
	got := inject(t, example, Options{Services: []string{"app"}})
	if !strings.Contains(got.Text, DefaultImage) {
		t.Errorf("the override does not name %q:\n%s", DefaultImage, got.Text)
	}
}
