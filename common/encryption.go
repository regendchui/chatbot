package common

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"sync"
)

var phoneCipherOnce sync.Once
var phoneCipherAEAD cipher.AEAD
var phoneCipherInitErr error

func EncryptPhone(phone string) (string, error) {
	normalized := strings.TrimSpace(phone)
	if normalized == "" {
		return "", nil
	}

	key, err := loadPhoneEncryptionKey()
	if err != nil {
		return "", fmt.Errorf("load encryption key: %w", err)
	}

	aead, err := getPhoneCipher()
	if err != nil {
		return "", fmt.Errorf("get phone cipher: %w", err)
	}

	nonceSize := aead.NonceSize()
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(normalized))
	sum := mac.Sum(nil)
	if len(sum) < nonceSize {
		return "", fmt.Errorf("internal: nonce size %d exceeds HMAC output", nonceSize)
	}
	nonce := sum[:nonceSize]

	ciphertext := aead.Seal(nil, nonce, []byte(normalized), nil)
	payload := append(nonce, ciphertext...)
	return base64.StdEncoding.EncodeToString(payload), nil
}

func DecryptPhone(encrypted string) (string, error) {
	normalized := strings.TrimSpace(encrypted)
	if normalized == "" {
		return "", nil
	}

	aead, err := getPhoneCipher()
	if err != nil {
		return "", fmt.Errorf("get phone cipher: %w", err)
	}

	raw, err := base64.StdEncoding.DecodeString(normalized)
	if err != nil {
		return "", fmt.Errorf("decode encrypted phone: %w", err)
	}

	nonceSize := aead.NonceSize()
	if len(raw) < nonceSize {
		return "", fmt.Errorf("encrypted phone payload too short")
	}

	nonce := raw[:nonceSize]
	ciphertext := raw[nonceSize:]
	plain, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt phone: %w", err)
	}
	return string(plain), nil
}

func getPhoneCipher() (cipher.AEAD, error) {
	phoneCipherOnce.Do(func() {
		key, err := loadPhoneEncryptionKey()
		if err != nil {
			phoneCipherInitErr = err
			return
		}

		block, err := aes.NewCipher(key)
		if err != nil {
			phoneCipherInitErr = fmt.Errorf("create AES block: %w", err)
			return
		}

		phoneCipherAEAD, err = cipher.NewGCM(block)
		if err != nil {
			phoneCipherInitErr = fmt.Errorf("create GCM: %w", err)
			return
		}
	})

	if phoneCipherInitErr != nil {
		return nil, phoneCipherInitErr
	}
	return phoneCipherAEAD, nil
}

func loadPhoneEncryptionKey() ([]byte, error) {
	rawKey := strings.TrimSpace(os.Getenv("PHONE_ENCRYPTION_KEY"))
	if rawKey == "" {
		return nil, fmt.Errorf("PHONE_ENCRYPTION_KEY is required and must be 32 bytes (or base64 of 32 bytes)")
	}

	if decoded, err := base64.StdEncoding.DecodeString(rawKey); err == nil && len(decoded) == 32 {
		return decoded, nil
	}

	keyBytes := []byte(rawKey)
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("invalid key length %d, expected 32 bytes for AES-256", len(keyBytes))
	}
	return keyBytes, nil
}
