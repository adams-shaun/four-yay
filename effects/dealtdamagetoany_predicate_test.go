package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// recordProvenanceCombat folds one DamageProvenance fact through the shared
// events.Apply fold, exactly as rules.Engine.emit emits it post-fold for a
// landed Damage event, with the caller's combat classification in the Text
// (rules/damage_ledger.go spells combat as events.DamageProvenanceCombat;
// an empty Text is the non-combat classification).
func recordProvenanceCombat(h *fakeHost, source, recipient state.ObjID, combat bool) {
	text := ""
	if combat {
		text = events.DamageProvenanceCombat
	}
	events.Apply(h.g, events.Event{Kind: events.DamageProvenance, Obj: source,
		IDs: []state.ObjID{recipient}, Amount: 1, Text: text})
}

// TestDealtDamageToAny is the effects leaf for Forge's dealtDamagetoAny and
// dealtCombatDamagetoAny (the "hasn't dealt damage yet" hexproof statics --
// Karakyk Guardian, Oyaminartok, Palladia-Mors, Ratonhnhaké:ton, Ruric Thar,
// Magecrusher). The words read the GAME-LONG source-side record the
// DamageProvenance fold writes (state.Object.DealtDamageToAnyGame /
// DealtCombatDamageToAnyGame), so an object that has dealt a non-combat
// 1 damage is no longer "hasn't dealt damage" but still is "hasn't dealt
// COMBAT damage" -- and the record must come from the fold itself, not a
// hand-set field. The precondition asserts the fold set the bools, so the
// test cannot pass with the fold writing nothing and the predicate reading a
// default.
func TestDealtDamageToAny(t *testing.T) {
	h := newHost(t, 2)
	dealerID := h.g.AddObject(mkCard(t, "Name:Dealer\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0).ID
	silentID := h.g.AddObject(mkCard(t, "Name:Silent\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0).ID

	// Non-combat damage first: the game word turns on, the combat word does
	// not (Ruric Thar's "hasn't dealt COMBAT damage yet" must survive it).
	recordProvenanceCombat(h, dealerID, silentID, false)
	if !h.g.Obj(dealerID).DealtDamageToAnyGame {
		t.Fatal("precondition: the DamageProvenance fold must set the game-long dealt record")
	}
	if h.g.Obj(dealerID).DealtCombatDamageToAnyGame {
		t.Fatal("precondition: non-combat damage must not set the combat-only record")
	}
	if len(h.g.Obj(dealerID).DamageDealtThisTurn) == 0 {
		t.Fatal("precondition: the fold must also append the per-turn record it classifies from")
	}

	// A real fold, not a hand-set field, must back the positive reads.
	if !MatchesObjectCtx(h.g, "Card.dealtDamagetoAny", h.g.Obj(dealerID), SpecContext{You: 0}) {
		t.Error("Card.dealtDamagetoAny must match an object that dealt damage this game")
	}
	if MatchesObjectCtx(h.g, "Card.dealtCombatDamagetoAny", h.g.Obj(dealerID), SpecContext{You: 0}) {
		t.Error("Card.dealtCombatDamagetoAny must stay FALSE for non-combat damage")
	}
	if MatchesObjectCtx(h.g, "Card.dealtDamagetoAny", h.g.Obj(silentID), SpecContext{You: 0}) {
		t.Error("Card.dealtDamagetoAny must not match the recipient that dealt nothing")
	}
	// Negated: the un-dealt recipient is "hasn't dealt damage yet", the
	// non-combat dealer is not.
	if got, ok := matchPredicate(h.g, "!dealtDamagetoAny", h.g.Obj(silentID), SpecContext{You: 0}); !ok || !got {
		t.Errorf("!dealtDamagetoAny on the silent object = %v,%v, want true,true", got, ok)
	}
	if got, ok := matchPredicate(h.g, "!dealtDamagetoAny", h.g.Obj(dealerID), SpecContext{You: 0}); !ok || got {
		t.Errorf("!dealtDamagetoAny on the dealer = %v,%v, want true,false", got, ok)
	}

	// The combat-only word must flip on a real combat-classified fold entry.
	recordProvenanceCombat(h, dealerID, silentID, true)
	if !h.g.Obj(dealerID).DealtCombatDamageToAnyGame {
		t.Fatal("precondition: the combat-classified fold entry must set the combat-only record")
	}
	if !MatchesObjectCtx(h.g, "Card.dealtCombatDamagetoAny", h.g.Obj(dealerID), SpecContext{You: 0}) {
		t.Error("Card.dealtCombatDamagetoAny must match after combat damage")
	}

	// The census and the matcher share the one recogniser, positively and
	// under '!'.
	for _, spec := range []string{
		"Card.dealtDamagetoAny", "Card.!dealtDamagetoAny",
		"Card.dealtCombatDamagetoAny", "Card.!dealtCombatDamagetoAny",
		"Card.Self+dealtDamagetoAny",
	} {
		if got := UnknownPredicates(spec); len(got) != 0 {
			t.Errorf("UnknownPredicates(%q) = %v, want empty", spec, got)
		}
	}
	// The per-turn source word is a DIFFERENT window: a game-long record can
	// never widen it (the recipient here dealt nothing, this turn or ever).
	if MatchesObjectCtx(h.g, "Card.dealtDamageThisTurn", h.g.Obj(silentID), SpecContext{You: 0}) {
		t.Error("dealtDamagetoAny must not widen dealtDamageThisTurn")
	}
}
