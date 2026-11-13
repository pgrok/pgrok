package oidcprovider

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"

	"github.com/go-jose/go-jose/v4"
	"github.com/pkg/errors"
	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/pgrok/pgrok/internal/database"
)

// accessTokenKeyID is the ID of the key that encrypts opaque access tokens.
// Using a fixed ID makes concurrent first starts agree on one key.
const accessTokenKeyID = "access-token"

type signingKey struct {
	id  string
	key *rsa.PrivateKey
}

var _ op.SigningKey = (*signingKey)(nil)

func (k *signingKey) SignatureAlgorithm() jose.SignatureAlgorithm { return jose.RS256 }
func (k *signingKey) Key() any                                    { return k.key }
func (k *signingKey) ID() string                                  { return k.id }

// publicKey is the public half of a signing key, published in the JWK set.
type publicKey struct {
	*signingKey
}

var _ op.Key = publicKey{}

func (k publicKey) Algorithm() jose.SignatureAlgorithm { return jose.RS256 }
func (k publicKey) Use() string                        { return "sig" }
func (k publicKey) Key() any                           { return &k.key.PublicKey }

// loadKeys returns the signing keys, newest first, and the key that encrypts
// access tokens, creating them on first use.
func loadKeys(ctx context.Context, db *database.DB) (signingKeys []*signingKey, accessTokenKey [32]byte, _ error) {
	keys, err := db.ListOIDCKeys(ctx, "sig")
	if err != nil {
		return nil, accessTokenKey, errors.Wrap(err, "list signing keys")
	}
	if len(keys) == 0 {
		key, err := newSigningKey()
		if err != nil {
			return nil, accessTokenKey, errors.Wrap(err, "generate signing key")
		}
		if err = db.CreateOIDCKeyIfNotExists(ctx, key); err != nil {
			return nil, accessTokenKey, errors.Wrap(err, "create signing key")
		}
		// List again to include keys created by instances starting concurrently.
		keys, err = db.ListOIDCKeys(ctx, "sig")
		if err != nil {
			return nil, accessTokenKey, errors.Wrap(err, "list signing keys")
		}
	}
	for _, key := range keys {
		block, _ := pem.Decode(key.Material)
		if block == nil {
			return nil, accessTokenKey, errors.Errorf("signing key %q: invalid PEM", key.ID)
		}
		private, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, accessTokenKey, errors.Wrapf(err, "signing key %q", key.ID)
		}
		signingKeys = append(signingKeys, &signingKey{id: key.ID, key: private})
	}

	var material [32]byte
	if _, err = rand.Read(material[:]); err != nil {
		return nil, accessTokenKey, errors.Wrap(err, "generate access token key")
	}
	err = db.CreateOIDCKeyIfNotExists(ctx, &database.OIDCKey{
		ID:        accessTokenKeyID,
		Use:       "enc",
		Algorithm: "A256GCM",
		Material:  material[:],
	})
	if err != nil {
		return nil, accessTokenKey, errors.Wrap(err, "create access token key")
	}
	keys, err = db.ListOIDCKeys(ctx, "enc")
	if err != nil {
		return nil, accessTokenKey, errors.Wrap(err, "list encryption keys")
	}
	for _, key := range keys {
		if key.ID == accessTokenKeyID && len(key.Material) == len(accessTokenKey) {
			copy(accessTokenKey[:], key.Material)
			return signingKeys, accessTokenKey, nil
		}
	}
	return nil, accessTokenKey, errors.New("access token key not found")
}

func newSigningKey() (*database.OIDCKey, error) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	id := make([]byte, 8)
	if _, err = rand.Read(id); err != nil {
		return nil, err
	}
	return &database.OIDCKey{
		ID:        hex.EncodeToString(id),
		Use:       "sig",
		Algorithm: string(jose.RS256),
		Material: pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(private),
		}),
	}, nil
}
