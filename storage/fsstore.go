package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type Meta struct {
	Size      int64     `json:"size"`
	ETag      string    `json:"etag"`
	CreatedAt time.Time `json:"createdAt"`
	Committed bool      `json:"committed"`
}

type Store interface {
	Create(ctx context.Context) (string, error)
	PutBlob(ctx context.Context, id string, r io.Reader) (int64, string, error)
	PutManifest(ctx context.Context, id string, r io.Reader) error
	Commit(ctx context.Context, id string) (Meta, error)
	StatBlob(ctx context.Context, id string) (Meta, error)
	OpenFile(ctx context.Context, id string) (*os.File, error)
	GetManifest(ctx context.Context, id string) (io.ReadCloser, error)
	Delete(ctx context.Context, id string) error
	GC(ctx context.Context, ttl time.Duration) error
}

type FSStore struct{ Root string }

func NewFSStore(root string) (*FSStore, error) {
	if err := os.MkdirAll(filepath.Join(root, "objects"), 0o755); err != nil {
		return nil, err
	}
	return &FSStore{Root: root}, nil
}

func (s *FSStore) objDir(id string) string       { return filepath.Join(s.Root, "objects", id) }
func (s *FSStore) blobTmp(id string) string      { return filepath.Join(s.objDir(id), "blob.tmp") }
func (s *FSStore) blobPath(id string) string     { return filepath.Join(s.objDir(id), "blob") }
func (s *FSStore) manifestPath(id string) string { return filepath.Join(s.objDir(id), "manifest.json") }
func (s *FSStore) metaPath(id string) string     { return filepath.Join(s.objDir(id), "meta.json") }

func (s *FSStore) Create(ctx context.Context) (string, error) {
	id := uuidLike()
	if err := os.MkdirAll(s.objDir(id), 0o755); err != nil {
		return "", err
	}
	m := Meta{Size: 0, ETag: "", CreatedAt: time.Now().UTC(), Committed: false}
	if err := writeJSON(s.metaPath(id), m); err != nil {
		return "", err
	}
	return id, nil
}

func (s *FSStore) PutBlob(ctx context.Context, id string, r io.Reader) (int64, string, error) {
	f, err := os.Create(s.blobTmp(id))
	if err != nil {
		return 0, "", err
	}
	defer f.Close()

	h := sha256.New()
	mw := io.MultiWriter(f, h)
	n, err := io.Copy(mw, r) // streamed, no buffering
	if err != nil {
		return 0, "", err
	}
	if err := f.Sync(); err != nil {
		return 0, "", err
	}
	etag := hex.EncodeToString(h.Sum(nil))

	m, err := s.readMeta(id)
	if err != nil {
		return 0, "", err
	}
	m.Size = n
	m.ETag = etag
	if err := writeJSON(s.metaPath(id), m); err != nil {
		return 0, "", err
	}
	return n, etag, nil
}

func (s *FSStore) PutManifest(ctx context.Context, id string, r io.Reader) error {
	f, err := os.Create(s.manifestPath(id))
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return err
	}
	return f.Sync()
}

func (s *FSStore) Commit(ctx context.Context, id string) (Meta, error) {
	m, err := s.readMeta(id)
	if err != nil {
		return Meta{}, err
	}
	if _, err := os.Stat(s.blobTmp(id)); err != nil {
		return Meta{}, err
	}
	if _, err := os.Stat(s.manifestPath(id)); err != nil {
		return Meta{}, err
	}
	if err := os.Rename(s.blobTmp(id), s.blobPath(id)); err != nil {
		return Meta{}, err
	}
	m.Committed = true
	if err := writeJSON(s.metaPath(id), m); err != nil {
		return Meta{}, err
	}
	return m, nil
}

func (s *FSStore) StatBlob(ctx context.Context, id string) (Meta, error) {
	m, err := s.readMeta(id)
	if err != nil {
		return Meta{}, err
	}
	// Committed blob present?
	if _, err := os.Stat(s.blobPath(id)); err == nil {
		return m, nil
	}
	// Pre-commit temp blob present? Report meta so caller can return 409.
	if _, err := os.Stat(s.blobTmp(id)); err == nil {
		return m, nil
	}
	// Neither committed nor temp blob found.
	return Meta{}, os.ErrNotExist
}

func (s *FSStore) OpenFile(ctx context.Context, id string) (*os.File, error) {
	return os.Open(s.blobPath(id))
}

func (s *FSStore) GetManifest(ctx context.Context, id string) (io.ReadCloser, error) {
	return os.Open(s.manifestPath(id))
}

func (s *FSStore) Delete(ctx context.Context, id string) error {
	return os.RemoveAll(s.objDir(id))
}

func (s *FSStore) GC(ctx context.Context, ttl time.Duration) error {
	base := filepath.Join(s.Root, "objects")
	entries, err := os.ReadDir(base)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		m, err := s.readMeta(id)
		if err != nil {
			_ = s.Delete(ctx, id)
			continue
		}
		if now.Sub(m.CreatedAt) >= ttl {
			_ = s.Delete(ctx, id)
		}
	}
	return nil
}

// helpers

func (s *FSStore) readMeta(id string) (Meta, error) {
	f, err := os.Open(s.metaPath(id))
	if err != nil {
		return Meta{}, err
	}
	defer f.Close()
	var m Meta
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return Meta{}, err
	}
	return m, nil
}

func writeJSON(path string, v any) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", " ")
	if err := enc.Encode(v); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func uuidLike() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
