// Package playlist loads a configured playlist as a bounded, read-only file.
package playlist

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const DefaultMaxBytes int64 = 2 << 20

type Config struct {
	Path        string
	Route       string
	MaxBytes    int64
	ContentType string
}

type File struct {
	Data        []byte
	ContentType string
	ModTime     time.Time
}

type Handler struct{ cfg Config }

func New(cfg Config) (*Handler, error) {
	if strings.TrimSpace(cfg.Path) == "" {
		return nil, errors.New("playlist path is required")
	}
	if err := ValidateRoute(cfg.Route); err != nil {
		return nil, err
	}
	if cfg.MaxBytes <= 0 || cfg.MaxBytes > 16<<20 {
		return nil, errors.New("playlist max bytes must be between 1 and 16777216")
	}
	if cfg.ContentType == "" {
		cfg.ContentType = "application/vnd.apple.mpegurl"
	}
	h := &Handler{cfg: cfg}
	if _, err := h.Read(); err != nil {
		return nil, err
	}
	return h, nil
}

func ValidateRoute(route string) error {
	if route == "" || !strings.HasPrefix(route, "/") || route == "/" || strings.ContainsAny(route, "?#%\\\r\n\t ") || strings.Contains(route, "//") {
		return errors.New("playlist route must be one canonical absolute path")
	}
	if route == "/status" || strings.HasPrefix(route, "/udp/") {
		return errors.New("playlist route conflicts with a reserved route")
	}
	return nil
}

func (h *Handler) Route() string { return h.cfg.Route }

func (h *Handler) Read() (File, error) {
	abs, err := filepath.Abs(h.cfg.Path)
	if err != nil {
		return File{}, fmt.Errorf("resolve playlist path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return File{}, fmt.Errorf("resolve playlist symlinks: %w", err)
	}
	if filepath.Clean(resolved) != filepath.Clean(abs) {
		return File{}, errors.New("playlist path must not contain symbolic links")
	}
	file, err := os.Open(resolved)
	if err != nil {
		return File{}, fmt.Errorf("open playlist: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return File{}, fmt.Errorf("stat playlist: %w", err)
	}
	if !info.Mode().IsRegular() {
		return File{}, errors.New("playlist must be a regular file")
	}
	if info.Size() > h.cfg.MaxBytes {
		return File{}, fmt.Errorf("playlist exceeds %d bytes", h.cfg.MaxBytes)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return File{}, fmt.Errorf("read playlist: %w", err)
	}
	if int64(len(data)) > h.cfg.MaxBytes {
		return File{}, fmt.Errorf("playlist exceeds %d bytes", h.cfg.MaxBytes)
	}
	return File{Data: data, ContentType: h.cfg.ContentType, ModTime: info.ModTime()}, nil
}
