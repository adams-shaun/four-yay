package rules

// Deferred second asks (the pro-shaper overwrite panic): an effect that moves
// a pay-2-life entry-replacement land keeps running its own body after the
// land's ReplaceWith$ body suspended the resolution on its unless-pay ask.
// When that body then asks AGAIN -- a mass return reaching a second such
// land, a library search reaching its ShuffleNonMandatory$ confirm -- the
// second ask used to overwrite the pending one and Engine.ask panicked
// ("ask overwrote a suspended resolution's pending decision"). The second ask
// is now deferred onto the resolution's continuation chain and posed right
// after the first one's answer, before the rest of the SubAbility$ chain.
//
// Test-local scripts only (authored here, never corpus text).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// deferredAskShockLand is an authored land that, as it enters, lets its
// controller pay 2 life or have it enter tapped.
const deferredAskShockLand = "Name:Deferred Ask Tarn\nTypes:Land\n" +
	"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ DBTap | ReplacementResult$ Updated\n" +
	"SVar:DBTap:DB$ Tap | ETB$ True | Defined$ Self | UnlessCost$ PayLife<2> | UnlessPayer$ You\n" +
	"Oracle:x\n"

// castDeferredAskSorcery puts spell in seat 0's hand (with the other cards
// already placed by the caller), casts it with the one G it costs and passes
// priority until the resolution poses its first non-priority decision.
func castDeferredAskSorcery(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	e.G.Players[0].Pool[state.MG] = 1
	e.askPriority(0)
	castFirst(t, e, "cast")
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision pending while resolving the sorcery")
		}
		if d.Kind != decision.KPriority {
			return d
		}
		castFirst(t, e, "pass")
	}
	t.Fatal("the sorcery never resolved to a mid-resolution ask")
	return nil
}

func deferredOptionIndex(t *testing.T, d *decision.Decision, label string) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Label == label {
			return o.Index
		}
	}
	t.Fatalf("no %q option in %+v", label, d.Options)
	return -1
}

// TestDeferredAskMassReturnOfTwoPayLifeLands: a sorcery returns two copies
// of the pay-2-life land from the graveyard at once, then gains 5 life. Both
// lands' asks are posed, one after the other, each applying to its own land,
// and the SubAbility$ runs exactly once, after both.
func TestDeferredAskMassReturnOfTwoPayLifeLands(t *testing.T) {
	t.Parallel()
	sweep := card(t, "Name:Deferred Ask Sweep\nManaCost:G\nTypes:Sorcery\n"+
		"A:SP$ ChangeZoneAll | ChangeType$ Land.YouCtrl | Origin$ Graveyard | Destination$ Battlefield | SubAbility$ DBGain\n"+
		"SVar:DBGain:DB$ GainLife | Defined$ You | LifeAmount$ 5\nOracle:x\n")
	land := card(t, deferredAskShockLand)
	e := handEngine(t, sweep)
	var lands []state.ObjID
	for i := 0; i < 2; i++ {
		o := e.G.AddObject(land, 0)
		o.Zone = state.ZGraveyard
		lands = append(lands, o.ID)
	}
	e.G.SetZone(state.ZGraveyard, 0, append([]state.ObjID(nil), lands...))

	first := castDeferredAskSorcery(t, e)
	if first.Kind != decision.KModes || first.ResumeKind != "unless_pay" {
		t.Fatalf("first ask = %s/%s, want the first land's unless_pay", first.Kind, first.ResumeKind)
	}
	firstLand := first.Source
	submitChoices(t, e, deferredOptionIndex(t, first, "Pay 2 life"))

	second := e.Pending()
	if second == nil || second.Kind != decision.KModes || second.ResumeKind != "unless_pay" {
		t.Fatalf("after the first answer, pending = %+v, want the second land's unless_pay", second)
	}
	secondLand := second.Source
	if secondLand == firstLand {
		t.Fatalf("the second ask is about the same land %d", firstLand)
	}
	if got := e.G.Players[0].Life; got != 18 {
		t.Fatalf("life after paying for the first land = %d, want 18 (and the sub not yet run)", got)
	}
	submitChoices(t, e, deferredOptionIndex(t, second, "Don't pay"))

	if o := e.G.Obj(firstLand); o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("paid land: zone %v tapped %v, want untapped on the battlefield", o.Zone, o.Tapped)
	}
	if o := e.G.Obj(secondLand); o.Zone != state.ZBattlefield || !o.Tapped {
		t.Fatalf("declined land: zone %v tapped %v, want tapped on the battlefield", o.Zone, o.Tapped)
	}
	if got := e.G.Players[0].Life; got != 23 {
		t.Fatalf("life = %d, want 23 (20 - 2 + the SubAbility$'s 5, run exactly once)", got)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want the sorcery resolved", e.G.Stack)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want priority after the resolution", d)
	}
}

// TestDeferredAskSearchShuffleConfirmAfterPayLifeLand: a sorcery searches
// for the pay-2-life land, puts it onto the battlefield with the may-shuffle
// confirm, then gains 5 life. The land's ask comes first, the shuffle confirm
// second, the SubAbility$ last.
func TestDeferredAskSearchShuffleConfirmAfterPayLifeLand(t *testing.T) {
	t.Parallel()
	search := card(t, "Name:Deferred Ask Search\nManaCost:G\nTypes:Sorcery\n"+
		"A:SP$ ChangeZone | Origin$ Library | Destination$ Battlefield | ChangeType$ Land.nonBasic | ChangeNum$ 1 | ShuffleNonMandatory$ True | SubAbility$ DBGain\n"+
		"SVar:DBGain:DB$ GainLife | Defined$ You | LifeAmount$ 5\nOracle:x\n")
	e := handEngine(t, search)
	o := e.G.AddObject(card(t, deferredAskShockLand), 0)
	o.Zone = state.ZLibrary
	tarn := o.ID
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{tarn}, e.G.Zone(state.ZLibrary, 0)...))

	d := castDeferredAskSorcery(t, e)
	if d.ResumeKind != "unless_pay" {
		// The search's own pick comes first.
		pick := -1
		for _, opt := range d.Options {
			if opt.Obj == tarn {
				pick = opt.Index
			}
		}
		if pick < 0 {
			t.Fatalf("search ask %s/%s does not offer the land: %+v", d.Kind, d.ResumeKind, d.Options)
		}
		submitChoices(t, e, pick)
		d = e.Pending()
	}
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "unless_pay" {
		t.Fatalf("pending = %+v, want the searched land's unless_pay", d)
	}
	submitChoices(t, e, deferredOptionIndex(t, d, "Pay 2 life"))

	d = e.Pending()
	if d == nil || d.ResumeKind != "search_mayshuffle" {
		t.Fatalf("after the land's answer, pending = %+v, want the deferred shuffle confirm", d)
	}
	if got := e.G.Players[0].Life; got != 18 {
		t.Fatalf("life before the confirm = %d, want 18 (the SubAbility$ must wait for the confirm)", got)
	}
	submitChoices(t, e, deferredOptionIndex(t, d, "No — keep the order"))

	if o := e.G.Obj(tarn); o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("paid land: zone %v tapped %v, want untapped on the battlefield", o.Zone, o.Tapped)
	}
	if got := e.G.Players[0].Life; got != 23 {
		t.Fatalf("life = %d, want 23 (the SubAbility$ ran exactly once, after the confirm)", got)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want the sorcery resolved", e.G.Stack)
	}
}
