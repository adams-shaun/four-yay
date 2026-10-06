package oraclegen

import (
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/compliance"
)

// xmageKnown is the folded name set of every card in XMage's database, as the
// committed set manifests list it. nil means no set is installed (unit tests
// that never loaded the manifests).
var xmageKnown atomic.Pointer[map[string]bool]

// SetXMageKnown installs the card names XMage's database holds; the caller
// (cmd/oraclediff) reads them from compliance/manifests/*.json. A split or
// double-faced name ("Fire // Ice") is installed under each face as well as
// whole, since the corpus names a face and XMage's database the pair. A nil or
// empty list uninstalls the set.
func SetXMageKnown(names []string) {
	if len(names) == 0 {
		xmageKnown.Store(nil)
		return
	}
	set := make(map[string]bool, len(names)*2)
	for _, n := range names {
		set[compliance.FoldName(n)] = true
		if strings.Contains(n, " // ") {
			for _, face := range strings.Split(n, " // ") {
				set[compliance.FoldName(face)] = true
			}
		}
	}
	xmageKnown.Store(&set)
}

// XMageKnown reports whether a probe or fixture card may be placed, cast or
// put in a zone: XMage's addCard throws "Couldn't find a card" on a name its
// database lacks (Un-set cards, Alchemy-only and Commander-only cards). Every
// probe picker that walks the corpus or lists names by hand gates on it. An
// Alchemy "A-" rebalance and a quoted name are never used, whatever the set
// holds: a deck list does not carry them (the exclusion the cost pickers had
// before this predicate). With no set installed that is all it checks.
func XMageKnown(name string) bool {
	if name == "" || strings.HasPrefix(name, "A-") || strings.Contains(name, `"`) {
		return false
	}
	if set := xmageKnown.Load(); set != nil {
		return (*set)[compliance.FoldName(name)]
	}
	return true
}
