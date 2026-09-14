package admin

import (
	"errors"

	"github.com/josipmusa/faultline/internal/faults"
	"github.com/josipmusa/faultline/internal/rules"
)

// validateRule checks a rule that arrived over the API. The shape of a rule
// belongs to internal/rules and the parameters of a fault to the catalogue in
// internal/faults, so this file only turns what they say into a 400 that names
// the field.
//
// The fault goes first. One answer is all a 400 carries, and a refused fault
// type is the answer that lists the catalogue, which is how the catalogue is
// meant to be learnt; a rule with no name and a misspelt fault should hear
// about the fault, not be sent back for the name.
func validateRule(r rules.Rule) error {
	if err := validateFault(r.Fault); err != nil {
		return err
	}
	if err := validateBehavior(r.Behavior); err != nil {
		return err
	}
	return asAPIError(rules.Validate(r))
}

// validateFault delegates to the fault named by the type: the catalogue in
// internal/faults owns what each fault accepts, so a new fault is validated
// here without this file changing.
func validateFault(f rules.Fault) error {
	return asAPIError(faults.Validate(f))
}

// validateBehavior delegates the same way. A rule without a behavior is the
// common case and is fine.
func validateBehavior(b *rules.Behavior) error {
	if b == nil {
		return nil
	}
	return asAPIError(faults.ValidateBehavior(*b))
}

// asAPIError turns a field one of the validators rejected into a 400 naming it,
// like fault.ms, behavior.n, or match.host.
func asAPIError(err error) error {
	var pe *faults.ParamError
	if errors.As(err, &pe) {
		return invalid(pe.Path(), "%s", pe.Message)
	}
	var fe *rules.FieldError
	if errors.As(err, &fe) {
		return invalid(fe.Field, "%s", fe.Message)
	}
	return err
}
