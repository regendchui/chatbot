package main // Use main package so encryption helpers are shared across app files.

import ( // Import cryptography and encoding packages.
	"crypto/aes"      // Provide AES block cipher implementation.
	"crypto/cipher"   // Provide AEAD interface used by GCM mode.
	"crypto/hmac"     // Derive deterministic nonce per phone for stable DB lookups.
	"crypto/sha256"   // HMAC-SHA256 for nonce derivation.
	"encoding/base64" // Encode encrypted bytes into DB-safe text.
	"fmt"             // Build wrapped errors for easier debugging.
	"os"              // Read encryption key from environment variables.
	"strings"         // Normalize key and phone input values.
	"sync"            // Initialize cipher once and reuse it safely.
) // End import block.

var phoneCipherOnce sync.Once   // Ensure cipher initialization runs only one time.
var phoneCipherAEAD cipher.AEAD // Cache initialized AES-GCM AEAD instance.
var phoneCipherInitErr error    // Cache initialization error for consistent behavior.

func EncryptPhone(phone string) (string, error) { // Encrypt phone number before writing to DB.
	normalized := strings.TrimSpace(phone) // Normalize whitespace from phone input.
	if normalized == "" {                  // Handle empty phone values safely.
		return "", nil // Return empty output for empty input.
	}

	key, err := loadPhoneEncryptionKey() // Same key as AES; needed for deterministic nonce.
	if err != nil {
		return "", fmt.Errorf("load encryption key: %w", err)
	}

	aead, err := getPhoneCipher() // Load initialized AES-GCM cipher instance.
	if err != nil {               // Check key/cipher initialization failure.
		return "", fmt.Errorf("get phone cipher: %w", err) // Return wrapped error to caller.
	}

	// Deterministic nonce so the same phone always maps to the same ciphertext (UNIQUE meta + lookups work).
	nonceSize := aead.NonceSize()
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(normalized))
	sum := mac.Sum(nil)
	if len(sum) < nonceSize {
		return "", fmt.Errorf("internal: nonce size %d exceeds HMAC output", nonceSize)
	}
	nonce := sum[:nonceSize]

	ciphertext := aead.Seal(nil, nonce, []byte(normalized), nil) // Encrypt normalized phone bytes.
	payload := append(nonce, ciphertext...)                      // Prefix nonce to ciphertext for later decryption.
	encoded := base64.StdEncoding.EncodeToString(payload)        // Encode payload as base64 text for DB.
	return encoded, nil                                          // Return encrypted base64 string.
} // End EncryptPhone function.

func DecryptPhone(encrypted string) (string, error) { // Decrypt DB value back to original phone when needed.
	normalized := strings.TrimSpace(encrypted) // Normalize encrypted input.
	if normalized == "" {                      // Handle empty encrypted value safely.
		return "", nil // Return empty output for empty input.
	}

	aead, err := getPhoneCipher() // Load initialized AES-GCM cipher instance.
	if err != nil {               // Check key/cipher initialization failure.
		return "", fmt.Errorf("get phone cipher: %w", err) // Return wrapped initialization error.
	}

	raw, err := base64.StdEncoding.DecodeString(normalized) // Decode base64 payload into raw bytes.
	if err != nil {                                         // Check decode failure.
		return "", fmt.Errorf("decode encrypted phone: %w", err) // Return wrapped decode error.
	}

	nonceSize := aead.NonceSize() // Read expected nonce size from AEAD instance.
	if len(raw) < nonceSize {     // Validate payload has at least nonce bytes.
		return "", fmt.Errorf("encrypted phone payload too short") // Return explicit payload format error.
	}

	nonce := raw[:nonceSize]                             // Split nonce portion from raw payload.
	ciphertext := raw[nonceSize:]                        // Split ciphertext portion from raw payload.
	plain, err := aead.Open(nil, nonce, ciphertext, nil) // Decrypt ciphertext with nonce.
	if err != nil {                                      // Check authentication/decryption failure.
		return "", fmt.Errorf("decrypt phone: %w", err) // Return wrapped decrypt error.
	}

	return string(plain), nil // Return decrypted phone number.
} // End DecryptPhone function.

func getPhoneCipher() (cipher.AEAD, error) { // Initialize or return cached AES-GCM cipher instance.
	phoneCipherOnce.Do(func() { // Run initialization block exactly once.
		key, err := loadPhoneEncryptionKey() // Read and validate key from environment.
		if err != nil {                      // Check key loading failure.
			phoneCipherInitErr = err // Cache initialization error for subsequent calls.
			return                   // Stop initialization when key is invalid.
		}

		block, err := aes.NewCipher(key) // Create AES block cipher from validated key.
		if err != nil {                  // Check AES block initialization failure.
			phoneCipherInitErr = fmt.Errorf("create AES block: %w", err) // Cache wrapped initialization error.
			return                                                       // Stop initialization when block creation fails.
		}

		phoneCipherAEAD, err = cipher.NewGCM(block) // Wrap AES block in GCM mode for authenticated encryption.
		if err != nil {                             // Check GCM creation failure.
			phoneCipherInitErr = fmt.Errorf("create GCM: %w", err) // Cache wrapped GCM initialization error.
			return                                                 // Stop initialization when GCM setup fails.
		}
	})

	if phoneCipherInitErr != nil { // Return initialization error when present.
		return nil, phoneCipherInitErr // Return cached initialization failure.
	}
	return phoneCipherAEAD, nil // Return initialized AEAD cipher instance.
} // End getPhoneCipher function.

func loadPhoneEncryptionKey() ([]byte, error) { // Read encryption key from env and validate format.
	rawKey := strings.TrimSpace(os.Getenv("PHONE_ENCRYPTION_KEY")) // Read key text from environment.
	if rawKey == "" {                                              // Check missing key.
		return nil, fmt.Errorf("PHONE_ENCRYPTION_KEY is required and must be 32 bytes (or base64 of 32 bytes)") // Return clear configuration error.
	}

	if decoded, err := base64.StdEncoding.DecodeString(rawKey); err == nil { // Try treating env value as base64 text.
		if len(decoded) == 32 { // Check decoded key length for AES-256.
			return decoded, nil // Return decoded key bytes when valid.
		}
	}

	keyBytes := []byte(rawKey) // Fallback to raw text bytes.
	if len(keyBytes) != 32 {   // Validate raw key length.
		return nil, fmt.Errorf("invalid key length %d, expected 32 bytes for AES-256", len(keyBytes)) // Return clear key length error.
	}

	return keyBytes, nil // Return raw key bytes when valid.
} // End loadPhoneEncryptionKey function.
