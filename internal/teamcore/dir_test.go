package teamcore

import (
	"path/filepath"
	"testing"
)

// TestDirIsTheSidecarsWhenOneRuns: MAGPIE_TEAMD_DIR is the sidecar's state
// folder, and saying it is what says a portal runs beside this magpie.
// Without it, there is no portal, and the folder a run outside the
// container would use is under magpie's config folder.
func TestDirIsTheSidecarsWhenOneRuns(t *testing.T) {
	t.Setenv("MAGPIE_TEAMD_DIR", "")
	if Served() {
		t.Error("a portal is said to run with no MAGPIE_TEAMD_DIR")
	}
	if got, want := Dir("/home/me/.config/magpie"), filepath.Join("/home/me/.config/magpie", "teamd"); got != want {
		t.Errorf("Dir is %q, want %q", got, want)
	}
	t.Setenv("MAGPIE_TEAMD_DIR", " /config/teamd ")
	if !Served() {
		t.Error("no portal is said to run with MAGPIE_TEAMD_DIR set")
	}
	if got := Dir("/home/me/.config/magpie"); got != "/config/teamd" {
		t.Errorf("Dir is %q, want the sidecar's /config/teamd", got)
	}
}
