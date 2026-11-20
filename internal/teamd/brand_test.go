package teamd

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// testIcon writes a small PNG and answers its path.
func testIcon(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "icon.png")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestEveryPageCarriesTheIcon: with an icon configured, every page draws it
// beside its head (or over its message) and as the tab's icon, in either
// language, and the page still loads nothing from anywhere: the only src is
// a data: URL, which reaches no origin.
func TestEveryPageCarriesTheIcon(t *testing.T) {
	f := newFixture(t)
	icon, err := loadIcon(testIcon(t))
	if err != nil {
		t.Fatal(err)
	}
	f.srv.icon = icon
	srcs := regexp.MustCompile(`(?:src|href)="([^"]*)"`)
	for _, path := range []string{
		"/team/" + f.tokenA,
		"/team/" + f.tokenA + "/usage",
		"/team/" + f.tokenA + "/connect",
		"/team/" + f.tokenA + "/members",
		"/team/" + f.tokenA + "/audit",
		"/team/" + f.tokenA + "/add",
		"/team/nope",
	} {
		for _, lang := range []string{"en-US", "zh-CN"} {
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.Header.Set("Accept-Language", lang)
			rec := httptest.NewRecorder()
			f.srv.Handler().ServeHTTP(rec, r)
			body := rec.Body.String()
			if !strings.Contains(body, `<link rel="icon" href="data:image/png;base64,`) {
				t.Errorf("%s (%s): no tab icon:\n%s", path, lang, body)
			}
			if !strings.Contains(body, `<img class="brand" src="data:image/png;base64,`) {
				t.Errorf("%s (%s): no icon on the page:\n%s", path, lang, body)
			}
			for _, m := range srcs.FindAllStringSubmatch(body, -1) {
				if u := m[1]; !strings.HasPrefix(u, "data:") && !strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "#") {
					t.Errorf("%s (%s): the page reaches %q", path, lang, u)
				}
			}
			if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "img-src data:;") {
				t.Errorf("%s: the CSP lets images come from elsewhere, or not from the page: %q", path, csp)
			}
		}
	}
}

// TestNoIconDrawsNone: a portal with no icon draws no image and no tab icon,
// so a deployment that names none reads as before.
func TestNoIconDrawsNone(t *testing.T) {
	f := newFixture(t)
	body := get(t, f.srv, "/team/"+f.tokenA).Body.String()
	for _, bad := range []string{`rel="icon"`, `<img`} {
		if strings.Contains(body, bad) {
			t.Errorf("a portal with no icon draws %q:\n%s", bad, body)
		}
	}
}

// TestAnIconThatIsntOneStopsTheStart: a file that is there and isn't a small
// image is refused when teamd starts, not drawn as a broken image.
func TestAnIconThatIsntOneStopsTheStart(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for name, path := range map[string]string{
		"text":    write("notes.png", []byte("this is not an image")),
		"html":    write("page.svg", []byte("<html><body>hi</body></html>")),
		"empty":   write("empty.png", nil),
		"huge":    write("huge.png", append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, maxIcon)...)),
		"missing": filepath.Join(dir, "nothing-here.png"),
	} {
		if _, err := loadIcon(path); err == nil {
			t.Errorf("%s: an icon file that isn't one was taken", name)
		}
	}
	svg := write("logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"/>`))
	if got, err := loadIcon(svg); err != nil || !strings.HasPrefix(got, "data:image/svg+xml;base64,") {
		t.Errorf("an SVG icon answered %q, %v", got, err)
	}

	// and New refuses it, which is the portal not starting
	f := newFixture(t)
	cfg := f.srv.cfg
	cfg.IconFile = write("bad.png", []byte("nope"))
	if _, err := New(cfg); err == nil {
		t.Error("teamd started with an icon file that isn't an image")
	}
}
