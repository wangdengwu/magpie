package gui

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/access"
)

// issueUnder makes a key that may issue keys, and answers its id.
func issueUnder(t *testing.T, name string, models ...string) string {
	t.Helper()
	secret, err := access.Update("add-key", access.Change{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	keys, err := access.List()
	if err != nil {
		t.Fatal(err)
	}
	id := keys[len(keys)-1].ID
	if secret == "" {
		t.Fatal("no secret for " + name)
	}
	if len(models) > 0 {
		if _, err := access.Update("models-key", access.Change{Key: id, Models: models}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := access.Update("issuer-key", access.Change{Key: id, Issues: true}); err != nil {
		t.Fatal(err)
	}
	return id
}

func issuedFromAdmin(t *testing.T, body string) (access.Issued, *http.Response) {
	t.Helper()
	w := adminDo(t, "POST", "/gateway-keys/issue-key", body)
	if w.Code != 200 {
		t.Fatalf("issue-key: %d %s", w.Code, w.Body)
	}
	var out access.Issued
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err, w.Body.String())
	}
	return out, w.Result()
}

// A team portal issues a member's key through the management API, with no
// key of its own: the key it names is the issuer, and what comes back is
// the one time the secret is shown.
func TestAdminIssuesAMemberKey(t *testing.T) {
	sandboxHome(t)
	team := issueUnder(t, "team-a-key", "claude/*")
	other := issueUnder(t, "team-b-key", "claude/*")

	k, _ := issuedFromAdmin(t, `{"issuer":"`+team+`","name":"Alice","ttlSeconds":`+
		"604800"+`}`)
	if !strings.HasPrefix(k.Key, access.Prefix) || k.Name != "Alice" || k.Issuer != team {
		t.Fatalf("issued %+v", k)
	}
	// it is the team's key's: another team's model is refused
	if w := adminDo(t, "POST", "/gateway-keys/issue-key",
		`{"issuer":"`+team+`","name":"bob","ttlSeconds":86400,"models":["openai/gpt-5"]}`); w.Code == 200 {
		t.Fatalf("issued a model the issuer may not use: %s", w.Body)
	}

	// the list names the team's own keys and no secret
	list := adminDo(t, "GET", "/gateway-keys/issued-keys?issuer="+team, "")
	if list.Code != 200 || !strings.Contains(list.Body.String(), "Alice") {
		t.Fatalf("list: %d %s", list.Code, list.Body)
	}
	if strings.Contains(list.Body.String(), k.Key) {
		t.Fatal("the list tells a key's secret")
	}
	if otherList := adminDo(t, "GET", "/gateway-keys/issued-keys?issuer="+other, ""); strings.Contains(otherList.Body.String(), "Alice") {
		t.Fatal("another issuer's list names this team's key")
	}

	// revoking another issuer's key answers as one that isn't there
	if w := adminDo(t, "POST", "/gateway-keys/revoke-key", `{"issuer":"`+other+`","id":"`+k.ID+`"}`); w.Code != http.StatusNotFound {
		t.Fatalf("revoked another issuer's key: %d %s", w.Code, w.Body)
	}
	if _, ok := access.Authenticate(k.Key); !ok {
		t.Fatal("a key answered 404 as revoked was revoked")
	}
	if w := adminDo(t, "POST", "/gateway-keys/revoke-key", `{"issuer":"`+team+`","id":"`+k.ID+`"}`); w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body)
	}
	if _, ok := access.Authenticate(k.Key); ok {
		t.Fatal("a revoked key still authenticates")
	}
}

// Only a key that may issue keys can be named as the issuer, and a member
// key can't be made one.
func TestAdminIssuesOnlyUnderAnIssuer(t *testing.T) {
	sandboxHome(t)
	plain, _ := access.Update("add-key", access.Change{Name: "Not An Issuer"})
	keys, _ := access.List()
	plainID := keys[len(keys)-1].ID
	if w := adminDo(t, "POST", "/gateway-keys/issue-key",
		`{"issuer":"`+plainID+`","name":"x","ttlSeconds":3600}`); w.Code == 0 || w.Code == 200 {
		t.Fatalf("issued under a key that may not: %d %s", w.Code, w.Body)
	}
	if w := adminDo(t, "POST", "/gateway-keys/issue-key",
		`{"issuer":"nosuchkey","name":"x","ttlSeconds":3600}`); w.Code == 200 {
		t.Fatalf("issued under a key that isn't there: %s", w.Body)
	}
	if w := adminDo(t, "POST", "/gateway-keys/issue-key",
		`{"issuer":"`+plainID+`","name":"x","ttlSeconds":"forever"}`); w.Code == 200 {
		t.Fatalf("issued with a ttl that isn't one: %s", w.Body)
	}
	_ = plain
	// a member key can't become an issuer, through the API or otherwise
	team := issueUnder(t, "team-a-key", "claude/*")
	k, _ := issuedFromAdmin(t, `{"issuer":"`+team+`","name":"Alice","ttlSeconds":3600}`)
	if _, err := access.Update("issuer-key", access.Change{Key: k.ID, Issues: true}); err == nil {
		t.Fatal("a member key was let issue")
	}
	if _, err := access.Update("admin-key", access.Change{Key: k.ID, Admin: "local"}); err == nil {
		t.Fatal("a member key was given the management level")
	}
}

// The hundred-year key the portal calls permanent is issued through the
// same route, and reads back with an expiry in this century's far end.
func TestAdminIssuesAPermanentMemberKey(t *testing.T) {
	sandboxHome(t)
	team := issueUnder(t, "team-a-key", "claude/*")
	k, _ := issuedFromAdmin(t, `{"issuer":"`+team+`","name":"Alice","ttlSeconds":`+
		"3153600000"+`}`)
	if d := time.Until(k.Expires); d < 99*365*24*time.Hour {
		t.Fatalf("a permanent key expires in %s", d)
	}
	if _, ok := access.Authenticate(k.Key); !ok {
		t.Fatal("a permanent key doesn't authenticate")
	}
}
