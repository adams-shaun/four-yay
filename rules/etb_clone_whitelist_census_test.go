package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// etbCloneCleanNames is the measured set of corpus cards carrying an
// ETBReplacement:Copy keyword whose DB$ Clone body's parameter set lies
// entirely inside etbCloneWhitelist's supported scope {Choices$, AddTypes$,
// AddKeywords$, SpellDescription$} AND whose values are inside it too (no
// SVar-resolved selector predicate, no multi-word AddKeywords$ head -- see
// etbCloneWhitelist). The pin is bidirectional: a corpus-pin
// bump or a scope change that alters membership fails here and forces a
// deliberate re-measure, so the supported boundary cannot widen (a new
// parameter accepted into etbCloneWhitelist) or narrow silently.
var etbCloneCleanNames = []string{
	"Clever Impersonator", "Clone", "Copy Artifact", "Copy Enchantment", "Copy Land",
	"Dack's Duplicate", "Deceptive Frostkite", "Glasspool Mimic",
	"Jwari Shapeshifter", "Malleable Impostor", "Masterwork of Ingenuity", "Mirror Image",
	"Mirrormade", "Mocking Doppelganger", "Naga Fleshcrafter", "Omni-Changeling",
	"Phyrexian Metamorph", "Sakashima of a Thousand Faces", "Sakashima's Protege", "Sakashima's Student", "Sculpting Steel",
	"Stunt Double", "Synth Infiltrator", "Vesuva", "Visage Bandit", "Waxen Shapethief",
}

// TestETBCloneWhitelistCensus walks every ETBReplacement:Copy Repl in the
// corpus and asserts etbCloneWhitelist classifies each body exactly as the
// scope says: offered iff every parameter is inside {Choices, AddTypes,
// AddKeywords, SpellDescription}. A card is never classified both ways.
func TestETBCloneWhitelistCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	var clean []string
	verdicts := map[string]bool{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			for i := range f.Repls {
				r := &f.Repls[i]
				if r.Params["Keyword"] != "ETBReplacement" || r.With == nil || r.With.API != "Clone" {
					continue
				}
				v := etbCloneWhitelist(r.With, f.SVars)
				if prev, ok := verdicts[f.Name]; ok && prev != v {
					t.Errorf("%s carries ETB Clone bodies classified both offered and not", f.Name)
				}
				verdicts[f.Name] = v
				if v {
					clean = append(clean, f.Name)
				}
			}
		}
	}
	slices.Sort(clean)
	t.Logf("etb clone carriers: %d total, %d inside the whitelist scope", len(verdicts), len(clean))
	if !slices.Equal(clean, etbCloneCleanNames) {
		t.Errorf("whitelist-clean carrier set moved; re-measure and re-pin.\n got (%d): %v\nwant (%d): %v",
			len(clean), clean, len(etbCloneCleanNames), etbCloneCleanNames)
	}
}

// TestETBCloneWhitelistRegressionCarriers pins the classification of the
// specific carriers the round-1 blacklist got wrong (it admitted
// IntoPlayTapped$/ChoiceTitle$-shaped bodies) and of the end-to-end test
// cards.
func TestETBCloneWhitelistRegressionCarriers(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	pins := []struct {
		name string
		want bool
	}{
		{"Malleable Impostor", true},
		{"Mirror Image", true},
		// Moritte's body rides SubAbility$ DBConditionEffect (the "if it's a
		// creature, it enters with two additional +1/+1 counters" condition),
		// so despite its plain Choices$ selector it is out of scope.
		{"Moritte of the Frost", false},
		// Supported KEYS, unsupported VALUES (see etbCloneWhitelist):
		// Mockingbird's Choices$ Creature.Other+cmcLEY needs an SVar
		// resolver the ETB matchers do not have, and Flesh Duplicate's
		// AddKeywords$ IfNew Vanishing:3 is a conditional this build cannot
		// install. Both keep the loud fallback; see
		// rules/etb_clone_unsupported_riders_test.go for the end-to-end pins.
		{"Mockingbird", false},
		{"Flesh Duplicate", false},
		// staticgoad1: Mocking Doppelganger's AddStaticAbilities$ FamilyTease
		// is a readable Goad$ static, so its ETB copy is offered and the
		// granted goad registers (rules/goad_granted_test.go pins it end to
		// end). A carrier whose AddStaticAbilities$ member is NOT a readable
		// Goad line stays withheld, the whitelist's own value gate.
		{"Mocking Doppelganger", true},
		{"Vesuva", true},                // IntoPlayTapped$ True now taps the entering copy
		{"Cursed Mirror", false},        // Duration$ UntilEndOfTurn
		{"Mirrorhall Mimic", false},     // ChoiceTitle$
		{"Vizier of Many Faces", false}, // SetColor$/AddTypes$ + Embalm$ provenance
	}
	for _, p := range pins {
		c, ok := reg.Lookup(p.name)
		if !ok {
			t.Fatalf("corpus missing %s", p.name)
		}
		found := false
		for _, f := range c.Faces {
			for i := range f.Repls {
				r := &f.Repls[i]
				if r.Params["Keyword"] != "ETBReplacement" || r.With == nil || r.With.API != "Clone" {
					continue
				}
				found = true
				if got := etbCloneWhitelist(r.With, f.SVars); got != p.want {
					t.Errorf("%s: etbCloneWhitelist = %v, want %v (body: %s)", p.name, got, p.want, r.With.Line)
				}
			}
		}
		if !found {
			t.Fatalf("%s carries no ETBReplacement:Copy DB$ Clone body", p.name)
		}
	}
}

// TestCursedMirrorETBKeepsLoudFallback pins the out-of-scope boundary end to
// end: a cast of Cursed Mirror (a Duration$ carrier the whitelist declines)
// with an eligible creature on the battlefield must offer no copy election,
// must enter as itself, must not copy, and must retain the loud
// unimplemented-API Clone fallback note.
func TestCursedMirrorETBKeepsLoudFallback(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Cursed Mirror"))
	bear := e.G.AddObject(corpusAlternativeCard(t, "Colossal Dreadmaw"), 0)
	bear.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{bear.ID})
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 2
	e.G.Players[0].Pool[state.MR] = 1
	castMode(t, e, id, "")
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && d.Source == id {
		t.Fatalf("out-of-scope carrier must not offer the copy election: %+v", d)
	}
	finishCast(t, e, id)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("mirror did not enter as itself: %+v", o)
	}
	if hasEvent(e, events.ClonePermanent, id) {
		t.Fatal("out-of-scope carrier must not copy")
	}
	if !hasNote(e, "unimplemented API Clone") {
		t.Fatal("loud Clone fallback note missing from the log")
	}
}
