package client

import "context"

// Config reports how this instance is configured: whether rule changes are
// written to a file and where, and how to send a child process through it -
// the proxy address, the hosts to reach directly, and whether HTTPS is being
// intercepted.
//
// It is the one call a caller wrapping a command needs before it can build the
// child's environment, so the answer is the same whether it came over a socket
// or from an instance in this process.
func (c *Client) Config(ctx context.Context) (Config, error) {
	var out Config
	err := c.get(ctx, "/api/config", &out)
	return out, err
}
