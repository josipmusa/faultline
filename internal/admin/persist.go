package admin

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/josipmusa/faultline/internal/config"
)

// A Persister writes a rule change back to the configuration file in use. The
// server has one only when Faultline was started with a file; without one,
// changes live in memory and are gone when it stops.
type Persister interface {
	// Change makes one change to the rule store and saves the result. The
	// mutation's own error comes back unchanged.
	Change(mutate func() error) error
}

// Persist makes every rule change through the API write the rule set back to
// the configuration file. Call it before Start.
func (s *Server) Persist(p Persister) { s.persist = p }

// change makes one change to the rules, through the configuration file when
// there is one.
func (s *Server) change(mutate func() error) error {
	if s.persist == nil {
		return mutate()
	}
	return s.persist.Change(mutate)
}

// saved turns what a change came back with into something the client can act
// on. The store's own errors pass through for the caller to map; what is left
// is the file standing in the way.
func (s *Server) saved(err error) error {
	var cfgErr *config.Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &cfgErr):
		return conflict("", "%v; nothing was changed, because a change would have to be written to a file "+
			"Faultline cannot read", cfgErr)
	case errors.Is(err, config.ErrNotSaved):
		return apiError{
			Message: fmt.Sprintf("the change was made in memory only: %v", err),
			status:  http.StatusInternalServerError,
		}
	}
	return err
}
