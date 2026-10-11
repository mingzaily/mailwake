package httpapi

import (
	"io/fs"
	"log/slog"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mingzaily/mailwake/internal/auth"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestEmbeddedManagementAssetsAndSecurityHeaders(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := New(auth.New(store, slog.Default()), newTestRuntime("", nil, nil), store, slog.Default())
	paths := []string{"/"}
	fs.WalkDir(assets, "webdist", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasPrefix(name, "webdist/assets/") {
			paths = append(paths, strings.TrimPrefix(name, "webdist"))
		}
		return nil
	})
	for _, path := range paths {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		policy := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(policy, "script-src 'self';") || strings.Contains(policy, "unsafe-inline") || strings.Contains(policy, "unsafe-eval") {
			t.Fatal("unexpected CSP", policy)
		}
		for _, directive := range []string{"default-src 'none'", "connect-src 'self'", "frame-ancestors 'none'", "base-uri 'none'", "form-action 'none'", "font-src 'self'", "img-src 'self' data:"} {
			if !strings.Contains(policy, directive) {
				t.Fatal("security directive missing", directive)
			}
		}
		if path == "/" {
			nonce := regexp.MustCompile(`name="style-nonce" content="([^"]+)"`).FindStringSubmatch(w.Body.String())
			if len(nonce) != 2 || !strings.Contains(policy, "'nonce-"+nonce[1]+"'") {
				t.Fatal("page nonce authorization missing")
			}
			for _, hash := range managementStyleHashes(assets) {
				if !strings.Contains(policy, "'"+hash+"'") {
					t.Fatal("bundled style authorization missing")
				}
			}
		}

		cache := "no-store"
		if path != "/" {
			cache = "public, max-age=31536000, immutable"
		}
		if w.Header().Get("Cache-Control") != cache || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("asset headers", path, w.Header())
		}
	}
	for _, path := range []string{"/app.js", "/style.css", "/logic_test.mjs", "/missing.mjs", "/assets/missing.js", "/overview"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("unexpected route", path, w.Code)
		}
	}
}
func TestEmbeddedIndexUsesExternalScriptsAndStyles(t *testing.T) {
	html, err := managementIndex(assets)
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`(?i)<style\b|\sstyle\s*=`).Match(html) {
		t.Fatal("inline styles")
	}
	for _, script := range regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`).FindAll(html, -1) {
		if !regexp.MustCompile(`\bsrc="/assets/[^" ]+\.js"`).Match(script) || !strings.HasSuffix(string(script), "></script>") {
			t.Fatal("inline or external-origin script", string(script))
		}
	}
	if !strings.Contains(string(html), `/assets/`) {
		if !strings.Contains(string(html), "运行 make web 构建界面") {
			t.Fatal("Go-only placeholder instructions missing")
		}
	}
}

func TestManagementIndexFallback(t *testing.T) {
	for _, files := range []fstest.MapFS{{}, {"webdist/.gitkeep": &fstest.MapFile{Data: []byte{}}}} {
		html, err := managementIndex(files)
		if err != nil || !strings.Contains(string(html), "运行 make web 构建界面") {
			t.Fatalf("fallback: %s %v", html, err)
		}
	}
	html, err := managementIndex(fstest.MapFS{"webdist/index.html": &fstest.MapFile{Data: []byte("built console")}})
	if err != nil || string(html) != "built console" {
		t.Fatalf("built index: %s %v", html, err)
	}
}

func TestManagementPageNoncesAreUnique(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := New(auth.New(store, slog.Default()), newTestRuntime("", nil, nil), store, slog.Default())
	first := httptest.NewRecorder()
	second := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest("GET", "/", nil))
	router.ServeHTTP(second, httptest.NewRequest("GET", "/", nil))
	if first.Header().Get("Content-Security-Policy") == second.Header().Get("Content-Security-Policy") {
		t.Fatal("page responses reused their nonce")
	}
}
