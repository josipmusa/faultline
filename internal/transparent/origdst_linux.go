//go:build linux

package transparent

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"

	"golang.org/x/sys/unix"
)

// ip6OriginalDst is IP6T_SO_ORIGINAL_DST, the IPv6 counterpart of
// SO_ORIGINAL_DST. The unix package names the IPv4 one and not this one.
const ip6OriginalDst = 80

// OriginalDst asks the kernel where a redirected connection was going before
// iptables sent it here. Without it a transparent listener knows only that
// something arrived: the destination is not in the connection, because the
// point of the redirect is that the client still believes it is talking to the
// upstream.
func OriginalDst(conn net.Conn) (netip.AddrPort, error) {
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return netip.AddrPort{}, fmt.Errorf("original destination: %T is not a TCP connection", conn)
	}
	raw, err := tcp.SyscallConn()
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("original destination: %w", err)
	}

	// The socket family decides which option holds the answer, and the local
	// address is what says which family the socket is.
	v6 := false
	if local, ok := conn.LocalAddr().(*net.TCPAddr); ok {
		v6 = local.IP.To4() == nil
	}

	var addr netip.AddrPort
	var inner error
	if err := raw.Control(func(fd uintptr) {
		if v6 {
			addr, inner = originalDst6(int(fd))
			return
		}
		addr, inner = originalDst4(int(fd))
	}); err != nil {
		return netip.AddrPort{}, fmt.Errorf("original destination: %w", err)
	}
	if inner != nil {
		return netip.AddrPort{}, inner
	}
	return addr, nil
}

// originalDst4 reads SO_ORIGINAL_DST, which answers with a sockaddr_in the
// unix package has no typed getter for: IPv6Mreq is the right size, so the
// bytes are read through it and taken apart here.
func originalDst4(fd int) (netip.AddrPort, error) {
	mreq, err := unix.GetsockoptIPv6Mreq(fd, unix.SOL_IP, unix.SO_ORIGINAL_DST)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("original destination: getsockopt SO_ORIGINAL_DST: %w", err)
	}
	// sockaddr_in: family, then the port and the address, both big-endian.
	port := binary.BigEndian.Uint16(mreq.Multiaddr[2:4])
	return netip.AddrPortFrom(netip.AddrFrom4([4]byte(mreq.Multiaddr[4:8])), port), nil
}

// originalDst6 reads IP6T_SO_ORIGINAL_DST, which answers with a sockaddr_in6.
func originalDst6(fd int) (netip.AddrPort, error) {
	info, err := unix.GetsockoptIPv6MTUInfo(fd, unix.SOL_IPV6, ip6OriginalDst)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("original destination: getsockopt IP6T_SO_ORIGINAL_DST: %w", err)
	}
	// The port sits in the struct in network order, so it is read back out of
	// its own bytes rather than byte-swapped by hand.
	port := binary.BigEndian.Uint16(binary.NativeEndian.AppendUint16(nil, info.Addr.Port))
	return netip.AddrPortFrom(netip.AddrFrom16(info.Addr.Addr).Unmap(), port), nil
}

// Supported reports whether transparent mode can run here at all. On Linux it
// can, or at least the kernel side of it can; whether iptables is installed
// and whether the process may use it are answered when the rules go in.
func Supported() error { return nil }
