package oauth2

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrorBuilders(t *testing.T) {
	matrix := []struct {
		err *Error
		cde string
		sta int
	}{
		{InvalidRequest("foo"), "invalid_request", http.StatusBadRequest},
		{InvalidClient("foo"), "invalid_client", http.StatusUnauthorized},
		{InvalidGrant("foo"), "invalid_grant", http.StatusBadRequest},
		{InvalidScope("foo"), "invalid_scope", http.StatusBadRequest},
		{UnauthorizedClient("foo"), "unauthorized_client", http.StatusBadRequest},
		{UnsupportedGrantType("foo"), "unsupported_grant_type", http.StatusBadRequest},
		{UnsupportedResponseType("foo"), "unsupported_response_type", http.StatusBadRequest},
		{AccessDenied("foo"), "access_denied", http.StatusForbidden},
		{ServerError("foo"), "server_error", http.StatusInternalServerError},
		{TemporarilyUnavailable("foo"), "temporarily_unavailable", http.StatusServiceUnavailable},
	}

	for _, i := range matrix {
		assert.Equal(t, i.sta, i.err.Status, i.err.Code)
		assert.Equal(t, i.cde, i.err.Code, i.err.Code)
		assert.Equal(t, "foo", i.err.Description, i.err.Code)
	}
}

func TestError(t *testing.T) {
	err := InvalidRequest("foo")
	assert.Error(t, err)
	assert.Equal(t, "invalid_request: foo", err.Error())
	assert.Equal(t, "invalid_request: foo", err.String())
	assert.Equal(t, map[string]string{
		"error":             "invalid_request",
		"error_description": "foo",
	}, err.Map())
}

func TestErrorMap(t *testing.T) {
	err := InvalidRequest("foo")
	err.State = "bar"
	err.URI = "http://example.com"

	assert.Equal(t, map[string]string{
		"error":             "invalid_request",
		"error_description": "foo",
		"error_uri":         "http://example.com",
		"state":             "bar",
	}, err.Map())
}

func TestWriteError(t *testing.T) {
	err1 := InvalidRequest("foo")
	rec := httptest.NewRecorder()

	err2 := WriteError(rec, err1)
	assert.NoError(t, err2)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{
		"error": "invalid_request",
		"error_description": "foo"
	}`, rec.Body.String())
}

func TestWriteErrorRedirect(t *testing.T) {
	err1 := InvalidRequest("foo")
	rec := httptest.NewRecorder()

	err2 := RedirectError(rec, "http://example.com", false, err1)
	assert.NoError(t, err2)
	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "http://example.com?error=invalid_request&error_description=foo", rec.HeaderMap.Get("Location"))
}

func TestWriteErrorFallback(t *testing.T) {
	err1 := errors.New("foo")
	rec := httptest.NewRecorder()

	err2 := WriteError(rec, err1)
	assert.NoError(t, err2)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{
		"error": "server_error"
	}`, rec.Body.String())
}

func TestWriteErrorRedirectFallback(t *testing.T) {
	err1 := errors.New("foo")
	rec := httptest.NewRecorder()

	err2 := RedirectError(rec, "http://example.com", false, err1)
	assert.NoError(t, err2)
	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "http://example.com?error=server_error", rec.HeaderMap.Get("Location"))
}
