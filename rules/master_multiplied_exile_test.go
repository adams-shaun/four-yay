package rules

// cantexile1: the CantExile half of The Master, Multiplied's reminder text --
// "Triggered abilities you control can't cause you to sacrifice or exile
// creature tokens you control." -- is evaluated, not skipped by the
// parameter whitelist. The card's `S:Mode$ CantExile | ValidCard$
// Creature.YouCtrl+token | ValidCause$ Triggered.YouCtrl | ForCost$ False`
// line is read by rules.Engine.ExileBlocked (rules/layers.go), the CantExile
// sibling of the CantSacrifice walk master_multiplied_restriction_test.go
// pins. BEFORE this fix effects.CantExileRestrictionParamsReadable did not
// exist and no exile mover consulted a CantExile restriction, so a triggered
// exile took the tokens.
//
// Every leaf drives the real ChangeZone path on real corpus cards and ends
// replay-verified: Fiend Hunter's ETB trigger (Triggered, YouCtrl) is the
// blocked cause; Swords to Plowshares (Spell) is the permitted contrast that
// a whitelist-without-evaluation fix -- the over-restriction the brief warns
// about -- would fail.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// masterExileCards looks up the four corpus cards every leaf in this file
// needs and fails fast when the corpus misses one.
func masterExileCards(t *testing.T) (master, alarm, hunter, swords *cards.Card) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"The Master, Multiplied", "Raise the Alarm",
		"Fiend Hunter", "Swords to Plowshares"} {
		if _, ok := reg.Lookup(name); !ok {
			t.Fatalf("corpus missing %s", name)
		}
	}
	master, _ = reg.Lookup("The Master, Multiplied")
	alarm, _ = reg.Lookup("Raise the Alarm")
	hunter, _ = reg.Lookup("Fiend Hunter")
	swords, _ = reg.Lookup("Swords to Plowshares")
	return master, alarm, hunter, swords
}

// exileMoveLanded reports whether the log carries a MoveZone to Exile for id.
func exileMoveLanded(e *Engine, id state.ObjID) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZExile {
			return true
		}
	}
	return false
}

// answerFixtureDecision answers one non-priority decision posed while driving
// a corpus trigger/spell to resolution: an optional-trigger ask is accepted, a
// target ask is answered with want (fatal when the option is absent -- the
// caller's precondition that the exile would otherwise have been legal), and
// any other single-choice ask takes its first option. It returns whether want's
// target ask was actually posed.
func answerFixtureDecision(t *testing.T, e *Engine, want state.ObjID) (sawTarget bool) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatalf("no decision pending")
	}
	switch d.Kind {
	case decision.KTriggerOptional:
		for _, o := range d.Options {
			if o.Kind == "yes" {
				submitChoices(t, e, o.Index)
				return false
			}
		}
		t.Fatalf("optional trigger offers no yes option: %+v", d.Options)
	case decision.KTarget:
		for _, o := range d.Options {
			if o.Obj == want {
				submitChoices(t, e, o.Index)
				return true
			}
		}
		t.Fatalf("target ask does not offer obj %d (the exile would not have been legal): %+v", want, d.Options)
	default:
		if len(d.Options) == 0 {
			t.Fatalf("non-priority decision %+v has no options", d)
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	return false
}

// driveToResolution passes priority and answers every ask until the stack is
// empty, recording whether the pending target ask ever offered want. It stops
// the moment the stack is empty and ordinary priority is pending (the engine
// always poses a priority decision, so draining on “no decision” would never
// terminate).
func driveToResolution(t *testing.T, e *Engine, want state.ObjID) bool {
	t.Helper()
	sawTarget := false
	for i := 0; i < 120; i++ {
		d := e.Pending()
		if d == nil {
			return sawTarget
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				return sawTarget
			}
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision offers no pass: %+v", d.Options)
			}
			submitChoices(t, e, idx)
			continue
		}
		if answerFixtureDecision(t, e, want) {
			sawTarget = true
		}
	}
	t.Fatalf("resolution did not settle (stack depth %d)", len(e.G.Stack))
	return false
}

// TestMasterMultipliedTriggeredExileBlocked is the filing symptom for the
// exile half: with The Master, Multiplied on the battlefield, a triggered
// ability its controller controls (Fiend Hunter's "when this enters, you may
// exile another target creature" ETB trigger, resolving through real
// effChangeZone) must not exile seat 0's Soldier token. The token stays on the
// battlefield, no MoveZone to Exile is emitted for it, and the exile-return
// trigger Fiend Hunter carries finds nothing.
func TestMasterMultipliedTriggeredExileBlocked(t *testing.T) {
	t.Parallel()
	master, alarm, hunter, _ := masterExileCards(t)
	bear := card(t, staticBearFixture)
	e, cfg := restrictionGame(t, 9311,
		[][]*cards.Card{{alarm, hunter}, nil},
		[][]*cards.Card{{master, bear}, nil})
	masterID := bearOnBoard(t, e, 0, master)
	tokA, tokB := makeTokens(t, e, alarm)
	// Precondition: the engine actually sees The Master's CantExile static --
	// reverting the registration (not just the reader) must fail here.
	statics := e.activeStatics("CantExile")
	if len(statics) != 1 {
		t.Fatalf("engine sees %d CantExile statics, want The Master's one", len(statics))
	}
	if got := statics[0].Source; got != masterID {
		t.Fatalf("CantExile static source = %d, want The Master %d", got, masterID)
	}

	hunterID := findAndMoveToHand(t, e, 0, "Fiend Hunter")
	addMana(t, e, 0, "WW")
	castFromPriority(t, e, hunterID)
	// Precondition: the triggered exile really targeted the token -- a
	// vacuous setup would make the "nothing moved" assertion pass silently.
	if !driveToResolution(t, e, tokA) {
		t.Fatal("the Fiend Hunter ETB exile never posed a target ask for the token")
	}
	if o := e.G.Obj(hunterID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Fiend Hunter is not on the battlefield: %+v", o)
	}
	if z := e.G.Obj(tokA).Zone; z != state.ZBattlefield {
		t.Fatalf("token A %s: a Triggered.YouCtrl cause must not exile it", z)
	}
	if z := e.G.Obj(tokB).Zone; z != state.ZBattlefield {
		t.Fatalf("token B %s: a Triggered.YouCtrl cause must not exile it", z)
	}
	if exileMoveLanded(e, tokA) {
		t.Fatal("a MoveZone to Exile took the token under The Master's CantExile restriction")
	}
	replayCheck(t, e, cfg)
}

// TestMasterMultipliedSpellCauseExileAllowed proves the ValidCause$ gate
// discriminates: the same static's `Triggered.YouCtrl` must NOT block a
// SPELL-caused exile, so Swords to Plowshares ("exile target creature",
// resolving as a spell) still takes the token -- the over-restriction a
// whitelist-without-evaluation fix would produce.
func TestMasterMultipliedSpellCauseExileAllowed(t *testing.T) {
	t.Parallel()
	master, alarm, _, swords := masterExileCards(t)
	bear := card(t, staticBearFixture)
	e, cfg := restrictionGame(t, 9312,
		[][]*cards.Card{{alarm, swords}, nil},
		[][]*cards.Card{{master, bear}, nil})
	bearOnBoard(t, e, 0, master)
	tokA, _ := makeTokens(t, e, alarm)
	// Precondition: the engine sees The Master's CantExile static, matching
	// this token -- asserted before the cast so a vacuous setup is caught.
	if n := len(e.activeStatics("CantExile")); n != 1 {
		t.Fatalf("engine sees %d CantExile statics, want The Master's one", n)
	}
	// A spell is not a Triggered cause, so with no cause in flight the reader
	// must not block -- the ValidCause$/ForCost$ discrimination itself.
	if e.ExileBlocked(tokA, false) {
		t.Fatal("ExileBlocked(tok, false) is true outside any trigger: the static over-restricts")
	}

	swordsID := findAndMoveToHand(t, e, 0, "Swords to Plowshares")
	addMana(t, e, 0, "W")
	castFromPriority(t, e, swordsID)
	if !driveToResolution(t, e, tokA) {
		t.Fatal("Swords to Plowshares never posed a target ask for the token")
	}
	// A token exiled to ZExile ceases to exist (CR 111.7), so the object's
	// final zone is `ceased`, not Exile -- the proof the spell-caused exile
	// landed is the MoveZone event plus the token leaving the battlefield.
	if z := e.G.Obj(tokA).Zone; z == state.ZBattlefield {
		t.Fatalf("token stayed on the battlefield after a spell-caused exile: ValidCause$ Triggered over-restricts (zone %s)", z)
	}
	if !exileMoveLanded(e, tokA) {
		t.Fatal("no MoveZone to Exile recorded for the spell-caused exile")
	}
	replayCheck(t, e, cfg)
}
