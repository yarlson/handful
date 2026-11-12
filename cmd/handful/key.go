package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// keyFileName is the file in the data folder that holds Handful's key, which writes to the
// local API need. It is kept across restarts; deleting it and restarting makes a new key.
const keyFileName = "ui-key"

// keyText is a key as ensureKey writes it: 32 random bytes in base64url without padding.
var keyText = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// ensureKey returns Handful's key from dataDir's key file and the file's path, and makes the
// key when the file is missing. The file is mode 0600; a wider mode is narrowed, and widened
// reports it. Anything but a regular file holding a key stops the start, naming the file.
func ensureKey(dataDir string) (key, path string, widened bool, err error) {
	path = filepath.Join(dataDir, keyFileName)
	key, err = newKey(path)
	if !errors.Is(err, fs.ErrExist) {
		return key, path, false, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", path, false, fmt.Errorf("read Handful's key: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", path, false, fmt.Errorf("%s is not a file; delete it, and Handful makes a new key at its next start", path)
	}
	if widened = info.Mode().Perm() != 0o600; widened {
		if err := os.Chmod(path, 0o600); err != nil {
			return "", path, false, fmt.Errorf("make %s readable by you only: %w", path, err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", path, false, fmt.Errorf("read Handful's key: %w", err)
	}
	key = strings.TrimSpace(string(raw))
	if !keyText.MatchString(key) {
		return "", path, false, fmt.Errorf("%s does not hold a Handful key; delete it, and Handful makes a new key at its next start", path)
	}
	return key, path, widened, nil
}

// newKey writes a new key to path, which must not exist yet; fs.ErrExist means it does.
func newKey(path string) (string, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", path, err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.Join(fmt.Errorf("make Handful's key: %w", err), f.Close(), os.Remove(path))
	}
	key := base64.RawURLEncoding.EncodeToString(raw)
	_, werr := f.WriteString(key + "\n")
	if err := errors.Join(werr, f.Close()); err != nil {
		return "", errors.Join(fmt.Errorf("write Handful's key to %s: %w", path, err), os.Remove(path))
	}
	return key, nil
}
