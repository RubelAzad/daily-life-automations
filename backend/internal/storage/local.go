// Package storage keeps uploaded file contents. LocalStore is for
// development; production can swap in private object storage behind Store.
package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// Store saves and serves file contents by opaque, server-generated keys.
type Store interface {
	// Save streams r to key and returns the byte count and SHA-256 (hex).
	Save(key string, r io.Reader) (size int64, sha string, err error)
	Open(key string) (io.ReadCloser, error)
	Delete(key string) error
}

var ErrInvalidKey = errors.New("storage: invalid key")

// keyPattern allows only "<digits>/<hex>", so a key can never escape Root.
var keyPattern = regexp.MustCompile(`^[0-9]+/[0-9a-f]{32}$`)

// NewKey returns a random key namespaced by user.
func NewKey(userID uint64) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d/%s", userID, hex.EncodeToString(b)), nil
}

type LocalStore struct {
	Root string
}

func (s LocalStore) path(key string) (string, error) {
	if !keyPattern.MatchString(key) {
		return "", ErrInvalidKey
	}
	return filepath.Join(s.Root, filepath.FromSlash(key)), nil
}

// Save writes to a temp file first and renames it into place, so a failed
// upload never leaves a partial file under the final key.
func (s LocalStore) Save(key string, r io.Reader) (int64, string, error) {
	dst, err := s.path(key)
	if err != nil {
		return 0, "", err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return 0, "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".upload-*")
	if err != nil {
		return 0, "", err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hash), r)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return 0, "", err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

func (s LocalStore) Open(key string) (io.ReadCloser, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

// Delete removes the file; a missing file is not an error.
func (s LocalStore) Delete(key string) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
