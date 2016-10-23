package bearer

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// An Error represents an unsuccessful bearer token authentication.
type Error struct {
	Name        string
	Description string
	URI         string
	Realm       string
	Scope       string
	Status      int
}

// Map returns a map of all fields that can be presented to the client.
func (e *Error) Map() map[string]string {
	m := make(map[string]string)

	// add name if present
	if e.Name != "" {
		m["error"] = e.Name
	}

	// add description if present
	if e.Description != "" {
		m["error_description"] = e.Description
	}

	// add uri if present
	if e.URI != "" {
		m["error_uri"] = e.URI
	}

	// add realm if present
	if e.Realm != "" {
		m["realm"] = e.Realm
	}

	// add scope if present
	if e.Scope != "" {
		m["scope"] = e.Scope
	}

	return m
}

// Params returns an string encoded representation of the error parameters.
func (e *Error) Params() string {
	// prepare params
	var params []string

	// add all params
	for k, v := range e.Map() {
		params = append(params, fmt.Sprintf(`%s="%s"`, k, v))
	}

	// sort params
	sort.Strings(params)

	return strings.Join(params, ", ")
}

// Error implements the error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Name, e.Description)
}

// ProtectedResource constructs and error that indicates that the requested
// resource needs authentication.
func ProtectedResource() *Error {
	return &Error{
		Status: http.StatusUnauthorized,
	}
}

// InvalidRequest constructs and error that indicates that the request is
// missing a required parameter, includes an unsupported parameter or parameter
// value, repeats the same parameter, uses more than one method for including an
// access token, or is otherwise malformed.
func InvalidRequest(description string) *Error {
	return &Error{
		Name:        "invalid_request",
		Description: description,
		Status:      http.StatusBadRequest,
	}
}

// InvalidToken constructs and error that indicates that the access token
// provided is expired, revoked, malformed, or invalid for
// other reasons.
func InvalidToken(description string) *Error {
	return &Error{
		Name:        "invalid_token",
		Description: description,
		Status:      http.StatusUnauthorized,
	}
}

// InsufficientScope constructs and error that indicates that the request
// requires higher privileges than provided by the access token.
func InsufficientScope(necessaryScope string) *Error {
	return &Error{
		Name:   "insufficient_scope",
		Scope:  necessaryScope,
		Status: http.StatusForbidden,
	}
}

// ServerError constructs an error that indicates that there was an internal
// server error.
//
// Note: This error type is not defined by the spec, but has been added to
// increase the readability of the source code.
func ServerError() *Error {
	return &Error{
		Status: http.StatusInternalServerError,
	}
}

// WriteError will write the specified error to the response writer. The function
// will fall back and write an internal server error if the specified error is
// not known.
func WriteError(w http.ResponseWriter, err error) error {
	// ensure complex error
	anError, ok := err.(*Error)
	if !ok || anError.Status == http.StatusInternalServerError {
		// write internal server error
		w.WriteHeader(http.StatusInternalServerError)

		// finish response
		_, err = w.Write(nil)

		return err
	}

	// get params
	params := anError.Params()

	// force at least one parameter
	if params == "" {
		params = `realm="OAuth2"`
	}

	// prepare response
	response := "Bearer " + params

	// set header
	w.Header().Set("WWW-Authenticate", response)

	// write header
	w.WriteHeader(anError.Status)

	// finish response
	_, err = w.Write(nil)

	return err
}