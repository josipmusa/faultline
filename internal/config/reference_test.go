package config

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/faults"
)

// publishedReference is the configuration reference readers get. It is
// generated from the same schema the editors read, and `make docs` writes it.
const publishedReference = "../../docs/reference.md"

func TestPublishedReferenceIsUpToDate(t *testing.T) {
	got, err := Reference()
	if err != nil {
		t.Fatalf("Reference: %v", err)
	}

	if *update {
		if err := os.WriteFile(publishedReference, got, 0o644); err != nil { //nolint:gosec // documentation is world readable
			t.Fatalf("writing %s: %v", publishedReference, err)
		}
	}

	want, err := os.ReadFile(publishedReference)
	if err != nil {
		t.Fatalf("reading %s: %v", publishedReference, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s does not match the catalogue; run `make docs` to write it", publishedReference)
	}
}

func TestReferenceDescribesEveryFaultAndBehavior(t *testing.T) {
	raw, err := Reference()
	if err != nil {
		t.Fatalf("Reference: %v", err)
	}
	text := string(raw)

	for _, name := range faults.Names() {
		if !strings.Contains(text, "\n### `"+name+"`\n") {
			t.Errorf("no heading for fault %q", name)
		}
	}
	for _, name := range faults.BehaviorNames() {
		if !strings.Contains(text, "\n### `"+name+"`\n") {
			t.Errorf("no heading for behavior %q", name)
		}
	}

	// Spot check one fault end to end: its tier, a parameter with its bound,
	// and that the required column is filled in.
	for _, want := range []string{
		"connection tier",
		"| `ms` | integer, 1 or more | yes | How long to hold the request back, in milliseconds |",
		"| `jitter_ms` | integer, 0 or more | no | Random extra delay on top, up to this many milliseconds |",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("reference lacks %q", want)
		}
	}

	// A parameter written in a pair says so, in words rather than as allOf.
	if !strings.Contains(text, "`after_bytes` or `percent`, not both") {
		t.Error("truncate's exclusive pair is not explained")
	}
	if !strings.Contains(text, "at least one of `remove` and `set`") {
		t.Error("headers' pair is not explained")
	}
}

func TestReferenceCoversTheTopLevelAndTheShapes(t *testing.T) {
	raw, err := Reference()
	if err != nil {
		t.Fatalf("Reference: %v", err)
	}
	text := string(raw)

	for _, want := range []string{
		"\n## Top level\n",
		"| `routes` |",
		"| `bypass` |",
		"| `rules` |",
		"| `scenarios` |",
		"\n## `route`\n",
		"\n## `rule`\n",
		"\n## `match`\n",
		"\n## `scenario`\n",
		"| `upstream` | string, matching `^https?://` | yes |",
		"| `port` | integer, 1 to 65535 | no |",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("reference lacks %q", want)
		}
	}
}
