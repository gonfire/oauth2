package server

import (
	"net/http"
	"time"

	"github.com/gonfire/oauth2"
	"github.com/gonfire/oauth2/bearer"
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
	authorizationCodes[authorizationCode.SignatureString()] = token{
		clientID:    r.ClientID,
		username:    owner.id,
		signature:   authorizationCode.SignatureString(),
		expiresAt:   time.Now().Add(authorizationCodeLifespan),
		scope:       r.Scope,
		redirectURI: r.RedirectURI,
	}

	// write response
	oauth2.RedirectCodeResponse(w, r.RedirectURI, res)
}

func tokenEndpoint(w http.ResponseWriter, r *http.Request) {
	// parse token request
	req, err := oauth2.ParseTokenRequest(r)
	if err != nil {
		oauth2.WriteError(w, err)
		return
	}

	// make sure the grant type is known
	if !oauth2.KnownGrantType(req.GrantType) {
		oauth2.WriteError(w, oauth2.InvalidRequest(req.State, "Unknown grant type"))
		return
	}

	// authenticate client
	client, found := clients[req.ClientID]
	if !found {
		oauth2.WriteError(w, oauth2.InvalidClient(req.State, "Unknown client"))
		return
	}

	// at this point the authentication server may check if the authenticated
	// client is public or confidential
	//
	// see: req.Confidential()

	// validate client
	if req.Confidential() && !sameHash(client.secret, req.ClientSecret) {
		oauth2.WriteError(w, oauth2.InvalidClient(req.State, "Unknown client"))
		return
	}

	// handle grant type
	switch req.GrantType {
	case oauth2.PasswordGrantType:
		handleResourceOwnerPasswordCredentialsGrant(w, req)
	case oauth2.ClientCredentialsGrantType:
		handleClientCredentialsGrant(w, req)
	case oauth2.AuthorizationCodeGrantType:
		handleAuthorizationCodeGrant(w, req)
	case oauth2.RefreshTokenGrantType:
		handleRefreshTokenGrant(w, req)
	}
}

func handleResourceOwnerPasswordCredentialsGrant(w http.ResponseWriter, r *oauth2.TokenRequest) {
	// authenticate resource owner
	owner, found := users[r.Username]
	if !found || !sameHash(owner.secret, r.Password) {
		oauth2.WriteError(w, oauth2.AccessDenied(r.State, oauth2.NoDescription))
		return
	}

	// check scope
	if !allowedScope.Includes(r.Scope) {
		oauth2.WriteError(w, oauth2.InvalidScope(r.State, oauth2.NoDescription))
		return
	}

	// issue tokens
	res := issueTokens(true, r.Scope, r.State, r.ClientID, r.Username)

	// write response
	oauth2.WriteTokenResponse(w, res)
}

func handleClientCredentialsGrant(w http.ResponseWriter, req *oauth2.TokenRequest) {
	// check scope
	if !allowedScope.Includes(req.Scope) {
		oauth2.WriteError(w, oauth2.InvalidScope(req.State, oauth2.NoDescription))
		return
	}

	// save tokens
	res := issueTokens(true, req.Scope, req.State, req.ClientID, "")

	// write response
	oauth2.WriteTokenResponse(w, res)
}

func handleAuthorizationCodeGrant(w http.ResponseWriter, r *oauth2.TokenRequest) {
	// parse authorization code
	authorizationCode, err := hmacsha.Parse(secret, r.Code)
	if err != nil {
		oauth2.WriteError(w, oauth2.InvalidRequest(r.State, err.Error()))
		return
	}

	// get stored authorization code by signature
	storedAuthorizationCode, found := authorizationCodes[authorizationCode.SignatureString()]
	if !found {
		oauth2.WriteError(w, oauth2.InvalidGrant(r.State, "Unknown authorization code"))
		return
	}

	// validate expiration
	if storedAuthorizationCode.expiresAt.Before(time.Now()) {
		oauth2.WriteError(w, oauth2.InvalidGrant(r.State, "Expired authorization code"))
		return
	}

	// validate ownership
	if storedAuthorizationCode.clientID != r.ClientID {
		oauth2.WriteError(w, oauth2.InvalidGrant(r.State, "Invalid authorization code ownership"))
		return
	}

	// validate redirect uri
	if storedAuthorizationCode.redirectURI != r.RedirectURI {
		oauth2.WriteError(w, oauth2.InvalidGrant(r.State, "Changed redirect uri"))
		return
	}

	// validate scope
	if !storedAuthorizationCode.scope.Includes(r.Scope) {
		oauth2.WriteError(w, oauth2.InvalidScope(r.State, "Scope exceeds the originally granted scope"))
		return
	}

	// issue tokens
	res := issueTokens(true, r.Scope, r.State, r.ClientID, storedAuthorizationCode.username)

	// delete used authorization code
	delete(authorizationCodes, authorizationCode.SignatureString())

	// write response
	oauth2.WriteTokenResponse(w, res)
}

func handleRefreshTokenGrant(w http.ResponseWriter, r *oauth2.TokenRequest) {
	// parse refresh token
	refreshToken, err := hmacsha.Parse(secret, r.RefreshToken)
	if err != nil {
		oauth2.WriteError(w, oauth2.InvalidRequest(r.State, err.Error()))
		return
	}

	// get stored refresh token by signature
	storedRefreshToken, found := refreshTokens[refreshToken.SignatureString()]
	if !found {
		oauth2.WriteError(w, oauth2.InvalidGrant(r.State, "Unknown refresh token"))
		return
	}

	// validate expiration
	if storedRefreshToken.expiresAt.Before(time.Now()) {
		oauth2.WriteError(w, oauth2.InvalidGrant(r.State, "Expired refresh token"))
		return
	}

	// validate ownership
	if storedRefreshToken.clientID != r.ClientID {
		oauth2.WriteError(w, oauth2.InvalidGrant(r.State, "Invalid refresh token ownership"))
		return
	}

	// inherit scope from stored refresh token
	if r.Scope.Empty() {
		r.Scope = storedRefreshToken.scope
	}

	// validate scope - a missing scope is always included
	if !storedRefreshToken.scope.Includes(r.Scope) {
		oauth2.WriteError(w, oauth2.InvalidScope(r.State, "Scope exceeds the originally granted scope"))
		return
	}

	// issue tokens
	res := issueTokens(true, r.Scope, r.State, r.ClientID, storedRefreshToken.username)

	// delete used refresh token
	delete(refreshTokens, refreshToken.SignatureString())

	// write response
	oauth2.WriteTokenResponse(w, res)
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
	accessTokens[accessToken.SignatureString()] = token{
		clientID:  clientID,
		username:  username,
		signature: accessToken.SignatureString(),
		expiresAt: time.Now().Add(tokenLifespan),
		scope:     scope,
	}

	// save refresh token if present
	if refreshToken != nil {
		refreshTokens[refreshToken.SignatureString()] = token{
			clientID:  clientID,
			username:  username,
			signature: refreshToken.SignatureString(),
			expiresAt: time.Now().Add(tokenLifespan),
			scope:     scope,
		}
	}

	return res
}
