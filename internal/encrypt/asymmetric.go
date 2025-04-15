package encrypt

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
)

type encryptor struct {
	public 		*rsa.PublicKey
	private 	*rsa.PrivateKey
	aesKey 		[]byte
}

func NewEncryptor() (*encryptor, error) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &encryptor{
		public:  &private.PublicKey,
		private: private,
	}, nil
}
func (e *encryptor) GetPublicKey() (*pem.Block, error) {
	publicKeyBytes := x509.MarshalPKCS1PublicKey(e.public)
	pemBlock := &pem.Block{
        Type:  "RSA PUBLIC KEY",
        Bytes: publicKeyBytes,
	}
	return pemBlock, nil
}

func (e *encryptor) DecryptAESKey(encryptedKey string) (err error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedKey)
    if err != nil {
        return
    }
	e.aesKey, err = rsa.DecryptOAEP(sha256.New(), rand.Reader, e.private, ciphertext, []byte(""))
	return
}