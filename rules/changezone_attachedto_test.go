package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// attachedToFixture builds a 40-card seat-0 deck whose first cards are the
// named fixtures (hand first, then battlefield placements, then graveyard
// placements, in that order) followed by basics, forces the CR 103.1 toss to
// seat 0 (seatZeroStart), and parks the engine at seat 0's Main 1. It then
// moves each named fixture to its zone (searchMoveByName scans hand and
// library, so the shuffle's landing spot does not matter) and returns the
// engine with the object ids keyed by card name -- the LAST placed copy of a
// duplicated name wins the key, so a test that needs every copy scans the
// zone itself. All cards are real compiled corpus cards; no Forge script
// text is committed here (the licensing rule).
func attachedToFixture(t *testing.T, reg *cards.Registry, seed uint64, hand, battlefield, graveyard []string) (*Engine, map[string]state.ObjID) {
	t.Helper()
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	forest := searchCorpusCard(t, reg, "Forest")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range hand {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for _, name := range battlefield {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for _, name := range graveyard {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for len(deck) < 40 {
		deck = append(deck, forest, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = forest
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"protagonist", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	ids := map[string]state.ObjID{}
	for _, name := range hand {
		ids[name] = searchMoveByName(t, e, name, state.ZHand)
	}
	for _, name := range battlefield {
		ids[name] = searchMoveByName(t, e, name, state.ZBattlefield)
	}
	for _, name := range graveyard {
		ids[name] = searchMoveByName(t, e, name, state.ZGraveyard)
	}
	return e, ids
}

// attachDrain answers every decision the resolution poses: a target ask with
// up to Max of its offered options (Min-0 asks answered Max-less choose
// nothing -- the caller asserts on the ask itself), a priority pass
// otherwise, until the stack is empty. Returns the LAST target ask it saw,
// for the caller's bounds assertions.
func attachDrain(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	var lastTarget *decision.Decision
	for n := 0; n < limit; n++ {
		if len(e.G.Stack) == 0 {
			return lastTarget
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch d.Kind {
		case decision.KTarget:
			lastTarget = d
			idxs := make([]int, 0, d.Max)
			for i := 0; i < d.Max && i < len(d.Options); i++ {
				idxs = append(idxs, d.Options[i].Index)
			}
			submitChoices(t, e, idxs...)
		case decision.KChoose:
			// An "as this enters" choice is now posed at the entry boundary
			// (the Utopia Sprawl returned by Retether asks for its colour as
			// it enters attached). The drain takes the first option -- the
			// deterministic answer the pre-migration cast-time flow recorded
			// -- so these assertions keep the board they were written against.
			if d.ResumeKind != "etb" || len(d.Options) == 0 {
				t.Fatalf("unexpected choose decision while draining: %+v", d)
			}
			submitChoices(t, e, d.Options[0].Index)
		case decision.KTriggerOrder:
			// The entry-boundary ask splits the mass return into two batches,
			// so the two identical Divine Favor entry triggers now reach one
			// order ask instead of being put on the stack one batch each.
			// Answer in the offered order: identical triggers, so the order is
			// immaterial to every assertion here.
			idxs := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				idxs = append(idxs, o.Index)
			}
			submitChoices(t, e, idxs...)
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			submitChoices(t, e, idx)
		default:
			t.Fatalf("unexpected decision %v while draining: %+v", d.Kind, d)
		}
	}
	t.Fatal("the stack never drained")
	return nil
}

// TestRetetherReturnsAurasAttached is the mass-return carrier of ChangeZone's
// AttachedTo$ (the issue agent-20260918T195920Z-7554f14f's second named
// carrier, never pinned by the merged fix -- its pin covered only Forum
// Filibuster's ValidTgts$ target-ask shape): `SP$ ChangeZone | Origin$
// Graveyard | Destination$ Battlefield | Defined$ ValidGraveyard Aura.YouOwn
// | AttachedTo$ Creature`. Every Aura returned from the graveyard enters the
// battlefield attached to the creature the battlefield walk resolves (the
// only one), via one real events.Attach per moved card, and SURVIVES -- the
// CR 704.5m SBA (rules/attach.go attachmentSBAs) does not sweep a legally
// attached Aura.
//
// Known residual divergence, characterised here and recorded in the report:
// the oracle's parenthetical "Aura cards that can't enchant a creature on
// the battlefield remain in your graveyard" is not read -- the Utopia Sprawl
// (K:Enchant:Forest) is attached to the Bear like every other returned Aura,
// fails auraStillMatchesEnchant, and is swept to the graveyard by the 704.5m
// SBA. Net zone outcome matches the oracle (graveyard); the event path
// differs (attach + sweep instead of remain), which is observable in the log.
func TestRetetherReturnsAurasAttached(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, ids := attachedToFixture(t, reg, 9311, []string{"Retether"}, []string{"Grizzly Bears"},
		[]string{"Divine Favor", "Divine Favor", "Utopia Sprawl"})
	bears := ids["Grizzly Bears"]

	addMana(t, e, 0, "CCCW")
	castNamed(t, e, "Retether")
	attachDrain(t, e, 80)

	// Both Divine Favors are on the battlefield, each attached to the Bear.
	var favors []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Divine Favor" {
			favors = append(favors, id)
		}
	}
	if len(favors) != 2 {
		t.Fatalf("%d Divine Favors on the battlefield, want 2 (obj %+v)", len(favors), tailEmit(e, 10))
	}
	for _, f := range favors {
		if got := e.G.Obj(f).AttachedTo; got != bears {
			t.Fatalf("Divine Favor %d AttachedTo = %d, want the Bear %d", f, got, bears)
		}
	}
	// One real Attach event per returned Aura.
	for _, f := range favors {
		n := 0
		for _, ev := range e.L.Events {
			if ev.Kind == events.Attach && ev.Obj == f && len(ev.IDs) == 1 && ev.IDs[0] == bears {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%d Attach events fastening Divine Favor %d to the Bear, want 1", n, f)
		}
	}
	// They survive: the 704.5m SBA swept nothing legally attached.
	if e.G.Obj(favors[0]).Zone != state.ZBattlefield || e.G.Obj(favors[1]).Zone != state.ZBattlefield {
		t.Fatalf("a legally attached Aura was swept (zones %s/%s)",
			e.G.Obj(favors[0]).Zone, e.G.Obj(favors[1]).Zone)
	}
	// The divergence carrier: the Sprawl is attached, found illegal, and
	// swept back to the graveyard -- the net zone the oracle's parenthetical
	// promises, reached by the SBA rather than by remaining.
	sprawl := e.G.Obj(ids["Utopia Sprawl"])
	if sprawl == nil || sprawl.Zone != state.ZGraveyard {
		t.Fatalf("the illegally enchantable Aura is %+v, want it back in the graveyard", sprawl)
	}
	swept := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == ids["Utopia Sprawl"] &&
			ev.From == state.ZBattlefield && ev.To == state.ZGraveyard &&
			strings.Contains(ev.Text, "can no longer legally enchant") {
			swept = true
		}
	}
	if !swept {
		t.Fatalf("no 704.5m sweep event for the Sprawl (tail %+v)", tailEmit(e, 12))
	}
}

// TestMantleOfTheAncientsEtbAttaches is the target-ask carrier of ChangeZone's
// AttachedTo$ (the issue's third named carrier, never pinned by the merged
// fix): Mantle's ETB trigger (`T:Mode$ ChangesZone ... Execute$ TrigMove`)
// poses the "return any number of target Aura and/or Equipment cards that
// could be attached to enchanted creature" ask -- ValidTgts$
// Aura.CanEnchantEquippedBy+YouOwn,Equipment.CanEnchantEquippedBy+YouOwn,
// TgtZone$ Graveyard -- and the answered ChangeZone moves the chosen card to
// the battlefield ATTACHED to the enchanted creature (`AttachedTo$ Valid
// Creature.EnchantedBy`), where it survives the CR 704.5m/n SBAs.
func TestMantleOfTheAncientsEtbAttaches(t *testing.T) {
	t.Parallel()
	run := func(t *testing.T, grave []string, wantName string, wantStaysGrave string) {
		t.Helper()
		reg := searchTestRegistry(t)
		e, ids := attachedToFixture(t, reg, 9313, []string{"Mantle of the Ancients"},
			[]string{"Grizzly Bears"}, grave)
		bears := ids["Grizzly Bears"]

		addMana(t, e, 0, "CCCWW")
		castNamed(t, e, "Mantle of the Ancients")
		// Mantle's own cast-time enchant ask: the only creature you control.
		dt := e.Pending()
		if dt == nil || dt.Kind != decision.KTarget {
			t.Fatalf("cast enchant ask = %+v, want a KTarget", dt)
		}
		tgtIdx := -1
		for _, o := range dt.Options {
			if o.Obj == bears {
				tgtIdx = o.Index
			}
		}
		if tgtIdx < 0 {
			t.Fatalf("the Bear was not offered to the enchant: %+v", dt.Options)
		}
		submitChoices(t, e, tgtIdx)

		// The ETB trigger's placement ask offers the graveyard cards the
		// CanEnchantEquippedBy filter admits; answer it and drain.
		ask := attachDrain(t, e, 80)
		if ask == nil {
			t.Fatalf("no return ask posed (tail %+v)", tailEmit(e, 10))
		}
		if ask.Min != 0 {
			t.Fatalf("return ask Min %d, want 0 (TargetMin$ 0: any number)", ask.Min)
		}

		want := ids[wantName]
		moved := e.G.Obj(want)
		if moved == nil || moved.Zone != state.ZBattlefield {
			t.Fatalf("the answered card did not enter the battlefield (obj %+v)", moved)
		}
		if moved.AttachedTo != bears {
			t.Fatalf("the returned card's AttachedTo = %d, want the enchanted Bear %d", moved.AttachedTo, bears)
		}
		attaches := 0
		for _, ev := range e.L.Events {
			if ev.Kind == events.Attach && ev.Obj == want && len(ev.IDs) == 1 && ev.IDs[0] == bears {
				attaches++
			}
		}
		if attaches != 1 {
			t.Fatalf("%d Attach events fastening the returned card to the Bear, want 1", attaches)
		}
		// It survives the CR 704.5m/n SBAs: a legal attachment stays.
		if e.G.Obj(want).Zone != state.ZBattlefield || e.G.Obj(want).AttachedTo != bears {
			t.Fatalf("the legal attachment did not survive (obj %+v)", e.G.Obj(want))
		}
		if wantStaysGrave != "" {
			if o := e.G.Obj(ids[wantStaysGrave]); o == nil || o.Zone != state.ZGraveyard {
				t.Fatalf("%s left the graveyard despite the one-card bound (obj %+v)", wantStaysGrave, o)
			}
		}
	}

	t.Run("aura", func(t *testing.T) {
		run(t, []string{"Divine Favor"}, "Divine Favor", "")
	})
	t.Run("equipment", func(t *testing.T) {
		run(t, []string{"Bonesplitter"}, "Bonesplitter", "")
	})
	// With BOTH an Aura and an Equipment eligible the ask's bound is the
	// RESOLVED TargetMax$ X (SVar:X:Count$ValidGraveyard
	// Aura.CanEnchantEquippedBy,Equipment.CanEnchantEquippedBy): the placement
	// ask (rules/trigger_queue.go pushTarget's askTarget) routes through
	// resolvedTargetBounds, which resolves the dynamic bound through the
	// effects numeric grammar against the triggering source's SVar table --
	// so Max is 2 and BOTH cards can be returned. Before the fix the literal
	// reader dropped "X" and the ask was capped at Max 1; this subtest pinned
	// that defect and was updated consciously when the bound was fixed.
	t.Run("both-eligible-resolved-bound", func(t *testing.T) {
		reg := searchTestRegistry(t)
		e, ids := attachedToFixture(t, reg, 9313, []string{"Mantle of the Ancients"},
			[]string{"Grizzly Bears"}, []string{"Divine Favor", "Bonesplitter"})
		addMana(t, e, 0, "CCCWW")
		castNamed(t, e, "Mantle of the Ancients")
		dt := e.Pending()
		if dt == nil || dt.Kind != decision.KTarget {
			t.Fatalf("cast enchant ask = %+v, want a KTarget", dt)
		}
		for _, o := range dt.Options {
			if o.Obj == ids["Grizzly Bears"] {
				submitChoices(t, e, o.Index)
				goto placed
			}
		}
		t.Fatal("the Bear was not offered to the enchant")
	placed:
		ask := attachDrain(t, e, 80)
		if ask == nil {
			t.Fatal("no return ask posed")
		}
		if len(ask.Options) != 2 || ask.Max != 2 {
			t.Fatalf("return ask = %+v, want both cards offered under the resolved Max-2 bound", ask)
		}
		// attachDrain answered the Max-2 ask with both options: both cards
		// return to the battlefield, each attached to the Bear, and both
		// survive the CR 704.5m/n SBAs.
		for _, name := range []string{"Divine Favor", "Bonesplitter"} {
			o := e.G.Obj(ids[name])
			if o == nil || o.Zone != state.ZBattlefield || o.AttachedTo != ids["Grizzly Bears"] {
				t.Fatalf("%s did not return attached to the Bear (obj %+v)", name, o)
			}
		}
	})
}
