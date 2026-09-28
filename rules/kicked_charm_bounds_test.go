package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// kickedCharmSrc is an inline (never corpus) Charm shaped like Inscription of
// Abundance: K:Kicker plus MinCharmNum$ X | CharmNum$ Y over
// SVar:X:Count$Kicked.0.1 and SVar:Y:Count$Kicked.3.1 -- "choose one; if
// kicked, choose any number instead". The modes are targetless so the mode
// ask is the only cast-time decision under test.
const kickedCharmSrc = "Name:Kicked Charm Probe\nManaCost:R\nTypes:Instant\nK:Kicker:R\n" +
	"A:SP$ Charm | MinCharmNum$ X | CharmNum$ Y | Choices$ DBOne,DBTwo,DBThree\n" +
	"SVar:DBOne:DB$ GainLife | Defined$ You | LifeAmount$ 1 | SpellDescription$ Gain 1 life.\n" +
	"SVar:DBTwo:DB$ GainLife | Defined$ You | LifeAmount$ 2 | SpellDescription$ Gain 2 life.\n" +
	"SVar:DBThree:DB$ GainLife | Defined$ You | LifeAmount$ 3 | SpellDescription$ Gain 3 life.\n" +
	"SVar:X:Count$Kicked.0.1\nSVar:Y:Count$Kicked.3.1\nOracle:x\n"

// kickedCharmModeAsk drives the REAL cast flow -- priority, the cast option
// (the kicked/unkicked choice is the cast option's Mode, CR 601.2b's
// announced additional cost), then castModeAsk's CR 601.2b mode
// announcement -- and returns the posed KModes decision.
func kickedCharmModeAsk(t *testing.T, kicked bool) *decision.Decision {
	t.Helper()
	e, _, id := newFixtureDeck(t, 31, kickedCharmSrc)
	addMana(t, e, 0, "RR")
	want := ""
	if kicked {
		want = "kicked"
	}
	idx := -1
	for _, o := range castOptions(t, e) {
		if o.Obj == id && o.Mode == want {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option with mode %q: %+v", want, castOptions(t, e))
	}
	submitChoices(t, e, idx)
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("kicked=%v: want the cast-time KModes ask, got %+v", kicked, d)
	}
	if d.ResumeKind != "cast_modes" {
		t.Fatalf("kicked=%v: mode ask ResumeKind = %q, want cast_modes", kicked, d.ResumeKind)
	}
	return d
}

// TestKickedCharmModeBoundsInTheRealCast pins Inscription of Abundance's
// shape end to end: kicked offers 0..3 modes (Count$Kicked.0.1 /
// Count$Kicked.3.1), unkicked exactly one.
func TestKickedCharmModeBoundsInTheRealCast(t *testing.T) {
	t.Parallel()
	if d := kickedCharmModeAsk(t, true); d.Min != 0 || d.Max != 3 {
		t.Fatalf("kicked mode bounds = %d..%d, want 0..3", d.Min, d.Max)
	}
	if d := kickedCharmModeAsk(t, false); d.Min != 1 || d.Max != 1 {
		t.Fatalf("unkicked mode bounds = %d..%d, want 1..1", d.Min, d.Max)
	}
}
