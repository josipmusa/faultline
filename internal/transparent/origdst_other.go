//go:build !linux

package transparent

import (
	"net"
	"net/netip"
)

// OriginalDst cannot be answered off Linux. Faultline says so here rather than
// letting a transparent listener come up and serve connections it has no
// destination for.
func OriginalDst(net.Conn) (netip.AddrPort, error) { return netip.AddrPort{}, ErrUnsupported }

// Supported refuses transparent mode before anything binds a port, so the
// answer arrives as a sentence at start-up rather than as a failure on the
// first connection.
func Supported() error { return ErrUnsupported }
