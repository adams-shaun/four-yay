package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestNormanOsbornGrantsMayhemAtCardManaCost pins Green Goblin's real
// continuous grant: its Mayhem:CardManaCost placeholder becomes the affected
// card's own mana cost before the Mayhem cast is priced.
func TestNormanOsbornGrantsMayhemAtCardManaCost(t *testing.T) {
	t.Parallel()
	e := handEngine(t,
		corpusAlternativeCard(t, "Norman Osborn"),
		corpusAlternativeCard(t, "Grizzly Bears"),
	)
	ids := e.G.Zone(state.ZHand, 0)
	if len(ids) != 2 {
		t.Fatalf("setup: hand has %d cards, want Norman and Bears", len(ids))
	}
	norman, bears := ids[0], ids[1]
	if o := e.G.Obj(norman); o == nil || o.Card == nil || len(o.Card.Faces) != 2 {
		t.Fatalf("setup: Norman is not a two-faced corpus card: %+v", o)
	}

	// Green Goblin's grant is on Norman's transformed face, so enter Norman
	// and flip him through real events before discarding the affected card.
	e.emit(events.Event{Kind: events.MoveZone, Obj: norman, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.FlipFace, Obj: norman, Amount: 1})
	if o := e.G.Obj(norman); o == nil || o.Face() == nil || o.Face().Name != "Green Goblin" {
		t.Fatalf("setup: Norman transformed face = %+v, want Green Goblin", o)
	}
	discardToGraveyard(t, e, bears, 0)

	// This proves the continuous handler ran and left the Forge placeholder
	// for mayhemCastCost to expand, rather than accidentally using a printed
	// Mayhem line on Bears (which has none).
	raw, ok := e.derivedKeywordParam(bears, "Mayhem")
	if !ok || raw != "CardManaCost" {
		t.Fatalf("Green Goblin grant = %q (ok %v), want CardManaCost", raw, ok)
	}
	mc, ok := e.mayhemCastCost(bears)
	if !ok || mc.Generic != 1 || mc.Colored[state.MG] != 1 {
		t.Fatalf("expanded Bears mayhem cost = %+v (ok %v), want {1}{G}", mc, ok)
	}

	addMana(t, e, 0, "G") // Green Goblin's real graveyard reduction pays the generic pip.
	opts := mayhemOptions(e, 0)
	if len(opts) != 1 || opts[0].Obj != bears {
		t.Fatalf("CardManaCost mayhem cast not offered: %+v", e.legalActions(0))
	}
	submitChoices(t, e, opts[0].Index)
	if got := e.G.Obj(bears).Zone; got != state.ZStack {
		t.Fatalf("CardManaCost mayhem spell zone = %v, want stack", got)
	}
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(bears).Zone; got != state.ZBattlefield {
		t.Fatalf("CardManaCost mayhem spell resolved to %v, want battlefield", got)
	}
}

const mayhemGrantSrc = "Name:Mayhem Grant\nTypes:Artifact\n" +
	"S:Mode$ Continuous | Affected$ Card.YouOwn+nonLand | AffectedZone$ Graveyard | AddKeyword$ Mayhem:B\nOracle:x\n"

const mayhemSacSpellSrc = "Name:Mayhem Sac Spell\nManaCost:9\nTypes:Sorcery\n" +
	"A:SP$ Draw | Cost$ 9 Sac<1/Creature> | NumCards$ 1\nOracle:x\n"

// TestGrantedMayhemOfferIncludesSpellAbilityAdditionalCost prevents a
// graveyard Mayhem option from bypassing an affected spell's mandatory
// SpellAbility Cost$. A cast that cannot sacrifice its required creature must
// not be offered; after a real creature enters, the same option must appear.
func TestGrantedMayhemOfferIncludesSpellAbilityAdditionalCost(t *testing.T) {
	t.Parallel()
	e := handEngine(t,
		card(t, mayhemGrantSrc),
		card(t, mayhemSacSpellSrc),
		card(t, bearSrc),
	)
	ids := e.G.Zone(state.ZHand, 0)
	if len(ids) != 3 {
		t.Fatalf("setup: hand has %d cards, want grant, spell, and bearer", len(ids))
	}
	grant, spell, bearer := ids[0], ids[1], ids[2]
	if sa := e.G.Obj(spell).Face().SpellAbility(); sa == nil || sa.Params["Cost"] != "9 Sac<1/Creature>" {
		t.Fatalf("setup: spell additional cost = %+v, want mandatory creature sacrifice", sa)
	}
	if cost := withSpellAbilityExtras(e.G.Obj(spell).Face(), ParseCost("B")); len(cost.Sac) != 1 {
		t.Fatalf("setup: Mayhem cost did not retain SpellAbility sacrifice: %+v", cost)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: grant, From: state.ZHand, To: state.ZBattlefield})
	discardToGraveyard(t, e, spell, 0)
	if raw, ok := e.derivedKeywordParam(spell, "Mayhem"); !ok || raw != "B" {
		t.Fatalf("setup: continuous Mayhem grant = %q (ok %v), want B", raw, ok)
	}
	addMana(t, e, 0, "B")
	cost := withSpellAbilityExtras(e.G.Obj(spell).Face(), ParseCost("B"))
	if e.nonManaCastable(0, spell, cost, false) {
		t.Fatalf("setup: non-mana gate accepted an unpayable cost: %+v", cost)
	}
	if e.offerCastable(0, spell, cost, spellScope("mayhem"), false) {
		t.Fatalf("setup: offer gate accepted an unpayable cost: %+v", cost)
	}

	if opts := mayhemOptions(e, 0); len(opts) != 0 {
		t.Fatalf("Mayhem offered despite unpayable mandatory sacrifice: %+v", opts)
	}
	// A real MoveZone changes the only missing payment resource. This proves
	// the withheld offer was about the additional cost rather than the grant,
	// timing, mana, or discard provenance.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bearer, From: state.ZHand, To: state.ZBattlefield})
	e.priorityRound()
	opts := mayhemOptions(e, 0)
	if len(opts) != 1 || opts[0].Obj != spell {
		t.Fatalf("Mayhem not offered after sacrifice bearer entered: %+v", e.legalActions(0))
	}
}
