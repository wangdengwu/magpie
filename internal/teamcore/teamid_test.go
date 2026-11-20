package teamcore

import (
	"strings"
	"testing"
)

// TestTeamIDMakesAnIDFromAName: the id a team goes by is its name in lower
// case, with everything else turned into one dash.
func TestTeamIDMakesAnIDFromAName(t *testing.T) {
	for name, want := range map[string]string{
		"Team A":            "team-a",
		"team-a":            "team-a",
		"  Payments  Team ": "payments-team",
		"Core/Platform":     "core-platform",
		"R&D (2026)":        "r-d-2026",
		"x":                 "x",
	} {
		got, err := teamID(name)
		if err != nil {
			t.Errorf("%q was refused: %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("%q makes the id %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"", "   ", "团队", "!!!"} {
		if _, err := teamID(name); err == nil {
			t.Errorf("%q was given an id", name)
		}
	}
	if long, err := teamID(strings.Repeat("a", 80)); err != nil || len(long) > 40 {
		t.Errorf("a long name makes the id %q (%v), want at most 40 characters", long, err)
	}
}
