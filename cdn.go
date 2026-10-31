package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	cdnPrefix      = "/__cdn/"
	maxCDNAssetLen = 32 << 20
	// mermaid@11 loads its diagram code as relative chunks. Pin the version
	// so a cached entry and a later chunk cannot mix across 11.x releases.
	mermaidVersion = "11.17.2"
)

var cdnHosts = map[string]bool{
	"cdn.jsdelivr.net": true,
	"unpkg.com":        true,
}

var errInvalidCDN = errors.New("invalid cdn path")

// cdnCache downloads front-end libraries on first use and keeps them under
// the user cache directory, so later runs do not need the network.
type cdnCache struct {
	dir      string
	client   *http.Client
	group    *fetchGroup
	warnOnce sync.Once
}

type cdnAsset struct {
	data        []byte
	contentType string
	cached      bool
}

type fetchGroup struct {
	mu    sync.Mutex
	calls map[string]*fetchCall
}

type fetchCall struct {
	done  chan struct{}
	asset cdnAsset
	err   error
}

func newFetchGroup() *fetchGroup {
	return &fetchGroup{calls: make(map[string]*fetchCall)}
}

// do runs fn once per key while callers for that key wait for the result.
// The preview loads mermaid chunks in parallel, and several of them can be
// requested before the first download finishes.
func (g *fetchGroup) do(key string, fn func() (cdnAsset, error)) (asset cdnAsset, err error) {
	g.mu.Lock()
	if call, ok := g.calls[key]; ok {
		g.mu.Unlock()
		<-call.done
		return call.asset, call.err
	}
	call := &fetchCall{done: make(chan struct{})}
	g.calls[key] = call
	g.mu.Unlock()

	defer func() {
		if p := recover(); p != nil {
			call.err = fmt.Errorf("cdn fetch panic: %v", p)
		}
		g.mu.Lock()
		delete(g.calls, key)
		g.mu.Unlock()
		close(call.done)
		asset, err = call.asset, call.err
	}()

	call.asset, call.err = fn()
	return call.asset, call.err
}

func newCDNCache() *cdnCache {
	c := &cdnCache{group: newFetchGroup()}
	c.client = &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || !allowedCDNHost(req.URL.Hostname()) {
				return fmt.Errorf("redirect to %s", req.URL)
			}
			return nil
		},
	}

	base, err := os.UserCacheDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "mdkite: cannot find cache directory: %v (CDN assets will not be cached)\n", err)
		return c
	}
	dir := filepath.Join(base, "mdkite", "cdn")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "mdkite: cannot create cache %s: %v (CDN assets will not be cached)\n", dir, err)
		return c
	}
	c.dir = dir
	return c
}

// localizeCDN points the preview at the cache proxy instead of the CDN.
// The original URL is kept in the path so relative imports and font URLs
// resolve back through the same proxy.
func localizeCDN(page string) string {
	page = strings.ReplaceAll(page, "mermaid@11/", "mermaid@"+mermaidVersion+"/")
	page = strings.ReplaceAll(page, "https://cdn.jsdelivr.net/", cdnPrefix+"https/cdn.jsdelivr.net/")
	page = strings.ReplaceAll(page, "https://unpkg.com/", cdnPrefix+"https/unpkg.com/")
	return page
}

func (s *previewServer) handleCDN(w http.ResponseWriter, r *http.Request) {
	if !allowGet(w, r) {
		return
	}
	upstream, name, err := cdnURL(r.URL.Path, r.URL.RawQuery)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	asset, err := s.cdn.get(upstream, name)
	if err != nil {
		// The Go process can have a different network path than the browser
		// (a proxy, for one). Let the browser fetch it directly rather than
		// breaking a preview that worked before the cache existed.
		s.cdn.warnOnce.Do(func() {
			fmt.Fprintf(os.Stderr, "mdkite: cannot cache CDN assets (%v); uncached ones will load directly\n", err)
		})
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, upstream, http.StatusTemporaryRedirect)
		return
	}
	w.Header().Set("Content-Type", asset.contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if asset.cached {
		w.Header().Set("X-Mdkite-Cache", "hit")
	} else {
		w.Header().Set("X-Mdkite-Cache", "miss")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(asset.data))
}

// cdnURL turns /__cdn/https/<host>/<path> back into a CDN URL. Only the
// two hosts the preview uses are accepted, and redirects are checked
// separately, so the proxy cannot be pointed at an arbitrary server.
func cdnURL(localPath, rawQuery string) (upstream, name string, err error) {
	rest, ok := strings.CutPrefix(localPath, cdnPrefix)
	if !ok {
		return "", "", errInvalidCDN
	}
	scheme, rest, ok := strings.Cut(rest, "/")
	if !ok || scheme != "https" {
		return "", "", errInvalidCDN
	}
	host, rest, ok := strings.Cut(rest, "/")
	if !ok || !cdnHosts[strings.ToLower(host)] {
		return "", "", errInvalidCDN
	}
	if rest == "" || strings.ContainsRune(rest, 0) || strings.Contains(rest, "\\") {
		return "", "", errInvalidCDN
	}
	for _, segment := range strings.Split(rest, "/") {
		if segment == ".." {
			return "", "", errInvalidCDN
		}
	}
	cleaned := path.Clean("/" + rest)
	if cleaned == "/" || cleaned == "/." || cleaned == "/.." {
		return "", "", errInvalidCDN
	}
	u := url.URL{Scheme: "https", Host: strings.ToLower(host), Path: cleaned}
	if rawQuery != "" {
		u.RawQuery = rawQuery
	}
	name = path.Base(cleaned)
	if name == "/" || name == "." || name == "" {
		name = "asset"
	}
	return u.String(), name, nil
}

func allowedCDNHost(host string) bool {
	host = strings.ToLower(host)
	if cdnHosts[host] {
		return true
	}
	for h := range cdnHosts {
		if strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

func (c *cdnCache) get(upstream, name string) (cdnAsset, error) {
	key := cacheKey(upstream)
	if c.dir != "" {
		if asset, err := c.read(key, name); err == nil {
			asset.cached = true
			return asset, nil
		}
	}
	return c.group.do(upstream, func() (cdnAsset, error) {
		// Another request for the same URL may have filled the cache while
		// this caller waited for the group slot.
		if c.dir != "" {
			if asset, err := c.read(key, name); err == nil {
				asset.cached = true
				return asset, nil
			}
		}
		asset, err := c.download(upstream, name)
		if err != nil {
			return cdnAsset{}, err
		}
		if c.dir != "" {
			if err := c.write(key, asset); err != nil {
				fmt.Fprintf(os.Stderr, "mdkite: cannot cache %s: %v\n", upstream, err)
			}
		}
		return asset, nil
	})
}

func (c *cdnCache) read(key, name string) (cdnAsset, error) {
	data, err := os.ReadFile(filepath.Join(c.dir, key))
	if err != nil {
		return cdnAsset{}, err
	}
	contentType := contentTypeFor(name)
	if contentType == "" {
		if b, err := os.ReadFile(filepath.Join(c.dir, key+".type")); err == nil {
			contentType = strings.TrimSpace(string(b))
		}
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return cdnAsset{data: data, contentType: contentType}, nil
}

func (c *cdnCache) download(upstream, name string) (cdnAsset, error) {
	req, err := http.NewRequest(http.MethodGet, upstream, nil)
	if err != nil {
		return cdnAsset{}, err
	}
	req.Header.Set("User-Agent", "mdkite/"+version)
	// Cache exactly what the CDN serves, without negotiating a compressed
	// body the proxy would have to decode.
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.client.Do(req)
	if err != nil {
		return cdnAsset{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return cdnAsset{}, fmt.Errorf("cdn returned %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCDNAssetLen+1))
	if err != nil {
		return cdnAsset{}, err
	}
	if len(data) > maxCDNAssetLen {
		return cdnAsset{}, fmt.Errorf("asset exceeds %d bytes", maxCDNAssetLen)
	}
	contentType := contentTypeFor(name)
	if contentType == "" {
		contentType = strings.TrimSpace(resp.Header.Get("Content-Type"))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return cdnAsset{data: data, contentType: contentType}, nil
}

func (c *cdnCache) write(key string, asset cdnAsset) error {
	tmp, err := os.CreateTemp(c.dir, ".asset-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(asset.data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(c.dir, key)); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.dir, key+".type"), []byte(asset.contentType), 0o600)
}

func cacheKey(upstream string) string {
	sum := sha256.Sum256([]byte(upstream))
	return hex.EncodeToString(sum[:])
}

func contentTypeFor(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json", ".map":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	case ".eot":
		return "application/vnd.ms-fontobject"
	case ".wasm":
		return "application/wasm"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".ico":
		return "image/x-icon"
	default:
		return ""
	}
}
