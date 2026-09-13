//go:build !linux

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// Off Linux the mode is refused before anything binds a port, so an operator
// who asks for it on a laptop is told why rather than left with a proxy that
// looks up but redirects nothing.
func TestTransparentIsRefusedOffLinux(t *testing.T) {
	var out bytes.Buffer
	cmd := newServeCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--transparent", "--admin-port", "0", "--proxy-port", "0"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("serve --transparent started on a platform that cannot redirect traffic")
	}
	if !strings.Contains(err.Error(), "--transparent") || !strings.Contains(err.Error(), "Linux") {
		t.Errorf("the error does not explain itself: %v", err)
	}
	if strings.Contains(out.String(), "admin:") {
		t.Errorf("the stack came up before the refusal:\n%s", out.String())
	}
}
