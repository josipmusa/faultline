package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	client "github.com/josipmusa/faultline/clients/go"
)

type resetResult struct {
	Reset bool `json:"reset" jsonschema:"true once the session has been put back to the start of a measurement"`
}

func addSessionTools(s *sdk.Server, c *client.Client) {
	sdk.AddTool(s, &sdk.Tool{
		Name: "reset_session",
		Description: "Put the session back to the start of a measurement. The recorded calls are " +
			"cleared and every rule's behavior state is re-armed, so a spent first_n or percent " +
			"applies again.\n\n" +
			"The rules themselves and the active scenario are left alone, so what you or the user set " +
			"up survives. Call this between two runs of the same check, or the second run measures the " +
			"first one's leftovers.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ noInput) (*sdk.CallToolResult, resetResult, error) {
		if err := c.ResetSession(ctx); err != nil {
			return nil, resetResult{}, err
		}
		return nil, resetResult{Reset: true}, nil
	})
}
