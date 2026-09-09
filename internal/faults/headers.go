package faults

import (
	"net/http"

	"github.com/josipmusa/faultline/internal/rules"
)

// headers changes a real response's headers on the way back: the fault for a
// dependency that stops sending a caching header, starts sending an unexpected
// one, or answers with a content type the client does not handle.
type headers struct{}

func init() { Register(headers{}) }

func (headers) Name() string { return "headers" }

func (headers) Tier() Tier { return TierResponse }

func (headers) Schema() Schema {
	return Schema{
		StrMap("set").Or("remove"),
		StrList("remove"),
	}
}

func (headers) New(params rules.Params) (Applier, error) {
	var f headersFault
	if err := Decode(params, &f); err != nil {
		return nil, err
	}
	return f, nil
}

type headersFault struct {
	Set    map[string]string `json:"set"`
	Remove []string          `json:"remove"`
}

// Respond leaves the body and the status alone: only the headers change.
// Removals happen first, so a name in both ends up set rather than gone.
func (f headersFault) Respond(_ string, req *http.Request, next http.RoundTripper) (*http.Response, error) {
	resp, err := next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	for _, name := range f.Remove {
		resp.Header.Del(name)
	}
	for name, value := range f.Set {
		resp.Header.Set(name, value)
	}
	return resp, nil
}
