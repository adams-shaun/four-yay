package rules

// Task destaltsvar1: DestAltSVar$ / DestinationAlternative$ corpus-carried
// regression tests. Every fixture card is loaded from the real compiled
// corpus (searchTestRegistry) so the tests pin the cards' ACTUAL script
// shapes. The bug these cover: effChangeZone computed Destination$ once and
// never read DestAltSVar$, so all six carriers silently took the primary
// zone.
//
// The selector shapes pinned end to end here:
//
//   - the_five_doctors: `MANDATORY Count$TimesKicked` over a library +
//     graveyard and/or search (OriginAlternative$), both directions.
//   - zukos_conviction: `MANDATORY X` with `SVar:X:Count$Kicked.1.0` over a
//     graveyard return, both directions, plus the Tapped$ True rider.
//   - from_father_to_son: `MANDATORY X` with
//     `SVar:X:Count$wasCastFromGraveyard.1.0` through the flashback cast.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// driveToTargetAsk answers pass decisions until the KTarget placement ask is
// pending, answers it with the option naming obj, and returns the next
// non-priority decision.
func driveToTargetAsk(t *testing.T, e *Engine, obj state.ObjID) *decision.Decision {
	t.Helper()
	d := passUntilNonPriority(t, e, 30)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want a KTarget ask", d)
	}
	answerTargetAsk(t, e, []state.ObjID{obj})
	return passUntilNonPriority(t, e, 30)
}

// TestTheFiveDoctorsDestAltKickedBattlefieldUnkickedHand pins both directions
// of `MANDATORY Count$TimesKicked`: the kicked cast puts the searched Doctor
// onto the battlefield, the unkicked cast puts it into hand.
func TestTheFiveDoctorsDestAltKickedBattlefieldUnkickedHand(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "The Five Doctors", "Doctor Strange, Surgeon")
	id := searchMoveByName(t, e, "The Five Doctors", state.ZHand)
	// Base {5}{G} + kicker {5} = 10 generic + G.
	addMana(t, e, 0, "GCCCCCCCCCC")
	submitChoices(t, e, castModeOption(t, e, id, "kicked"))
	d := passUntilNonPriority(t, e, 30)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("pending = %+v, want a search KChoose", d)
	}
	picked := d.Options[0].Obj
	submitChoices(t, e, 0)
	if got := e.G.Obj(picked).Zone; got != state.ZBattlefield {
		t.Fatalf("kicked Doctor destination = %s, want battlefield (DestAltSVar MANDATORY Count$TimesKicked)", got)
	}
	replayCheck(t, e, cfg)
}

func TestTheFiveDoctorsDestAltUnkickedGoesToHand(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "The Five Doctors", "Doctor Strange, Surgeon")
	id := searchMoveByName(t, e, "The Five Doctors", state.ZHand)
	addMana(t, e, 0, "GCCCCC") // base {5}{G} only
	submitChoices(t, e, plainCastOption(t, e, id).Index)
	d := passUntilNonPriority(t, e, 30)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("pending = %+v, want a search KChoose", d)
	}
	picked := d.Options[0].Obj
	submitChoices(t, e, 0)
	if got := e.G.Obj(picked).Zone; got != state.ZHand {
		t.Fatalf("unkicked Doctor destination = %s, want hand", got)
	}
	replayCheck(t, e, cfg)
}

// TestZukosConvictionDestAltKickedBattlefieldTappedUnkickedHand pins the
// `MANDATORY X`/`Count$Kicked.1.0` shape and the Tapped$ True rider on the
// alternate branch.
func TestZukosConvictionDestAltKickedBattlefieldTappedUnkickedHand(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Zuko's Conviction", "Grizzly Bears")
	id := searchMoveByName(t, e, "Zuko's Conviction", state.ZHand)
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZGraveyard)
	// Base {B} + kicker {4} = 4 generic + B.
	addMana(t, e, 0, "BCCCC")
	submitChoices(t, e, castModeOption(t, e, id, "kicked"))
	driveToTargetAsk(t, e, bear)
	if got := e.G.Obj(bear).Zone; got != state.ZBattlefield {
		t.Fatalf("kicked Zuko destination = %s, want battlefield", got)
	}
	if !e.G.Obj(bear).Tapped {
		t.Fatalf("kicked Zuko returned creature not tapped (Tapped$ True rider)")
	}
	replayCheck(t, e, cfg)
}

func TestZukosConvictionDestAltUnkickedGoesToHand(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Zuko's Conviction", "Grizzly Bears")
	id := searchMoveByName(t, e, "Zuko's Conviction", state.ZHand)
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZGraveyard)
	addMana(t, e, 0, "B")
	submitChoices(t, e, plainCastOption(t, e, id).Index)
	driveToTargetAsk(t, e, bear)
	if got := e.G.Obj(bear).Zone; got != state.ZHand {
		t.Fatalf("unkicked Zuko destination = %s, want hand", got)
	}
	if e.G.Obj(bear).Tapped {
		t.Fatalf("unkicked Zuko returned creature should not be tapped")
	}
	replayCheck(t, e, cfg)
}

// TestFromFatherToSonDestAltFlashbackBattlefield pins `MANDATORY X` with the
// SVar-name `X = Count$wasCastFromGraveyard.1.0` through the flashback cast:
// the searched Vehicle enters the battlefield instead of hand.
func TestFromFatherToSonDestAltFlashbackBattlefield(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "From Father to Son", "Kylox's Voltstrider")
	id := searchMoveByName(t, e, "From Father to Son", state.ZGraveyard)
	// Flashback {4}{W}{W}{W} = 4 generic + WWW.
	addMana(t, e, 0, "WWWCCCC")
	var fb int = -1
	for _, o := range castOptions(t, e) {
		if o.Obj == id && o.Mode == "flashback" {
			fb = o.Index
		}
	}
	if fb < 0 {
		t.Fatalf("flashback cast not offered: %+v", castOptions(t, e))
	}
	submitChoices(t, e, fb)
	d := passUntilNonPriority(t, e, 30)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("pending = %+v, want a search KChoose", d)
	}
	picked := d.Options[0].Obj
	submitChoices(t, e, 0)
	if got := e.G.Obj(picked).Zone; got != state.ZBattlefield {
		t.Fatalf("flashback Vehicle destination = %s, want battlefield (Count$wasCastFromGraveyard)", got)
	}
	replayCheck(t, e, cfg)
}
