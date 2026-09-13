package main

import (
	"context"
	"fmt"
	"os"

	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/transparent"
)

// startTransparent puts transparent mode in force on a stack that is already
// up: the two redirect listeners first, then the packet-filter rules that send
// traffic to them. That order is not a preference. A rule pointing at a port
// nothing is listening on breaks every outbound call in the namespace, and in
// transparent mode that is every call the application makes.
func (s *stack) startTransparent(ctx context.Context, bind string) error {
	if err := transparent.Supported(); err != nil {
		return fmt.Errorf("--transparent: %w", err)
	}
	if err := s.proxy.StartTransparent(bind, forward.TransparentHTTPPort, forward.TransparentHTTPSPort); err != nil {
		return fmt.Errorf("--transparent: %w", err)
	}

	// Faultline's own calls to the upstream leave the same network namespace
	// and would be redirected straight back into Faultline, so its user is
	// exempted. That is the one thing transparent mode asks of the
	// application's image: not to run as this user.
	uid := os.Getuid()
	nat, err := transparent.New(transparent.Options{
		HTTPPort:  forward.TransparentHTTPPort,
		HTTPSPort: forward.TransparentHTTPSPort,
		ExemptUID: uid,
	})
	if err != nil {
		return fmt.Errorf("--transparent: %w", err)
	}
	if err := nat.Install(ctx); err != nil {
		return fmt.Errorf("--transparent: %w; redirecting traffic needs NET_ADMIN, which a container is given with cap_add: [NET_ADMIN]", err)
	}

	s.nat, s.transparentUID = nat, uid
	return nil
}

// stopTransparent takes the redirect rules back out. Leaving them in a
// namespace Faultline is no longer listening in would black-hole every
// outbound call the application makes.
func (s *stack) stopTransparent(ctx context.Context) {
	if s.nat == nil {
		return
	}
	s.nat.Remove(ctx)
	s.nat = nil
}
