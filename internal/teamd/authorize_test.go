package teamd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAuthorizeAnswersTheTokenTeam is the page that only lists a team: a
// valid token with no target answers its team and that team's group.
func TestAuthorizeAnswersTheTokenTeam(t *testing.T) {
	f := newFixture(t)
	g, err := f.srv.Authorize(context.Background(), f.tokenA, Target{})
	if err != nil {
		t.Fatalf("a valid token was refused: %v", err)
	}
	if g.Team().ID != "team-a" {
		t.Errorf("the token answered team %q, want team-a", g.Team().ID)
	}
	if g.Group().ID != groupA {
		t.Errorf("the grant holds group %q, want %q", g.Group().ID, groupA)
	}
	if got := g.Accounts(); len(got) != 2 {
		t.Errorf("the grant holds %d accounts, want 2: %q", len(got), got)
	}
}

// TestAuthorizeGrantsAnAccountOfTheTeamsGroup: the account a team holds
// may be acted on, told apart as magpie tells accounts apart.
func TestAuthorizeGrantsAnAccountOfTheTeamsGroup(t *testing.T) {
	f := newFixture(t)
	for _, target := range []string{acctA1, acctA2, strings.ToUpper(acctA1), "  " + acctA1 + " "} {
		if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{Account: target}); err != nil {
			t.Errorf("the team's own account %q was refused: %v", target, err)
		}
	}
}

// TestAuthorizeRefusesAnotherTeamsAccount is the boundary itself, not a
// caller of it: team A's token may not name team B's account.
func TestAuthorizeRefusesAnotherTeamsAccount(t *testing.T) {
	f := newFixture(t)
	_, err := f.srv.Authorize(context.Background(), f.tokenA, Target{Account: acctB1})
	if !errors.Is(err, ErrNotYours) {
		t.Fatalf("team A naming team B's account answered %v, want ErrNotYours", err)
	}
	// and nothing of where it is: a team learns nothing about another
	for _, leak := range []string{"team-b", groupB, "Team B"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the refusal names %q: %v", leak, err)
		}
	}
}

// TestAuthorizeRefusesAnAccountNoGroupHolds: an id that is shaped like an
// account but is in no group is refused, not passed on to magpie to judge.
func TestAuthorizeRefusesAnAccountNoGroupHolds(t *testing.T) {
	f := newFixture(t)
	for _, target := range []string{"claude/ffffffffffffffff", "codex/1a2b3c4d5e6f7a80", "claude/", "/x", "claude", "a/b/c", "@" + groupB, "!" + acctA1} {
		if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{Account: target}); !errors.Is(err, ErrNotYours) {
			t.Errorf("the account %q answered %v, want ErrNotYours", target, err)
		}
	}
}

// TestAuthorizeChecksTheGroupAnActionNames: an action naming a group may
// only name this team's own.
func TestAuthorizeChecksTheGroupAnActionNames(t *testing.T) {
	f := newFixture(t)
	if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{Group: groupA}); err != nil {
		t.Errorf("the team's own group was refused: %v", err)
	}
	if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{Group: strings.ToUpper(groupA)}); err != nil {
		t.Errorf("the team's own group, in upper case, was refused: %v", err)
	}
	if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{Group: groupB}); !errors.Is(err, ErrNotYours) {
		t.Errorf("another team's group answered %v, want ErrNotYours", err)
	}
}

// TestAuthorizeRefusesAnUnknownToken: a link that names no team is 404 and
// says nothing more, whatever was cut off it.
func TestAuthorizeRefusesAnUnknownToken(t *testing.T) {
	f := newFixture(t)
	for _, token := range []string{"", "   ", "nope", f.tokenA[:len(f.tokenA)-1], f.tokenA + "x", f.tokenB[:20]} {
		g, err := f.srv.Authorize(context.Background(), token, Target{})
		if !errors.Is(err, ErrNoTeam) {
			t.Errorf("the token %q answered %v, want ErrNoTeam", token, err)
		}
		if g.Team().ID != "" || g.Group().ID != "" {
			t.Errorf("a refused token answered a grant: %+v", g)
		}
	}
}

// TestAuthorizeRefusesARotatedToken: rotating replaces the hash, so the
// old link stops working at once and the new one starts.
func TestAuthorizeRefusesARotatedToken(t *testing.T) {
	f := newFixture(t)
	newToken, newHash, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	f.teams[0].TokenHash = newHash
	if err := f.srv.Store().Save(f.teams); err != nil {
		t.Fatal(err)
	}
	if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{}); !errors.Is(err, ErrNoTeam) {
		t.Errorf("the rotated-out token answered %v, want ErrNoTeam", err)
	}
	if _, err := f.srv.Authorize(context.Background(), newToken, Target{}); err != nil {
		t.Errorf("the new token was refused: %v", err)
	}
}

// TestAuthorizeRefusesATeamWithNoToken: a team ops made but gave no link
// to answers every token as no team.
func TestAuthorizeRefusesATeamWithNoToken(t *testing.T) {
	f := newFixture(t)
	f.teams[0].TokenHash = ""
	if err := f.srv.Store().Save(f.teams); err != nil {
		t.Fatal(err)
	}
	if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{}); !errors.Is(err, ErrNoTeam) {
		t.Errorf("a team with no token answered %v, want ErrNoTeam", err)
	}
}

// TestAuthorizeRefusesWhenTheGroupIsGone: the page reads the group live,
// so a group ops removed takes the team's pages down with it.
func TestAuthorizeRefusesWhenTheGroupIsGone(t *testing.T) {
	f := newFixture(t)
	f.magpie.set([]Group{{ID: groupB, Name: "Team B", Accounts: []string{acctB1}}})
	if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{}); !errors.Is(err, ErrNoGroup) {
		t.Errorf("a team whose group is gone answered %v, want ErrNoGroup", err)
	}
	// and an account that was in it is refused too, not let through
	if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{Account: acctA1}); err == nil {
		t.Error("an account of a group magpie doesn't have was allowed")
	}
}

// TestAuthorizeFailsClosedWhenMagpieCantBeRead: an unreadable group is
// unknown, and unknown is refused — never "this team holds everything".
func TestAuthorizeFailsClosedWhenMagpieCantBeRead(t *testing.T) {
	f := newFixture(t)
	f.magpie.fail(500, `{"error":"account-groups.json doesn't read"}`)
	g, err := f.srv.Authorize(context.Background(), f.tokenA, Target{Account: acctA1})
	if err == nil {
		t.Fatalf("an unreadable magpie answered a grant: %+v", g)
	}
	if errors.Is(err, ErrNoTeam) || errors.Is(err, ErrNotYours) {
		t.Errorf("an unreadable magpie was answered as a refusal: %v", err)
	}
	if errors.Is(err, ErrNoGroup) {
		t.Errorf("a read that failed is reported as a group magpie doesn't have: %v", err)
	}
	if g.Team().ID != "" || g.Group().ID != "" {
		t.Errorf("a failed read answered a grant: %+v", g)
	}
}

// TestAuthorizeFailsClosedWhenTheTeamsCantBeRead: teams.json that doesn't
// parse is unknown too, and every link is refused.
func TestAuthorizeFailsClosedWhenTheTeamsCantBeRead(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.srv.Store().Dir(), teamsFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{}); err == nil {
		t.Fatal("a teams.json that doesn't parse let a token through")
	} else if errors.Is(err, ErrNoTeam) {
		t.Errorf("a file that failed to read is reported as a link that names no team: %v", err)
	}
}

// TestGrantHoldsOnlyItsGroup: the grant a page is handed answers for its
// own accounts and no others, whatever it is asked.
func TestGrantHoldsOnlyItsGroup(t *testing.T) {
	f := newFixture(t)
	g, err := f.srv.Authorize(context.Background(), f.tokenA, Target{})
	if err != nil {
		t.Fatal(err)
	}
	for _, own := range []string{acctA1, acctA2, strings.ToUpper(acctA2)} {
		if !g.Holds(own) {
			t.Errorf("the grant doesn't hold its own account %q", own)
		}
	}
	for _, other := range []string{acctB1, "claude/ffffffffffffffff", "", "claude", "@" + groupA} {
		if g.Holds(other) {
			t.Errorf("the grant holds %q, which is not in its group", other)
		}
	}
	// the accounts it hands back are a copy: a page changing them changes
	// nothing the next request reads
	got := g.Accounts()
	got[0] = "claude/tampered"
	if !g.Holds(acctA1) {
		t.Error("changing the accounts a grant handed back changed the grant")
	}
}

// TestAuthorizeAsksMagpieEveryTime: nothing is cached, so a group ops
// changed holds on the next action.
func TestAuthorizeAsksMagpieEveryTime(t *testing.T) {
	f := newFixture(t)
	if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{}); err != nil {
		t.Fatal(err)
	}
	before := f.magpie.callsTo()
	f.magpie.set([]Group{{ID: groupA, Name: "Team A", Accounts: []string{acctA1}}})
	if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{Account: acctA2}); !errors.Is(err, ErrNotYours) {
		t.Errorf("an account taken out of the group answered %v, want ErrNotYours", err)
	}
	if f.magpie.callsTo() == before {
		t.Error("the second action didn't ask magpie for the group")
	}
}
