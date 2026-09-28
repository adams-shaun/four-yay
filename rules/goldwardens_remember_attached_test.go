package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Goldwardens' Gambit is the Rebellion Rising carrier of RememberAttached$:
// `DB$ RepeatEach | UseImprinted$ True | DefinedCards$ DirectRemembered |
// RepeatSubAbility$ DBAttach` loops over the five Rebel tokens and each
// iteration's `DB$ Attach | Choices$ Equipment.YouCtrl+!IsRemembered | ... |
// RememberAttached$ True` lets the caster attach a DIFFERENT Equipment to
// each token — the equipment just attached joins the Remembered (both
// halves, the same discipline RememberTokens$ uses) so the next iteration's
// !IsRemembered filter no longer offers it.
//
// Before the read the Choices$ object pool did not exist at all and the
// Attach went nowhere: obj fell back to the resolving source (the Gambit
// itself) and every iteration emitted "cannot attach: no legal target"
// after its Optional$ election, because the RepeatEach subject binding does
// not survive the suspension and the re-entry re-derived Defined$ Imprinted
// to nothing.
func TestGoldwardensGambitRemembersAttachedEquipment(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Goldwardens' Gambit", "Lightning Greaves", "Swiftfoot Boots")
	greaves := searchMoveByName(t, e, "Lightning Greaves", state.ZBattlefield)
	boots := searchMoveByName(t, e, "Swiftfoot Boots", state.ZBattlefield)
	id := searchMoveByName(t, e, "Goldwardens' Gambit", state.ZHand)
	addMana(t, e, 0, "RRRRRRRR") // {6}{R}{R} less the two Equipment affinity, plus slack

	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Goldwardens' Gambit: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passPriorityUntilAsk(t, e, "Choose an equipment to attach to this token")

	// The RepeatEach loop's five iterations ask one equipment choice each.
	// Iteration 1 offers both equipment in battlefield zone order; iteration
	// 2 must offer ONLY the Boots — the Greaves iteration 1 attached is in
	// the Remembered set its !IsRemembered filter excludes.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Prompt != "Choose an equipment to attach to this token" {
		t.Fatalf("expected the first iteration's equipment choice, got %+v", d)
	}
	if len(d.Options) != 2 || d.Options[0].Obj != greaves || d.Options[1].Obj != boots {
		t.Fatalf("first iteration options = %+v, want [Greaves, Boots] in zone order", d.Options)
	}
	if d.Min != 0 || d.Max != 1 {
		t.Fatalf("Optional$ choice bounds = %d..%d, want 0..1", d.Min, d.Max)
	}
	submitChoices(t, e, d.Options[0].Index) // attach the Greaves to the first token

	d = e.Pending()
	if d == nil || d.Prompt != "Choose an equipment to attach to this token" {
		t.Fatalf("expected the second iteration's equipment choice, got %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != boots {
		t.Fatalf("second iteration options = %+v, want only the Boots — the attached Greaves must be excluded by !IsRemembered", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index) // attach the Boots to the second token

	// Iterations 3..5 have no unattached Equipment left: the Optional$ pool
	// is empty, which is a silent decline — no ask, no Note.
	if d := e.Pending(); d != nil && d.Prompt == "Choose an equipment to attach to this token" {
		t.Fatalf("an iteration with an empty Choices$ pool must not ask: %+v", d)
	}

	// Exactly two Attach events: each chosen Equipment onto its own token,
	// in iteration order.
	var tokens []state.ObjID
	for _, tok := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(tok); o != nil && o.IsToken {
			tokens = append(tokens, tok)
		}
	}
	if len(tokens) != 5 {
		t.Fatalf("battlefield tokens = %d, want 5", len(tokens))
	}
	var attachs []events.Event
	var remembered [][]state.ObjID
	for _, ev := range e.L.Events {
		switch ev.Kind {
		case events.Attach:
			attachs = append(attachs, ev)
		case events.Choose:
			// The equipment remember entries: the spell's persistent
			// Remembered also carries the five token entries (the SP$'s
			// RememberTokens$), so keep only the ones naming an Equipment.
			if ev.Counter == "remembered" && ev.Obj == id && len(ev.IDs) > 0 &&
				(ev.IDs[0] == greaves || ev.IDs[0] == boots) {
				remembered = append(remembered, append([]state.ObjID(nil), ev.IDs...))
			}
		}
	}
	if len(attachs) != 2 ||
		attachs[0].Obj != greaves || len(attachs[0].IDs) != 1 || attachs[0].IDs[0] != tokens[0] ||
		attachs[1].Obj != boots || len(attachs[1].IDs) != 1 || attachs[1].IDs[0] != tokens[1] {
		t.Fatalf("attachs = %+v (tokens %v), want Greaves->%d then Boots->%d", attachs, tokens, tokens[0], tokens[1])
	}
	// The event-backed half of the remember: one Choose "remembered" entry
	// per attached Equipment on the resolution's source (the Gambit), before
	// the trailing DBCleanup's clear-remembered removes them.
	if len(remembered) != 2 || remembered[0][0] != greaves || remembered[1][0] != boots {
		t.Fatalf("remembered entries = %+v, want [Greaves] then [Boots] on the Gambit", remembered)
	}
	// The attachments themselves landed.
	if e.G.Obj(greaves).AttachedTo != tokens[0] || e.G.Obj(boots).AttachedTo != tokens[1] {
		t.Fatalf("greaves.AttachedTo = %d, boots.AttachedTo = %d, want %d and %d",
			e.G.Obj(greaves).AttachedTo, e.G.Obj(boots).AttachedTo, tokens[0], tokens[1])
	}
	replayCheck(t, e, cfg)
}

// passPriorityUntilAsk passes every priority decision (either seat's) until
// any non-priority decision is pending -- the caller then asserts on what
// came up. want names (as documentation only) the prompt the caller expects.
func passPriorityUntilAsk(t *testing.T, e *Engine, want string) {
	t.Helper()
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			return
		}
		if d.Kind != decision.KPriority {
			// Any non-priority decision surfaces: the caller asserts on it.
			// want (a prompt prefix) only documents what the caller waits for.
			return
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
			}
		}
		if pass < 0 {
			t.Fatalf("no pass option on priority decision %+v", d)
		}
		submitChoices(t, e, pass)
	}
	t.Fatal("priority never yielded the expected ask")
}
