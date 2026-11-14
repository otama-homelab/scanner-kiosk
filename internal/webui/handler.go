// Package webui serves the built public frontend. It never reads document roots.
package webui

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

func New(publicURL string) (http.Handler, error) {
	assets, err := embeddedFS()
	if err != nil {
		return nil, err
	}
	return fromFS(publicURL, assets)
}
func fromFS(publicURL string, assets fs.FS) (http.Handler, error) {
	origin, err := url.Parse(publicURL)
	if err != nil || origin.Scheme != "https" || origin.Host == "" {
		return nil, errors.New("invalid frontend origin")
	}
	type asset struct {
		body  []byte
		media string
	}
	files := map[string]asset{}
	total := 0
	err = fs.WalkDir(assets, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		ext := path.Ext(name)
		auxiliaryAsset := (strings.HasPrefix(name, "pdfjs/") && (ext == ".bcmap" || ext == ".pfb" || ext == ".ttf" || ext == ".wasm")) || name == "pdfjs/NOTICE.txt" || name == "ui/NOTICE.txt"
		if ext != ".js" && ext != ".mjs" && ext != ".css" && ext != ".svg" && name != "index.html" && !auxiliaryAsset {
			return errors.New("unsupported public asset")
		}
		data, err := fs.ReadFile(assets, name)
		if err != nil {
			return err
		}
		total += len(data)
		if len(data) > 2<<20 || total > 16<<20 || len(files) >= 1000 {
			return errors.New("frontend exceeds limits")
		}
		route := "/" + name
		if name == "index.html" {
			route = "/"
		}
		media := mime.TypeByExtension(ext)
		if media == "" {
			media = "application/octet-stream"
		}
		files[route] = asset{data, media}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if _, ok := files["/"]; !ok {
		return nil, fs.ErrInvalid
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if r.Host != origin.Host {
			http.Error(w, "Origin denied", 403)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", 405)
			return
		}
		value, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", value.media)
		w.Header().Set("Content-Length", strconv.Itoa(len(value.body)))
		if r.Method == "HEAD" {
			w.WriteHeader(200)
			return
		}
		_, _ = w.Write(value.body)
	}), nil
}
