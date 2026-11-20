package teamcore

import (
	"os"
	"path/filepath"
	"strings"
)

// Dir is where magpie's own window finds the teams the portal serves: the
// sidecar's state folder, which the container names in MAGPIE_TEAMD_DIR,
// else "teamd" under configDir, magpie's config folder, for a run outside
// the container.
//
// configDir is the caller's to give: this package is the portal's too, and
// the portal's image holds nothing that would know magpie's config folder.
func Dir(configDir string) string {
	if d := strings.TrimSpace(os.Getenv("MAGPIE_TEAMD_DIR")); d != "" {
		return d
	}
	return filepath.Join(configDir, "teamd")
}

// Served says a portal runs beside this magpie: the container names the
// sidecar's folder in MAGPIE_TEAMD_DIR. A magpie of one person's has no
// teams to make, so it has no Team portal page and makes no teamd folder.
func Served() bool { return strings.TrimSpace(os.Getenv("MAGPIE_TEAMD_DIR")) != "" }
