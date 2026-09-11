package mcp

import (
	"fmt"
	"strings"

	"github.com/josipmusa/faultline/internal/faults"
)

// A fault's parameters are an open bag whose keys depend on its type, so the
// input schema can hold the set of types but not the parameters of each one:
// see addRuleSchema. The parameters go into the tool description instead,
// generated from the registry at startup the same way GET /api/catalogue is, so
// a fault added to internal/faults shows up in what the agent reads with no
// change here.

// faultCatalogue is the list of faults and their parameters, for the add_rule
// description.
func faultCatalogue() string {
	var b strings.Builder

	b.WriteString("Faults, by type:\n")
	for _, f := range faults.All() {
		fmt.Fprintf(&b, "  %s", f.Name())
		if tier := f.Tier(); tier != "" {
			fmt.Fprintf(&b, " (needs %s traffic)", tierNeeds(string(tier)))
		}
		fmt.Fprintf(&b, ": %s\n", describeSchema(f.Schema()))
	}

	b.WriteString("\nBehaviors, which gate when a fault applies, by type:\n")
	for _, behavior := range faults.AllBehaviors() {
		fmt.Fprintf(&b, "  %s: %s\n", behavior.Name(), describeSchema(behavior.Schema()))
	}

	return b.String()
}

// tierNeeds says what a fault's tier means for whether it can apply, in the
// words the agent needs rather than the internal name. A response-tier fault on
// encrypted traffic does nothing until the client trusts the Faultline CA, and
// that is the one thing worth warning about here.
func tierNeeds(tier string) string {
	if tier == string(faults.TierResponse) {
		return "intercepted or plain, not encrypted: the CA must be trusted for HTTPS"
	}
	return "any"
}

// describeSchema writes one fault's parameters as a sentence, with the bounds
// the schema declares so the agent does not have to discover them by being
// refused.
func describeSchema(schema faults.Schema) string {
	if len(schema) == 0 {
		return "no parameters"
	}

	parts := make([]string, 0, len(schema))
	for _, field := range schema {
		var p strings.Builder
		p.WriteString(field.Name())
		p.WriteString(" (")
		p.WriteString(string(field.Kind()))
		if field.IsRequired() {
			p.WriteString(", required")
		}
		if low, ok := field.MinValue(); ok {
			fmt.Fprintf(&p, ", min %d", low)
		}
		if high, ok := field.MaxValue(); ok {
			fmt.Fprintf(&p, ", max %d", high)
		}
		if partner, exclusive := field.Partner(); partner != "" {
			if exclusive {
				fmt.Fprintf(&p, ", exactly one of it and %s", partner)
			} else {
				fmt.Fprintf(&p, ", with %s", partner)
			}
		}
		p.WriteString(")")
		if d := field.Description(); d != "" {
			p.WriteString(" " + d)
		}
		parts = append(parts, p.String())
	}
	return strings.Join(parts, "; ")
}
