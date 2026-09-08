package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// apiError is the body every failing endpoint returns: a human readable message
// and, when one input caused the failure, the field that names it.
type apiError struct {
	Message string `json:"error"`
	Field   string `json:"field,omitempty"`

	status int
}

func (e apiError) Error() string { return e.Message }

// invalid reports input the caller can fix. Field is the dotted path into the
// request body, or the query parameter, that is wrong.
func invalid(field, format string, args ...any) apiError {
	return apiError{Message: fmt.Sprintf(format, args...), Field: field, status: http.StatusBadRequest}
}

func missing(format string, args ...any) apiError {
	return apiError{Message: fmt.Sprintf(format, args...), status: http.StatusNotFound}
}

func conflict(field, format string, args ...any) apiError {
	return apiError{Message: fmt.Sprintf(format, args...), Field: field, status: http.StatusConflict}
}

// unavailable reports that the server cannot take the request on right now,
// which so far only happens while it is shutting down.
func unavailable(format string, args ...any) apiError {
	return apiError{Message: fmt.Sprintf(format, args...), status: http.StatusServiceUnavailable}
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		s.log.Error("writing response body", "err", err)
	}
}

// fail answers with err when it is an apiError, and with a logged, opaque 500
// otherwise, so an internal failure never leaks its detail to the caller.
func (s *Server) fail(w http.ResponseWriter, err error) {
	var apiErr apiError
	if !errors.As(err, &apiErr) {
		s.log.Error("request failed", "err", err)
		apiErr = apiError{Message: "internal error", status: http.StatusInternalServerError}
	}
	s.writeJSON(w, apiErr.status, apiErr)
}
