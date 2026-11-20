package teamcore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// two account group ids, of the shape magpie makes
const (
	groupA = "6f1c2a9d4b70"
	groupB = "b3d8e04c17a2"
)

// TestTeamsRoundTrip: what is saved is what is read back, in the shape
// teams.json is documented to hold.
func TestTeamsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	made := time.Date(2026, 11, 18, 9, 30, 0, 0, time.UTC)
	want := []Team{
		{ID: "team-a", Name: "Team A", GroupID: groupA, KeyID: "aaaa1111bbbb2222cccc3333", TokenHash: HashToken("a-token"), CreatedAt: made},
		{ID: "team-b", Name: "Team B", GroupID: groupB, KeyID: "dddd4444eeee5555ffff6666", CreatedAt: made, RotatedAt: made.Add(time.Hour)},
	}
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Teams()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("read %d teams, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID || got[i].GroupID != want[i].GroupID || got[i].KeyID != want[i].KeyID {
			t.Errorf("team %d read back as %+v, want %+v", i, got[i], want[i])
		}
		if got[i].TokenHash != want[i].TokenHash {
			t.Errorf("team %d's token hash read back as %q, want %q", i, got[i].TokenHash, want[i].TokenHash)
		}
		if !got[i].CreatedAt.Equal(want[i].CreatedAt) {
			t.Errorf("team %d was made at %v, read back at %v", i, want[i].CreatedAt, got[i].CreatedAt)
		}
	}
}

// TestTeamsKeepsNoToken is the promise the file rests on: the token is
// printed once and never written anywhere.
func TestTeamsKeepsNoToken(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	token, hash, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save([]Team{{ID: "team-a", Name: "Team A", GroupID: groupA, TokenHash: hash}}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, TeamsFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), token) {
		t.Fatalf("%s holds the team's token:\n%s", TeamsFile, b)
	}
	if !strings.Contains(string(b), hash) {
		t.Errorf("%s doesn't hold the token's hash:\n%s", TeamsFile, b)
	}
	// a token of another team's must not be readable out of it either
	other, _, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), other) {
		t.Error("teams.json holds a token that was never issued")
	}
}

// TestFilesAreNotWorldReadable: teams.json holds the hashes, and
// audit.jsonl what each team did, which are for teamd and ops alone.
func TestFilesAreNotWorldReadable(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save([]Team{{ID: "team-a", GroupID: groupA}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Audit(Entry{Team: "team-a", Action: "add", Result: "ok"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{TeamsFile, AuditFile} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm&0o077 != 0 {
			t.Errorf("%s is mode %04o: group and others can read it", name, perm)
		}
	}
}

// TestTeamsUnreadableIsAnError: a file that doesn't parse is unknown, and
// unknown is never "no teams".
func TestTeamsUnreadableIsAnError(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, TeamsFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Teams(); err == nil {
		t.Fatalf("a %s that doesn't parse read as %d teams, want an error", TeamsFile, len(got))
	}
}

// TestNoTeamsFileIsNoTeams: a fresh install has no teams, which is not an
// error.
func TestNoTeamsFileIsNoTeams(t *testing.T) {
	s, err := NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	teams, err := s.Teams()
	if err != nil {
		t.Fatalf("a missing %s read as an error: %v", TeamsFile, err)
	}
	if len(teams) != 0 {
		t.Errorf("a missing %s read as %d teams", TeamsFile, len(teams))
	}
}

// TestUpdateRefusedWritesNothing: a change fn refuses leaves the file as
// it was, so a refused rotation can't take a team's link away.
func TestUpdateRefusedWritesNothing(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	first := []Team{{ID: "team-a", GroupID: groupA, TokenHash: HashToken("keep me")}}
	if err := s.Save(first); err != nil {
		t.Fatal(err)
	}
	refused := errors.New("no")
	err = s.Update(func(teams []Team) ([]Team, error) {
		teams[0].TokenHash = HashToken("gone")
		return nil, refused
	})
	if !errors.Is(err, refused) {
		t.Fatalf("Update answered %v, want the refusal", err)
	}
	got, err := s.Teams()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TokenHash != first[0].TokenHash {
		t.Errorf("a refused change wrote the file: %+v", got)
	}
}

// TestUpdateRefusesAnUnreadableFile: a change made over a file that
// couldn't be parsed is refused, never written back over every team.
func TestUpdateRefusesAnUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	broken := []byte("{not json")
	path := filepath.Join(dir, TeamsFile)
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	// the change a caller would make reads no teams and writes them all
	// back: with an unreadable file, that is one team gone
	err = s.Update(func(teams []Team) ([]Team, error) { return teams, nil })
	if err == nil {
		t.Fatal("a change over a teams.json that doesn't parse was written")
	}
	b, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(b) != string(broken) {
		t.Errorf("a refused change wrote the file: %q", b)
	}
}

// TestAuditAppendsOneLineEach: audit.jsonl only grows, one JSON line per
// change, oldest first.
func TestAuditAppendsOneLineEach(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	want := []Entry{
		{At: at, Team: "team-a", Action: "add", Account: "claude/1a2b", Who: "team-a", Result: "ok"},
		{At: at.Add(time.Minute), Team: "team-a", Action: "off", Account: "claude/1a2b", Who: "ops", Result: "refused: not this team's"},
	}
	for _, e := range want {
		if err := s.Audit(e); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, AuditFile))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) != len(want) {
		t.Fatalf("audit.jsonl holds %d lines, want %d:\n%s", len(lines), len(want), b)
	}
	for i, line := range lines {
		var got Entry
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("audit.jsonl line %d doesn't read: %v\n%s", i+1, err, line)
		}
		if got.Team != want[i].Team || got.Action != want[i].Action || got.Account != want[i].Account || got.Who != want[i].Who || got.Result != want[i].Result {
			t.Errorf("audit.jsonl line %d read back as %+v, want %+v", i+1, got, want[i])
		}
		if !got.At.Equal(want[i].At) {
			t.Errorf("audit.jsonl line %d is dated %v, want %v", i+1, got.At, want[i].At)
		}
	}
}
