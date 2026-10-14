// Package attachments stores message attachment bytes outside PostgreSQL.
// The database keeps metadata (filename, size, SHA-256, storage key); a
// Storage backend keeps the bytes. Local disk is the first backend; an
// S3-compatible backend can implement the same interface later.
package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// Object describes stored bytes.
type Object struct {
	Key    string
	Size   int64
	SHA256 string
}

// Storage persists attachment bytes under workspace-scoped keys.
type Storage interface {
	// Put streams body into storage and returns its key, size and SHA-256.
	Put(ctx context.Context, workspaceID string, body io.Reader) (Object, error)
	// Open returns the bytes stored under key.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}

var (
	workspacePattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	keyPattern       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/[0-9a-f]{2}/[0-9a-f]{64}$`)
)

// LocalDisk stores objects as content-addressed files below a root directory:
// <root>/<workspace_id>/<sha256[0:2]>/<sha256>. Writing the same bytes twice
// yields the same key, so a retried import never leaves duplicate files.
type LocalDisk struct {
	root string
}

// NewLocalDisk creates the root directory when needed.
func NewLocalDisk(root string) (*LocalDisk, error) {
	if root == "" {
		return nil, errors.New("attachments directory is required")
	}
	if err := os.MkdirAll(filepath.Join(root, ".incoming"), 0o750); err != nil {
		return nil, fmt.Errorf("create attachments directory: %w", err)
	}
	return &LocalDisk{root: root}, nil
}

func (d *LocalDisk) Put(ctx context.Context, workspaceID string, body io.Reader) (Object, error) {
	if !workspacePattern.MatchString(workspaceID) {
		return Object{}, errors.New("store attachment: invalid workspace id")
	}
	temp, err := os.CreateTemp(filepath.Join(d.root, ".incoming"), "upload-*")
	if err != nil {
		return Object{}, fmt.Errorf("store attachment: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = temp.Close()
			_ = os.Remove(temp.Name())
		}
	}()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(temp, hash), contextReader{ctx: ctx, reader: body})
	if err != nil {
		return Object{}, fmt.Errorf("store attachment: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return Object{}, fmt.Errorf("store attachment: %w", err)
	}
	if err := temp.Close(); err != nil {
		return Object{}, fmt.Errorf("store attachment: %w", err)
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	key := workspaceID + "/" + sum[:2] + "/" + sum
	target := filepath.Join(d.root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return Object{}, fmt.Errorf("store attachment: %w", err)
	}
	if err := os.Rename(temp.Name(), target); err != nil {
		return Object{}, fmt.Errorf("store attachment: %w", err)
	}
	committed = true
	return Object{Key: key, Size: size, SHA256: sum}, nil
}

func (d *LocalDisk) Open(_ context.Context, key string) (io.ReadCloser, error) {
	if !keyPattern.MatchString(key) {
		return nil, errors.New("open attachment: invalid key")
	}
	file, err := os.Open(filepath.Join(d.root, filepath.FromSlash(key)))
	if err != nil {
		return nil, fmt.Errorf("open attachment: %w", err)
	}
	return file, nil
}

// Checksum returns the size and hex SHA-256 of everything readable from r.
func Checksum(r io.Reader) (int64, string, error) {
	hash := sha256.New()
	size, err := io.Copy(hash, r)
	if err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
