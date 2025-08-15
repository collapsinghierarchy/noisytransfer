package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/collapsinghierarchy/noisytransfer/storage"
)

type Server struct {
	Store   storage.Store
	BaseURL string        // e.g., http://localhost:8080
	TTL     time.Duration // GC TTL
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/objects", s.handleObjects)
	mux.HandleFunc("/objects/", s.handleObject)
}

func (s *Server) handleObjects(w http.ResponseWriter, r *http.Request) {
	rid := newRID(w)
	switch r.Method {
	case http.MethodPost:
		id, err := s.Store.Create(r.Context())
		if err != nil {
			writeProblem(w, rid, 500, "NC_STORE_CREATE", "Create failed", err.Error(), nil)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"objectId":    id,
			"uploadUrl":   fmt.Sprintf("%s/objects/%s/blob", s.BaseURL, id),
			"manifestUrl": fmt.Sprintf("%s/objects/%s/manifest", s.BaseURL, id),
		})
	default:
		writeProblem(w, rid, 405, "NC_METHOD_NOT_ALLOWED", "Method not allowed", "", map[string]any{"allow": "POST"})
	}
}

func (s *Server) handleObject(w http.ResponseWriter, r *http.Request) {
	rid := newRID(w)
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/objects/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeProblem(w, rid, 400, "NC_BAD_REQUEST", "Missing object id", "", nil)
		return
	}
	id := parts[0]
	if len(parts) == 1 {
		writeProblem(w, rid, 400, "NC_BAD_REQUEST", "Missing subresource", "", nil)
		return
	}
	switch parts[1] {
	case "blob":
		s.srvBlob(w, r, rid, id)
	case "manifest":
		s.srvManifest(w, r, rid, id)
	case "commit":
		s.srvCommit(w, r, rid, id)
	default:
		writeProblem(w, rid, 404, "NC_NOT_FOUND", "Unknown subresource", "", map[string]any{"sub": parts[1]})
	}
}

func (s *Server) srvBlob(w http.ResponseWriter, r *http.Request, rid, id string) {
	switch r.Method {
	case http.MethodPut:
		limit := http.MaxBytesReader(w, r.Body, 1<<63-1) // rely on proxy limits
		defer limit.Close()
		_, etag, err := s.Store.PutBlob(r.Context(), id, limit)
		if err != nil {
			writeProblem(w, rid, 500, "NC_UPLOAD_FAILED", "Upload failed", err.Error(), map[string]any{"objectId": id})
			return
		}
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet, http.MethodHead:
		meta, err := s.Store.StatBlob(r.Context(), id)
		if err != nil {
			writeProblem(w, rid, 404, "NC_NOT_FOUND", "Object not found", err.Error(), map[string]any{"objectId": id})
			return
		}
		if !meta.Committed {
			writeProblem(w, rid, 409, "NC_NOT_COMMITTED", "Blob not committed", "", map[string]any{"objectId": id})
			return
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("ETag", meta.ETag)
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		f, err := s.Store.OpenFile(r.Context(), id)
		if err != nil {
			writeProblem(w, rid, 404, "NC_NOT_FOUND", "Open failed", err.Error(), map[string]any{"objectId": id})
			return
		}
		defer f.Close()
		stat, _ := f.Stat()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", meta.ETag)
		http.ServeContent(w, r, "", stat.ModTime(), f) // Range + 206 handled by stdlib
	default:
		writeProblem(w, rid, 405, "NC_METHOD_NOT_ALLOWED", "Method not allowed", "", map[string]any{"allow": "PUT,GET,HEAD"})
	}
}

func (s *Server) srvManifest(w http.ResponseWriter, r *http.Request, rid, id string) {
	switch r.Method {
	case http.MethodPut:
		defer r.Body.Close()
		if err := s.Store.PutManifest(r.Context(), id, r.Body); err != nil {
			writeProblem(w, rid, 500, "NC_MANIFEST_WRITE", "Manifest write failed", err.Error(), map[string]any{"objectId": id})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		rc, err := s.Store.GetManifest(r.Context(), id)
		if err != nil {
			writeProblem(w, rid, 404, "NC_NOT_FOUND", "Manifest not found", err.Error(), map[string]any{"objectId": id})
			return
		}
		defer rc.Close()
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.Copy(w, rc); err != nil {
			// client gone; ignore
		}
	default:
		writeProblem(w, rid, 405, "NC_METHOD_NOT_ALLOWED", "Method not allowed", "", map[string]any{"allow": "PUT,GET"})
	}
}

func (s *Server) srvCommit(w http.ResponseWriter, r *http.Request, rid, id string) {
	if r.Method != http.MethodPost {
		writeProblem(w, rid, 405, "NC_METHOD_NOT_ALLOWED", "Method not allowed", "", map[string]any{"allow": "POST"})
		return
	}
	meta, err := s.Store.Commit(r.Context(), id)
	if err != nil {
		status := 500
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			status = 404
		}
		writeProblem(w, rid, status, "NC_COMMIT_FAILED", "Commit failed", err.Error(), map[string]any{"objectId": id})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meta)
}

func (s *Server) StartGC(ctx context.Context) {
	if s.TTL <= 0 {
		return
	}
	t := time.NewTicker(1 * time.Hour)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = s.Store.GC(context.Background(), s.TTL)
			}
		}
	}()
}
