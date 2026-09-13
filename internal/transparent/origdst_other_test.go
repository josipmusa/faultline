//go:build !linux

package transparent

import (
	"errors"
	"net"
	"testing"
)

func TestOriginalDstIsRefusedOffLinux(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })

	if _, err := OriginalDst(server); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("got %v, want ErrUnsupported", err)
	}
}
