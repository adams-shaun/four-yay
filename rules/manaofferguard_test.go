package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// manaOfferGrantBoard builds the cardfuzz shape this ticket reproduces: a
// battlefield Enchantment granting a recipient Artifact a tap-for-mana
// activated ability through S:Mode$ Continuous | AddAbility$. It returns the
// engine and the recipient/grantor ids.
//
// The briefs's exact repro (cardfuzz seed 12345, MBX-6 ManaBrew lane) is a
// phased-out permanent whose Continuous AddAbility$ grant stayed in the offer
// walk's pass-scoped statics snapshot while the guard/handler's fresh walk
// dropped it, so the seat was offered "Activate ... for mana" and Submit
// rejected it as stale ("the source has no activatable mana ability").
func manaOfferGrantBoard(t *testing.T) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	recipient := card(t, "Name:Offer recipient\nTypes:Artifact\nOracle:x\n")
	grantor := card(t, "Name:Offer grantor\nTypes:Enchantment\n"+
		"S:Mode$ Continuous | Affected$ Artifact | AddAbility$ Grant\n"+
		"SVar:Grant:AB$ Mana | Cost$ T | Produced$ C\nOracle:x\n")
	e := handEngine(t, recipient, grantor)
	ids := append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...)
	rec, gr := ids[0], ids[1]
	for _, id := range ids {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	}
	if o := e.G.Obj(rec); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: recipient not a battlefield permanent: %+v", o)
	}
	if o := e.G.Obj(gr); o == nil || o.Zone != state.ZBattlefield || o.PhasedOut {
		t.Fatalf("precondition: grantor not a phased-in battlefield permanent: %+v", o)
	}
	return e, rec, gr
}

// manaOfferFreshCounts returns the offered membership (the pass-scoped
// snapshot) and the fresh membership (the nil-statics walk the guard and the
// activation handler use) for the recipient. A fresh actionStaticSource per
// call models a fresh legalActions pass, so a stale cached snapshot cannot
// make the two agree.
func manaOfferFreshCounts(e *Engine, rec state.ObjID) (snapshot, fresh int) {
	return len(e.availableManaAbilitiesUsing(&actionStaticSource{e: e}, 0, rec)),
		len(e.availableManaAbilities(0, rec))
}

// TestPhasedOutManaGrantorOfferMatchesFreshWalk pins the two edits of
// manaofferguard1. The offer walk's statics snapshot and the guard/handler's
// fresh walk must be ONE membership: a phased-out grantor (CR 702.25b,
// treated as though it does not exist) must be absent from both, and the
// priority offer must not carry an activate option for the recipient. The
// phased-in control on the same board proves the grant is real and the offer
// probe is not vacuous.
func TestPhasedOutManaGrantorOfferMatchesFreshWalk(t *testing.T) {
	e, rec, gr := manaOfferGrantBoard(t)

	// Phased-in control: the grant exists and both walks agree on it.
	if s, f := manaOfferFreshCounts(e, rec); s == 0 || f == 0 || s != f {
		t.Fatalf("phased-in control: snapshot=%d fresh=%d, want equal and non-zero", s, f)
	}
	if !phaseOutActivateOffered(e, 0, rec) {
		t.Fatal("phased-in control: the granted mana ability was not offered")
	}

	// Phase the GRANTOR out: CR 702.25b says the grant does not exist.
	e.emit(events.Event{Kind: events.PhaseOut, Obj: gr, Amount: 1})
	if o := e.G.Obj(gr); o == nil || !o.PhasedOut {
		t.Fatal("precondition: the grantor was not phased out")
	}

	s, f := manaOfferFreshCounts(e, rec)
	if s != 0 || f != 0 {
		t.Fatalf("CR 702.25b: phased-out grantor still grants -- snapshot=%d fresh=%d, want 0 and 0", s, f)
	}
	if s != f {
		t.Fatalf("offer walk and fresh walk disagree on membership: snapshot=%d fresh=%d", s, f)
	}
	if phaseOutActivateOffered(e, 0, rec) {
		t.Fatal("CR 702.25b: the offer still carries an activate option for a phased-out grantor's grant")
	}

	// Phase back in (Amount -1): the grant returns for both walks, so the
	// phased-out assertion above was not a permanently empty board.
	e.emit(events.Event{Kind: events.PhaseOut, Obj: gr, Amount: -1})
	if o := e.G.Obj(gr); o == nil || o.PhasedOut {
		t.Fatal("precondition: the grantor did not phase back in")
	}
	if s, f := manaOfferFreshCounts(e, rec); s == 0 || f == 0 || s != f {
		t.Fatalf("phased-back-in: snapshot=%d fresh=%d, want equal and non-zero", s, f)
	}
	if !phaseOutActivateOffered(e, 0, rec) {
		t.Fatal("phased-back-in: the granted mana ability was not offered again")
	}
}

// TestPhasedOutManaGrantorCannotDisagreeWithGuard is the guard-level half of
// manaofferguard1: the exact error the reported seat hit was
// priorityOptionStale returning "the source has no activatable mana ability"
// for an option the offer walk had already emitted. With the fix the offer
// walk no longer emits that option at all, and any activate option it does
// emit for the recipient must pass the guard's own handler check. The
// phased-in control proves the guard path is exercised (not a no-op board).
func TestPhasedOutManaGrantorCannotDisagreeWithGuard(t *testing.T) {
	e, rec, gr := manaOfferGrantBoard(t)

	// Phased-in control: the offered activate option resolves through the
	// guard's handler membership (priorityOptionStale = ""), so the guard
	// really is looking at this option and not skipping it.
	assertGuardAgreesWithOffer := func(wantOffer bool) {
		t.Helper()
		offered := 0
		for _, opt := range e.legalActions(0) {
			if opt.Kind != "activate" || opt.Obj != rec {
				continue
			}
			offered++
			if reason := e.priorityOptionStale(0, opt); reason != "" {
				t.Fatalf("offer/handler disagree on %q: %s", opt.Label, reason)
			}
		}
		if offered == 0 && wantOffer {
			t.Fatal("expected an activate option for the recipient, got none")
		}
		if offered != 0 && !wantOffer {
			t.Fatalf("phase-out: offer still has %d activate option(s) for the recipient", offered)
		}
	}
	assertGuardAgreesWithOffer(true)

	// The guard's fresh membership for the recipient is non-zero while phased
	// in -- the precondition that makes the phase-out assertion meaningful.
	if n := len(e.availableManaAbilitiesForWindow(0, rec, true)); n == 0 {
		t.Fatal("precondition: fresh guard membership is empty while the grantor is phased in")
	}

	e.emit(events.Event{Kind: events.PhaseOut, Obj: gr, Amount: 1})
	if o := e.G.Obj(gr); o == nil || !o.PhasedOut {
		t.Fatal("precondition: the grantor was not phased out")
	}
	// The guard's own membership is now empty: this is the string the reported
	// seat saw, and it must no longer correspond to any offered option.
	if n := len(e.availableManaAbilitiesForWindow(0, rec, true)); n != 0 {
		t.Fatalf("fresh guard membership = %d, want 0 for a phased-out grantor", n)
	}
	assertGuardAgreesWithOffer(false)
}
