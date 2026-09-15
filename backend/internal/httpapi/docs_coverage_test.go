package httpapi

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestDocsCoverRouter keeps the API reference in sync with the router: every
// route the server actually wires must be documented in internal/apidocs
// (the spec served at /api/openapi.yaml), and the spec must not document a
// route the server no longer has. Docs/self-service routes are exempt — the
// spec documents the game API, not itself.
func TestDocsCoverRouter(t *testing.T) {
	handler := New(Options{AppOrigin: "http://localhost:3000"}).Handler()

	documented := map[string]bool{}
	parsed := struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}{}
	if err := yaml.Unmarshal(readSpec(t), &parsed); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	for path, methods := range parsed.Paths {
		for method := range methods {
			documented[strings.ToUpper(method)+" "+path] = true
		}
	}

	wired := map[string]bool{}
	for _, r := range handler.Routes() {
		path := toOpenAPIPath(r.Path)
		if path == "" {
			path = "/"
		}
		key := r.Method + " " + path
		if key == "GET /api/docs" || key == "GET /api/openapi.yaml" {
			continue // self-referential doc routes
		}
		if wired[key] {
			continue
		}
		wired[key] = true
		if !documented[key] {
			t.Errorf("router wires %s but openapi.yaml does not document it", key)
		}
	}
	for key := range documented {
		if !wired[key] {
			t.Errorf("openapi.yaml documents %s but router does not wire it", key)
		}
	}
	if t.Failed() {
		return
	}

	var keys []string
	for k := range wired {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Logf("docs coverage: %d routes documented", len(keys))
}

func TestDocsOpenAPIValid(t *testing.T) {
	var doc struct {
		OpenAPI    string                    `yaml:"openapi"`
		Info       map[string]any            `yaml:"info"`
		Paths      map[string]map[string]any `yaml:"paths"`
		Components map[string]any            `yaml:"components"`
	}
	if err := yaml.Unmarshal(readSpec(t), &doc); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	if doc.OpenAPI != "3.1.0" {
		t.Errorf("openapi version = %q, want 3.1.0", doc.OpenAPI)
	}
	if doc.Info["title"] == "" || doc.Info["version"] == "" {
		t.Errorf("spec info must set title and version: %+v", doc.Info)
	}
	if len(doc.Paths) == 0 {
		t.Fatal("spec has no paths")
	}
	if _, ok := doc.Components["securitySchemes"]; !ok {
		t.Error("spec must declare the sessionCookie security scheme")
	}
}

// toOpenAPIPath converts gin's ":param" wildcards into OpenAPI "{param}"
// placeholders so route lists compare 1:1 with the spec.
func toOpenAPIPath(p string) string {
	var b strings.Builder
	segments := strings.Split(p, "/")
	for i, seg := range segments {
		if i > 0 {
			b.WriteByte('/')
		}
		if strings.HasPrefix(seg, ":") {
			b.WriteByte('{')
			b.WriteString(strings.TrimPrefix(seg, ":"))
			b.WriteByte('}')
			continue
		}
		b.WriteString(seg)
	}
	return b.String()
}

func readSpec(t *testing.T) []byte {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		path := filepath.Join(dir, "internal", "apidocs", "openapi.yaml")
		b, err := os.ReadFile(path)
		if err == nil {
			return b
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("openapi.yaml not found while walking up from the test package")
	return nil
}
