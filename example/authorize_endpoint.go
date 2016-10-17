package main

import (
	"net/http"
	"time"

	"github.com/gonfire/oauth2"
)

func authorizeEndpoint(w http.ResponseWriter, r *http.Request) {
	// parse oauth2 authorization request
	req, err := oauth2.ParseAuthorizationRequest(r)
	if err != nil {
		oauth2.WriteError(w, err)
		return
	}

	// get client
	client, found := clients[req.ClientID]
	if !found {
		oauth2.WriteError(w, oauth2.InvalidClient(""))
		return
	}

	// validate redirect uri
	if client.redirectURI != req.RedirectURI {
		oauth2.WriteError(w, oauth2.InvalidRequest("Invalid redirect URI"))
		return
	}

	// show info notice on a GET request
	if r.Method == "GET" {
		w.Write([]byte("This authentication server does not provide an authorization form"))
		return
	}

	// triage grant type
	if req.ResponseType.Token() {
		handleImplicitGrant(w, r, req)
	} else if req.ResponseType.Code() {
		handleAuthorizationCodeGrantAuthorization(w, r, req)
	} else {
		oauth2.WriteError(w, oauth2.UnsupportedResponseType(""))
	}
}

func handleImplicitGrant(w http.ResponseWriter, r *http.Request, req *oauth2.AuthorizationRequest) {
	// check scope
	if !allowedScope.Includes(req.Scope) {
		oauth2.RedirectError(w, req.RedirectURI, true, oauth2.InvalidScope(""))
		return
	}

	// check user credentials
	owner, found := users[r.PostForm.Get("username")]
	if !found || !sameHash(owner.secret, r.PostForm.Get("password")) {
		oauth2.RedirectError(w, req.RedirectURI, true, oauth2.AccessDenied(""))
		return
	}

	// generate new access token
	accessToken, err := oauth2.GenerateToken(secret, 32)
	if err != nil {
		panic(err)
	}

	// prepare response
	res := oauth2.NewBearerTokenResponse(accessToken.String(), int(tokenLifespan/time.Second))

	// set granted scope
	res.Scope = req.Scope

	// set state
	res.State = req.State

	// save access token
	accessTokens[accessToken.SignatureString()] = token{
		clientID:  req.ClientID,
		username:  owner.id,
		signature: accessToken.SignatureString(),
		expiresAt: time.Now().Add(tokenLifespan),
		scope:     req.Scope,
	}

	// write response
	oauth2.RedirectTokenResponse(w, req.RedirectURI, res)
}

func handleAuthorizationCodeGrantAuthorization(w http.ResponseWriter, r *http.Request, req *oauth2.AuthorizationRequest) {
	// check scope
	if !allowedScope.Includes(req.Scope) {
		oauth2.RedirectError(w, req.RedirectURI, false, oauth2.InvalidScope(""))
		return
	}

	// check user credentials
	owner, found := users[r.PostForm.Get("username")]
	if !found || !sameHash(owner.secret, r.PostForm.Get("password")) {
		oauth2.RedirectError(w, req.RedirectURI, false, oauth2.AccessDenied(""))
		return
	}

	// generate new authorization code
	authorizationCode, err := oauth2.GenerateToken(secret, 32)
	if err != nil {
		panic(err)
	}

	// prepare response
	res := oauth2.NewCodeResponse(authorizationCode.String())

	// set state
	res.State = req.State

	// save authorization code
	authorizationCodes[authorizationCode.SignatureString()] = token{
		clientID:    req.ClientID,
		username:    owner.id,
		signature:   authorizationCode.SignatureString(),
		expiresAt:   time.Now().Add(authorizationCodeLifespan),
		scope:       req.Scope,
		redirectURI: req.RedirectURI,
	}

	// write response
	oauth2.RedirectCodeResponse(w, req.RedirectURI, res)
}
