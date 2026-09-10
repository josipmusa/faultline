package admin

import (
	"net/http"

	"github.com/josipmusa/faultline/internal/faults"
)

// The catalogue is what the binary can do to traffic, published so the rule
// editor can render a form for a fault it was never told about. It is a
// projection of the registered faults and behaviors, not a second description
// of them: everything here is read off faults.Schema, which is also what
// validates a rule and what the published JSON Schema is built from. A fault
// added to the catalogue therefore appears in the editor with no change here
// and none in the UI.

type catalogue struct {
	Faults    []catalogueEntry `json:"faults"`
	Behaviors []catalogueEntry `json:"behaviors"`
}

// catalogueEntry is one fault or one behavior. A behavior has no tier: it says
// when a fault applies, not how much of the traffic Faultline has to see.
type catalogueEntry struct {
	Name   string           `json:"name"`
	Tier   string           `json:"tier,omitempty"`
	Fields []catalogueField `json:"fields"`
}

// catalogueField is one parameter, with the constraints the form turns into
// input attributes. A constraint the schema does not declare is left out
// rather than sent as a zero, since zero is a legal bound.
type catalogueField struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
	Min         *int   `json:"min,omitempty"`
	Max         *int   `json:"max,omitempty"`
	// Chars is the alphabet a text parameter is spelled with, empty when any
	// text will do.
	Chars string `json:"chars,omitempty"`
	// Partner is the other parameter this one is written with, and Exclusive
	// says whether that means exactly one of the two rather than at least one.
	Partner   string `json:"partner,omitempty"`
	Exclusive bool   `json:"exclusive,omitempty"`
}

func (s *Server) listCatalogue(w http.ResponseWriter, _ *http.Request) {
	out := catalogue{
		Faults:    make([]catalogueEntry, 0, len(faults.All())),
		Behaviors: make([]catalogueEntry, 0, len(faults.AllBehaviors())),
	}
	for _, f := range faults.All() {
		out.Faults = append(out.Faults, catalogueEntry{
			Name:   f.Name(),
			Tier:   string(f.Tier()),
			Fields: catalogueFields(f.Schema()),
		})
	}
	for _, b := range faults.AllBehaviors() {
		out.Behaviors = append(out.Behaviors, catalogueEntry{
			Name:   b.Name(),
			Fields: catalogueFields(b.Schema()),
		})
	}
	s.writeJSON(w, http.StatusOK, out)
}

func catalogueFields(schema faults.Schema) []catalogueField {
	out := make([]catalogueField, 0, len(schema))
	for _, field := range schema {
		published := catalogueField{
			Name:        field.Name(),
			Kind:        string(field.Kind()),
			Description: field.Description(),
			Required:    field.IsRequired(),
			Chars:       field.AllowedChars(),
		}
		if low, ok := field.MinValue(); ok {
			published.Min = &low
		}
		if high, ok := field.MaxValue(); ok {
			published.Max = &high
		}
		published.Partner, published.Exclusive = field.Partner()
		out = append(out, published)
	}
	return out
}
