package documents

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
)

// storagePathRe adalah satu-satunya bentuk storage_path yang diterima:
// <2 hex>/<32 hex>. Nama acak, tanpa nama asli dan tanpa ekstensi.
var storagePathRe = regexp.MustCompile(`^[0-9a-f]{2}/[0-9a-f]{32}$`)

// Storage adalah private storage berbasis direktori lokal (di luar
// web/static, tidak pernah dilayani langsung oleh server statis). Seluruh
// akses melalui os.Root sehingga path tidak bisa keluar dari direktori.
type Storage struct {
	dir string
}

// NewStorage membuat direktori (0700) bila belum ada.
func NewStorage(dir string) (*Storage, error) {
	if dir == "" {
		return nil, errors.New("storage: direktori wajib diisi")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("storage: membuat direktori: %w", err)
	}
	return &Storage{dir: dir}, nil
}

func (s *Storage) root() (*os.Root, error) { return os.OpenRoot(s.dir) }

func newStoragePath() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	h := hex.EncodeToString(b)
	return h[:2] + "/" + h, nil
}

// Put menulis data ke file baru bernama acak (0600) dan mengembalikan
// storage_path relatif.
func (s *Storage) Put(data []byte) (string, error) {
	r, err := s.root()
	if err != nil {
		return "", err
	}
	defer r.Close()
	p, err := newStoragePath()
	if err != nil {
		return "", err
	}
	if err := r.Mkdir(path.Dir(p), 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("storage: mkdir: %w", err)
	}
	f, err := r.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("storage: create: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = r.Remove(p)
		return "", fmt.Errorf("storage: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = r.Remove(p)
		return "", fmt.Errorf("storage: sync: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = r.Remove(p)
		return "", fmt.Errorf("storage: close: %w", err)
	}
	return p, nil
}

// Open membuka file untuk dibaca.
func (s *Storage) Open(p string) (*os.File, error) {
	if !storagePathRe.MatchString(p) {
		return nil, errors.New("storage: path tidak valid")
	}
	r, err := s.root()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.Open(p)
}

// Remove menghapus file (dipakai untuk rollback).
func (s *Storage) Remove(p string) error {
	if !storagePathRe.MatchString(p) {
		return errors.New("storage: path tidak valid")
	}
	r, err := s.root()
	if err != nil {
		return err
	}
	defer r.Close()
	return r.Remove(p)
}
