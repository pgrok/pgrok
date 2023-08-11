package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/charmbracelet/log"
	"github.com/flamego/flamego"
	"github.com/golang-jwt/jwt/v4"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

func main() {
	port := flag.Int("port", 9833, "The port to listen on")
	flag.Parse()

	issuer := fmt.Sprintf("http://localhost:%d", *port)

	f := flamego.New()
	f.Get("/.well-known/openid-configuration", func() string {
		return fmt.Sprintf(`
{
  "issuer": "%[1]s",
  "authorization_endpoint": "%[1]s/oauth2/authorize",
  "token_endpoint": "%[1]s/oauth2/token",
  "userinfo_endpoint": "%[1]s/oauth2/userinfo",
  "jwks_uri": "%[1]s/oauth/discovery/keys",
  "id_token_signing_alg_values_supported": [
    "RS256"
  ],
  "scopes_supported": [
    "openid",
    "profile",
    "email"
  ],
  "token_endpoint_auth_methods_supported": [
    "client_secret_post"
  ],
  "claims_supported": [
    "sub",
    "iss",
    "aud",
    "exp",
    "iat",
    "nonce"
  ]
}`, issuer)
	})

	var audience, nonce string
	f.Get("/oauth2/authorize", func(c flamego.Context) {
		redirectURI := c.Query("redirect_uri")
		state := c.Query("state")
		audience = c.Query("client_id")
		nonce = c.Query("nonce")
		c.Redirect(fmt.Sprintf("%s?state=%s&code=masterpiece", redirectURI, state))
	})

	rs256, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal("Failed to generate RSA key", "error", err)
	}
	f.Post("/oauth2/token", func(w http.ResponseWriter) error {
		token := jwt.NewWithClaims(
			jwt.SigningMethodRS256,
			jwt.MapClaims{
				"iss":   issuer,
				"sub":   "unknwon",
				"aud":   audience,
				"exp":   time.Now().Add(time.Hour).Unix(),
				"iat":   time.Now().Unix(),
				"nonce": nonce,
			},
		)
		rawIDToken, err := token.SignedString(rs256)
		if err != nil {
			return err
		}

		w.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "dc101770-30fc-42c1-9bc9-d6adc81253e8",
			"token_type":    "Bearer",
			"refresh_token": "0c1e40d5-e537-4b60-82e7-d21bba5ac6ca",
			"expires_in":    3600,
			"id_token":      rawIDToken,
		})
	})
	f.Get("/oauth/discovery/keys", func(w http.ResponseWriter) error {
		key, err := jwk.FromRaw(rs256.PublicKey)
		if err != nil {
			return err
		}

		err = key.Set(jwk.KeyUsageKey, "sig")
		if err != nil {
			return err
		}
		err = key.Set(jwk.AlgorithmKey, "RS256")
		if err != nil {
			return err
		}
		marshalledKey, err := json.Marshal(key)
		if err != nil {
			return err
		}

		w.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(w).Encode(map[string]any{
			"keys": []json.RawMessage{marshalledKey},
		})
	})

	f.Get("/oauth2/userinfo", func(w http.ResponseWriter) error {
		w.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(w).Encode(map[string]any{
			"sub":   "unknwon",
			"name":  "Joe Chen",
			"email": "unknwon@example.com",
		})
	})
	f.Run(*port)
}
