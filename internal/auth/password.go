// Package auth contains functions related to authorization
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/setup"
	"tubarr/internal/utils/logging"
)

const asterisks = "********"

// PasswordManager manages password encryption, decryption, etc.
type PasswordManager struct {
	key         []byte
	aesFilepath string
	StarText    string
}

// NewPasswordManager returns a password manager using the configuration file's AES key file.
func NewPasswordManager() (pm *PasswordManager, err error) {
	pm = &PasswordManager{}

	hashedKey, err := pm.ensureAESKey()
	if err != nil {
		return nil, err
	}

	pm.key = hashedKey
	return pm, nil
}

// Encrypt encrypts plaintext and returns base64 encoded ciphertext.
func (pm *PasswordManager) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	block, err := aes.NewCipher(pm.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts base64 encoded passwords to plaintext.
func (pm *PasswordManager) Decrypt(encoded string) (plaintextPassword string, err error) {
	if encoded == "" {
		return "", nil
	}

	if len(pm.key) == 0 {
		return "", fmt.Errorf("encryption key is empty - check that %s exists and is readable", pm.aesFilepath)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(pm.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// StarPassword returns a string of asterisks matching the password length.
func StarPassword(p string) string {
	if p == "" {
		return ""
	}
	return asterisks
}

// ensureAESKey loads the key from the config file, or creates it if it doesn't exist.
func (pe *PasswordManager) ensureAESKey() (hashed []byte, err error) {
	const aesFilename = "/.passwords/aes.txt"

	// Ensure directory exists
	dir := filepath.Join(setup.HomeTubarrDir, "/.passwords")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create password directory: %w", err)
	}

	pe.aesFilepath = filepath.Join(setup.HomeTubarrDir, aesFilename)

	// Check if key file exists
	if _, err := os.Stat(pe.aesFilepath); os.IsNotExist(err) { // File doesn't exist, create new key
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("failed to generate encryption key: %w", err)
		}

		keyStr := base64.StdEncoding.EncodeToString(key)
		if err := os.WriteFile(pe.aesFilepath, []byte(keyStr), consts.PermsPrivateFile); err != nil {
			return nil, fmt.Errorf("failed to write encryption key: %w", err)
		}

		logging.S("Generated new encryption key at: %s", pe.aesFilepath)
		return key, nil

	} else if err != nil { // Some other error checking the file
		return nil, fmt.Errorf("failed to check key file: %w", err)
	}

	// File exists, read it
	data, err := os.ReadFile(pe.aesFilepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read encryption key: %w", err)
	}

	// Decode from base64
	keyStr := strings.TrimSpace(string(data))
	key, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil {
		return nil, fmt.Errorf("invalid encryption key format in %q: %w", pe.aesFilepath, err)
	}

	// Validate key length
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d bytes in %q", len(key), pe.aesFilepath)
	}

	return key, nil
}
