package config

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/josipmusa/faultline/internal/faults"
)

// publishedSchema is the copy editors and other tools read. It is generated,
// and `make schema` writes it.
const publishedSchema = "../../schema/faultline.schema.json"

var update = flag.Bool("update", false, "write the generated JSON Schema to "+publishedSchema)

func TestPublishedSchemaIsUpToDate(t *testing.T) {
	got, err := JSONSchema()
	if err != nil {
		t.Fatalf("JSONSchema: %v", err)
	}

	if *update {
		if err := os.WriteFile(publishedSchema, got, 0o644); err != nil { //nolint:gosec // a published schema is world readable
			t.Fatalf("writing %s: %v", publishedSchema, err)
		}
	}

	want, err := os.ReadFile(publishedSchema)
	if err != nil {
		t.Fatalf("reading %s: %v", publishedSchema, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s does not match the catalogue; run `make schema` to write it", publishedSchema)
	}
}

func TestJSONSchemaDescribesEveryFaultAndBehavior(t *testing.T) {
	raw, err := JSONSchema()
	if err != nil {
		t.Fatalf("JSONSchema: %v", err)
	}

	var doc struct {
		Defs map[string]struct {
			OneOf []struct {
				Title      string                     `json:"title"`
				Required   []string                   `json:"required"`
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"oneOf"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the schema is not valid JSON: %v", err)
	}

	for _, tt := range []struct {
		def   string
		names []string
	}{
		{"fault", faults.Names()},
		{"behavior", faults.BehaviorNames()},
	} {
		titles := map[string]int{}
		for _, variant := range doc.Defs[tt.def].OneOf {
			titles[variant.Title] = len(variant.Properties)
		}
		for _, name := range tt.names {
			if _, ok := titles[name]; !ok {
				t.Errorf("$defs.%s has no branch for %q", tt.def, name)
			}
		}
		if len(titles) != len(tt.names) {
			t.Errorf("$defs.%s has %d branches, want %d", tt.def, len(titles), len(tt.names))
		}
	}

	// Spot check one branch end to end: the shape of a parameter, its bounds,
	// and which ones a user has to write.
	for _, variant := range doc.Defs["fault"].OneOf {
		if variant.Title != "delay" {
			continue
		}
		var ms struct {
			Type    string `json:"type"`
			Minimum int    `json:"minimum"`
		}
		if err := json.Unmarshal(variant.Properties["ms"], &ms); err != nil {
			t.Fatalf("delay.ms: %v", err)
		}
		if ms.Type != "integer" || ms.Minimum != 1 {
			t.Errorf("delay.ms = %+v, want a whole number of 1 or more", ms)
		}
		if len(variant.Required) != 2 || variant.Required[0] != "type" || variant.Required[1] != "ms" {
			t.Errorf("delay requires %v, want type and ms", variant.Required)
		}
	}
}

func TestJSONSchemaWritesParameterPairs(t *testing.T) {
	raw, err := JSONSchema()
	if err != nil {
		t.Fatalf("JSONSchema: %v", err)
	}

	var doc struct {
		Defs map[string]struct {
			OneOf []struct {
				Title string           `json:"title"`
				AllOf []map[string]any `json:"allOf"`
			} `json:"oneOf"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the schema is not valid JSON: %v", err)
	}

	branches := map[string]string{}
	for _, variant := range doc.Defs["fault"].OneOf {
		for _, pair := range variant.AllOf {
			for kind := range pair {
				branches[variant.Title] = kind
			}
		}
	}
	// truncate takes after_bytes or percent but not both; headers takes set or
	// remove and is happy with both.
	if branches["truncate"] != "oneOf" {
		t.Errorf("truncate pair = %q, want oneOf", branches["truncate"])
	}
	if branches["headers"] != "anyOf" {
		t.Errorf("headers pair = %q, want anyOf", branches["headers"])
	}
	if kind, ok := branches["delay"]; ok {
		t.Errorf("delay has a %s pair, want none", kind)
	}
}
