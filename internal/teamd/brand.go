package teamd

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// The portal's icon: an image ops names with -icon-file, drawn beside each
// page's head and as the browser tab's icon.
//
// It is a file and not something in this package, because whose portal this
// is belongs to where it is deployed, not to the code: a build with no icon
// draws every page exactly as it did before.
//
// The image is read once, at start, and put in the page as a data: URL.
// Nothing is fetched: the token is in the address bar, and a page that
// loaded an image from anywhere would hand it over in a Referer. A data:
// URL reaches no origin, which is why the pages' CSP lets img-src take
// data: and nothing else.

// maxIcon is the largest icon the portal takes. Every page carries it, so a
// big one is a big page on every request.
const maxIcon = 64 << 10

// iconTypes are the images a browser draws in <img> and as a tab's icon.
var iconTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true,
	"image/webp": true, "image/x-icon": true, "image/svg+xml": true,
}

// loadIcon reads path as the portal's icon, as a data: URL; "" for no path.
// A file that is there and isn't a small image is refused, at start: a
// portal told to draw an icon and drawing none would say nothing about why.
func loadIcon(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("the icon file can't be read: %w", err)
	}
	if len(b) == 0 || len(b) > maxIcon {
		return "", fmt.Errorf("the icon file %s is %d bytes: it must hold an image of at most %d", path, len(b), maxIcon)
	}
	typ := http.DetectContentType(b)
	// an SVG sniffs as text: it is taken by its name and its root element
	if strings.EqualFold(filepath.Ext(path), ".svg") && bytes.Contains(b, []byte("<svg")) {
		typ = "image/svg+xml"
	}
	if !iconTypes[typ] {
		return "", fmt.Errorf("the icon file %s is %s, not an image a browser draws as an icon", path, typ)
	}
	return "data:" + typ + ";base64," + base64.StdEncoding.EncodeToString(b), nil
}

// withIcon puts the icons in a page: the tab's icon after its title, and the
// mark at the start of its head, or over the message of a page that is one.
// A page with neither is returned as it is.
//
// They are two images because they are drawn in two shapes: a browser draws
// a tab's icon in a square, so a wide wordmark there is squeezed into one,
// while the head has room for the wordmark and makes a square mark small.
//
// It is done here, where every page passes, rather than in eight
// templates: a page added later carries the icons without being told to.
func withIcon(body, icon, favicon string) string {
	if favicon != "" {
		body = strings.Replace(body, "</title>", `</title>`+"\n"+`<link rel="icon" href="`+favicon+`">`, 1)
	}
	if icon == "" {
		return body
	}
	if strings.Contains(body, `<div class="head">`) {
		return strings.Replace(body, `<div class="head">`, `<div class="head">`+"\n"+`  <img class="brand" src="`+icon+`" alt="">`, 1)
	}
	return strings.Replace(body, `<div class="said">`, `<div class="said">`+"\n"+`<img class="brand" src="`+icon+`" alt="">`, 1)
}
