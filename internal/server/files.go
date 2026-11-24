package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/hostfs"
)

// V0.message-links-files: a file path in a message opens the file from the
// thread's host. Only the thread's own folders are read (hostfs.ThreadFile).

// maxStatPaths is how many paths one stat call may ask about.
const maxStatPaths = 64

// threadFile finds p among the thread's folders: its folder, the worktrees
// of its changes, and the temp folder (an agent's scratch files).
func (s *Server) threadFile(t core.Thread, p string) (hostfs.File, error) {
	dirs := []string{t.Cwd}
	for _, c := range t.Changes {
		if c.Workspace != nil && c.Workspace.Path != "" {
			dirs = append(dirs, c.Workspace.Path)
		}
	}
	home := ""
	if s.FS != nil {
		home = s.FS.Home
	}
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return hostfs.ThreadFile(home, dirs, []string{os.TempDir(), "/tmp"}, p)
}

// filesStat answers which of the asked paths are files of the thread, so the
// client links only those. A path that is not served is left out.
func (s *Server) filesStat(w http.ResponseWriter, r *http.Request) {
	t, ok := s.Engine.Thread(r.PathValue("id"))
	if !ok {
		api.WriteJSON(w, 404, map[string]any{"error": "no such thread"})
		return
	}
	var q struct {
		Paths []string `json:"paths"`
	}
	if !api.DecodeBody(w, r, &q, 1<<18) {
		return
	}
	if len(q.Paths) > maxStatPaths {
		api.WriteJSON(w, 400, map[string]any{"error": "too many paths"})
		return
	}
	type fileDTO struct {
		Path string `json:"path"`
		Abs  string `json:"abs"`
		Size int64  `json:"size"`
		Kind string `json:"kind"`
	}
	files := []fileDTO{}
	for _, p := range q.Paths {
		if f, err := s.threadFile(t, p); err == nil {
			files = append(files, fileDTO{Path: p, Abs: f.Abs, Size: f.Size, Kind: f.Kind})
		}
	}
	api.WriteJSON(w, 200, map[string]any{"files": files})
}

// file serves one file of the thread. Text is served as plain text and
// everything in a sandbox, so a file never runs as a page of Fyr's origin.
func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	t, ok := s.Engine.Thread(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	m, err := s.threadFile(t, r.URL.Query().Get("path"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(m.Abs)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	disposition := "inline"
	if r.URL.Query().Get("download") != "" || m.Kind == "other" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", m.MediaType)
	w.Header().Set("Content-Disposition", disposition+"; filename*=UTF-8''"+url.PathEscape(filepath.Base(m.Abs)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Cache-Control", "no-store") // a working file changes
	http.ServeContent(w, r, "", time.Time{}, f)
}
