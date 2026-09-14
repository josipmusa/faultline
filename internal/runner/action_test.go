package runner

import (
	"os"
	"strings"
	"testing"
)

// actionPath is the composite GitHub Action, which sets the same environment
// for the rest of a workflow job that Env sets for a child process.
const actionPath = "../../setup/action.yml"

// TestTheActionSetsEveryVariableTheRunnerOwns: the action cannot ask Go for
// this list, so it repeats it in YAML. A variable added here and forgotten
// there would leave a workflow's later steps quietly unproxied or untrusting,
// which looks exactly like an application that coped.
func TestTheActionSetsEveryVariableTheRunnerOwns(t *testing.T) {
	body, err := os.ReadFile(actionPath)
	if err != nil {
		t.Fatalf("reading the action: %v", err)
	}
	yaml := string(body)

	names := append(append([]string{}, proxyVars...), trustVars...)
	names = append(names, nodeEnvProxy)

	for _, name := range names {
		// The action writes NAME=value lines into $GITHUB_ENV, so the name
		// followed by = is the shape to look for.
		if !strings.Contains(yaml, name+"=") {
			t.Errorf("%s is set for a wrapped child but never written to $GITHUB_ENV by %s", name, actionPath)
		}
	}
}
