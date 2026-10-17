package dashboard

import (
	"embed"
	"net/http"
	"strings"
)

// Third-party assets compiled into the binary, so the dashboard renders
// identically with no network — the same offline promise as the rest of
// the page — together with the licence each one requires to travel with
// every copy. Provenance (re-verify against upstream before replacing any
// file; TestStaticLicencesMatchUpstreamNotices pins the notices):
//
//   - Space Grotesk — Fontsource 5.1.0 latin variable-weight woff2
//     (@fontsource-variable/space-grotesk). SIL OFL 1.1, no Reserved Font
//     Name, so a subset may keep the name. LICENSE-space-grotesk.txt.
//   - IBM Plex Mono — IBM's own Latin-1 web subsets, unmodified, from
//     github.com/IBM/plex packages/plex-mono/fonts/split/woff2. SIL OFL 1.1
//     with Reserved Font Name "Plex": a third-party re-subset would be a
//     Modified Version that may not use the name, which is why these are
//     IBM's files and not a CDN's. LICENSE-ibm-plex-mono.txt is IBM's.
//   - Lucide icons — the nav and theme-toggle icon path data inlined in
//     render.go. ISC (Lucide) plus MIT for the Feather-derived icons such
//     as "moon"; LICENSE-lucide.txt is Lucide's file, covering both.
//
// The licences are reachable at /_static/LICENSE-*.txt; the licences only
// require that they accompany the copies, so nothing in the UI shows them.
//
//go:embed static/*.woff2 static/LICENSE-*.txt
var staticFiles embed.FS

// staticPathPrefix is where the stylesheet's @font-face rules point.
const staticPathPrefix = "/_static/"

// serveStatic answers a /_static/ request with one embedded file. Only an
// exact, flat file name is accepted — no directory listing, no path
// traversal — and only GET/HEAD. ok=false means the path isn't a static
// route at all and the caller should keep routing.
func serveStatic(w http.ResponseWriter, r *http.Request) (ok bool) {
	name, isStatic := strings.CutPrefix(r.URL.Path, staticPathPrefix)
	if !isStatic {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return true
	}
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		http.NotFound(w, r)
		return true
	}
	b, err := staticFiles.ReadFile("static/" + name)
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	switch {
	case strings.HasSuffix(name, ".woff2"):
		w.Header().Set("Content-Type", "font/woff2")
		// Embedded in this binary, so a file name can never change
		// content within one version.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		return true
	}
	_, _ = w.Write(b)
	return true
}

// fontFaceCSS declares the embedded faces. font-display:swap paints text
// in the fallback font immediately, then swaps — never invisible text.
const fontFaceCSS = `@font-face{font-family:"Space Grotesk";src:url(/_static/space-grotesk-latin-wght-normal.woff2) format("woff2");font-weight:300 700;font-style:normal;font-display:swap}
@font-face{font-family:"IBM Plex Mono";src:url(/_static/IBMPlexMono-Regular-Latin1.woff2) format("woff2");font-weight:400;font-style:normal;font-display:swap}
@font-face{font-family:"IBM Plex Mono";src:url(/_static/IBMPlexMono-Medium-Latin1.woff2) format("woff2");font-weight:500;font-style:normal;font-display:swap}
@font-face{font-family:"IBM Plex Mono";src:url(/_static/IBMPlexMono-SemiBold-Latin1.woff2) format("woff2");font-weight:600;font-style:normal;font-display:swap}
@font-face{font-family:"IBM Plex Mono";src:url(/_static/IBMPlexMono-Bold-Latin1.woff2) format("woff2");font-weight:700;font-style:normal;font-display:swap}
`
