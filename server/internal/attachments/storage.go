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
	"strconv"
	"strings"
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

// Stat returns the size of the object stored under key.
func (d *LocalDisk) Stat(_ context.Context, key string) (int64, error) {
	if !keyPattern.MatchString(key) {
		return 0, errors.New("stat attachment: invalid key")
	}
	info, err := os.Stat(filepath.Join(d.root, filepath.FromSlash(key)))
	if err != nil {
		return 0, fmt.Errorf("stat attachment: %w", err)
	}
	return info.Size(), nil
}

// Stater is implemented by backends that can check an object without reading it.
type Stater interface {
	Stat(ctx context.Context, key string) (int64, error)
}

// Exists returns nil when the object is stored; a missing object wraps os.ErrNotExist.
func Exists(ctx context.Context, storage Storage, key string) error {
	if stater, ok := storage.(Stater); ok {
		_, err := stater.Stat(ctx, key)
		return err
	}
	body, err := storage.Open(ctx, key)
	if err != nil {
		return err
	}
	return body.Close()
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

const trashDir = ".trash"

// Trash moves every file of the workspace to <root>/.trash/<workspace id>-<unix time> in one
// rename on the same filesystem, so it is atomic. found is false when the workspace had no
// directory. The returned func moves the files back; call it when the deletion it belongs to did
// not commit.
func (d *LocalDisk) Trash(_ context.Context, workspaceID string, now time.Time) (undo func() error, found bool, err error) {
	if !workspacePattern.MatchString(workspaceID) {
		return nil, false, errors.New("trash workspace attachments: invalid workspace id")
	}
	source := filepath.Join(d.root, workspaceID)
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		return func() error { return nil }, false, nil
	} else if err != nil {
		return nil, false, fmt.Errorf("trash workspace attachments: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(d.root, trashDir), 0o750); err != nil {
		return nil, false, fmt.Errorf("trash workspace attachments: %w", err)
	}
	entry := TrashEntry{WorkspaceID: workspaceID, TrashedAt: now}
	entry.Name = fmt.Sprintf("%s-%d", workspaceID, now.Unix())
	if err := os.Rename(source, filepath.Join(d.root, trashDir, entry.Name)); err != nil {
		return nil, false, fmt.Errorf("trash workspace attachments: %w", err)
	}
	return func() error { return d.RestoreTrash(context.Background(), entry) }, true, nil
}

// TrashEntry is the files of one deleted workspace waiting in the trash.
type TrashEntry struct {
	WorkspaceID string
	Name        string
	TrashedAt   time.Time
}

// ListTrash returns the trash entries; names that do not parse are ignored.
func (d *LocalDisk) ListTrash(_ context.Context) ([]TrashEntry, error) {
	entries, err := os.ReadDir(filepath.Join(d.root, trashDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list trash: %w", err)
	}
	var found []TrashEntry
	for _, entry := range entries {
		name := entry.Name()
		cut := strings.LastIndex(name, "-")
		if cut < 0 || !workspacePattern.MatchString(name[:cut]) {
			continue
		}
		unix, parseErr := strconv.ParseInt(name[cut+1:], 10, 64)
		if parseErr != nil {
			continue
		}
		found = append(found, TrashEntry{WorkspaceID: name[:cut], Name: name, TrashedAt: time.Unix(unix, 0).UTC()})
	}
	return found, nil
}

// RestoreTrash moves the entry back to the workspace directory. It refuses when that directory
// exists, so nothing is overwritten.
func (d *LocalDisk) RestoreTrash(_ context.Context, entry TrashEntry) error {
	target := filepath.Join(d.root, entry.WorkspaceID)
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("restore trash %s: %s already exists", entry.Name, entry.WorkspaceID)
	}
	if err := os.Rename(filepath.Join(d.root, trashDir, entry.Name), target); err != nil {
		return fmt.Errorf("restore trash %s: %w", entry.Name, err)
	}
	return nil
}

// RemoveTrash deletes one trash entry for good.
func (d *LocalDisk) RemoveTrash(_ context.Context, entry TrashEntry) error {
	if err := os.RemoveAll(filepath.Join(d.root, trashDir, entry.Name)); err != nil {
		return fmt.Errorf("remove trash %s: %w", entry.Name, err)
	}
	return nil
}

// Staging is implemented by backends that keep an upload in parts on their own disk: StagingFile is
// the file the parts are written into, Adopt stores it under its content address by renaming it (no
// second copy, so a 200 MiB upload never needs twice the room) and Staged counts the files staged now.
type Staging interface {
	StagingFile(uploadID string) (string, error)
	Adopt(ctx context.Context, workspaceID, path string) (Object, error)
	Staged() (int, error)
	RemoveStaged(uploadID string) error
}

var uploadPattern = workspacePattern

// stagedPrefix marks the files of uploads in parts in .incoming; NewLocalDisk removes them, like any
// temp file, 24 hours after they last changed.
const stagedPrefix = "media-"

// StagingFile is the path of the file the parts of upload uploadID go into.
func (d *LocalDisk) StagingFile(uploadID string) (string, error) {
	if !uploadPattern.MatchString(uploadID) {
		return "", errors.New("stage upload: invalid upload id")
	}
	return filepath.Join(d.root, ".incoming", stagedPrefix+uploadID), nil
}

// RemoveStaged removes the file of an upload in parts, if it is there.
func (d *LocalDisk) RemoveStaged(uploadID string) error {
	path, err := d.StagingFile(uploadID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Staged counts the uploads in parts on this disk.
func (d *LocalDisk) Staged() (int, error) {
	entries, err := os.ReadDir(filepath.Join(d.root, ".incoming"))
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), stagedPrefix) {
			count++
		}
	}
	return count, nil
}

// Adopt hashes the staged file at path and renames it to its content address in the workspace; the
// same bytes already stored are replaced by an identical file.
func (d *LocalDisk) Adopt(ctx context.Context, workspaceID, path string) (Object, error) {
	if !workspacePattern.MatchString(workspaceID) || filepath.Dir(path) != filepath.Join(d.root, ".incoming") || !strings.HasPrefix(filepath.Base(path), stagedPrefix) {
		return Object{}, errors.New("adopt upload: invalid path")
	}
	file, err := os.Open(path)
	if err != nil {
		return Object{}, fmt.Errorf("adopt upload: %w", err)
	}
	size, sum, err := Checksum(contextReader{ctx: ctx, reader: file})
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Object{}, fmt.Errorf("adopt upload: %w", err)
	}
	key := workspaceID + "/" + sum[:2] + "/" + sum
	target := filepath.Join(d.root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return Object{}, fmt.Errorf("adopt upload: %w", err)
	}
	if err := syncFile(path); err != nil {
		return Object{}, fmt.Errorf("adopt upload: %w", err)
	}
	if err := os.Rename(path, target); err != nil {
		return Object{}, fmt.Errorf("adopt upload: %w", err)
	}
	for _, dir := range []string{filepath.Dir(target), filepath.Dir(filepath.Dir(target))} {
		if err := d.syncDir(dir); err != nil {
			return Object{}, fmt.Errorf("adopt upload: sync directory: %w", err)
		}
	}
	return Object{Key: key, Size: size, SHA256: sum}, nil
}

func syncFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
