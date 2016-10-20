package server

import (
	"net/http"
	"time"

	"github.com/gonfire/oauth2"
	"github.com/gonfire/oauth2/bearer"
	"github.com/gonfire/oauth2/delegate"
	"github.com/gonfire/oauth2/hmacsha"
)

func authorizationEndpoint(w http.ResponseWriter, r *http.Request) {
	// parse authorization request
	req, err := oauth2.ParseAuthorizationRequest(r)
	if err != nil {
		oauth2.WriteError(w, err)
		return
	}

	// make sure the response type is known
	if !oauth2.KnownResponseType(req.ResponseType) {
		oauth2.WriteError(w, oauth2.InvalidRequest(req.State, "Unknown response type"))
		return
	}

	// get client
	client, found := clients[req.ClientID]
	if !found {
		oauth2.WriteError(w, oauth2.InvalidClient(req.State, "Unknown client"))
		return
	}

	// validate redirect uri
	if client.redirectURI != req.RedirectURI {
		oauth2.WriteError(w, oauth2.InvalidRequest(req.State, "Invalid redirect URI"))
		return
	}

	// show info notice on a GET request
	if r.Method == "GET" {
		w.Write([]byte("This authentication server does not provide an authorization form."))
		return
	}

	// triage based on response type
	switch req.ResponseType {
	case oauth2.TokenResponseType:
		handleImplicitGrant(w, req)
	case oauth2.CodeResponseType:
		handleAuthorizationCodeGrantAuthorization(w, req)
	}
}

func handleImplicitGrant(w http.ResponseWriter, r *oauth2.AuthorizationRequest) {
	// validate scope
	if !allowedScope.Includes(r.Scope) {
		oauth2.RedirectError(w, r.RedirectURI, true, oauth2.InvalidScope(r.State, oauth2.NoDescription))
		return
	}

	// validate user credentials
	owner, found := users[r.HTTP.PostForm.Get("username")]
	if !found || !sameHash(owner.secret, r.HTTP.PostForm.Get("password")) {
		oauth2.RedirectError(w, r.RedirectURI, true, oauth2.AccessDenied(r.State, oauth2.NoDescription))
		return
	}

	// issue tokens
	res := issueTokens(false, r.Scope, r.State, r.ClientID, owner.id)

	// write response
	oauth2.RedirectTokenResponse(w, r.RedirectURI, res)
}

func handleAuthorizationCodeGrantAuthorization(w http.ResponseWriter, r *oauth2.AuthorizationRequest) {
	// validate scope
	if !allowedScope.Includes(r.Scope) {
		oauth2.RedirectError(w, r.RedirectURI, false, oauth2.InvalidScope(r.State, oauth2.NoDescription))
		return
	}

	// validate user credentials
	owner, found := users[r.HTTP.PostForm.Get("username")]
	if !found || !sameHash(owner.secret, r.HTTP.PostForm.Get("password")) {
		oauth2.RedirectError(w, r.RedirectURI, false, oauth2.AccessDenied(r.State, oauth2.NoDescription))
		return
	}

	// generate new authorization code
	authorizationCode := hmacsha.MustGenerate(secret, 32)

	// prepare response
	res := oauth2.NewCodeResponse(authorizationCode.String())

	// set state
	res.State = r.State

	// save authorization code
	authorizationCodes[authorizationCode.SignatureString()] = &token{
		clientID:        r.ClientID,
		resourceOwnerID: owner.id,
		signature:       authorizationCode.SignatureString(),
		expiresAt:       time.Now().Add(authorizationCodeLifespan),
		scope:           r.Scope,
		redirectURI:     r.RedirectURI,
	}

	// write response
	oauth2.RedirectCodeResponse(w, r.RedirectURI, res)
}

func tokenEndpoint(d *Delegate) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// pre process token request
		tr, c, err := delegate.TokenRequest(d, r)
		if err != nil {
			oauth2.WriteError(w, err)
			return
		}

		// handle grant type
		switch tr.GrantType {
		case oauth2.PasswordGrantType:
			// handle resource owner password credentials grant
			res, err := delegate.PasswordGrant(d, c, tr)
			if err != nil {
				oauth2.WriteError(w, err)
				return
			}

			// write response
			oauth2.WriteTokenResponse(w, res)
		case oauth2.ClientCredentialsGrantType:
			// handle client credentials grant
			res, err := delegate.ClientCredentialsGrant(d, c, tr)
			if err != nil {
				oauth2.WriteError(w, err)
				return
			}

			// write response
			oauth2.WriteTokenResponse(w, res)
		case oauth2.AuthorizationCodeGrantType:
			// handle client credentials grant
			res, err := delegate.AuthorizationCodeGrant(d, c, tr)
			if err != nil {
				oauth2.WriteError(w, err)
				return
			}

			// write response
			oauth2.WriteTokenResponse(w, res)
		case oauth2.RefreshTokenGrantType:
			// handle client credentials grant
			res, err := delegate.RefreshTokenGrant(d, c, tr)
			if err != nil {
				oauth2.WriteError(w, err)
				return
			}

			// write response
			oauth2.WriteTokenResponse(w, res)
		}
	}
}

func issueTokens(issueRefreshToken bool, scope oauth2.Scope, state, clientID, username string) *oauth2.TokenResponse {
	// generate new access token
	accessToken := hmacsha.MustGenerate(secret, 32)

	// generate new refresh token
	refreshToken := hmacsha.MustGenerate(secret, 32)

	// prepare response
	res := bearer.NewTokenResponse(accessToken.String(), int(tokenLifespan/time.Second))

	// set granted scope
	res.Scope = scope

	// set state
	res.State = state

	// set refresh token
	res.RefreshToken = refreshToken.String()

	// disable refresh token if not requested
	if !issueRefreshToken {
		refreshToken = nil
	}

	// save access token
	accessTokens[accessToken.SignatureString()] = &token{
		clientID:        clientID,
		resourceOwnerID: username,
		signature:       accessToken.SignatureString(),
		expiresAt:       time.Now().Add(tokenLifespan),
		scope:           scope,
	}

	// save refresh token if present
	if refreshToken != nil {
		refreshTokens[refreshToken.SignatureString()] = &token{
			clientID:        clientID,
			resourceOwnerID: username,
			signature:       refreshToken.SignatureString(),
			expiresAt:       time.Now().Add(tokenLifespan),
			scope:           scope,
		}
	}

	return res
}
