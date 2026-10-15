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
	"time"
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
	// With expectedSize >= 0, bytes of any other length are rejected and
	// nothing is stored.
	Put(ctx context.Context, workspaceID string, body io.Reader, expectedSize int64) (Object, error)
	// Open returns the bytes stored under key.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes the bytes stored under key.
	Delete(ctx context.Context, key string) error
}

// SpaceReporter is implemented by backends that can report free space.
type SpaceReporter interface {
	Available() (int64, error)
}

var (
	workspacePattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	keyPattern       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/[0-9a-f]{2}/[0-9a-f]{64}$`)
)

// LocalDisk stores objects as content-addressed files below a root directory:
// <root>/<workspace_id>/<sha256[0:2]>/<sha256>. Writing the same bytes twice
// yields the same key, so a retried import never leaves duplicate files.
type LocalDisk struct {
	root    string
	syncDir func(string) error
}

// staleUpload is the age after which a temp file in .incoming cannot belong
// to a running upload any more and is removed at startup.
const staleUpload = 24 * time.Hour

// NewLocalDisk creates the root directory when needed and removes stale
// temp files left by a crash.
func NewLocalDisk(root string) (*LocalDisk, error) {
	if root == "" {
		return nil, errors.New("attachments directory is required")
	}
	incoming := filepath.Join(root, ".incoming")
	if err := os.MkdirAll(incoming, 0o750); err != nil {
		return nil, fmt.Errorf("create attachments directory: %w", err)
	}
	entries, err := os.ReadDir(incoming)
	if err != nil {
		return nil, fmt.Errorf("read attachments directory: %w", err)
	}
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && !entry.IsDir() && time.Since(info.ModTime()) > staleUpload {
			_ = os.Remove(filepath.Join(incoming, entry.Name()))
		}
	}
	return &LocalDisk{root: root, syncDir: syncDirectory}, nil
}

func (d *LocalDisk) Put(ctx context.Context, workspaceID string, body io.Reader, expectedSize int64) (Object, error) {
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
	source := io.Reader(contextReader{ctx: ctx, reader: body})
	if expectedSize >= 0 {
		source = io.LimitReader(source, expectedSize+1)
	}
	size, err := io.Copy(io.MultiWriter(temp, hash), source)
	if err != nil {
		return Object{}, fmt.Errorf("store attachment: %w", err)
	}
	if expectedSize >= 0 && size != expectedSize {
		return Object{}, fmt.Errorf("store attachment: size mismatch: expected %d bytes, received %s", expectedSize, received(size, expectedSize))
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
	// The rename is durable only once the directories holding it are synced.
	for _, dir := range []string{filepath.Dir(target), filepath.Dir(filepath.Dir(target))} {
		if err := d.syncDir(dir); err != nil {
			return Object{}, fmt.Errorf("store attachment: sync directory: %w", err)
		}
	}
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

func (d *LocalDisk) Delete(_ context.Context, key string) error {
	if !keyPattern.MatchString(key) {
		return errors.New("delete attachment: invalid key")
	}
	if err := os.Remove(filepath.Join(d.root, filepath.FromSlash(key))); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete attachment: %w", err)
	}
	return nil
}

// Available returns the bytes free for this process on the attachments volume.
func (d *LocalDisk) Available() (int64, error) {
	return availableBytes(d.root)
}

func syncDirectory(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}

func received(size, expected int64) string {
	if size > expected {
		return "more"
	}
	return fmt.Sprint(size)
}

// DeleteWorkspace removes every object of the workspace, including files no row points to.
func (d *LocalDisk) DeleteWorkspace(_ context.Context, workspaceID string) error {
	if !workspacePattern.MatchString(workspaceID) {
		return errors.New("delete workspace attachments: invalid workspace id")
	}
	if err := os.RemoveAll(filepath.Join(d.root, workspaceID)); err != nil {
		return fmt.Errorf("delete workspace attachments: %w", err)
	}
	return nil
}
