package main

import (
	"bytes"
	_ "embed"
	"errors"
	"html"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed assets/index.html
var indexHTML string

//go:embed assets/LICENSE
var uiLicense string

type previewServer struct {
	filePath   string
	baseDir    string
	baseName   string
	index      []byte
	cdn        *cdnCache
	httpServer *http.Server
	broker     *broker
	done       chan struct{}
	stopOnce   sync.Once
}

func newPreviewServer(filePath string) *previewServer {
	s := &previewServer{
		filePath: filePath,
		baseDir:  filepath.Dir(filePath),
		baseName: filepath.Base(filePath),
		cdn:      newCDNCache(),
		broker:   newBroker(),
		done:     make(chan struct{}),
	}
	s.index = renderIndex(s.baseName)

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/content.md", s.handleContent)
	mux.HandleFunc("/__live/events", s.handleEvents)
	mux.HandleFunc("/__live/asset", s.handleAsset)
	mux.HandleFunc(cdnPrefix, s.handleCDN)
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	s.httpServer = &http.Server{
		Handler:           loopbackHostOnly(mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return s
}

// loopbackHostOnly rejects a request whose Host is not a loopback name.
// Without it, a DNS-rebinding page could read the preview from 127.0.0.1.
func loopbackHostOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		switch strings.ToLower(strings.Trim(host, "[]")) {
		case "127.0.0.1", "localhost", "::1":
			next.ServeHTTP(w, r)
		default:
			http.Error(w, "forbidden host", http.StatusForbidden)
		}
	})
}

func (s *previewServer) stop() {
	s.stopOnce.Do(func() { close(s.done) })
}

func renderIndex(baseName string) []byte {
	page := strings.Replace(indexHTML, `data-live-token="__LIVE_TOKEN__"`, `data-live-token=""`, 1)
	page = strings.NewReplacer(
		"__BOTTOM_PADDING__", "0.5",
		"__MERMAID_ELK__", "false",
		"__THEME__", "dark",
		"__ALLOW_HTML__", "true",
		"__YAML_MODE__", "panel",
	).Replace(page)
	title := html.EscapeString(baseName) + " · mdkite"
	page = strings.Replace(page, "<title>mdkite</title>", "<title>"+title+"</title>", 1)
	// Keep the UI's MIT notice in the page source, including when only the
	// binary is handed on. A comment before <!DOCTYPE> would force quirks
	// mode, so it goes inside <head>.
	page = strings.Replace(page, "<head>", "<head>\n<!--\n"+uiLicense+"-->", 1)
	return []byte(localizeCDN(page))
}

func (s *previewServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(s.index)
}

func (s *previewServer) handleContent(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	content, err := readContent(s.filePath)
	if err != nil {
		http.Error(w, "cannot read "+s.baseName+": "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(content)
}

func readContent(filePath string) ([]byte, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".mmd", ".mermaid":
		var buf bytes.Buffer
		buf.Grow(len(content) + 16)
		buf.WriteString("```mermaid\n")
		buf.Write(content)
		if len(content) > 0 && content[len(content)-1] != '\n' {
			buf.WriteByte('\n')
		}
		buf.WriteString("```\n")
		return buf.Bytes(), nil
	default:
		return content, nil
	}
}

func allowGet(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

var assetExtensions = map[string]bool{
	".apng": true,
	".avif": true,
	".bmp":  true,
	".gif":  true,
	".ico":  true,
	".jpeg": true,
	".jpg":  true,
	".png":  true,
	".svg":  true,
	".webp": true,
}

var errInvalidAsset = errors.New("invalid asset path")

func (s *previewServer) handleAsset(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	name, err := resolveAsset(s.baseDir, r.URL.Query().Get("p"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.EqualFold(filepath.Ext(name), ".svg") {
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func resolveAsset(baseDir, rel string) (string, error) {
	if rel == "" || strings.ContainsRune(rel, 0) || strings.Contains(rel, "\\") || strings.Contains(rel, ":") {
		return "", errInvalidAsset
	}
	if path.IsAbs(rel) {
		return "", errInvalidAsset
	}
	last := path.Base(rel)
	if last == "." || last == ".." || last == "/" {
		return "", errInvalidAsset
	}
	if !assetExtensions[strings.ToLower(path.Ext(last))] {
		return "", errInvalidAsset
	}
	// Resolve links before the containment check: an image that points
	// outside the Markdown file's directory must not be served.
	realBase, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(realBase, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	// The requested name can be a link to a file of another kind; judge
	// the name actually served as well.
	if !assetExtensions[strings.ToLower(filepath.Ext(real))] {
		return "", errInvalidAsset
	}
	inside, err := filepath.Rel(realBase, real)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
		return "", errInvalidAsset
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		return "", errInvalidAsset
	}
	return real, nil
}
