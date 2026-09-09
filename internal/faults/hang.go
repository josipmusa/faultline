package faults

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/josipmusa/faultline/internal/rules"
)

// hang is the fault that never answers, the way a dependency does when it
// accepts a connection and then stops responding: the client waits until its
// own timeout fires, which is exactly the behaviour worth rehearsing. It needs
// nothing but a connection, so it applies to encrypted traffic too.
type hang struct{}

func init() { Register(hang{}) }

func (hang) Name() string { return "hang" }

func (hang) Tier() Tier { return TierConnection }

func (hang) Schema() Schema {
	return Schema{
		Int("max_ms").Min(1),
	}
}

func (hang) New(params rules.Params) (Applier, error) {
	var f hangFault
	if err := Decode(params, &f); err != nil {
		return nil, err
	}
	return f, nil
}

// hangFault is a configured hang. MaxMS caps how long Faultline holds a
// connection nobody is going to answer; without it the wait lasts as long as
// the client does.
type hangFault struct {
	MaxMS int `json:"max_ms"`
}

// Respond holds the request and never sends it. A client that gives up ends
// the wait and gets its own context error; the cap abandons the connection
// instead, because a hang that answered at its cap would be a status fault.
func (f hangFault) Respond(_ string, req *http.Request, _ http.RoundTripper) (*http.Response, error) {
	if err := f.hold(req.Context()); err != nil {
		return nil, err
	}
	abortClient(req.Context())
	return nil, ErrClientReset
}

// Dial holds the tunnel request and never opens it, so the CONNECT goes
// unanswered for as long as the client waits.
func (f hangFault) Dial(ctx context.Context, _, _ string, _ DialFunc) (net.Conn, error) {
	if err := f.hold(ctx); err != nil {
		return nil, err
	}
	return nil, ErrClientReset
}

// hold waits for the client to give up, returning its context error, or
// returns nil when the cap ran out first. Without a cap it only ever ends the
// first way.
func (f hangFault) hold(ctx context.Context) error {
	if f.MaxMS <= 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	timer := time.NewTimer(time.Duration(f.MaxMS) * time.Millisecond)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
