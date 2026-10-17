package dashboard

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// staticRequest drives the real dashboard handler. Font routes are answered
// before any snapshot work, so a nil cache is fine — if that ordering ever
// regressed, this would panic instead of passing.
func staticRequest(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	dashboardHandler(Options{}, nil).ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestFontRouteServesEveryEmbeddedFace(t *testing.T) {
	names, err := fs.Glob(staticFiles, "static/*.woff2")
	if err != nil || len(names) != 5 {
		t.Fatalf("want 5 embedded woff2 files, got %v (err %v)", names, err)
	}
	for _, n := range names {
		rec := staticRequest(t, http.MethodGet, staticPathPrefix+strings.TrimPrefix(n, "static/"))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", n, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "font/woff2" {
			t.Errorf("%s: Content-Type %q", n, ct)
		}
		if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
			t.Errorf("%s: should be cached as immutable", n)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: missing nosniff", n)
		}
		if !strings.HasPrefix(rec.Body.String(), "wOF2") {
			t.Errorf("%s: body is not a WOFF2 file", n)
		}
	}
}

func TestFontRouteServesLicences(t *testing.T) {
	for n, want := range map[string]string{
		"LICENSE-space-grotesk.txt": "SIL Open Font License",
		"LICENSE-ibm-plex-mono.txt": "SIL Open Font License",
		"LICENSE-lucide.txt":        "ISC License",
	} {
		rec := staticRequest(t, http.MethodGet, staticPathPrefix+n)
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
			t.Fatalf("%s: status %d, type %q", n, rec.Code, rec.Header().Get("Content-Type"))
		}
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: not the %s text", n, want)
		}
	}
}

func TestFontRouteRejectsEverythingElse(t *testing.T) {
	for path, want := range map[string]int{
		staticPathPrefix:                         http.StatusNotFound, // no directory listing
		staticPathPrefix + "nope.woff2":          http.StatusNotFound,
		staticPathPrefix + "../static.go":        http.StatusNotFound,
		staticPathPrefix + "static/x.woff2":      http.StatusNotFound,
		staticPathPrefix + `..\static.go`:        http.StatusNotFound,
		staticPathPrefix + ".hidden":             http.StatusNotFound,
		staticPathPrefix + "LICENSE-missing.txt": http.StatusNotFound,
	} {
		if rec := staticRequest(t, http.MethodGet, path); rec.Code != want {
			t.Errorf("GET %s: status %d, want %d", path, rec.Code, want)
		}
	}
	rec := staticRequest(t, http.MethodPost, staticPathPrefix+"ibm-plex-mono-latin-400-normal.woff2")
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST: status %d, Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestFontRouteHeadSendsHeadersOnly(t *testing.T) {
	rec := staticRequest(t, http.MethodHead, staticPathPrefix+"space-grotesk-latin-wght-normal.woff2")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "font/woff2" || rec.Body.Len() != 0 {
		t.Errorf("HEAD: status %d, type %q, body %d bytes", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
}

// Every @font-face URL must name a file that's actually embedded, and the
// page must carry the rules.
func TestFontFaceRulesPointAtEmbeddedFiles(t *testing.T) {
	urls := regexp.MustCompile(`url\(/_static/([^)]+)\)`).FindAllStringSubmatch(fontFaceCSS, -1)
	if len(urls) != 5 {
		t.Fatalf("want 5 @font-face sources, got %d", len(urls))
	}
	for _, m := range urls {
		if _, err := staticFiles.ReadFile("static/" + m[1]); err != nil {
			t.Errorf("@font-face points at %s, which isn't embedded", m[1])
		}
	}
	page := layout("t", "", "1.0", nil, "")
	if !strings.Contains(page, `@font-face{font-family:"Space Grotesk"`) {
		t.Error("page missing the Space Grotesk @font-face rule")
	}
}

// Each licence must carry its upstream copyright notice verbatim — the OFL
// and ISC both require the original notice to travel with every copy. A
// redistributor's rewritten notice once replaced IBM's (dropping the
// Reserved Font Name "Plex") before an audit caught it; this pins the real
// first lines from github.com/IBM/plex, github.com/floriankarsten/
// space-grotesk and github.com/lucide-icons/lucide.
func TestStaticLicencesMatchUpstreamNotices(t *testing.T) {
	for file, want := range map[string][]string{
		"LICENSE-ibm-plex-mono.txt": {`Copyright © 2017 IBM Corp. with Reserved Font Name "Plex"`, "SIL OPEN FONT LICENSE Version 1.1"},
		"LICENSE-space-grotesk.txt": {"Copyright 2020 The Space Grotesk Project Authors (https://github.com/floriankarsten/space-grotesk)", "SIL OPEN FONT LICENSE Version 1.1"},
		"LICENSE-lucide.txt":        {"Copyright (c) 2026 Lucide Icons and Contributors", "Copyright (c) 2013-present Cole Bemis"},
	} {
		b, err := staticFiles.ReadFile("static/" + file)
		if err != nil {
			t.Fatalf("%s not embedded: %v", file, err)
		}
		for _, w := range want {
			if !strings.Contains(string(b), w) {
				t.Errorf("%s: missing upstream notice %q", file, w)
			}
		}
	}
	// Every embedded font needs a licence file beside it.
	names, _ := fs.Glob(staticFiles, "static/*.woff2")
	for _, n := range names {
		lic := "LICENSE-space-grotesk.txt"
		if strings.Contains(n, "IBMPlexMono") {
			lic = "LICENSE-ibm-plex-mono.txt"
		}
		if _, err := staticFiles.ReadFile("static/" + lic); err != nil {
			t.Errorf("%s has no licence %s embedded", n, lic)
		}
	}
}
