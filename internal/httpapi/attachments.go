package httpapi

import (
	"github.com/notif-dev/notif/internal/engine"
	"io"
	"mime"
	"net/http"
	"strings"
)

func (s *Server) attachmentRoutes() {
	m := s.mux
	m.HandleFunc("GET /api/v1/conversations/{id}/attachments", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, e := s.Store.Attachments(r.Context(), actor(r), r.PathValue("id"))
		return ok(w, x, e)
	}))
	m.HandleFunc("POST /api/v1/conversations/{id}/attachments", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		r.Body = http.MaxBytesReader(w, r.Body, (5<<20)+65536)
		if e := r.ParseMultipartForm(65536); e != nil {
			return engine.Bad("Invalid upload or attachment exceeds 5 MiB.")
		}
		defer r.MultipartForm.RemoveAll()
		file, header, e := r.FormFile("file")
		if e != nil {
			return engine.Bad("A file is required.")
		}
		defer file.Close()
		data, e := io.ReadAll(io.LimitReader(file, (5<<20)+1))
		if e != nil {
			return e
		}
		media := strings.Split(header.Header.Get("Content-Type"), ";")[0]
		x, e := s.Store.UploadAttachment(r.Context(), actor(r), r.PathValue("id"), header.Filename, media, data)
		return ok(w, x, e)
	}))
	m.HandleFunc("GET /api/v1/attachments/{id}", s.protect(func(w http.ResponseWriter, r *http.Request) error {
		x, file, e := s.Store.DownloadAttachment(r.Context(), actor(r), r.PathValue("id"))
		if e != nil {
			return e
		}
		defer file.Close()
		w.Header().Set("Content-Type", x.MediaType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": x.FileName}))
		w.Header().Set("Cache-Control", "private, no-store")
		http.ServeContent(w, r, x.FileName, x.CreatedAt, file)
		return nil
	}))
}
