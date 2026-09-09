package main

import (
	"strings"
	"testing"

	client "github.com/josipmusa/faultline/clients/go"
)

func TestUpstreamsListsWhatWasSeen(t *testing.T) {
	i := newInstance(t)
	i.record(t, "1", "httpbin.org", false)
	i.record(t, "2", "httpbin.org", true)

	wantLine(t, i.run(t, "upstreams"), "httpbin.org", "plain", "2", "1")
}

func TestUpstreamsJSONIsTheAPIsOwnShape(t *testing.T) {
	i := newInstance(t)
	i.record(t, "1", "httpbin.org", true)

	list := decodeJSON[[]client.Upstream](t, i.run(t, "upstreams", "--json"))

	if len(list) != 1 || list[0].Host != "httpbin.org" || list[0].Faulted != 1 {
		t.Fatalf("upstreams --json = %+v, want httpbin.org with one faulted request", list)
	}
}

func TestUpstreamsWithNothingSeenSaysSo(t *testing.T) {
	i := newInstance(t)

	if out := i.run(t, "upstreams"); !strings.Contains(out, "no upstreams") {
		t.Errorf("upstreams printed %q, want it to say none were seen", out)
	}
}
