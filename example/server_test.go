package example

import (
	"testing"
	"time"

	"github.com/256dpi/oauth2"
	"github.com/256dpi/oauth2/hmacsha"
	"github.com/256dpi/oauth2/spec"
	"golang.org/x/crypto/bcrypt"
)

func TestSpec(t *testing.T) {
	addOwner(clients, &owner{
		id:           "client1",
		secret:       mustHash("foo"),
		redirectURI:  "http://example.com/callback1",
		confidential: true,
	})

	addOwner(clients, &owner{
		id:           "client2",
		secret:       mustHash("foo"),
		redirectURI:  "http://example.com/callback2",
		confidential: false,
	})

	addOwner(users, &owner{
		id:     "user1",
		secret: mustHash("foo"),
	})

	unknownToken := hmacsha.MustGenerate(secret, 16)
	validToken := hmacsha.MustGenerate(secret, 16)
	expiredToken := hmacsha.MustGenerate(secret, 16)
	insufficientToken := hmacsha.MustGenerate(secret, 16)

	addCredential(accessTokens, &credential{
		clientID:  "client1",
		signature: validToken.SignatureString(),
		scope:     allowedScope,
		expiresAt: time.Now().Add(time.Hour),
	})

	addCredential(accessTokens, &credential{
		clientID:  "client1",
		signature: expiredToken.SignatureString(),
		scope:     allowedScope,
		expiresAt: time.Now().Add(-time.Hour),
	})

	addCredential(accessTokens, &credential{
		clientID:  "client1",
		signature: insufficientToken.SignatureString(),
		scope:     oauth2.Scope{},
		expiresAt: time.Now().Add(time.Hour),
	})

	unknownRefreshToken := hmacsha.MustGenerate(secret, 16)
	validRefreshToken := hmacsha.MustGenerate(secret, 16)
	expiredRefreshToken := hmacsha.MustGenerate(secret, 16)

	addCredential(refreshTokens, &credential{
		clientID:  "client1",
		signature: validRefreshToken.SignatureString(),
		scope:     allowedScope,
		expiresAt: time.Now().Add(time.Hour),
	})

	addCredential(refreshTokens, &credential{
		clientID:  "client1",
		signature: expiredRefreshToken.SignatureString(),
		scope:     allowedScope,
		expiresAt: time.Now().Add(-time.Hour),
	})

	unknownAuthorizationCode := hmacsha.MustGenerate(secret, 16)
	expiredAuthorizationCode := hmacsha.MustGenerate(secret, 16)

	addCredential(authorizationCodes, &credential{
		clientID:  "client1",
		signature: expiredAuthorizationCode.SignatureString(),
		scope:     allowedScope,
		expiresAt: time.Now().Add(-time.Hour),
	})

	config := spec.Default(newHandler())

	config.RevocationEndpoint = "/oauth2/revoke"

	config.PasswordGrantSupport = true
	config.ClientCredentialsGrantSupport = true
	config.ImplicitGrantSupport = true
	config.AuthorizationCodeGrantSupport = true
	config.RefreshTokenGrantSupport = true

	config.ConfidentialClientID = "client1"
	config.ConfidentialClientSecret = "foo"
	config.PublicClientID = "client2"

	config.ResourceOwnerUsername = "user1"
	config.ResourceOwnerPassword = "foo"

	config.InvalidScope = "baz"
	config.ValidScope = "foo bar"
	config.ExceedingScope = "foo bar baz"

	config.ExpectedExpiresIn = int(tokenLifespan / time.Second)

	config.InvalidToken = "invalid"
	config.UnknownToken = unknownToken.String()
	config.ValidToken = validToken.String()
	config.ExpiredToken = expiredToken.String()
	config.InsufficientToken = insufficientToken.String()

	config.InvalidRedirectURI = "http://invalid.com"
	config.PrimaryRedirectURI = "http://example.com/callback1"
	config.SecondaryRedirectURI = "http://example.com/callback2"

	config.InvalidRefreshToken = "invalid"
	config.UnknownRefreshToken = unknownRefreshToken.String()
	config.ValidRefreshToken = validRefreshToken.String()
	config.ExpiredRefreshToken = expiredRefreshToken.String()

	config.InvalidAuthorizationCode = "invalid"
	config.UnknownAuthorizationCode = unknownAuthorizationCode.String()
	config.ExpiredAuthorizationCode = expiredAuthorizationCode.String()

	config.InvalidAuthorizationParams = map[string]string{
		"username": "user1",
		"password": "invalid",
	}

	config.ValidAuthorizationParams = map[string]string{
		"username": "user1",
		"password": "foo",
	}

	config.CodeReplayMitigation = true

	spec.Run(t, config)
}

func mustHash(password string) []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		panic(err)
	}

	return hash
}
