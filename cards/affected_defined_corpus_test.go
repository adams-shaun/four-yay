package cards_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// affectedDefinedUnread is every AffectedDefined$ value NormalizeAffectedDefined
// deliberately leaves alone, with the cards that carry it. A new upstream
// value fails this test by name instead of silently scoping a static to
// nothing.
var affectedDefinedUnread = map[string][]string{
	// Havengul Lich's granted static names the Effect's source; no legacy
	// Affected$ form existed for it.
	"EffectSource": {"Havengul Lich"},
}

func TestEveryAffectedDefinedValueIsNormalizedOrNamed(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	got := map[string]map[string]bool{}
	equippedBy := 0
	note := func(card string, p map[string]string) {
		if strings.Contains(p["Affected"], ".EquippedBy") {
			equippedBy++
		}
		v, ok := p["AffectedDefined"]
		if !ok {
			return
		}
		if got[v] == nil {
			got[v] = map[string]bool{}
		}
		got[v][card] = true
	}
	for _, c := range reg.AllCards() {
		if c == nil || len(c.Faces) == 0 {
			continue
		}
		name := c.Faces[0].Name
		for _, f := range c.Faces {
			for _, st := range f.Statics {
				note(name, st.Params)
			}
			for _, body := range f.SVars {
				if !strings.Contains(body, "Mode$") || !strings.Contains(body, "AffectedDefined$") {
					continue
				}
				if sts, ok := cards.ParseStaticLines(body); ok {
					note(name, sts[0].Params)
				}
			}
		}
	}
	for v, set := range got {
		var names []string
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
		want, named := affectedDefinedUnread[v]
		if !named {
			t.Errorf("AffectedDefined$ %q is neither normalized nor named in affectedDefinedUnread: %v", v, names)
			continue
		}
		if strings.Join(want, "|") != strings.Join(names, "|") {
			t.Errorf("AffectedDefined$ %q carriers %v, table says %v", v, names, want)
		}
	}
	for v := range affectedDefinedUnread {
		if got[v] == nil {
			t.Errorf("affectedDefinedUnread row %q is stale: no card carries it", v)
		}
	}
	// Vacuity guard: the old corpus carried Creature.EquippedBy on ~580
	// cards; if the rewrite stopped running, this count collapses.
	if equippedBy < 500 {
		t.Errorf("only %d statics scope to .EquippedBy; the AffectedDefined$ rewrite is not reaching the corpus", equippedBy)
	}
}
