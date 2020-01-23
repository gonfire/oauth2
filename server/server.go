// Package server provides a basic in-memory OAuth2 authentication server
// intended for testing purposes. The implementation may be used to as a
// reference or template to build a custom OAuth2 authentication server.
package server

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/256dpi/oauth2"
)

// Config is used to configure a server.
type Config struct {
	Secret                    []byte
	KeyLength                 int
	AllowedScope              oauth2.Scope
	AccessTokenLifespan       time.Duration
	RefreshTokenLifespan      time.Duration
	AuthorizationCodeLifespan time.Duration
}

// Default will return a default configuration.
func Default(secret []byte, allowed oauth2.Scope) Config {
	return Config{
		Secret:                    secret,
		KeyLength:                 16,
		AllowedScope:              allowed,
		AccessTokenLifespan:       time.Hour,
		RefreshTokenLifespan:      7 * 24 * time.Hour,
		AuthorizationCodeLifespan: 10 * time.Minute,
	}
}

// MustGenerate will generate a new token.
func (c Config) MustGenerate() *oauth2.HS256Token {
	return oauth2.MustGenerateHS256Token(c.Secret, c.KeyLength)
}

// Entity represents a client or resource owner.
type Entity struct {
	Secret       string
	RedirectURI  string
	Confidential bool
}

// Credential represents an access token, refresh token or authorization code.
type Credential struct {
	ClientID    string
	Username    string
	ExpiresAt   time.Time
	Scope       oauth2.Scope
	RedirectURI string
	Code        string
	Used        bool
}

// Server implements a basic in-memory OAuth2 authentication server intended for
// testing purposes.
type Server struct {
	Config             Config
	Clients            map[string]*Entity
	Users              map[string]*Entity
	AccessTokens       map[string]*Credential
	RefreshTokens      map[string]*Credential
	AuthorizationCodes map[string]*Credential
	Mutex              sync.Mutex
}

// New creates and returns a new server.
func New(config Config) *Server {
	return &Server{
		Config:             config,
		Clients:            map[string]*Entity{},
		Users:              map[string]*Entity{},
		AccessTokens:       map[string]*Credential{},
		RefreshTokens:      map[string]*Credential{},
		AuthorizationCodes: map[string]*Credential{},
	}
}

// Authorize will authorize the request and require a valid access token. An
// error has already be written to the client if false is returned.
func (s *Server) Authorize(w http.ResponseWriter, r *http.Request, required oauth2.Scope) bool {
	// acquire mutex
	s.Mutex.Lock()
	defer s.Mutex.Unlock()

	// parse bearer token
	tk, err := oauth2.ParseBearerToken(r)
	if err != nil {
		_ = oauth2.WriteBearerError(w, err)
		return false
	}

	// parse token
	token, err := oauth2.ParseHS256Token(s.Config.Secret, tk)
	if err != nil {
		_ = oauth2.WriteBearerError(w, oauth2.InvalidToken("malformed token"))
		return false
	}

	// get token
	accessToken, found := s.AccessTokens[token.SignatureString()]
	if !found {
		_ = oauth2.WriteBearerError(w, oauth2.InvalidToken("unknown token"))
		return false
	}

	// validate expiration
	if accessToken.ExpiresAt.Before(time.Now()) {
		_ = oauth2.WriteBearerError(w, oauth2.InvalidToken("expired token"))
		return false
	}

	// validate scope
	if !accessToken.Scope.Includes(required) {
		_ = oauth2.WriteBearerError(w, oauth2.InsufficientScope(required.String()))
		return false
	}

	return true
}

// ServeHTTP will handle the provided request based on the last path segment
// of the request URL.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// acquire mutex
	s.Mutex.Lock()
	defer s.Mutex.Unlock()

	// get path
	path := r.URL.Path

	// get latest path segment
	idx := strings.LastIndexByte(path, '/')
	if idx >= 0 {
		path = path[idx+1:]
	}

	// check path
	switch path {
	case "authorize":
		s.authorizationEndpoint(w, r)
	case "token":
		s.tokenEndpoint(w, r)
	case "introspect":
		s.introspectionEndpoint(w, r)
	case "revoke":
		s.revocationEndpoint(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) authorizationEndpoint(w http.ResponseWriter, r *http.Request) {
	// parse authorization request
	req, err := oauth2.ParseAuthorizationRequest(r)
	if err != nil {
		_ = oauth2.WriteError(w, err)
		return
	}

	// make sure the response type is known
	if !oauth2.KnownResponseType(req.ResponseType) {
		_ = oauth2.WriteError(w, oauth2.InvalidRequest("unknown response type"))
		return
	}

	// get client
	client, found := s.Clients[req.ClientID]
	if !found {
		_ = oauth2.WriteError(w, oauth2.InvalidClient("unknown client"))
		return
	}

	// validate redirect uri
	if client.RedirectURI != req.RedirectURI {
		_ = oauth2.WriteError(w, oauth2.InvalidRequest("invalid redirect URI"))
		return
	}

	// show notice for GET requests
	if r.Method == "GET" {
		_, _ = w.Write([]byte("This authentication server does not provide an authorization form.\n" +
			"Please submit the resource owners username and password in a POST request."))
		return
	}

	// read username and password
	username := r.PostForm.Get("username")
	password := r.PostForm.Get("password")

	// triage based on response type
	switch req.ResponseType {
	case oauth2.TokenResponseType:
		s.handleImplicitGrant(w, username, password, req)
	case oauth2.CodeResponseType:
		s.handleAuthorizationCodeGrantAuthorization(w, username, password, req)
	}
}

func (s *Server) handleImplicitGrant(w http.ResponseWriter, username, password string, rq *oauth2.AuthorizationRequest) {
	// validate scope
	if !s.Config.AllowedScope.Includes(rq.Scope) {
		_ = oauth2.WriteError(w, oauth2.InvalidScope("").SetRedirect(rq.RedirectURI, rq.State, true))
		return
	}

	// validate user credentials
	owner, found := s.Users[username]
	if !found || owner.Secret != password {
		_ = oauth2.WriteError(w, oauth2.AccessDenied("").SetRedirect(rq.RedirectURI, rq.State, true))
		return
	}

	// issue tokens
	r := s.issueTokens(false, rq.Scope, rq.ClientID, username, "")

	// redirect token
	r.SetRedirect(rq.RedirectURI, rq.State)

	// write response
	_ = oauth2.WriteTokenResponse(w, r)
}

func (s *Server) handleAuthorizationCodeGrantAuthorization(w http.ResponseWriter, username, password string, rq *oauth2.AuthorizationRequest) {
	// validate scope
	if !s.Config.AllowedScope.Includes(rq.Scope) {
		_ = oauth2.WriteError(w, oauth2.InvalidScope("").SetRedirect(rq.RedirectURI, rq.State, false))
		return
	}

	// validate user credentials
	owner, found := s.Users[username]
	if !found || owner.Secret != password {
		_ = oauth2.WriteError(w, oauth2.AccessDenied("").SetRedirect(rq.RedirectURI, rq.State, false))
		return
	}

	// generate new authorization code
	authorizationCode := s.Config.MustGenerate()

	// prepare response
	r := oauth2.NewCodeResponse(authorizationCode.String(), rq.RedirectURI, rq.State)

	// save authorization code
	s.AuthorizationCodes[authorizationCode.SignatureString()] = &Credential{
		ClientID:    rq.ClientID,
		Username:    username,
		ExpiresAt:   time.Now().Add(s.Config.AuthorizationCodeLifespan),
		Scope:       rq.Scope,
		RedirectURI: rq.RedirectURI,
	}

	// write response
	_ = oauth2.WriteCodeResponse(w, r)
}

func (s *Server) tokenEndpoint(w http.ResponseWriter, r *http.Request) {
	// parse token request
	req, err := oauth2.ParseTokenRequest(r)
	if err != nil {
		_ = oauth2.WriteError(w, err)
		return
	}

	// make sure the grant type is known
	if !oauth2.KnownGrantType(req.GrantType) {
		_ = oauth2.WriteError(w, oauth2.InvalidRequest("unknown grant type"))
		return
	}

	// find client
	client, found := s.Clients[req.ClientID]
	if !found {
		_ = oauth2.WriteError(w, oauth2.InvalidClient("unknown client"))
		return
	}

	// authenticate client
	if client.Confidential && client.Secret != req.ClientSecret {
		_ = oauth2.WriteError(w, oauth2.InvalidClient("unknown client"))
		return
	}

	// handle grant type
	switch req.GrantType {
	case oauth2.PasswordGrantType:
		s.handleResourceOwnerPasswordCredentialsGrant(w, req)
	case oauth2.ClientCredentialsGrantType:
		s.handleClientCredentialsGrant(w, req)
	case oauth2.AuthorizationCodeGrantType:
		s.handleAuthorizationCodeGrant(w, req)
	case oauth2.RefreshTokenGrantType:
		s.handleRefreshTokenGrant(w, req)
	}
}

func (s *Server) handleResourceOwnerPasswordCredentialsGrant(w http.ResponseWriter, rq *oauth2.TokenRequest) {
	// authenticate resource owner
	owner, found := s.Users[rq.Username]
	if !found || owner.Secret != rq.Password {
		_ = oauth2.WriteError(w, oauth2.AccessDenied(""))
		return
	}

	// check scope
	if !s.Config.AllowedScope.Includes(rq.Scope) {
		_ = oauth2.WriteError(w, oauth2.InvalidScope(""))
		return
	}

	// issue tokens
	r := s.issueTokens(true, rq.Scope, rq.ClientID, rq.Username, "")

	// write response
	_ = oauth2.WriteTokenResponse(w, r)
}

func (s *Server) handleClientCredentialsGrant(w http.ResponseWriter, rq *oauth2.TokenRequest) {
	// check client confidentiality
	if !s.Clients[rq.ClientID].Confidential {
		_ = oauth2.WriteError(w, oauth2.InvalidClient("unknown client"))
		return
	}

	// check scope
	if !s.Config.AllowedScope.Includes(rq.Scope) {
		_ = oauth2.WriteError(w, oauth2.InvalidScope(""))
		return
	}

	// save tokens
	r := s.issueTokens(true, rq.Scope, rq.ClientID, "", "")

	// write response
	_ = oauth2.WriteTokenResponse(w, r)
}

func (s *Server) handleAuthorizationCodeGrant(w http.ResponseWriter, rq *oauth2.TokenRequest) {
	// parse authorization code
	authorizationCode, err := oauth2.ParseHS256Token(s.Config.Secret, rq.Code)
	if err != nil {
		_ = oauth2.WriteError(w, oauth2.InvalidRequest(err.Error()))
		return
	}

	// get stored authorization code by signature
	storedAuthorizationCode, found := s.AuthorizationCodes[authorizationCode.SignatureString()]
	if !found {
		_ = oauth2.WriteError(w, oauth2.InvalidGrant("unknown authorization code"))
		return
	}

	// check if used
	if storedAuthorizationCode.Used {
		// revoke all access tokens
		for key, token := range s.AccessTokens {
			if token.Code == authorizationCode.SignatureString() {
				delete(s.AccessTokens, key)
			}
		}

		// revoke all refresh tokens
		for key, token := range s.RefreshTokens {
			if token.Code == authorizationCode.SignatureString() {
				delete(s.RefreshTokens, key)
			}
		}

		_ = oauth2.WriteError(w, oauth2.InvalidGrant("unknown authorization code"))
		return
	}

	// validate expiration
	if storedAuthorizationCode.ExpiresAt.Before(time.Now()) {
		_ = oauth2.WriteError(w, oauth2.InvalidGrant("expired authorization code"))
		return
	}

	// validate ownership
	if storedAuthorizationCode.ClientID != rq.ClientID {
		_ = oauth2.WriteError(w, oauth2.InvalidGrant("invalid authorization code ownership"))
		return
	}

	// validate redirect uri
	if storedAuthorizationCode.RedirectURI != rq.RedirectURI {
		_ = oauth2.WriteError(w, oauth2.InvalidGrant("changed redirect uri"))
		return
	}

	// issue tokens
	r := s.issueTokens(true, storedAuthorizationCode.Scope, rq.ClientID, storedAuthorizationCode.Username, authorizationCode.SignatureString())

	// mark authorization code
	storedAuthorizationCode.Used = true

	// write response
	_ = oauth2.WriteTokenResponse(w, r)
}

func (s *Server) handleRefreshTokenGrant(w http.ResponseWriter, rq *oauth2.TokenRequest) {
	// parse refresh token
	refreshToken, err := oauth2.ParseHS256Token(s.Config.Secret, rq.RefreshToken)
	if err != nil {
		_ = oauth2.WriteError(w, oauth2.InvalidRequest(err.Error()))
		return
	}

	// get stored refresh token by signature
	storedRefreshToken, found := s.RefreshTokens[refreshToken.SignatureString()]
	if !found {
		_ = oauth2.WriteError(w, oauth2.InvalidGrant("unknown refresh token"))
		return
	}

	// validate expiration
	if storedRefreshToken.ExpiresAt.Before(time.Now()) {
		_ = oauth2.WriteError(w, oauth2.InvalidGrant("expired refresh token"))
		return
	}

	// validate ownership
	if storedRefreshToken.ClientID != rq.ClientID {
		_ = oauth2.WriteError(w, oauth2.InvalidGrant("invalid refresh token ownership"))
		return
	}

	// inherit scope from stored refresh token
	if rq.Scope.Empty() {
		rq.Scope = storedRefreshToken.Scope
	}

	// validate scope - a missing scope is always included
	if !storedRefreshToken.Scope.Includes(rq.Scope) {
		_ = oauth2.WriteError(w, oauth2.InvalidScope("scope exceeds the originally granted scope"))
		return
	}

	// issue tokens
	r := s.issueTokens(true, rq.Scope, rq.ClientID, storedRefreshToken.Username, "")

	// delete used refresh token
	delete(s.RefreshTokens, refreshToken.SignatureString())

	// write response
	_ = oauth2.WriteTokenResponse(w, r)
}

func (s *Server) revocationEndpoint(w http.ResponseWriter, r *http.Request) {
	// parse authorization request
	req, err := oauth2.ParseRevocationRequest(r)
	if err != nil {
		_ = oauth2.WriteError(w, err)
		return
	}

	// check token type hint
	if req.TokenTypeHint != "" && !oauth2.KnownTokenType(req.TokenTypeHint) {
		_ = oauth2.WriteError(w, oauth2.UnsupportedTokenType(""))
		return
	}

	// get client
	client, found := s.Clients[req.ClientID]
	if !found {
		_ = oauth2.WriteError(w, oauth2.InvalidClient("unknown client"))
		return
	}

	// authenticate client
	if client.Confidential && client.Secret != req.ClientSecret {
		_ = oauth2.WriteError(w, oauth2.InvalidClient("unknown client"))
		return
	}

	// parse token
	token, err := oauth2.ParseHS256Token(s.Config.Secret, req.Token)
	if err != nil {
		_ = oauth2.WriteError(w, oauth2.InvalidRequest(err.Error()))
		return
	}

	// check access token
	if accessToken, found := s.AccessTokens[token.SignatureString()]; found {
		// check owner
		if accessToken.ClientID != req.ClientID {
			_ = oauth2.WriteError(w, oauth2.InvalidClient("wrong client"))
			return
		}

		// revoke token
		s.revokeToken(req.ClientID, s.AccessTokens, token.SignatureString())
	}

	// check refresh token
	if refreshToken, found := s.RefreshTokens[token.SignatureString()]; found {
		// check owner
		if refreshToken.ClientID != req.ClientID {
			_ = oauth2.WriteError(w, oauth2.InvalidClient("wrong client"))
			return
		}

		// revoke token
		s.revokeToken(req.ClientID, s.RefreshTokens, token.SignatureString())
	}

	// write header
	w.WriteHeader(http.StatusOK)
}

func (s *Server) introspectionEndpoint(w http.ResponseWriter, r *http.Request) {
	// parse authorization request
	req, err := oauth2.ParseIntrospectionRequest(r)
	if err != nil {
		_ = oauth2.WriteError(w, err)
		return
	}

	// check token type hint
	if req.TokenTypeHint != "" && !oauth2.KnownTokenType(req.TokenTypeHint) {
		_ = oauth2.WriteError(w, oauth2.UnsupportedTokenType(""))
		return
	}

	// get client
	client, found := s.Clients[req.ClientID]
	if !found {
		_ = oauth2.WriteError(w, oauth2.InvalidClient("unknown client"))
		return
	}

	// authenticate client
	if client.Confidential && client.Secret != req.ClientSecret {
		_ = oauth2.WriteError(w, oauth2.InvalidClient("unknown client"))
		return
	}

	// parse token
	token, err := oauth2.ParseHS256Token(s.Config.Secret, req.Token)
	if err != nil {
		_ = oauth2.WriteError(w, oauth2.InvalidRequest(err.Error()))
		return
	}

	// prepare response
	res := &oauth2.IntrospectionResponse{}

	// check access token
	if accessToken, found := s.AccessTokens[token.SignatureString()]; found {
		// check owner
		if accessToken.ClientID != req.ClientID {
			_ = oauth2.WriteError(w, oauth2.InvalidClient("wrong client"))
			return
		}

		// set response
		res.Active = true
		res.Scope = accessToken.Scope.String()
		res.ClientID = accessToken.ClientID
		res.Username = accessToken.Username
		res.TokenType = oauth2.AccessToken
		res.ExpiresAt = accessToken.ExpiresAt.Unix()
	}

	// check refresh token
	if refreshToken, found := s.RefreshTokens[token.SignatureString()]; found {
		// check owner
		if refreshToken.ClientID != req.ClientID {
			_ = oauth2.WriteError(w, oauth2.InvalidClient("wrong client"))
			return
		}

		// set response
		res.Active = true
		res.Scope = refreshToken.Scope.String()
		res.ClientID = refreshToken.ClientID
		res.Username = refreshToken.Username
		res.TokenType = oauth2.RefreshToken
		res.ExpiresAt = refreshToken.ExpiresAt.Unix()
	}

	// write response
	_ = oauth2.WriteIntrospectionResponse(w, res)
}

func (s *Server) issueTokens(issueRefreshToken bool, scope oauth2.Scope, clientID, username, code string) *oauth2.TokenResponse {
	// generate access token
	accessToken := s.Config.MustGenerate()

	// generate refresh token if requested
	var refreshToken *oauth2.HS256Token
	if issueRefreshToken {
		refreshToken = s.Config.MustGenerate()
	}

	// prepare response
	r := oauth2.NewBearerTokenResponse(accessToken.String(), int(s.Config.AccessTokenLifespan/time.Second))

	// set granted scope
	r.Scope = scope

	// set refresh token if available
	if refreshToken != nil {
		r.RefreshToken = refreshToken.String()
	}

	// save access token
	s.AccessTokens[accessToken.SignatureString()] = &Credential{
		ClientID:  clientID,
		Username:  username,
		ExpiresAt: time.Now().Add(s.Config.AccessTokenLifespan),
		Scope:     scope,
		Code:      code,
	}

	// save refresh token if available
	if refreshToken != nil {
		s.RefreshTokens[refreshToken.SignatureString()] = &Credential{
			ClientID:  clientID,
			Username:  username,
			ExpiresAt: time.Now().Add(s.Config.RefreshTokenLifespan),
			Scope:     scope,
			Code:      code,
		}
	}

	return r
}

func (s *Server) revokeToken(clientID string, list map[string]*Credential, signature string) {
	// get token
	token, ok := list[signature]
	if !ok {
		return
	}

	// check client id
	if token.ClientID != clientID {
		return
	}

	// remove token
	delete(list, signature)
}