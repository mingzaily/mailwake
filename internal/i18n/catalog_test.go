package i18n

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/fault"
)

func TestCatalogParityAndParameters(t *testing.T) {
	english := catalogs[Default]
	for locale, catalog := range catalogs {
		if len(catalog) != len(english) {
			t.Errorf("%s: key count differs", locale)
		}
		for key, source := range english {
			translated, ok := catalog[key]
			if !ok || translated == "" {
				t.Errorf("%s: missing %s", locale, key)
				continue
			}
			a, b := placeholder.FindAllString(source, -1), placeholder.FindAllString(translated, -1)
			slices.Sort(a)
			slices.Sort(b)
			if !reflect.DeepEqual(a, b) {
				t.Errorf("%s: placeholder mismatch for %s", locale, key)
			}
		}
	}
	if got := Message("zz", "unauthorized", nil); got != english["unauthorized"] {
		t.Fatal(got)
	}
	if got := Message("zh-CN", "bark_http_error", map[string]string{"status": "503"}); got != "Bark 返回 HTTP 状态 503。" {
		t.Fatal(got)
	}
	if got := Message("en", "notification.title", map[string]string{"account": "Work {account}", "folder": "{folder}<script>"}); got != "Work {account} · {folder}<script>" {
		t.Fatal("parameter values must remain literal", got)
	}
	if got := ErrorMessage("en", fault.New("imap_connection_failed")); got != english["imap_connection_failed"] {
		t.Fatal(got)
	}
}

func TestLanguageNegotiation(t *testing.T) {
	for _, tc := range []struct{ header, want string }{
		{"", "en"}, {"fr-FR", "fr"}, {"en-US", "en"}, {"zh-CN", "zh-CN"}, {"zh", "zh-CN"},
		{"zh-CN;q=0.2,en;q=0.9", "en"}, {"en;q=0.1,zh-CN;q=0.8", "zh-CN"}, {"zh-CN;q=0,en;q=1", "en"}, {"*", "en"}, {"broken!", "en"},
		{"zh-Hans", "zh-CN"}, {"zh-SG", "zh-CN"}, {"zh-TW", "zh-Hant"}, {"zh-HK", "zh-Hant"}, {"zh-MO", "zh-Hant"}, {"zh-Hant-CN", "zh-Hant"}, {"zh-Hans-TW", "zh-CN"},
		{"ja-JP", "ja"}, {"ko-KR", "ko"}, {"de-AT", "de"}, {"es-MX", "es"}, {"pt-PT", "pt-BR"}, {"pt", "pt-BR"},
		{"it-IT,fr-CA;q=0.8", "fr"}, {"ja;q=0,de;q=0.5", "de"}, {"it-IT", "en"}, {"fr;q=0", "en"},
	} {
		if got := Match(tc.header); got != tc.want {
			t.Errorf("%q: got %s, want %s", tc.header, got, tc.want)
		}
	}
}

// Check static message references in production Go code as part of normal tests.
func TestGoMessageKeysExist(t *testing.T) {
	for _, root := range []string{"..", "../../cmd"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			check := func(expr ast.Expr) {
				literal, ok := expr.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return
				}
				key, _ := strconv.Unquote(literal.Value)
				if catalogs[Default][key] == "" {
					t.Errorf("%s: unknown message key %s", path, key)
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.CallExpr:
					selector, ok := n.Fun.(*ast.SelectorExpr)
					if !ok {
						break
					}
					pkg, ok := selector.X.(*ast.Ident)
					if !ok {
						break
					}
					if pkg.Name == "fault" && selector.Sel.Name == "New" {
						check(n.Args[0])
					}
					if pkg.Name == "fault" && selector.Sel.Name == "From" {
						check(n.Args[1])
					}
					if pkg.Name == "i18n" && selector.Sel.Name == "Message" {
						check(n.Args[1])
					}
				case *ast.KeyValueExpr:
					if key, ok := n.Key.(*ast.Ident); ok && key.Name == "Code" {
						check(n.Value)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, locale := range Languages() {
		data, err := Catalog(locale.Code)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]string
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLanguageResolution(t *testing.T) {
	for _, item := range Languages() {
		if code, ok := Resolve(item.Code); !ok || code != item.Code || !Supported(code) {
			t.Errorf("catalog %q does not resolve: %q %v", item.Code, code, ok)
		}
	}
	for _, invalid := range []string{"", "und", "zz", "it", "*", "../../en", "en!"} {
		if code, ok := Resolve(invalid); ok {
			t.Errorf("accepted %q as %q", invalid, code)
		}
	}
}
