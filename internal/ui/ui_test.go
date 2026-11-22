package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(t *testing.T, h http.Handler, method, path string) (*http.Response, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	res := rec.Result()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func TestServesTheBuiltUI(t *testing.T) {
	h := New(fstest.MapFS{
		"index.html":        {Data: []byte("<html>moipctl</html>")},
		"assets/app.abc.js": {Data: []byte("console.log(1)")},
		"favicon.svg":       {Data: []byte("<svg/>")},
	})

	res, body := get(t, h, "GET", "/")
	if res.StatusCode != 200 || !strings.Contains(body, "moipctl") {
		t.Fatalf("/: %d %q", res.StatusCode, body)
	}
	if res.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("the page must not be cached, got %q", res.Header.Get("Cache-Control"))
	}
	for _, want := range []string{"default-src 'self'", "frame-ancestors 'none'", "connect-src 'self'"} {
		if !strings.Contains(res.Header.Get("Content-Security-Policy"), want) {
			t.Errorf("CSP lacks %q: %q", want, res.Header.Get("Content-Security-Policy"))
		}
	}
	if strings.Contains(res.Header.Get("Content-Security-Policy"), "unsafe-inline") || strings.Contains(res.Header.Get("Content-Security-Policy"), "unsafe-eval") {
		t.Errorf("the policy must not allow inline or eval: %q", res.Header.Get("Content-Security-Policy"))
	}
	if res.Header.Get("X-Content-Type-Options") != "nosniff" || res.Header.Get("X-Frame-Options") != "DENY" {
		t.Errorf("security headers missing: %v", res.Header)
	}

	res, _ = get(t, h, "GET", "/assets/app.abc.js")
	if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("hashed asset: %d %q", res.StatusCode, res.Header.Get("Cache-Control"))
	}
	if res, _ := get(t, h, "GET", "/favicon.svg"); res.StatusCode != 200 || res.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("favicon: %d %q", res.StatusCode, res.Header.Get("Cache-Control"))
	}
	if res, _ := get(t, h, "GET", "/nope"); res.StatusCode != 404 {
		t.Errorf("unknown path: %d", res.StatusCode)
	}
	if res, _ := get(t, h, "HEAD", "/"); res.StatusCode != 200 {
		t.Errorf("HEAD: %d", res.StatusCode)
	}
	if res, _ := get(t, h, "POST", "/"); res.StatusCode != http.StatusMethodNotAllowed || res.Header.Get("Allow") == "" {
		t.Errorf("POST: %d", res.StatusCode)
	}
}

func TestWithoutABuiltUIItSaysSo(t *testing.T) {
	h := New(fstest.MapFS{".gitkeep": {Data: nil}})
	res, body := get(t, h, "GET", "/")
	if res.StatusCode != 200 || !strings.Contains(body, "make frontend") || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("/: %d %q (%s)", res.StatusCode, body, res.Header.Get("Content-Type"))
	}
	if res.Header.Get("Content-Security-Policy") == "" {
		t.Error("the headers apply to this page too")
	}
	if res, _ := get(t, h, "GET", "/x"); res.StatusCode != 404 {
		t.Errorf("/x: %d", res.StatusCode)
	}
}

func TestEmbeddedHandlerExists(t *testing.T) {
	// the repository only has dist/.gitkeep; the handler must still build and answer
	res, _ := get(t, Handler(), "GET", "/")
	if res.StatusCode != 200 {
		t.Fatalf("/: %d", res.StatusCode)
	}
}
