package client

import (
	"context"
	"net/http"
	"net/url"
)

// Rules lists every rule the instance holds, in the order it applies them.
func (c *Client) Rules(ctx context.Context) ([]Rule, error) {
	var out []Rule
	if err := c.get(ctx, "/api/rules", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Rule reads one rule by id.
func (c *Client) Rule(ctx context.Context, id string) (Rule, error) {
	var out Rule
	if err := c.get(ctx, "/api/rules/"+url.PathEscape(id), &out); err != nil {
		return Rule{}, err
	}
	return out, nil
}

// AddRule creates a rule and returns it as stored, which is where an id the
// server minted appears.
func (c *Client) AddRule(ctx context.Context, r Rule) (Rule, error) {
	var out Rule
	if err := c.do(ctx, http.MethodPost, "/api/rules", r, &out); err != nil {
		return Rule{}, err
	}
	return out, nil
}

// DeleteRule removes a rule.
func (c *Client) DeleteRule(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/rules/"+url.PathEscape(id), nil, nil)
}

// SetRuleEnabled turns a rule on or off, which also resets its behavior state.
func (c *Client) SetRuleEnabled(ctx context.Context, id string, enabled bool) (Rule, error) {
	action := "/disable"
	if enabled {
		action = "/enable"
	}

	var out Rule
	if err := c.do(ctx, http.MethodPost, "/api/rules/"+url.PathEscape(id)+action, nil, &out); err != nil {
		return Rule{}, err
	}
	return out, nil
}
