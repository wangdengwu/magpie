package teamcore

import "strings"

// FindGroup is the group with id, told apart in lower case as magpie tells
// group ids apart.
func FindGroup(groups []Group, id string) (Group, bool) {
	for _, g := range groups {
		if strings.EqualFold(g.ID, strings.TrimSpace(id)) {
			return g, true
		}
	}
	return Group{}, false
}
