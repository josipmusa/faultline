package client_test

import (
	"context"
	"slices"
	"testing"
)

// The config endpoint is how a caller wrapping a child process learns where to
// send its traffic, so the client has to carry those fields through rather than
// only the file ones the UI reads.
func TestConfigReportsHowToAttachAChild(t *testing.T) {
	h := newHarness(t)
	h.api.ProxiesAt("http://localhost:9001", true)

	got, err := h.client.Config(context.Background())
	if err != nil {
		t.Fatalf("Config: %v", err)
	}

	if got.ProxyURL != "http://localhost:9001" {
		t.Errorf("proxy_url is %q, want the address a child should use", got.ProxyURL)
	}
	if !got.Intercepting {
		t.Error("intercepting is false while the CA is terminating HTTPS")
	}
	if !slices.Contains(got.NoProxy, "localhost") {
		t.Errorf("no_proxy is %v, want the entries that keep a child off the admin port", got.NoProxy)
	}
}
