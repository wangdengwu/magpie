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

// TestTheIconKeepsItsShape: what is set is the icon's height, never its
// width. The portal takes whatever image ops names, and a wordmark is wide
// where an icon is square: a fixed width would stretch either.
func TestTheIconKeepsItsShape(t *testing.T) {
	for _, rule := range []string{".head img.brand {", ".said img.brand {"} {
		i := strings.Index(pageStyle, rule)
		if i < 0 {
			t.Fatalf("the icon's rule %q is gone", rule)
		}
		body := pageStyle[i : strings.Index(pageStyle[i:], "}")+i]
		if !strings.Contains(body, "height:") || !strings.Contains(body, "width:auto") {
			t.Errorf("the icon's rule is %q: it must set a height and leave the width to the image", body)
		}
		// max-width is the cap a wordmark may reach; a plain width is the
		// one that stretches it
		if withoutMax := strings.ReplaceAll(body, "max-width", "maxwidth"); regexp.MustCompile(`\bwidth:\s*\d`).MatchString(withoutMax) {
			t.Errorf("the icon's rule is %q: a fixed width stretches a wordmark", body)
		}
	}
}

// TestADarkMarkOnADarkPageGetsALightPlate: a mark drawn in its owner's dark
// colour is 1.4:1 on this page's dark ground — Weee!'s dark blue on
// #1a1a1e — which is past reading. The colour is not the page's to change,
// so the mark gets a white plate there, and the light mode is untouched.
func TestADarkMarkOnADarkPageGetsALightPlate(t *testing.T) {
	i := strings.Index(pageStyle, "@media (prefers-color-scheme: dark) {\n  .head img.brand")
	if i < 0 {
		t.Fatal("the icon has no plate in dark mode")
	}
	block := pageStyle[i : strings.Index(pageStyle[i:], "\n}")+i]
	for _, want := range []string{"background:#fff", "padding:", "box-sizing:content-box", ".said img.brand"} {
		if !strings.Contains(block, want) {
			t.Errorf("the dark-mode plate doesn't say %q:\n%s", want, block)
		}
	}
	// and the light mode still sets no ground at all
	light := pageStyle[strings.Index(pageStyle, ".head img.brand {"):]
	light = light[:strings.Index(light, "}")]
	if strings.Contains(light, "background") {
		t.Errorf("the light mode gives the mark a ground it doesn't need: %q", light)
	}
}
