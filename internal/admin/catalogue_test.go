package admin

import (
	"net/http"
	"testing"

	"github.com/josipmusa/faultline/internal/faults"
)

func TestCatalogueListsEveryFaultAndBehavior(t *testing.T) {
	s := newTestServer(t)

	w := do(t, s, http.MethodGet, "/api/catalogue", "")
	wantStatus(t, w, http.StatusOK)
	got := decodeBody[catalogue](t, w)

	if len(got.Faults) != len(faults.Names()) {
		t.Errorf("catalogue has %d faults, want %d", len(got.Faults), len(faults.Names()))
	}
	if len(got.Behaviors) != len(faults.BehaviorNames()) {
		t.Errorf("catalogue has %d behaviors, want %d", len(got.Behaviors), len(faults.BehaviorNames()))
	}

	// The editor renders whatever is here, so an entry with no name or a
	// parameter with no kind would reach it as a blank control.
	for _, entry := range append(got.Faults, got.Behaviors...) {
		if entry.Name == "" {
			t.Error("an entry has no name")
		}
		for _, field := range entry.Fields {
			if field.Name == "" || field.Kind == "" || field.Description == "" {
				t.Errorf("%s: parameter %+v is missing a name, kind or description", entry.Name, field)
			}
		}
	}
}

func TestCatalogueCarriesTiersAndConstraints(t *testing.T) {
	s := newTestServer(t)

	got := decodeBody[catalogue](t, do(t, s, http.MethodGet, "/api/catalogue", ""))
	byName := map[string]catalogueEntry{}
	for _, entry := range got.Faults {
		byName[entry.Name] = entry
	}

	// delay is the whole shape in one fault: a tier, a required parameter with
	// a lower bound, and an optional one.
	delay := byName["delay"]
	if delay.Tier != string(faults.TierConnection) {
		t.Errorf("delay tier = %q, want connection", delay.Tier)
	}
	if len(delay.Fields) != 2 {
		t.Fatalf("delay has %d parameters, want ms and jitter_ms", len(delay.Fields))
	}
	ms := delay.Fields[0]
	if ms.Name != "ms" || ms.Kind != string(faults.KindInt) || !ms.Required {
		t.Errorf("delay.ms = %+v, want a required integer", ms)
	}
	if ms.Min == nil || *ms.Min != 1 {
		t.Errorf("delay.ms has no minimum of 1: %+v", ms.Min)
	}
	if delay.Fields[1].Required {
		t.Error("delay.jitter_ms should be optional")
	}

	// status carries an upper bound too, and a fault with no parameters still
	// appears, so the editor can offer it.
	if code := byName["status"].Fields[0]; code.Max == nil || *code.Max != 599 {
		t.Errorf("status.code has no maximum of 599: %+v", code.Max)
	}
	if refuse, ok := byName["refuse"]; !ok || len(refuse.Fields) != 0 {
		t.Errorf("refuse = %+v, want an entry with no parameters", refuse)
	}
	if byName["status"].Tier != string(faults.TierResponse) {
		t.Errorf("status tier = %q, want response", byName["status"].Tier)
	}
}

func TestCataloguePublishesPairsAndAlphabets(t *testing.T) {
	s := newTestServer(t)

	got := decodeBody[catalogue](t, do(t, s, http.MethodGet, "/api/catalogue", ""))
	byName := map[string]catalogueEntry{}
	for _, entry := range append(got.Faults, got.Behaviors...) {
		byName[entry.Name] = entry
	}

	// truncate takes exactly one of its two parameters, headers at least one of
	// its two. The form says "one of these" off these fields, so which kind of
	// pair it is has to survive the trip.
	after := byName["truncate"].Fields[0]
	if after.Partner != "percent" || !after.Exclusive {
		t.Errorf("truncate.after_bytes = %+v, want an exclusive pair with percent", after)
	}
	set := byName["headers"].Fields[0]
	if set.Partner != "remove" || set.Exclusive {
		t.Errorf("headers.set = %+v, want an inclusive pair with remove", set)
	}
	if set.Kind != string(faults.KindStrMap) {
		t.Errorf("headers.set kind = %q, want a string map", set.Kind)
	}
	if remove := byName["headers"].Fields[1]; remove.Kind != string(faults.KindStrList) {
		t.Errorf("headers.remove kind = %q, want a string list", remove.Kind)
	}

	// pattern is spelled with an alphabet, which the form turns into an input
	// pattern rather than waiting for the server to reject a typo.
	p := byName["pattern"].Fields[0]
	if p.Chars != "FP" {
		t.Errorf("pattern.pattern chars = %q, want FP", p.Chars)
	}
	// A behavior has no tier: it decides when a fault applies, not how much of
	// the traffic Faultline has to see.
	if byName["pattern"].Tier != "" {
		t.Errorf("behavior pattern has tier %q, want none", byName["pattern"].Tier)
	}
}
