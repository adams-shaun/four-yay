package rules

// This file pins the CopyPermanent double-mint defect: when a trigger's
// fire-time capture and its sub-ability's RememberChanged$ both append the
// same object to the resolution's Ctx.Remembered, a Defined$
// TriggeredCardLKICopy selector reads a two-element list and the mint loop
// emitted one CopyToken per duplicate. The dedupe in effects/copypermanent.go
// keys on object identity and preserves first-seen order, so a repeated entry
// mints exactly once and a DIFFERENT remembered object still survives.
//
// Driver: tokenRememberedBoard (rules/token_remembered_test.go), the real
// compiled Hofri Ghostforge board.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestHofriCopyPermanentMintsOneSpirit is the headline pin. Hofri's dies
// trigger seeds Ctx.Remembered/Captured with the dying bearer, then TrigChange
// exiles that same bearer with RememberChanged$ True -- appending a SECOND
// entry for it -- and DBCopy's Defined$ TriggeredCardLKICopy reads the raw
// list. Before the dedupe that produced two CopyToken events, two Spirit
// tokens and a two-entry memory on each; one dies-trigger resolution must mint
// exactly one.
func TestHofriCopyPermanentMintsOneSpirit(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := tokenRememberedBoard(t, reg, "Hofri Ghostforge", "Vampire Nighthawk")
	hofri, bearer := ids["Hofri Ghostforge"], ids["Vampire Nighthawk"]
	// Precondition: the rule reads the battlefield LKI of a dying permanent,
	// so both the source and the bearer must start on the battlefield.
	if e.G.Obj(hofri).Zone != state.ZBattlefield || e.G.Obj(bearer).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Hofri and bearer must be on the battlefield (hofri=%v bearer=%v)",
			e.G.Obj(hofri).Zone, e.G.Obj(bearer).Zone)
	}
	// Precondition: the bearer's printed name is non-empty, so a token minted
	// from it is identifiable below.
	if name := e.G.Obj(bearer).Face().Name; name == "" {
		t.Fatalf("precondition: bearer has no printed name")
	}

	// Hofri's dies trigger first exiles the creature, then its sub-ability
	// CopyPermanent uses that exiled card's LKI -- the same driver as
	// TestHofriCopyPermanentGrants.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bearer, From: state.ZBattlefield, To: state.ZGraveyard})
	e.putTriggersOnStack()
	e.resolveTop()
	passUntilStackEmpty(t, e, 40)

	// The trigger must have actually resolved (its ChangeZone moved the
	// bearer to exile), or this test is vacuous.
	if z := e.G.Obj(bearer).Zone; z != state.ZExile {
		t.Fatalf("precondition: Hofri's ChangeZone did not exile the bearer (zone %v, log %+v)",
			z, e.L.Events)
	}

	// Count the mints in the event log.
	mints := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.CopyToken {
			mints++
		}
	}
	if mints != 1 {
		t.Fatalf("Hofri minted %d CopyToken events, want exactly 1; log %+v", mints, e.L.Events)
	}

	// Count the Spirit tokens on seat 0's battlefield (the bearer has left).
	var spirits []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken && id != bearer {
			spirits = append(spirits, id)
		}
	}
	if len(spirits) != 1 {
		t.Fatalf("Hofri created %d Spirit tokens on the battlefield, want exactly 1; log %+v",
			len(spirits), e.L.Events)
	}

	// The copy's memory must hold the bearer exactly once. A first-token-wins
	// selection is the exact hole that let the double-mint ship, so assert
	// both the length and the identity.
	spirit := spirits[0]
	o := e.G.Obj(spirit)
	if o == nil {
		t.Fatalf("precondition: minted Spirit %d has no object", spirit)
	}
	if len(o.Remembered) != 1 {
		t.Fatalf("Spirit's Remembered has %d entries, want exactly 1: %+v", len(o.Remembered), o.Remembered)
	}
	if o.Remembered[0].IsPlayer || o.Remembered[0].Obj != bearer {
		t.Fatalf("Spirit remembers %+v, want the single bearer object %d", o.Remembered, bearer)
	}

	// The whole round-trip replays byte-identically.
	replayCheck(t, e, cfg)
}
