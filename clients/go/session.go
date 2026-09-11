package client

import (
	"context"
	"net/http"
)

// ResetSession puts the session back to the start of a measurement: what
// Faultline observed is cleared, and every rule's behavior state is re-armed so
// a spent first_n applies again. The rules themselves and the active scenario
// are left alone, so the setup being measured survives.
func (c *Client) ResetSession(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/sessions/current/reset", nil, nil)
}
