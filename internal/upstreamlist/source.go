package upstreamlist

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

type BootstrapFingerprint string
type BootstrapAssessment struct {
	Required                 bool
	Path                     string
	DefaultContents          string
	MissingParentDirectories []string
	Fingerprint              BootstrapFingerprint
}

const defaultUpstreamList = "# One upstream host or origin per line.\n# api.dev.example.com\n"

func AssessBootstrap(path string) BootstrapAssessment {
	path = filepath.Clean(path)
	assessment := BootstrapAssessment{Path: path, DefaultContents: defaultUpstreamList}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return assessment
	}
	assessment.Required = true
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		if _, err := os.Lstat(parent); err == nil {
			break
		}
		assessment.MissingParentDirectories = append(assessment.MissingParentDirectories, parent)
		next := filepath.Dir(parent)
		if next == parent {
			break
		}
	}
	sum := sha256.Sum256([]byte(path + "\x00" + defaultUpstreamList))
	assessment.Fingerprint = BootstrapFingerprint(hex.EncodeToString(sum[:]))
	return assessment
}

func Bootstrap(path string) error {
	return createFile(filepath.Clean(path), defaultUpstreamList)
}

type creationError struct {
	path string
	err  error
}

func (e *creationError) Error() string {
	return fmt.Sprintf("create upstream list %q: %v", e.path, e.err)
}
func (e *creationError) Unwrap() error { return e.err }

func createFile(path, content string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return &creationError{path, err}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return &creationError{path, err}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return &creationError{path, err}
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		return &creationError{path, err}
	}
	if err := file.Close(); err != nil {
		return &creationError{path, err}
	}
	return nil
}
