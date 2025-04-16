package encrypt

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
)

type encryptor struct {
	public 		*rsa.PublicKey
	private 	*rsa.PrivateKey
	aesKey 		[]byte
}

func NewEncryptor() (*encryptor, error) {
	private, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return nil, err
	}
	return &encryptor{
		public:  &private.PublicKey,
		private: private,
	}, nil
}

func (e *encryptor) GetPublicKey() (*pem.Block, error) {
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(e.public)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}
	pemBlock := &pem.Block{
        Type:  "RSA PUBLIC KEY",
        Bytes: publicKeyBytes,
	}
	return pemBlock, nil
}

func (e *encryptor) DecryptAESKey(encryptedKey string) error {
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedKey)
    if err != nil {
        return fmt.Errorf("failed to decode base64: %w", err)
    }
	e.aesKey, err = rsa.DecryptOAEP(sha256.New(), rand.Reader, e.private, ciphertext, []byte(""))
	return err
}