package delegate

import (
	"net/http"
	"time"

	"github.com/gonfire/oauth2"
	"github.com/gonfire/oauth2/bearer"
)

func ProcessTokenRequest(d Delegate, r *http.Request) (*oauth2.TokenRequest, Client, error) {
	// parse token request
	req, err := oauth2.ParseTokenRequest(r)
	if err != nil {
		return nil, nil, err
	}

	// make sure the grant type is known
	if !oauth2.KnownGrantType(req.GrantType) {
		return nil, nil, oauth2.InvalidRequest(oauth2.NoState, "Unknown grant type")
	}

	// load client
	client, err := d.LookupClient(req.ClientID)
	if err == ErrNotFound {
		return nil, nil, oauth2.InvalidClient(oauth2.NoState, "Unknown client")
	} else if err != nil {
		return nil, nil, oauth2.ServerError(oauth2.NoState, "Failed to lookup client")
	}

	// authenticate client if confidential
	if client.Confidential() && !client.ValidSecret(req.ClientSecret) {
		return nil, nil, oauth2.InvalidClient(oauth2.NoState, "Unknown client")
	}

	return req, client, nil
}

func HandlePasswordGrant(d Delegate, c Client, r *oauth2.TokenRequest) (*oauth2.TokenResponse, error) {
	// get resource owner
	ro, err := d.LookupResourceOwner(r.Username)
	if err == ErrNotFound {
		return nil, oauth2.AccessDenied(oauth2.NoState, "Unknown resource owner")
	} else if err != nil {
		return nil, oauth2.ServerError(oauth2.NoState, "Failed to lookup resource owner")
	}

	// authenticate resource owner
	if !ro.ValidSecret(r.Password) {
		return nil, oauth2.AccessDenied(oauth2.NoState, "Unknown resource owner")
	}

	// grant scope
	grantedScope, err := d.GrantScope(c, ro, r.Scope)
	if err == ErrRejected {
		return nil, oauth2.InvalidScope(oauth2.NoState, "The scope has not been granted")
	} else if err != nil {
		return nil, oauth2.ServerError(oauth2.NoState, "Failed to grant scope")
	}

	// issue tokens
	res, err := HandleTokenResponse(d, c, ro, grantedScope, true)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func HandleClientCredentialsGrant(d Delegate, c Client, r *oauth2.TokenRequest) (*oauth2.TokenResponse, error) {
	// grant scope
	grantedScope, err := d.GrantScope(c, nil, r.Scope)
	if err == ErrRejected {
		return nil, oauth2.InvalidScope(oauth2.NoState, "The scope has not been granted")
	} else if err != nil {
		return nil, oauth2.ServerError(oauth2.NoState, "Failed to grant scope")
	}

	// issue tokens
	res, err := HandleTokenResponse(d, c, nil, grantedScope, true)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func HandleAuthorizationCodeGrant(d AuthorizationCodeDelegate, c Client, r *oauth2.TokenRequest) (*oauth2.TokenResponse, error) {
	// get authorization code
	ac, err := d.LookupAuthorizationCode(r.Code)
	if err == ErrMalformed {
		return nil, oauth2.InvalidRequest(oauth2.NoState, "Malformed authorization code")
	} else if err == ErrNotFound {
		return nil, oauth2.InvalidGrant(oauth2.NoState, "Unknown authorization code")
	} else if err != nil {
		return nil, oauth2.ServerError(oauth2.NoState, "Failed to lookup authorization code")
	}

	// validate expiration
	if ac.ExpiresAt().Before(time.Now()) {
		return nil, oauth2.InvalidGrant(oauth2.NoState, "Expired authorization code")
	}

	// validate redirect uri
	if ac.RedirectURI() != r.RedirectURI {
		return nil, oauth2.InvalidGrant(oauth2.NoState, "Changed redirect uri")
	}

	// validate ownership
	if ac.ClientID() != c.ID() {
		return nil, oauth2.InvalidGrant(oauth2.NoState, "Invalid authorization code ownership")
	}

	// prepare resource owner
	var ro ResourceOwner

	// validate resource owner by lookup if present
	if ac.ResourceOwnerID() != "" {
		ro, err = d.LookupResourceOwner(ac.ResourceOwnerID())
		if err == ErrNotFound {
			return nil, oauth2.ServerError(oauth2.NoState, "Expected to find resource owner")
		} else if err != nil {
			return nil, oauth2.ServerError(oauth2.NoState, "Failed to lookup resource owner")
		}
	}

	// issue tokens
	res, err := HandleTokenResponse(d, c, ro, ac.Scope(), true)
	if err != nil {
		return nil, err
	}

	// remove used authorization code
	err = d.RemoveAuthorizationCode(r.Code)
	if err != nil {
		return nil, oauth2.ServerError(oauth2.NoState, "Failed to remove authorization code")
	}

	return res, nil
}

func HandleRefreshTokenGrant(d RefreshTokenDelegate, c Client, r *oauth2.TokenRequest) (*oauth2.TokenResponse, error) {
	// get refresh token
	rt, err := d.LookupRefreshToken(r.RefreshToken)
	if err == ErrMalformed {
		return nil, oauth2.InvalidRequest(oauth2.NoState, "Malformed refresh token")
	} else if err == ErrNotFound {
		return nil, oauth2.InvalidGrant(oauth2.NoState, "Unknown refresh token")
	} else if err != nil {
		return nil, oauth2.ServerError(oauth2.NoState, "Failed to lookup refresh token")
	}

	// validate expiration
	if rt.ExpiresAt().Before(time.Now()) {
		return nil, oauth2.InvalidGrant(oauth2.NoState, "Expired refresh token")
	}

	// inherit scope from stored refresh token
	if r.Scope.Empty() {
		r.Scope = rt.Scope()
	}

	// validate scope
	if !rt.Scope().Includes(r.Scope) {
		return nil, oauth2.InvalidScope(oauth2.NoState, "New scope exceeds granted scope")
	}

	// validate client ownership
	if rt.ClientID() != c.ID() {
		return nil, oauth2.InvalidGrant(oauth2.NoState, "Invalid refresh token ownership")
	}

	// prepare resource owner
	var ro ResourceOwner

	// validate resource owner by lookup if present
	if rt.ResourceOwnerID() != "" {
		ro, err = d.LookupResourceOwner(rt.ResourceOwnerID())
		if err == ErrNotFound {
			return nil, oauth2.ServerError(oauth2.NoState, "Expected to find resource owner")
		} else if err != nil {
			return nil, oauth2.ServerError(oauth2.NoState, "Failed to lookup resource owner")
		}
	}

	// issue tokens
	res, err := HandleTokenResponse(d, c, ro, r.Scope, true)
	if err != nil {
		return nil, err
	}

	// remove used refresh token
	err = d.RemoveRefreshToken(r.RefreshToken)
	if err != nil {
		return nil, oauth2.ServerError(oauth2.NoState, "Failed to remove refresh token")
	}

	return res, nil
}

func HandleTokenResponse(d Delegate, c Client, ro ResourceOwner, scope oauth2.Scope, issueRefreshToken bool) (*oauth2.TokenResponse, error) {
	// issue access token
	accessToken, expiresIn, err := d.IssueAccessToken(c, ro, scope)
	if err != nil {
		return nil, oauth2.ServerError(oauth2.NoState, "Failed to issue access token")
	}

	// prepare response
	res := bearer.NewTokenResponse(accessToken, expiresIn)

	// set granted scope
	res.Scope = scope

	// issue refresh token if available and implemented
	rtd, ok := d.(RefreshTokenDelegate)
	if ok && issueRefreshToken {
		refreshToken, err := rtd.IssueRefreshToken(c, ro, scope)
		if err != nil {
			return nil, oauth2.ServerError(oauth2.NoState, "Failed to issue refresh token")
		}

		// set refresh token
		res.RefreshToken = refreshToken
	}

	return res, nil
}
