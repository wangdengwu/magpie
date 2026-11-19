package teamd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

// What models a team's key may use, for the connect page.
//
// There are two answers, and which one is available is what decides:
//
//   - when the team's key is mounted, the gateway itself is asked, with
//     that key. That is the exact list a member's own client gets from
//     GET /v1/models, so the page can't disagree with what they see.
//   - when it isn't, magpie's management API is asked what the key is held
//     to, and that list is filtered here. It is the same answer the model
//     picker is built from, and it can be narrower than the gateway's —
//     a routing group a key names is a group of accounts, not a list of
//     model ids, and this side can't expand it. The page says so.

// ErrNoModels is magpie answering nothing the page can list.
var ErrNoModels = errors.New("magpie listed no models for this key")

// maxModels bounds what a page lists: a key allowed every model on a
// gateway with many providers would otherwise make a wall of text. It is
// the list a member scrolls, not the catalogue.
const maxModels = 60

// modelsFromGateway asks the gateway what the key may use, with the key.
//
// It goes over loopback to the same magpie the admin client calls: the
// gateway and the management API are one server, and a member's address
// may be a name this container can't resolve.
func (c *AdminClient) modelsFromGateway(ctx context.Context, key string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /v1/models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			// the key magpie has and the key mounted here aren't the same
			// one: that is worth saying plainly rather than as a failed read
			return nil, fmt.Errorf("the gateway refused the mounted key: %w", ErrKeyRefused)
		}
		return nil, c.answerError("GET /v1/models", resp)
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswer)).Decode(&out); err != nil {
		return nil, fmt.Errorf("GET /v1/models: magpie's answer doesn't read: %w", err)
	}
	ids := make([]string, 0, len(out.Data))
	for _, d := range out.Data {
		if strings.TrimSpace(d.ID) != "" {
			ids = append(ids, d.ID)
		}
	}
	return ids, nil
}

// ErrKeyRefused is the gateway turning away the key mounted for the page.
// It is the one failure worth telling a lead about directly: the snippets
// on the page would not work.
var ErrKeyRefused = errors.New("the gateway refused the key mounted for this page")

// modelsForKey is what magpie's management API says the key may use,
// filtered from the full catalogue by what the key is held to.
//
// A key held to nothing may use every model. A key held to some may use the
// ones named, each provider's "*", or a routing group's members — and a
// group is expanded by the gateway, not here, so a key held to one is
// reported as an incomplete list rather than a wrong one.
func (c *AdminClient) modelsForKey(ctx context.Context, allowed []string) (ids []string, complete bool, err error) {
	var out modelsAnswer
	if err := c.get(ctx, "gateway-keys/models", &out); err != nil {
		return nil, false, err
	}
	complete = true
	var groups []string
	for _, a := range allowed {
		if strings.HasPrefix(a, "group/") {
			groups = append(groups, a)
		}
	}
	if len(groups) > 0 {
		complete = false
	}
	for _, e := range out.Accounts {
		// the list carries accounts and keys too, and an account's id is
		// "claude/<stable id>", which looks like a model's
		// "<provider>/<model>" and is not one: a member naming it reaches
		// nothing, and an account id on a page sent around is a thing
		// nobody needs to see
		if e.Account || e.Key || !strings.Contains(e.ID, "/") {
			continue
		}
		if len(allowed) > 0 && !allowedModel(allowed, e.ID) {
			continue
		}
		ids = append(ids, e.ID)
	}
	return ids, complete, nil
}

// allowedModel says a key held to allowed may use the model id, by the
// forms magpie writes them in: the model itself, or its provider's "*".
func allowedModel(allowed []string, id string) bool {
	provider, _, _ := strings.Cut(id, "/")
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		switch {
		case strings.EqualFold(a, id):
			return true
		case a == strings.ToLower(provider)+"/*":
			return true
		case strings.HasPrefix(a, "group/"), strings.HasPrefix(a, "@"), strings.HasPrefix(a, "!"):
			// a group is the gateway's to expand; the caller has said the
			// list may be short
			continue
		}
	}
	return false
}

// ModelsFor is the team's key's models, and whether the list is the whole
// one. The key is asked of the gateway when it is mounted, which is the
// same answer a member gets; otherwise magpie's management API is asked
// and the list is filtered.
func (s *Server) ModelsFor(ctx context.Context, keyID string, mounted string) (ids []string, complete bool, err error) {
	if mounted != "" {
		ids, err := s.admin.modelsFromGateway(ctx, mounted)
		if err == nil {
			return capModels(ids), true, nil
		}
		if errors.Is(err, ErrKeyRefused) {
			return nil, false, err
		}
		// a gateway that can't be read falls through to the management
		// API, which is a different route on the same server: one being
		// unreadable is worth one more try, not a blank page
	}
	keys, err := s.admin.GatewayKeys(ctx)
	if err != nil {
		return nil, false, err
	}
	i := slices.IndexFunc(keys, func(k GatewayKey) bool { return strings.EqualFold(k.ID, keyID) })
	if i < 0 {
		return nil, false, fmt.Errorf("magpie has no gateway key %s", keyID)
	}
	ids, complete, err = s.admin.modelsForKey(ctx, keys[i].Models)
	if err != nil {
		return nil, false, err
	}
	return capModels(ids), complete, nil
}

// capModels is the first maxModels of ids.
func capModels(ids []string) []string {
	if len(ids) > maxModels {
		return ids[:maxModels]
	}
	return ids
}
