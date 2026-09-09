package client

import (
	"context"
	"net/http"
	"net/url"
)

// Scenarios lists the scenarios the configuration file declares, and says
// which one is active.
func (c *Client) Scenarios(ctx context.Context) ([]Scenario, error) {
	var out []Scenario
	if err := c.get(ctx, "/api/scenarios", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetScenarioActive activates or deactivates a scenario. Activating one
// deactivates whichever was active, and resets the behavior state of the rules
// it turns on, so a rehearsal starts from the beginning.
func (c *Client) SetScenarioActive(ctx context.Context, name string, active bool) (Scenario, error) {
	action := "/deactivate"
	if active {
		action = "/activate"
	}

	var out Scenario
	if err := c.do(ctx, http.MethodPost, "/api/scenarios/"+url.PathEscape(name)+action, nil, &out); err != nil {
		return Scenario{}, err
	}
	return out, nil
}
