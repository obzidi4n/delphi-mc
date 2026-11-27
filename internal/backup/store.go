// Package backup stores server snapshots encrypted at rest in the panel's
// data directory.
package backup

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

const (
	magic      = "DMCB1"
	chunkSize  = 1 << 20
	keySize    = 32
	nonceSize  = 12
	prefixSize = 8
)

var idPattern = regexp.MustCompile(`^b_[0-9a-f]{16}$`)

// Store is an encrypted backup directory.
type Store struct {
	Dir string
	Key []byte
}

// New loads or creates the backup key and directory.
func New(dir, keyPath string) (*Store, error) {
	if dir == "" || keyPath == "" {
		return nil, errors.New("backup: directory and key path are required")
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("backup: create %s: %w", dir, err)
	}
	key, err := loadOrCreateKey(keyPath)
	if err != nil {
		return nil, err
	}
	return &Store{Dir: dir, Key: key}, nil
}

func loadOrCreateKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != keySize {
			return nil, fmt.Errorf("backup: key at %s must be %d bytes", path, keySize)
		}
		return data, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("backup: read key: %w", err)
	}
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("backup: generate key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("backup: create key dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, key, 0o600); err != nil {
		return nil, fmt.Errorf("backup: write key: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, fmt.Errorf("backup: publish key: %w", err)
	}
	return key, nil
}

// Path returns the file for a backup id.
func (s *Store) Path(id string) (string, error) {
	if !idPattern.MatchString(id) {
		return "", errors.New("backup: invalid id")
	}
	return filepath.Join(s.Dir, id+".enc"), nil
}

func (s *Store) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(s.Key)
	if err != nil {
		return nil, fmt.Errorf("backup: cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

// Encrypt streams src into an encrypted file and returns the plaintext
// sha256 and size.
func (s *Store) Encrypt(id string, src io.Reader) (string, int64, error) {
	target, err := s.Path(id)
	if err != nil {
		return "", 0, err
	}
	aead, err := s.gcm()
	if err != nil {
		return "", 0, err
	}
	prefix := make([]byte, prefixSize)
	if _, err := rand.Read(prefix); err != nil {
		return "", 0, fmt.Errorf("backup: nonce: %w", err)
	}
	tmp, err := os.CreateTemp(s.Dir, ".upload-*")
	if err != nil {
		return "", 0, fmt.Errorf("backup: temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write([]byte(magic)); err != nil {
		tmp.Close()
		return "", 0, err
	}
	if _, err := tmp.Write(prefix); err != nil {
		tmp.Close()
		return "", 0, err
	}

	hasher := sha256.New()
	var size int64
	buf := make([]byte, chunkSize)
	var counter uint32
	for {
		n, readErr := io.ReadFull(src, buf)
		if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
			tmp.Close()
			return "", 0, fmt.Errorf("backup: read: %w", readErr)
		}
		if n > 0 {
			plain := buf[:n]
			hasher.Write(plain)
			size += int64(n)
			nonce := make([]byte, nonceSize)
			copy(nonce, prefix)
			binary.BigEndian.PutUint32(nonce[prefixSize:], counter)
			counter++
			sealed := aead.Seal(nil, nonce, plain, nil)
			var length [4]byte
			binary.BigEndian.PutUint32(length[:], uint32(len(sealed)))
			if _, err := tmp.Write(length[:]); err != nil {
				tmp.Close()
				return "", 0, err
			}
			if _, err := tmp.Write(sealed); err != nil {
				tmp.Close()
				return "", 0, err
			}
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
	}
	var terminator [4]byte
	if _, err := tmp.Write(terminator[:]); err != nil {
		tmp.Close()
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return "", 0, fmt.Errorf("backup: publish: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}

// Decrypt streams a backup to dst, verifying its structure and tags.
func (s *Store) Decrypt(id string, dst io.Writer) error {
	path, err := s.Path(id)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("backup: open: %w", err)
	}
	defer file.Close()
	aead, err := s.gcm()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(file)
	header := make([]byte, len(magic)+prefixSize)
	if _, err := io.ReadFull(reader, header); err != nil {
		return fmt.Errorf("backup: header: %w", err)
	}
	if string(header[:len(magic)]) != magic {
		return errors.New("backup: not a DelphiMC backup")
	}
	prefix := header[len(magic):]
	var counter uint32
	for {
		var length [4]byte
		if _, err := io.ReadFull(reader, length[:]); err != nil {
			return fmt.Errorf("backup: chunk header: %w", err)
		}
		n := binary.BigEndian.Uint32(length[:])
		if n == 0 {
			return nil
		}
		if n > chunkSize+16 || n < 16 {
			return errors.New("backup: invalid chunk")
		}
		sealed := make([]byte, n)
		if _, err := io.ReadFull(reader, sealed); err != nil {
			return fmt.Errorf("backup: chunk: %w", err)
		}
		nonce := make([]byte, nonceSize)
		copy(nonce, prefix)
		binary.BigEndian.PutUint32(nonce[prefixSize:], counter)
		counter++
		plain, err := aead.Open(nil, nonce, sealed, nil)
		if err != nil {
			return errors.New("backup: corrupt or key mismatch")
		}
		if _, err := dst.Write(plain); err != nil {
			return err
		}
	}
}

// Remove deletes a backup file, ignoring a missing one.
func (s *Store) Remove(id string) error {
	path, err := s.Path(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("backup: remove: %w", err)
	}
	return nil
}
