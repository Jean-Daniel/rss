package server

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// staticHandler serves the built front end from the binary. Files under
// _app/immutable/ have content hashes in their names and are cached forever;
// everything else is revalidated with an ETag. Any path that is not a file
// gets index.html, so the single-page app can handle its own routes.
// Pre-compressed .br/.gz copies made at build time are used when the browser
// accepts them, so nothing is compressed at request time.
type staticHandler struct {
	files fs.FS
	etags sync.Map // path -> ETag
}

func newStaticHandler(files fs.FS) http.Handler {
	if files == nil {
		return http.HandlerFunc(devPlaceholder)
	}
	return &staticHandler{files: files}
}

func devPlaceholder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("This build does not include the front end.\n" +
		"During development, run `make web-dev` and open http://localhost:5173.\n" +
		"Production images are built with `-tags embedweb`.\n"))
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || !h.isFile(name) {
		if path.Ext(name) != "" && name != "index.html" {
			http.NotFound(w, r) // a missing asset, not an app route
			return
		}
		name = "index.html"
	}

	hdr := w.Header()
	if strings.HasPrefix(name, "_app/immutable/") {
		hdr.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		hdr.Set("Cache-Control", "no-cache")
	}
	ctype := mime.TypeByExtension(path.Ext(name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	if name == "manifest.webmanifest" {
		ctype = "application/manifest+json"
	}
	hdr.Set("Content-Type", ctype)
	hdr.Add("Vary", "Accept-Encoding")

	served := name
	accept := r.Header.Get("Accept-Encoding")
	switch {
	case acceptsEncoding(accept, "br") && h.isFile(name+".br"):
		served = name + ".br"
		hdr.Set("Content-Encoding", "br")
	case acceptsEncoding(accept, "gzip") && h.isFile(name+".gz"):
		served = name + ".gz"
		hdr.Set("Content-Encoding", "gzip")
	}

	f, err := h.files.Open(served)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "unreadable file", http.StatusInternalServerError)
		return
	}
	hdr.Set("ETag", h.etag(served))
	http.ServeContent(w, r, "", time.Time{}, rs)
}

func (h *staticHandler) isFile(name string) bool {
	st, err := fs.Stat(h.files, name)
	return err == nil && !st.IsDir()
}

func (h *staticHandler) etag(name string) string {
	if v, ok := h.etags.Load(name); ok {
		return v.(string)
	}
	data, err := fs.ReadFile(h.files, name)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	tag := `"` + hex.EncodeToString(sum[:12]) + `"`
	h.etags.Store(name, tag)
	return tag
}

func acceptsEncoding(header, enc string) bool {
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(name), enc) {
			continue
		}
		// "br;q=0" means "not br".
		if q, ok := strings.CutPrefix(strings.ReplaceAll(params, " ", ""), "q="); ok {
			if v, err := strconv.ParseFloat(q, 64); err == nil && v == 0 {
				return false
			}
		}
		return true
	}
	return false
}
