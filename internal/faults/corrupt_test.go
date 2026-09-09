package faults

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/rules"
)

func buildCorrupt(t *testing.T, params rules.Params) Applier {
	t.Helper()
	applier, err := Build(rules.Fault{Type: "corrupt", Params: params})
	if err != nil {
		t.Fatalf("building the corrupt fault: %v", err)
	}
	return applier
}

func TestCorruptChangesTheBodyWithoutChangingItsLength(t *testing.T) {
	body := strings.Repeat("abcdefgh", 50)

	resp := respondThrough(t, buildCorrupt(t, rules.Params{"percent": 100}), "garbled", body, nil)
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if len(got) != len(body) {
		t.Errorf("body is %d bytes, want %d: flipping bits keeps the length", len(got), len(body))
	}
	if string(got) == body {
		t.Error("the body came through untouched")
	}
	if resp.ContentLength != int64(len(body)) {
		t.Errorf("content length = %d, want %d", resp.ContentLength, len(body))
	}
}

func TestCorruptRuinsJSON(t *testing.T) {
	body := `{"slideshow":{"author":"Yours Truly","title":"Sample Slide Show"}}`

	resp := respondThrough(t, buildCorrupt(t, rules.Params{"percent": 100}), "garbled", body, nil)
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	var into any
	if err := json.Unmarshal(got, &into); err == nil {
		t.Errorf("the corrupted body %q still parses as JSON", got)
	}
}

func TestCorruptLeavesMostOfALowPercentageAlone(t *testing.T) {
	body := strings.Repeat("a", 5000)

	resp := respondThrough(t, buildCorrupt(t, rules.Params{"percent": 1}), "garbled", body, nil)
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	changed := 0
	for i := range got {
		if got[i] != body[i] {
			changed++
		}
	}
	if changed == 0 {
		t.Error("nothing was corrupted at all")
	}
	if changed > 250 {
		t.Errorf("%d of 5000 bytes changed, want roughly 1 percent", changed)
	}
}
