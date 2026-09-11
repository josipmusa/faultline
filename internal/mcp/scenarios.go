package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	client "github.com/josipmusa/faultline/clients/go"
)

type scenarioNameInput struct {
	Name string `json:"name" jsonschema:"the name of the scenario"`
}

// scenariosOutput wraps the scenario list in an object: the wire format
// defines structuredContent as a record, and a bare array there fails a
// client that enforces that shape.
type scenariosOutput struct {
	Scenarios []client.Scenario `json:"scenarios" jsonschema:"the scenarios the configuration file declares, and which is active"`
}

func addScenarioTools(s *sdk.Server, c *client.Client) {
	sdk.AddTool(s, &sdk.Tool{
		Name: "list_scenarios",
		Description: "List the named scenarios the configuration file declares, and say which one is " +
			"active. A scenario is a set of rules that describe one situation, such as a dependency " +
			"being down, so it can be rehearsed in one step.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ noInput) (*sdk.CallToolResult, scenariosOutput, error) {
		list, err := c.Scenarios(ctx)
		return nil, scenariosOutput{Scenarios: list}, err
	})

	sdk.AddTool(s, &sdk.Tool{
		Name: "activate_scenario",
		Description: "Put a scenario in force: the rules it names are enabled and every other rule is " +
			"disabled, so exactly the situation it describes is what the application meets. Whichever " +
			"scenario was active is deactivated, and the behavior state of the rules it turns on starts " +
			"over, so a rehearsal begins from the beginning.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in scenarioNameInput) (*sdk.CallToolResult, client.Scenario, error) {
		scenario, err := c.SetScenarioActive(ctx, in.Name, true)
		return nil, scenario, err
	})

	sdk.AddTool(s, &sdk.Tool{
		Name: "deactivate_scenario",
		Description: "Take a scenario out of force, disabling the rules it named. Faultline goes back " +
			"to watching the traffic without breaking any of it.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in scenarioNameInput) (*sdk.CallToolResult, client.Scenario, error) {
		scenario, err := c.SetScenarioActive(ctx, in.Name, false)
		return nil, scenario, err
	})
}
