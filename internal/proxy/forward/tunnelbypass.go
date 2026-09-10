package forward

import (
	"net/http"
)

// tunnelTransport is what a request inside an open tunnel travels over: the
// fault pipeline normally, and a transport with nothing of Faultline's in it
// once the host has been put on the bypass list.
//
// The list is consulted per request rather than only at the CONNECT, because a
// client keeps its tunnel open: without this, bypassing a busy host would go
// unnoticed until the client happened to reconnect, which for a client with a
// request every second is never.
//
// Interception cannot be taken back for a connection that is already
// terminated, so what a bypass promises the application arrives in two parts.
// No rules and no recording apply from the next request on, which is what the
// application and the operator can both see. Not being decrypted at all waits
// for the next CONNECT. Breaking the connection to force one sooner is the
// alternative, and it is worse: a bypass is how somebody takes a host out of
// the way, and it should not be the thing that finally breaks it.
type tunnelTransport struct {
	faulted   http.RoundTripper
	untouched http.RoundTripper

	// addr is the tunnel's target, as the CONNECT named it. Every request on
	// this tunnel goes to it, whatever the request line says.
	addr string
	// bypassed answers for addr and notes the sighting, so a host passed
	// through inside an open tunnel still reports what it is doing.
	bypassed func(addr string) bool
}

func (t *tunnelTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.bypassed(t.addr) {
		return t.untouched.RoundTrip(r)
	}
	return t.faulted.RoundTrip(r)
}
