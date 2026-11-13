package cryptoutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestNewPEM(t *testing.T) {
	tests := []struct {
		name    string
		keygen  func() ([]byte, error)
		keyType string
	}{
		{name: "RSA", keygen: NewRSAPEM, keyType: ssh.KeyAlgoRSA},
		{name: "Ed25519", keygen: NewEd25519PEM, keyType: ssh.KeyAlgoED25519},
		{name: "ECDSA", keygen: NewECDSAPEM, keyType: ssh.KeyAlgoECDSA256},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pem, err := test.keygen()
			require.NoError(t, err)
			signer, err := ssh.ParsePrivateKey(pem)
			require.NoError(t, err)
			assert.Equal(t, test.keyType, signer.PublicKey().Type())
		})
	}
}
