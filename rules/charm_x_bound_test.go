package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// A Charm sub-ability's dynamic TargetMin$/TargetMax$ X bound must resolve
// against the cast's PAID X, not a hardcoded zero (the two charm ask sites
// are the only ask sites that dropped the x). Both pins drive the real cast
// flow -- cast choice, the X announcement (KChoose, Kind "x"), the charm
// mode ask, then the sequential per-mode target asks -- on an inline
// Kozilek's-Command-shaped fixture (script text written inline, never a
// committed corpus file).

const charmXBoundSrc = "Name:X Charm\nManaCost:X R R\nTypes:Instant\n" +
	"A:SP$ Charm | Choices$ DBBolt,DBExileGrave | CharmNum$ 2 | Announce$ X\n" +
	// LoseLife (registered) rather than Damage (not a registered API in
	// this build): the bolt leg is a bound, not an effect, probe.
	"SVar:DBBolt:DB$ LoseLife | ValidTgts$ Player | LifeAmount$ 1 | TargetMin$ 1 | TargetMax$ 1 | SpellDescription$ Bolt.\n" +
	"SVar:DBExileGrave:DB$ ChangeZone | TargetMin$ 0 | TargetMax$ X | Origin$ Graveyard | Destination$ Exile | ValidTgts$ Card | TgtPrompt$ Choose target card in a graveyard | SpellDescription$ Exile up to X target cards from graveyards.\n" +
	"SVar:X:Count$xPaid\nOracle:x\n"

// charmXCastFlow drives a cast of charmXBoundSrc through the real cast flow
// -- X announce (xLabel picks it, e.g. "X = 2"), the charm mode ask, the
// bolt slot's sequential ask, and the answer to that ask -- and returns the
// NEXT pending decision: the exile slot's own ask when the bound admits one,
// the resolution priority round when the bound is a resolved zero. The caller
// must have put the graveyard cards in place and funded the pool.
func charmXCastFlow(t *testing.T, e *Engine, xLabel string) *decision.Decision {
	t.Helper()
	addMana(t, e, 0, "RRRR")
	opt := castByName(t, e, 0, "X Charm")
	if opt == nil {
		t.Fatal("X Charm must be castable from hand")
	}
	submitChoices(t, e, opt.Index)
	// The X announcement is posed BEFORE the mode ask.
	xd := e.Pending()
	if xd == nil || xd.Kind != decision.KChoose || len(xd.Options) == 0 || xd.Options[0].Kind != "x" {
		t.Fatalf("want the X announce first, got %+v", xd)
	}
	xidx := -1
	for _, o := range xd.Options {
		if o.Label == xLabel {
			xidx = o.Index
		}
	}
	if xidx < 0 {
		t.Fatalf("no %s option among the announce options: %+v", xLabel, xd.Options)
	}
	submitChoices(t, e, xidx)
	md := e.Pending()
	if md == nil || md.Kind != decision.KModes || md.ResumeKind != "cast_modes" {
		t.Fatalf("want the charm mode ask after the announce, got %+v", md)
	}
	if len(md.Options) != 2 {
		t.Fatalf("mode ask offers %d options, want both sub-abilities: %+v", len(md.Options), md.Options)
	}
	submitChoices(t, e, md.Options[0].Index, md.Options[1].Index)
	// The bolt slot asks first (chosen-mode order), bounded 1..1 over the
	// player candidates.
	bd := e.Pending()
	if bd == nil || bd.Kind != decision.KTarget || bd.ResumeKind != "charm_mode_seq" || bd.Max != 1 {
		t.Fatalf("want the bolt slot's sequential charm ask (Max 1), got %+v", bd)
	}
	bidx := -1
	for _, o := range bd.Options {
		if o.Kind == "player" && o.Player == 1 {
			bidx = o.Index
		}
	}
	if bidx < 0 {
		t.Fatalf("opponent seat 1 not offered to the bolt slot: %+v", bd.Options)
	}
	submitChoices(t, e, bidx)
	return e.Pending()
}

// TestCharmSubDynamicBoundReadsPaidX pins the reported bug: announcing X = 2
// and choosing the "Exile up to X target cards from graveyards" mode bounds
// that slot's ask to the PAID X (Min 0 / Max 2) with BOTH graveyard cards
// offered -- never the hardcoded Max 0 that made the ask illegal.
func TestCharmSubDynamicBoundReadsPaidX(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 6320, charmXBoundSrc, testBearSrc, testBearSrc)
	// The two legal candidates are deck-native cards moved to the graveyard
	// with events (replayCheck rebuilds the game from the log alone, so an
	// AddObject'd outsider would make the replay diverge).
	g0 := searchMoveByNameSeat(t, e, 0, "Bear", state.ZGraveyard)
	g1 := searchMoveByNameSeat(t, e, 0, "Bear", state.ZGraveyard)
	if g0 == g1 {
		t.Fatal("fixture put the same object in the graveyard twice")
	}
	if e.G.Obj(g0).Zone != state.ZGraveyard || e.G.Obj(g1).Zone != state.ZGraveyard {
		t.Fatalf("precondition: %s / %s, want both cards in the graveyard",
			e.G.Obj(g0).Zone, e.G.Obj(g1).Zone)
	}
	ed := charmXCastFlow(t, e, "X = 2")
	if ed == nil || ed.Kind != decision.KTarget || ed.ResumeKind != "charm_mode_seq" {
		t.Fatalf("want the exile slot's sequential charm ask, got %+v", ed)
	}
	if ed.Min != 0 || ed.Max != 2 {
		t.Fatalf("exile slot ask = Min %d Max %d, want Min 0 Max 2 (the paid X)", ed.Min, ed.Max)
	}
	if len(ed.Options) != 2 {
		t.Fatalf("exile slot offers %d options, want both graveyard cards: %+v", len(ed.Options), ed.Options)
	}
	byObj := map[state.ObjID]int{}
	for _, o := range ed.Options {
		byObj[o.Obj] = o.Index
	}
	for _, gid := range []state.ObjID{g0, g1} {
		if _, ok := byObj[gid]; !ok {
			t.Fatalf("graveyard card %d not offered as an exile option: %+v", gid, ed.Options)
		}
	}
	submitChoices(t, e, byObj[g0], byObj[g1])
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(g0).Zone; z != state.ZExile {
		t.Fatalf("graveyard card %d zone = %s, want exile", g0, z)
	}
	if z := e.G.Obj(g1).Zone; z != state.ZExile {
		t.Fatalf("graveyard card %d zone = %s, want exile", g1, z)
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("X Charm zone = %s, want graveyard after resolving", z)
	}
	if life := e.G.Players[1].Life; life != 19 {
		t.Fatalf("opponent life = %d, want 19 (the bolt mode resolved too)", life)
	}
	replayCheck(t, e, cfg)
}

// TestCharmSubResolvedZeroTakesNoAsk pins the complementary leg: with X
// announced 0, the exile slot's bound RESOLVES to zero, and a resolved-zero
// slot is answered silently as an empty group -- the ask is never posted
// (the ask.go post-condition would panic on Min 0 / Max 0 with candidates).
func TestCharmSubResolvedZeroTakesNoAsk(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 6321, charmXBoundSrc, testBearSrc, testBearSrc)
	// The two legal candidates are deck-native cards moved to the graveyard
	// with events (replayCheck rebuilds the game from the log alone, so an
	// AddObject'd outsider would make the replay diverge).
	g0 := searchMoveByNameSeat(t, e, 0, "Bear", state.ZGraveyard)
	g1 := searchMoveByNameSeat(t, e, 0, "Bear", state.ZGraveyard)
	// A resolved Max 0 slot takes NO ask: after the bolt answer the next
	// decision is the priority round that resolves the spell, never a target
	// ask for the exile slot.
	ed := charmXCastFlow(t, e, "X = 0")
	if ed.Kind == decision.KTarget {
		t.Fatalf("exile slot posed an ask for a resolved zero bound: %+v", ed)
	}
	if ed.Kind != decision.KPriority {
		t.Fatalf("want the resolution priority round after the silent slot, got %+v", ed)
	}
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(g0).Zone; z != state.ZGraveyard {
		t.Fatalf("graveyard card %d zone = %s, want graveyard (nothing was exiled)", g0, z)
	}
	if z := e.G.Obj(g1).Zone; z != state.ZGraveyard {
		t.Fatalf("graveyard card %d zone = %s, want graveyard (nothing was exiled)", g1, z)
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("X Charm zone = %s, want graveyard after resolving", z)
	}
	if life := e.G.Players[1].Life; life != 19 {
		t.Fatalf("opponent life = %d, want 19 (the bolt mode resolved too)", life)
	}
	replayCheck(t, e, cfg)
}
