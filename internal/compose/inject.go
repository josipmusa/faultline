package compose

import (
	_ "embed"
	"fmt"
	"strings"
	"text/template"
)

const (
	// DefaultImage is the published Faultline image the override names unless
	// the caller asks for another.
	DefaultImage = "ghcr.io/josipmusa/faultline:latest"

	// DefaultOutput is the override `faultline compose inject` writes.
	DefaultOutput = "docker-compose.faultline.yml"

	// caDir is where the CA volume is mounted: the image sets XDG_CONFIG_HOME
	// to /var/lib, so this is where `ca init` inside it writes and where the
	// proxy reads. The application is only ever pointed at the certificate
	// file itself; nothing about Faultline's own directories reaches its
	// environment.
	caDir = "/var/lib/faultline"

	// CACertPath is the certificate inside the mounted volume, the one every
	// trust variable in the override points at.
	CACertPath = caDir + "/ca.crt"

	adminPort = 9000
	proxyPort = 9001

	// proxyService and caService are the names the override takes in the
	// project's namespace. A source that already uses one of them is a
	// conflict rather than something to shadow.
	proxyService = "faultline"
	caService    = "faultline-ca"
	caVolume     = "faultline-ca"

	// noProxy keeps a service's calls to itself off the proxy. Compose service
	// names are not in it: a call to another service in the project is traffic
	// worth seeing.
	noProxy = "localhost,127.0.0.1"
)

//go:embed override.yaml.tmpl
var overrideTemplate string

// Options are what the override is rendered with.
type Options struct {
	// Image is the Faultline image, defaulting to DefaultImage.
	Image string
	// Services are the services to attach, at least one.
	Services []string
	// SourcePath and OutputPath appear in the comment holding the command to
	// run. They default to what the CLI would use.
	SourcePath string
	OutputPath string
}

// Result is a rendered override and anything the caller should hear about it.
type Result struct {
	Text string
	// Warnings are about the source, not about the override: things the
	// override takes precedence over, which is worth saying out loud.
	Warnings []string
}

type templateData struct {
	Image      string
	SourcePath string
	OutputPath string
	ProxyURL   string
	NoProxy    string
	CADir      string
	CACertPath string
	AdminPort  int
	ProxyPort  int
	Services   []templateService
}

type templateService struct{ Name string }

var rendered = template.Must(template.New("override").Parse(overrideTemplate))

// Inject renders the override attaching the named services of src to a
// Faultline companion. It refuses a service that is not there and a source
// that already uses one of the names the override needs.
func Inject(src *Source, opts Options) (Result, error) {
	if len(opts.Services) == 0 {
		return Result{}, fmt.Errorf("name the service to attach with --service; %s declares %s",
			src.Path, strings.Join(src.ServiceNames(), ", "))
	}
	for _, name := range opts.Services {
		if !src.HasService(name) {
			return Result{}, fmt.Errorf("%s declares no service %q; it declares %s",
				src.Path, name, strings.Join(src.ServiceNames(), ", "))
		}
	}
	for _, name := range []string{proxyService, caService} {
		if src.HasService(name) {
			return Result{}, fmt.Errorf("%s already declares a service called %q, "+
				"which is the name the override needs; rename it, or write the override by hand",
				src.Path, name)
		}
	}
	if src.HasVolume(caVolume) {
		return Result{}, fmt.Errorf("%s already declares a volume called %q, "+
			"which is the name the override needs for the CA; rename it, "+
			"or write the override by hand", src.Path, caVolume)
	}

	data := templateData{
		Image:      firstNonEmpty(opts.Image, DefaultImage),
		SourcePath: firstNonEmpty(opts.SourcePath, src.Path),
		OutputPath: firstNonEmpty(opts.OutputPath, DefaultOutput),
		ProxyURL:   fmt.Sprintf("http://%s:%d", proxyService, proxyPort),
		NoProxy:    noProxy,
		CADir:      caDir,
		CACertPath: CACertPath,
		AdminPort:  adminPort,
		ProxyPort:  proxyPort,
	}
	var warnings []string
	for _, name := range opts.Services {
		data.Services = append(data.Services, templateService{Name: name})
		if src.SetsProxy(name) {
			warnings = append(warnings, fmt.Sprintf(
				"%s already sets HTTP_PROXY or HTTPS_PROXY in %s; the override replaces it "+
					"with Faultline's", name, src.Path))
		}
	}

	var out strings.Builder
	if err := rendered.Execute(&out, data); err != nil {
		return Result{}, fmt.Errorf("rendering the override: %w", err)
	}
	return Result{Text: out.String(), Warnings: warnings}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
