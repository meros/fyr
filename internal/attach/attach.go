// Package attach stores files the user attaches to a message (SPEC 0.5
// V0.18): images, documents, logs, photos. Each file lives in its own
// directory under Dir with a meta.json beside it.
package attach

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var ErrTooLarge = errors.New("the file is larger than the limit")

// Meta describes one stored attachment.
type Meta struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType"`
	Path      string `json:"path"`
}

type Store struct {
	Dir string
	Max int64 // bytes; 0 means 25 MB
}

var idRe = regexp.MustCompile(`^[0-9a-f]{20}$`)

func cleanName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == '/' {
			return -1
		}
		return r
	}, name)
	if name == "." || name == "/" || name == ".." || strings.TrimSpace(name) == "" {
		name = "file"
	}
	if r := []rune(name); len(r) > 120 {
		name = string(r[len(r)-120:])
	}
	return name
}

// Save stores r under a new id. The media type falls back to the name's
// extension, then to application/octet-stream.
func (s *Store) Save(name, mediaType string, r io.Reader) (Meta, error) {
	max := s.Max
	if max == 0 {
		max = 25 << 20
	}
	b := make([]byte, 10)
	rand.Read(b)
	m := Meta{ID: hex.EncodeToString(b), Name: cleanName(name)}
	m.MediaType = strings.TrimSpace(strings.Split(mediaType, ";")[0])
	if m.MediaType == "" || m.MediaType == "application/octet-stream" {
		if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(m.Name))); t != "" {
			m.MediaType = strings.Split(t, ";")[0]
		} else {
			m.MediaType = "application/octet-stream"
		}
	}
	dir := filepath.Join(s.Dir, m.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Meta{}, err
	}
	m.Path = filepath.Join(dir, m.Name)
	f, err := os.OpenFile(m.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Meta{}, err
	}
	n, err := io.Copy(f, io.LimitReader(r, max+1))
	f.Close()
	if err == nil && n > max {
		err = fmt.Errorf("%w (%d MB)", ErrTooLarge, max>>20)
	}
	if err != nil {
		os.RemoveAll(dir)
		return Meta{}, err
	}
	m.Size = n
	j, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), j, 0o600); err != nil {
		return Meta{}, err
	}
	return m, nil
}

// Get returns a stored attachment by id.
func (s *Store) Get(id string) (Meta, error) {
	if !idRe.MatchString(id) {
		return Meta{}, fmt.Errorf("no attachment %q", id)
	}
	b, err := os.ReadFile(filepath.Join(s.Dir, id, "meta.json"))
	if err != nil {
		return Meta{}, fmt.Errorf("no attachment %q", id)
	}
	var m Meta
	return m, json.Unmarshal(b, &m)
}
