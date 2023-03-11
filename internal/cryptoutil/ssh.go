package cryptoutil

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"

	"github.com/pkg/errors"
)

type KeyAlgorithm string

const (
	KeyAlgorithmRSA     = "rsa"
	KeyAlgorithmEd25519 = "ed25519"
	KeyAlgorithmECDSA   = "ecdsa"
)

// NewRSAPEM creates a new RSA key in PEM format.
func NewRSAPEM() ([]byte, error) {
	private, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, err
	}

	err = private.Validate()
	if err != nil {
		return nil, err
	}
	return marshalToPEM(private)
}

// NewEd25519PEM creates a new ed25519 key in PEM format.
func NewEd25519PEM() ([]byte, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, errors.Wrap(err, "generate key")
	}
	return marshalToPEM(private)
}

// NewECDSAPEM creates a new ECDSA key in PEM format.
func NewECDSAPEM() ([]byte, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return marshalToPEM(private)
}

func marshalToPEM(key any) ([]byte, error) {
	data, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, errors.Wrap(err, "marshal private key")
	}

	return pem.EncodeToMemory(
		&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: data,
		},
	), nil
}
