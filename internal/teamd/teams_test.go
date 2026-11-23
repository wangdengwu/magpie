package teamd

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// TestAddRecordsATeamMagpieHas: a name, a group and a key magpie has make a
// team, with no token until one is asked for.
func TestAddRecordsATeamMagpieHas(t *testing.T) {
	f := newFixture(t)
	f.magpie.set([]Group{
		{ID: groupA, Name: "Team A", Accounts: []string{acctA1, acctA2}},
		{ID: groupB, Name: "Team B", Accounts: []string{acctB1}},
		{ID: groupC, Name: "Team C"},
	})
	team, err := f.ops().Add(context.Background(), NewTeam{Name: "Team C", GroupID: groupC, KeyID: keyC})
	if err != nil {
		t.Fatal(err)
	}
	if team.ID != "team-c" {
		t.Errorf("the team's id is %q, want team-c", team.ID)
	}
	if team.GroupID != groupC || team.KeyID != keyC {
		t.Errorf("the team holds group %q and key %q, want %q and %q", team.GroupID, team.KeyID, groupC, keyC)
	}
	if team.TokenHash != "" {
		t.Error("a team made without a token already holds one")
	}
	if !team.CreatedAt.Equal(opsClock) {
		t.Errorf("the team was made at %v, want the clock %v", team.CreatedAt, opsClock)
	}
	onDisk := f.teamsOnDisk(t)
	if len(onDisk) != 3 {
		t.Fatalf("teams.json holds %d teams, want 3", len(onDisk))
	}
	if !strings.Contains(f.auditOnDisk(t), `"action":"team-add"`) {
		t.Errorf("no audit line says the team was added:\n%s", f.auditOnDisk(t))
	}
}

// TestAddChecksMagpieBeforeRecording: a group or a key magpie doesn't have
// makes no team at all.
func TestAddChecksMagpieBeforeRecording(t *testing.T) {
	f := newFixture(t)
	before := len(f.teamsOnDisk(t))
	for _, tc := range []struct {
		name string
		in   NewTeam
		says string
	}{
		{"a group magpie has not", NewTeam{Name: "Team C", GroupID: "0000aaaa1111", KeyID: keyB}, "no account group"},
		{"a key magpie has not", NewTeam{Name: "Team D", GroupID: groupC, KeyID: "000000000000000000000000"}, "no gateway key"},
		{"a key switched off", NewTeam{Name: "Team C", GroupID: groupC, KeyID: keyOff}, "switched off"},
		{"a group another team has", NewTeam{Name: "Team C", GroupID: groupA, KeyID: keyC}, "already"},
		{"a key another team has", NewTeam{Name: "Team C", GroupID: groupC, KeyID: keyA}, "already"},
		{"a name with no id", NewTeam{Name: "团队", GroupID: groupC, KeyID: keyC}, "no Latin letters"},
		{"no name", NewTeam{Name: "  ", GroupID: groupC, KeyID: keyC}, "needs a name"},
	} {
		_, err := f.ops().Add(context.Background(), tc.in)
		if err == nil {
			t.Errorf("%s made a team", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s answered %q, which doesn't say %q", tc.name, err, tc.says)
		}
	}
	if got := len(f.teamsOnDisk(t)); got != before {
		t.Errorf("a refused add recorded a team: %d teams, want %d", got, before)
	}
}

// TestTokenIssuesAndReplacesALink: a team made here has no link until
// `teams token` issues one, and then that link resolves.
func TestTokenIssuesAndReplacesALink(t *testing.T) {
	f := newFixture(t)
	if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{}); err != nil {
		t.Fatalf("the fixture's own token was refused: %v", err)
	}
	token, team, err := f.ops().Token("team-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if team.ID != "team-a" {
		t.Errorf("the token was issued for %q", team.ID)
	}
	if token == "" || token == f.tokenA {
		t.Fatalf("the token issued is %q, which is empty or the one it replaced", token)
	}
	// the hash is on disk and the token is not
	b := f.teamsOnDisk(t)
	if strings.Contains(f.auditOnDisk(t), token) {
		t.Error("audit.jsonl holds the token")
	}
	for _, onDisk := range b {
		if onDisk.ID == "team-a" && onDisk.TokenHash != HashToken(token) {
			t.Errorf("teams.json holds %q, not the hash of the new token", onDisk.TokenHash)
		}
	}
	// the new link works and the old one is cut off at once
	if _, err := f.srv.Authorize(context.Background(), token, Target{}); err != nil {
		t.Errorf("the new token was refused: %v", err)
	}
	if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{}); !errors.Is(err, ErrNoTeam) {
		t.Errorf("the replaced token answered %v, want ErrNoTeam", err)
	}
	if !strings.Contains(f.auditOnDisk(t), `"action":"team-token"`) {
		t.Error("no audit line says the token was issued")
	}
}

// TestTokenForATeamThereIsNot: an unknown id says so and issues nothing.
func TestTokenForATeamThereIsNot(t *testing.T) {
	f := newFixture(t)
	before := f.teamsOnDisk(t)
	if _, _, err := f.ops().Token("team-c", 0); err == nil {
		t.Fatal("a token was issued for a team that isn't there")
	}
	after := f.teamsOnDisk(t)
	if len(after) != len(before) {
		t.Errorf("a refused token changed teams.json: %d teams, want %d", len(after), len(before))
	}
	for i := range before {
		if before[i].TokenHash != after[i].TokenHash {
			t.Errorf("a refused token rotated %q's link", before[i].ID)
		}
	}
}

// TestListShowsEachTeamAndWhatItsGroupHolds: the line ops audits the pool
// by.
func TestListShowsEachTeamAndWhatItsGroupHolds(t *testing.T) {
	f := newFixture(t)
	rows, err := f.ops().List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("listed %d teams, want 2", len(rows))
	}
	if rows[0].Team.ID != "team-a" || rows[1].Team.ID != "team-b" {
		t.Fatalf("listed %q and %q, want team-a and team-b", rows[0].Team.ID, rows[1].Team.ID)
	}
	if !rows[0].Known || rows[0].Accounts != 2 {
		t.Errorf("team-a's group is listed as %d accounts (known %v), want 2", rows[0].Accounts, rows[0].Known)
	}
	if rows[0].Team.TokenHash == "" {
		t.Error("team-a is listed as having no token")
	}
}

// TestListSaysWhatItDoesNotKnow: a magpie that can't be read leaves the
// counts unknown and still lists the teams.
func TestListSaysWhatItDoesNotKnow(t *testing.T) {
	f := newFixture(t)
	f.magpie.fail(500, `{"error":"no"}`)
	rows, err := f.ops().List(context.Background())
	if err != nil {
		t.Fatalf("a listing failed because magpie was down: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("listed %d teams with magpie down, want 2", len(rows))
	}
	for _, r := range rows {
		if r.Known {
			t.Errorf("%s's accounts are given as %d, but magpie wasn't read", r.Team.ID, r.Accounts)
		}
	}
}

// TestRemoveRefusesAGroupThatStillHoldsAccounts: forgetting a team whose
// pool is not empty would leave accounts nobody can reach through the
// portal, so it is refused and the accounts are named.
func TestRemoveRefusesAGroupThatStillHoldsAccounts(t *testing.T) {
	f := newFixture(t)
	_, err := f.ops().Remove(context.Background(), "team-a", false)
	if err == nil {
		t.Fatal("a team whose group holds accounts was forgotten")
	}
	for _, account := range []string{acctA1, acctA2} {
		if !strings.Contains(err.Error(), account) {
			t.Errorf("the refusal doesn't name %q: %v", account, err)
		}
	}
	if len(f.teamsOnDisk(t)) != 2 {
		t.Error("a refused removal took the team away")
	}
}

// TestRemoveWithForceForgetsTheTeamAndLeavesMagpie: force forgets the
// mapping and changes nothing in magpie, which is said.
func TestRemoveWithForceForgetsTheTeamAndLeavesMagpie(t *testing.T) {
	f := newFixture(t)
	before := f.magpie.callsTo()
	gone, err := f.ops().Remove(context.Background(), "team-a", true)
	if err != nil {
		t.Fatal(err)
	}
	if gone.Team.ID != "team-a" {
		t.Errorf("forgot %q, want team-a", gone.Team.ID)
	}
	if !gone.Read || len(gone.Accounts()) != 2 {
		t.Errorf("the removal says %d accounts are left (read %v), want the group's 2", len(gone.Accounts()), gone.Read)
	}
	teams := f.teamsOnDisk(t)
	if len(teams) != 1 || teams[0].ID != "team-b" {
		t.Fatalf("teams.json holds %+v, want team-b alone", teams)
	}
	// nothing in magpie was touched: the calls the removal made were reads
	if got := f.magpie.callsTo() - before; got != 1 {
		t.Errorf("the removal called magpie %d times, want 1 (the groups it read)", got)
	}
	if !strings.Contains(f.auditOnDisk(t), `"action":"team-rm"`) {
		t.Error("no audit line says the team was forgotten")
	}
}

// TestRemoveATeamWhoseGroupIsEmpty: a pool already emptied comes away
// cleanly.
func TestRemoveATeamWhoseGroupIsEmpty(t *testing.T) {
	f := newFixture(t)
	f.magpie.set([]Group{
		{ID: groupA, Name: "Team A", Accounts: []string{acctA1, acctA2}},
		{ID: groupB, Name: "Team B"},
	})
	gone, err := f.ops().Remove(context.Background(), "team-b", false)
	if err != nil {
		t.Fatalf("a team whose group is empty was refused: %v", err)
	}
	if len(gone.Accounts()) != 0 {
		t.Errorf("the removal says %d accounts are left, want none", len(gone.Accounts()))
	}
	if len(f.teamsOnDisk(t)) != 1 {
		t.Error("the team is still in teams.json")
	}
}

// TestRemoveNeedsForceWhenItCannotSeeMagpie: an unreadable group is
// unknown, and unknown is refused unless ops says otherwise.
func TestRemoveNeedsForceWhenItCannotSeeMagpie(t *testing.T) {
	f := newFixture(t)
	f.magpie.fail(500, `{"error":"no"}`)
	if _, err := f.ops().Remove(context.Background(), "team-a", false); err == nil {
		t.Fatal("a removal went ahead without being able to read magpie's groups")
	}
	if len(f.teamsOnDisk(t)) != 2 {
		t.Error("a refused removal took the team away")
	}
	gone, err := f.ops().Remove(context.Background(), "team-a", true)
	if err != nil {
		t.Fatalf("a forced removal was refused: %v", err)
	}
	if gone.Read {
		t.Error("the removal says it read a group it could not")
	}
}

// A key that manages magpie is never a team's: it could change every team,
// and a team's key must reach its own accounts only.
func TestAddRefusesAKeyThatManagesMagpie(t *testing.T) {
	f := newFixture(t)
	f.magpie.keys[0].Admin = "local"
	_, err := f.ops().Add(context.Background(), NewTeam{Name: "team-c", GroupID: groupC, KeyID: keyA})
	if err == nil || !strings.Contains(err.Error(), "manages magpie") {
		t.Fatalf("a team was made on a key that manages magpie: %v", err)
	}
}

// TestAddHoldsTheKeyToItsGroup: a key that doesn't name the team's group
// reaches every account magpie has, of every provider — so a team on it
// would draw one group's accounts and spend another's. Add puts the group
// on the key, so a team made from the page is held as the portal draws it,
// and ops never has to run a command to make the boundary real.
func TestAddHoldsTheKeyToItsGroup(t *testing.T) {
	for _, tc := range []struct {
		name     string
		accounts []string
		held     bool // Add has to write the key
	}{
		{"no accounts at all", nil, true},
		{"another group", []string{"@" + groupA}, true},
		{"its group kept out", []string{"!@" + groupC}, true},
		{"an account, not the group", []string{"claude/" + acctA1}, true},
		{"its group", []string{"@" + groupC}, false},
		{"its group beside another", []string{"@" + groupC, "@" + groupA}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.magpie.keys[2].Accounts = tc.accounts // keyC
			if _, err := f.ops().Add(context.Background(), NewTeam{Name: "Team C", GroupID: groupC, KeyID: keyC}); err != nil {
				t.Fatalf("the team wasn't made: %v", err)
			}
			want := []string{}
			if tc.held {
				want = []string{keyC + "@" + groupC}
			}
			if got := f.magpie.heldKeys(); !slices.Equal(got, want) {
				t.Errorf("Add held %v, want %v", got, want)
			}
			// and the key magpie keeps now names the group, which is what
			// makes the boundary real on the next request
			if !slices.Contains(f.magpie.keys[2].Accounts, "@"+groupC) {
				t.Errorf("the key's accounts are %v, which don't name the group", f.magpie.keys[2].Accounts)
			}
			if got := len(f.teamsOnDisk(t)); got != 3 {
				t.Errorf("teams.json holds %d teams, want 3", got)
			}
		})
	}
}

// TestAddRecordsNoTeamWhenTheKeyCantBeHeld: a write that fails stops the
// team from being made. A team whose key reaches every account of magpie
// is the one thing the portal must never draw, so nothing is recorded.
func TestAddRecordsNoTeamWhenTheKeyCantBeHeld(t *testing.T) {
	f := newFixture(t)
	f.magpie.keys[2].Accounts = nil // keyC
	f.magpie.refuseHold = 500
	_, err := f.ops().Add(context.Background(), NewTeam{Name: "Team C", GroupID: groupC, KeyID: keyC})
	if err == nil || !strings.Contains(err.Error(), "couldn't be held") {
		t.Fatalf("a team was made on a key that couldn't be held: %v", err)
	}
	if got := len(f.teamsOnDisk(t)); got != 2 {
		t.Errorf("a failed hold recorded a team: %d teams, want 2", got)
	}
}
