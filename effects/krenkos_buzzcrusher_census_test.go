package effects

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// buzzcrusherSearchCensus is the corpus ratchet for the script shape the
// zone_changePrelude guard gates: a ChangeZone that is an optional search
// owned by `DefinedPlayer$ RememberedController` and fed by a
// `RememberDestroyed$ True` Destroy in the same SubAbility chain (Krenko's
// Buzzcrusher's "for each land destroyed this way, its controller may search
// their library for a basic land card"). The guard keys on that exact pair, so
// any new carrier appears here and fails until it is examined; a removed
// carrier fails as stale.
var buzzcrusherSearchCensus = []string{"Krenko's Buzzcrusher"}

// TestBuzzcrusherDestroyedLandSearchCensus walks every corpus SVar body and
// reports the cards whose script pairs a Destroy with `RememberDestroyed$
// True` and an optional ChangeZone search owned by `DefinedPlayer$
// RememberedController`. It is the black-box companion to the focused
// behaviour test: a new carrier of the shape, or a removal of this one, fails
// here visibly rather than silently growing stale.
func TestBuzzcrusherDestroyedLandSearchCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	got := map[string]bool{}
	for _, card := range reg.AllCards() {
		for _, face := range card.Faces {
			if faceHasDestroyedLandSearch(face) {
				got[face.Name] = true
			}
		}
	}
	var names []string
	for name := range got {
		names = append(names, name)
	}
	sort.Strings(names)
	if strings.Join(names, "|") != strings.Join(buzzcrusherSearchCensus, "|") {
		t.Errorf("destroyed-land optional RememberedController search carriers = %q, want %q", names, buzzcrusherSearchCensus)
	}
}

// faceHasDestroyedLandSearch reports whether this face has any chain that
// contains both a Destroy with `RememberDestroyed$ True` and an optional
// ChangeZone search whose owner is `DefinedPlayer$ RememberedController`.
func faceHasDestroyedLandSearch(face *cards.Face) bool {
	names := make([]string, 0, len(face.SVars))
	for name := range face.SVars {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if chainHasDestroyedLandSearch(face.SVars, name, map[string]bool{}) {
			return true
		}
	}
	return false
}

// chainHasDestroyedLandSearch walks an SVar's SubAbility chain (the Forge
// resolver's own chaining, not RepeatSubAbility$/Choices$ which are separate
// bodies) looking for the Destroy/RememberDestroyed pair and the optional
// RememberedController search anywhere on the chain.
func chainHasDestroyedLandSearch(svars map[string]string, start string, seen map[string]bool) bool {
	destroyed, optionalSearch := false, false
	for name := strings.TrimSpace(start); name != ""; {
		if seen[name] {
			break
		}
		seen[name] = true
		sa := cards.ResolveSVar(svars, name)
		if sa == nil {
			break
		}
		if sa.API == "Destroy" && strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberDestroyed)), "True") {
			destroyed = true
		}
		if sa.API == "ChangeZone" {
			cz := ChangeZoneOf(sa)
			if cz.OptionalTrue && cz.DefinedPlayer.Text == "RememberedController" {
				optionalSearch = true
			}
		}
		name = strings.TrimSpace(sa.ParamStr(cards.PKSubAbility))
	}
	return destroyed && optionalSearch
}
