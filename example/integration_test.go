package main

import (
	"testing"
	"time"

	"github.com/gonfire/oauth2/spec"
)

func TestSpec(t *testing.T) {
	addOwner(clients, owner{
		id:          "client1",
		secret:      mustHash("foo"),
		redirectURI: "http://example.com/callback",
	})

	addOwner(users, owner{
		id:     "user1",
		secret: mustHash("foo"),
	})

	invalidRefreshToken := mustGenerateToken()
	validRefreshToken := mustGenerateToken()

	addToken(refreshTokens, token{
		clientID:  "client1",
		signature: validRefreshToken.SignatureString(),
		scope:     allowedScope,
		expiresAt: time.Now().Add(time.Hour),
	})

	config := spec.Default(newHandler())

	config.PasswordGrant = true
	config.ClientCredentialsGrant = true
	config.ImplicitGrant = true
	config.AuthorizationCodeGrant = true
	config.RefreshTokenGrant = true

	config.ClientID = "client1"
	config.ClientSecret = "foo"
	config.OwnerUsername = "user1"
	config.OwnerPassword = "foo"
	config.InvalidScope = "baz"
	config.ValidScope = "foo bar"
	config.ExceedingScope = "foo bar baz"
	config.ExpectedExpireIn = int(tokenLifespan / time.Second)
	config.ValidRedirectURI = "http://example.com/callback"
	config.InvalidRefreshToken = invalidRefreshToken.String()
	config.ValidRefreshToken = validRefreshToken.String()

	config.TokenAuthorizationParams = map[string]string{
		"username": config.OwnerUsername,
		"password": config.OwnerPassword,
	}

	config.CodeAuthorizationParams = map[string]string{
		"username": config.OwnerUsername,
		"password": config.OwnerPassword,
	}

	spec.Run(t, config)
}
