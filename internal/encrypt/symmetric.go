package encrypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func (e *encryptor) DecryptMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var requestBody []byte
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		ciphertext, err := e.decryptAES(requestBody)
		if err != nil {
			http.Error(w, "Decryption failed", http.StatusInternalServerError)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(ciphertext))
		next.ServeHTTP(w, r)
	})
}


func (e *encryptor) EncryptAES(response []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.aesKey)
	if err != nil {
		return nil, err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := aead.Seal(nonce, nonce, response, nil)
	return ciphertext, nil
}

func (e *encryptor) decryptAES(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.aesKey)
	if err != nil {
		return nil, err
	}

	aead, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }

	nonceSize := aead.NonceSize()
    if len(ciphertext) < nonceSize {
        return nil, fmt.Errorf("empty ciphertext")
    }

	nonce, ciphertextActual := ciphertext[:nonceSize], ciphertext[nonceSize:]
    plaintext, err := aead.Open(nil, nonce, ciphertextActual, nil)
    if err != nil {
        return nil, err
    }

    return plaintext, nil
}