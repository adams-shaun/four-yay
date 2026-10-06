package rules

// The bare `Condition$ Delirium` gate (task agent-20260918T222201Z-c613f1f2):
// effects/conditions.go's bare `Condition$` branch used to evaluate only
// Kicked/Foretold/Revolt/Blessing, so a bare `Condition$ Delirium` sub-effect
// was UNRESOLVED and ran UNCONDITIONALLY — Descend upon the Sinful's
// DB$ Token created its 4/4 flying Angel even with an empty graveyard, an
// over-grant that looks like the card works. The gate now reads
// Host.DeliriumHolds, which the Engine implements as the ONE
// graveyardCardTypeCount census the replacement Delirium$ clause
// (rules/replacement.go), the Continuous static gate (rules/layers.go) and
// the ability-offer gate (rules/legal.go) already shared — so every Delirium
// spelling answers identically.
//
// The pin drives the REAL compiled corpus card (never a re-written copy) end
// to end: cast with fewer than four distinct card types in the caster's
// graveyard → no Angel; mill to four distinct types → exactly one Angel
// token. The middle arm mills FOUR cards but only THREE distinct types, so
// the distinct-type census (not a card count) is what the gate reads.

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const angelTokenKey = "w_4_4_angel_flying"

// seedAngelToken backs the TokenCreate with the corpus's REAL token script
// (gateFixture seeds an empty token table; the entreat_the_angels_test.go
// precedent fills it from the corpus registry, so the minted Angel is the
// real w_4_4_angel_flying token, not a test fixture).
func seedAngelToken(t *testing.T, e *Engine) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Tokens[angelTokenKey]
	if !ok {
		t.Fatal("corpus token registry has no w_4_4_angel_flying for Descend's TokenScript$")
	}
	setFixtureToken(e, angelTokenKey, c)
}

const deliriumBearSrc = "Name:Delirium Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"
const deliriumPlainsSrc = "Name:Delirium Plains\nTypes:Land\nOracle:x\n"
const deliriumBoltSrc = "Name:Delirium Bolt\nTypes:Instant\nOracle:x\n"
const deliriumOathSrc = "Name:Delirium Oath\nTypes:Enchantment\nOracle:x\n"

// millExtras moves the named authored extras from wherever the opening deal
// left them (hand or library) to seat 0's GRAVEYARD with logged MoveZone
// events — exactly what graveyardCardTypeCount's census reads.
func millExtras(t *testing.T, e *Engine, names ...string) {
	t.Helper()
	for _, name := range names {
		gateMoveFromLibrary(t, e, name, state.ZGraveyard)
	}
}

// graveyardDeliriumTypes is the precondition check: the distinct core card
// types seat 0's graveyard holds, computed independently of the engine's
// census (a direct scan of the same folded state).
func graveyardDeliriumTypes(t *testing.T, e *Engine, seat state.PlayerID) []string {
	t.Helper()
	core := map[string]bool{"Artifact": true, "Battle": true, "Creature": true,
		"Enchantment": true, "Instant": true, "Kindred": true, "Land": true,
		"Planeswalker": true, "Sorcery": true}
	seen := map[string]bool{}
	var order []string
	for _, id := range e.G.Zone(state.ZGraveyard, seat) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		for _, typ := range o.Face().Types {
			if core[typ] && !seen[typ] {
				seen[typ] = true
				order = append(order, typ)
			}
		}
	}
	return order
}

// castDescend drives a real Descend upon the Sinful cast (4WW) resolving off
// the stack. ChangeZoneAll names no targets, so the cast ask is the only
// decision the flow poses.
func castDescend(t *testing.T, e *Engine, dec state.ObjID) {
	t.Helper()
	addMana(t, e, 0, "WWWWWW")
	opt := castOptMode(t, castOptions(t, e), dec, "")
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
}

// TestDescendUponTheSinfulBareDeliriumGatesTheAngel is the brief's named
// card: the chained DB$ Token creates its 4/4 white Angel creature token
// with flying only when the caster's graveyard already held four or more
// distinct core card types at resolution.
func TestDescendUponTheSinfulBareDeliriumGatesTheAngel(t *testing.T) {
	t.Run("empty graveyard", func(t *testing.T) {
		e, cfg, dec := gateFixture(t, 941, "Descend upon the Sinful")
		seedAngelToken(t, e)
		castDescend(t, e, dec)
		if got := battlefieldNamed(t, e, "Angel Token"); got != 0 {
			t.Fatalf("empty graveyard created %d Angel tokens, want 0", got)
		}
		if o := e.G.Obj(dec); o.Zone != state.ZGraveyard {
			t.Fatalf("resolved Descend in %s, want graveyard", o.Zone)
		}
		replayCheck(t, e, cfg)
	})
	t.Run("four cards, three types", func(t *testing.T) {
		// Four cards but only three DISTINCT core types (two creatures): the
		// census counts types, not cards, so the gate still denies.
		e, cfg, dec := gateFixture(t, 942, "Descend upon the Sinful",
			deliriumBearSrc, deliriumPlainsSrc, deliriumBoltSrc,
			"Name:Delirium Bear 2\nTypes:Creature\nPT:2/2\nOracle:x\n")
		seedAngelToken(t, e)
		millExtras(t, e, "Delirium Bear", "Delirium Bear 2", "Delirium Plains", "Delirium Bolt")
		if got := len(graveyardDeliriumTypes(t, e, 0)); got != 3 {
			t.Fatalf("precondition: %d distinct graveyard types, want 3", got)
		}
		castDescend(t, e, dec)
		if got := battlefieldNamed(t, e, "Angel Token"); got != 0 {
			t.Fatalf("three distinct types created %d Angel tokens, want 0", got)
		}
		replayCheck(t, e, cfg)
	})
	t.Run("four distinct types", func(t *testing.T) {
		e, cfg, dec := gateFixture(t, 943, "Descend upon the Sinful",
			deliriumBearSrc, deliriumPlainsSrc, deliriumBoltSrc, deliriumOathSrc)
		seedAngelToken(t, e)
		millExtras(t, e, "Delirium Bear", "Delirium Plains", "Delirium Bolt", "Delirium Oath")
		types := graveyardDeliriumTypes(t, e, 0)
		if len(types) != 4 {
			t.Fatalf("precondition: %d distinct graveyard types (%v), want 4", len(types), types)
		}
		castDescend(t, e, dec)
		if got := battlefieldNamed(t, e, "Angel Token"); got != 1 {
			t.Fatalf("four distinct types created %d Angel tokens, want 1", got)
		}
		angel := state.ObjID(0)
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Angel Token" {
				angel = id
			}
		}
		if angel == 0 {
			t.Fatal("the minted Angel vanished from the battlefield")
		}
		o := e.G.Obj(angel)
		if !o.IsToken {
			t.Fatalf("the Angel mint is IsToken=false")
		}
		if o.Face().Name != "Angel Token" || o.Face().Power() != 4 || o.Face().Toughness() != 4 {
			t.Fatalf("Angel face %q %d/%d, want Angel Token 4/4",
				o.Face().Name, o.Face().Power(), o.Face().Toughness())
		}
		replayCheck(t, e, cfg)
	})
	t.Run("the census is the controller's, not the opponent's", func(t *testing.T) {
		// Only seat 0 mills; seat 1 holds the four types but never casts, so
		// the seat-0 cast's gate reads seat 0's graveyard alone. (The two
		// subtests above already pin both directions of the gate; this one
		// pins whose graveyard the census counts.)
		e, cfg, dec := gateFixture(t, 944, "Descend upon the Sinful",
			deliriumBearSrc, deliriumPlainsSrc, deliriumBoltSrc, deliriumOathSrc)
		seedAngelToken(t, e)
		millExtras(t, e, "Delirium Bear", "Delirium Plains", "Delirium Bolt", "Delirium Oath")
		castDescend(t, e, dec)
		if got := battlefieldNamed(t, e, "Angel Token"); got != 1 {
			t.Fatalf("controller's four types created %d Angel tokens, want 1", got)
		}
		replayCheck(t, e, cfg)
	})
}
