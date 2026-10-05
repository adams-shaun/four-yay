package rules

// The Adamant keyword (CR 702.5): "Adamant — If at least N mana of a
// specified colour was spent to cast this spell, [effect]." The count is the
// PER-COLOUR part of the mana actually paid -- a generic pip paid from a
// coloured source counts toward that colour -- so it needs the pay-time
// per-colour spend capture (Object.ManaColorSpent, the FlagManaColorSpent
// CastInfo) and the Count$Adamant head that reads it.
//
// Two distinct corpus shapes exercise it, both pinned here with BOTH a
// qualifying and a non-qualifying cast:
//
//   - the etbCounter compiled gate: `K:etbCounter:P1P1:1:Adamant$ Blue`
//     (Ardenvale/Embereth/Locthwain/Vantress Paladin), which the expander
//     compiles to a replacement CheckSVar$ Count$Adamant_3.<colour>.1.0;
//   - a ChangesZone trigger with `CheckSVar$ CastSA>Count$Adamant_2.<colour>.2.0`
//     (Catharsis, Deceit, Vibrance, Wistfulness, Emptiness).
//
// Each fixture is a real-script SHAPE written inline (never a .cards/ .txt),
// per the repo's licensing rule.

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

const adamantSentrySrc = "Name:Adamant Sentry\nManaCost:3 U\nTypes:Creature Soldier\nPT:1/1\n" +
	"K:etbCounter:P1P1:1:Adamant$ Blue:Adamant — If at least three blue mana was spent to cast this spell, CARDNAME enters with a +1/+1 counter on it.\n" +
	"Oracle:x\n"

const adamantRagerSrc = "Name:Adamant Rager\nManaCost:2 R\nTypes:Creature Elemental\nPT:2/2\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | CheckSVar$ CastSA>Count$Adamant_2.Red.2.0 | ValidCard$ Card.Self | Execute$ TrigGain | TriggerDescription$ When CARDNAME enters, if {R}{R} was spent to cast it, you gain 1 life.\n" +
	"SVar:TrigGain:DB$ GainLife | Defined$ You | LifeAmount$ 1\n" +
	"Oracle:x\n"

const adamantHengeSrc = "Name:Adamant Henge\nManaCost:4 G\nTypes:Creature Giant\nPT:3/3\n" +
	"K:etbCounter:P1P1:1:Adamant$ Any:Adamant — If at least three mana of the same color was spent to cast this spell, CARDNAME enters with a +1/+1 counter on it.\n" +
	"Oracle:x\n"

// TestAdamantEtbCounterAppliesOnlyWhenColourThresholdPaid casts the
// etbCounter Adamant carrier twice: once paying at least three blue mana
// (the counter must apply) and once paying only one blue with three
// colourless toward the generic (the counter must NOT apply). The
// ManaColorSpent precondition proves the per-colour capture actually ran, so
// the non-qualifying case fails for the right reason (the gate) and not
// because the whole feature is unregistered.
func TestAdamantEtbCounterAppliesOnlyWhenColourThresholdPaid(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		mana     string
		wantBlue int32
		want     int32
	}{
		{"threeBlue", "UUUU", 4, 1},
		{"oneBlueThreeColourless", "CCCU", 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, cfg, find := etbConfig(t, 114, []string{adamantSentrySrc}, nil)
			id := find("Adamant Sentry", 0)
			addMana(t, e, 0, tc.mana)
			castFirst(t, e, "cast")
			passUntilStackEmpty(t, e, 20)
			o := e.G.Obj(id)
			if o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: zone=%s, want battlefield", o.Zone)
			}
			if got := o.ManaColorSpent[state.MU]; got != tc.wantBlue {
				t.Fatalf("precondition: blue spent = %d, want %d (mana %q; capture did not run)",
					got, tc.wantBlue, tc.mana)
			}
			if got := o.Counter("P1P1"); got != tc.want {
				t.Fatalf("P1P1 counters = %d, want %d (blue spent %d, mana %q)",
					got, tc.want, tc.wantBlue, tc.mana)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestAdamantAnyEtbCounterRequiresThreeOfOneColour pins the keyword's
// "at least three mana of the SAME colour" form (Henge Walker's
// `Adamant$ Any`), which the etbCounter expander compiles to
// Count$Adamant_3.Any.1.0. Five green mana qualifies (one colour reaches
// three); two green + two red + one colourless does not (no single colour
// reaches three).
func TestAdamantAnyEtbCounterRequiresThreeOfOneColour(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		mana      string
		wantGreen int32
		wantRed   int32
		want      int32
	}{
		{"fiveGreen", "GGGGG", 5, 0, 1},
		{"twoGreenTwoRedOneColourless", "GGRRC", 2, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, cfg, find := etbConfig(t, 116, []string{adamantHengeSrc}, nil)
			id := find("Adamant Henge", 0)
			addMana(t, e, 0, tc.mana)
			castFirst(t, e, "cast")
			passUntilStackEmpty(t, e, 20)
			o := e.G.Obj(id)
			if o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: zone=%s, want battlefield", o.Zone)
			}
			if got := o.ManaColorSpent[state.MG]; got != tc.wantGreen {
				t.Fatalf("precondition: green spent = %d, want %d (mana %q; capture did not run)",
					got, tc.wantGreen, tc.mana)
			}
			if got := o.ManaColorSpent[state.MR]; got != tc.wantRed {
				t.Fatalf("precondition: red spent = %d, want %d (mana %q; capture did not run)",
					got, tc.wantRed, tc.mana)
			}
			if got := o.Counter("P1P1"); got != tc.want {
				t.Fatalf("P1P1 counters = %d, want %d (max single colour %d, mana %q)",
					got, tc.want, max(tc.wantGreen, tc.wantRed), tc.mana)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// Adamant carrier twice: once paying {R}{R} (the trigger's CheckSVar$
// CastSA>Count$Adamant_2.Red.2.0 must read the 2+ red and fire, gaining 1
// life) and once paying one red with two colourless (the gate must suppress
// the trigger -- no life gained). The ManaColorSpent assertion is the
// precondition that separates "the gate correctly denied" from "the head
// never evaluated at all": the red spend is present on the permanent either
// way.
func TestAdamantCountHeadTriggerGatesOnColourThreshold(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		mana     string
		wantRed  int32
		wantLife int32
	}{
		{"twoRed", "RRR", 3, 1},
		{"oneRedTwoColourless", "CCR", 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, cfg, find := etbConfig(t, 115, []string{adamantRagerSrc}, nil)
			id := find("Adamant Rager", 0)
			addMana(t, e, 0, tc.mana)
			lifeBefore := e.G.Players[0].Life
			castFirst(t, e, "cast")
			passUntilStackEmpty(t, e, 20)
			o := e.G.Obj(id)
			if o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: zone=%s, want battlefield", o.Zone)
			}
			if got := o.ManaColorSpent[state.MR]; got != tc.wantRed {
				t.Fatalf("precondition: red spent = %d, want %d (mana %q; capture did not run)",
					got, tc.wantRed, tc.mana)
			}
			if got := e.G.Players[0].Life - lifeBefore; got != tc.wantLife {
				t.Fatalf("life gained = %d, want %d (red spent %d, mana %q)",
					got, tc.wantLife, tc.wantRed, tc.mana)
			}
			replayCheck(t, e, cfg)
		})
	}
}
