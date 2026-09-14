package faults

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// ProxyErrorHandler is the error handler every proxy edge gives its
// httputil.ReverseProxy. It tells the three ways a round trip ends without a
// response apart, because they call for different things: a rule that reset
// the connection has already dealt with the client; a client that gave up
// waiting, which is what a hang fault is for, left nobody to answer and no
// upstream to blame; and an upstream that really failed is told to the client
// plainly, with the detail kept in the log. upstreamOf names the upstream a
// request was for, since each edge knows that differently.
func ProxyErrorHandler(log *slog.Logger, upstreamOf func(*http.Request) string) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		switch {
		case errors.Is(err, ErrClientReset):
			return // a rule already reset the connection; there is nobody left to tell
		case errors.Is(err, context.Canceled):
			log.Debug("client gave up before a response", "upstream", upstreamOf(r), "method", r.Method, "path", r.URL.Path)
			return
		}
		// The upstream really failed. Faultline says so plainly rather than
		// inventing a response, and keeps the detail in the log.
		log.Error("upstream unreachable", "upstream", upstreamOf(r), "method", r.Method, "path", r.URL.Path, "err", err)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "faultline: upstream unreachable\n")
	}
}
