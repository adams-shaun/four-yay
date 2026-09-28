package rules

// CR 107.4f-family converge (task converge1): "enters with a +1/+1 counter
// on it for each color of mana spent to cast it" and its mid-spell siblings
// (Radiant Flames' damage, Painful Truths' draw) read Count$Converge. Before
// the fix that head fell off effects/count.go's expression evaluator to the
// unresolvable-value convention (0) -- the same silent-zero signature the
// TriggerCount$ bug had -- because the per-colour spend was never captured:
// payManaCast discarded the payment delta payManaForSpent returns. The fix
// captures the FULL spent delta (restricted batches included) at the cast's
// payment and carries it onto the object through a trailing pay-time
// CastInfo with state.FlagConverged -- the same transport the replicate
// count rides one flag over -- and evalCountBody's "Converge" head reads it
// off the source object.
//
// Every fixture here is a real Forge script SHAPE (Crystalline Crawler and
// Radiant Flames are corpus carriers, byte-identical scripts) written inline
// per the repo's licensing rule: never a .cards/ .txt.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const crystallineCrawlerSrc = "Name:Crystalline Crawler\nManaCost:4\nTypes:Artifact Creature Construct\nPT:1/1\n" +
	"K:etbCounter:P1P1:X:no Condition:Converge — CARDNAME enters with a +1/+1 counter on it for each color of mana spent to cast it.\n" +
	"SVar:X:Count$Converge\n" +
	"A:AB$ Mana | Cost$ SubCounter<1/P1P1> | Produced$ Any | SpellDescription$ Add one mana of any color.\n" +
	"A:AB$ PutCounter | Cost$ T | CounterType$ P1P1 | CounterNum$ 1 | SpellDescription$ Put a +1/+1 counter on CARDNAME.\n" +
	"DeckHas:Ability$Counters\n" +
	"Oracle:Converge — Crystalline Crawler enters with a +1/+1 counter on it for each color of mana spent to cast it.\\nRemove a +1/+1 counter from Crystalline Crawler: Add one mana of any color.\\n{T}: Put a +1/+1 counter on Crystalline Crawler.\n"

const radiantFlamesSrc = "Name:Radiant Flames\nManaCost:2 R\nTypes:Sorcery\n" +
	"A:SP$ DamageAll | NumDmg$ X | ValidCards$ Creature | ValidDescription$ each creature. | SpellDescription$ Converge — CARDNAME deals X damage to each creature, where X is the number of colors of mana spent to cast this spell.\n" +
	"SVar:X:Count$Converge\n" +
	"AI:RemoveDeck:All\n" +
	"Oracle:Converge — Radiant Flames deals X damage to each creature, where X is the number of colors of mana spent to cast this spell.\n"

// TestConvergeCrawlerEntersWithColourCount is the brief's own probe on the
// real corpus carrier: {4} paid with two colours enters with exactly 2
// +1/+1 counters, the SAME cast shape paid with one colour enters with
// exactly 1. Two different values on purpose -- the defect is a silent
// zero, and a single-value test can pass by coincidence. The generic
// deduction spends pool colours in the fixed MC,W,U,B,R,G order (the search
// that resolveManaWith closes with), so {4} from R,R,G,G pays 2 R + 2 G and
// the delta is exactly the two colours.
func TestConvergeCrawlerEntersWithColourCount(t *testing.T) {
	t.Parallel()
	t.Run("two colours of mana", func(t *testing.T) {
		e, cfg, find := etbConfig(t, 131, []string{crystallineCrawlerSrc}, nil)
		id := find("Crystalline Crawler", 0)
		addMana(t, e, 0, "RRGG") // {4} paid 2 R + 2 G
		castFirst(t, e, "cast")
		passUntilStackEmpty(t, e, 20)
		o := e.G.Obj(id)
		if o.Zone != state.ZBattlefield || o.Counter("P1P1") != 2 {
			t.Fatalf("Crawler zone=%s counters=%d, want battlefield/2 (two colours spent)", o.Zone, o.Counter("P1P1"))
		}
		if o.ConvergeColours != 2 {
			t.Fatalf("Crawler ConvergeColours=%d, want 2", o.ConvergeColours)
		}
		replayCheck(t, e, cfg)
	})
	t.Run("one colour of mana", func(t *testing.T) {
		e, cfg, find := etbConfig(t, 138, []string{crystallineCrawlerSrc}, nil) // seed: toss starts seat 0
		id := find("Crystalline Crawler", 0)
		addMana(t, e, 0, "RRRR") // {4} paid 4 R
		castFirst(t, e, "cast")
		passUntilStackEmpty(t, e, 20)
		o := e.G.Obj(id)
		if o.Zone != state.ZBattlefield || o.Counter("P1P1") != 1 {
			t.Fatalf("Crawler zone=%s counters=%d, want battlefield/1 (one colour spent)", o.Zone, o.Counter("P1P1"))
		}
		if o.ConvergeColours != 1 {
			t.Fatalf("Crawler ConvergeColours=%d, want 1", o.ConvergeColours)
		}
		replayCheck(t, e, cfg)
	})
}

// TestConvergeColourlessSpendCountsZero pins the "not a colour" half: {C}
// and generic mana spent to cast a converge card count ZERO colours, so the
// Crawler paid 4 colourless enters with no +1/+1 counters at all. The
// capture still ran -- the pay-time CastInfo was emitted for the converge
// face and recorded the genuine zero -- which is what the ConvergeColours
// field assertion pins (a colourless-only cast is a real zero, not a
// missing capture).
func TestConvergeColourlessSpendCountsZero(t *testing.T) {
	t.Parallel()
	e, cfg, find := etbConfig(t, 133, []string{crystallineCrawlerSrc}, nil)
	id := find("Crystalline Crawler", 0)
	addMana(t, e, 0, "CCCC") // {4} paid entirely as {C}
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield || o.Counter("P1P1") != 0 {
		t.Fatalf("Crawler zone=%s counters=%d, want battlefield/0 (colourless is not a colour)", o.Zone, o.Counter("P1P1"))
	}
	if o.ConvergeColours != 0 {
		t.Fatalf("Crawler ConvergeColours=%d, want 0", o.ConvergeColours)
	}
	// The face-gated CastInfo carried the zero through the log, so a replay
	// derives the same field -- and the capture is observable in the event
	// stream itself.
	sawConvergeCastInfo := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == id && events.FlagsFrom(ev.Counter)&state.FlagConverged != 0 {
			if ev.Amount != 0 {
				t.Fatalf("converge CastInfo Amount=%d, want 0", ev.Amount)
			}
			sawConvergeCastInfo = true
		}
	}
	if !sawConvergeCastInfo {
		t.Fatal("no FlagConverged CastInfo event on the colourless-only converge cast")
	}
	replayCheck(t, e, cfg)
}

// TestConvergeRadiantFlamesDealsPerColour pins the MID-RESOLUTION read site
// (not the ETB replacement): Radiant Flames' DamageAll resolves X through
// SVar:X:Count$Converge off the cast spell on the stack -- {2}{R} paid 2 R +
// 1 G is two colours, so the opposing 2/2 takes 2 and dies. Under the silent
// zero the same cast dealt 0 and the bear survived.
func TestConvergeRadiantFlamesDealsPerColour(t *testing.T) {
	t.Parallel()
	bear := "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, find := etbConfig(t, 134, []string{radiantFlamesSrc}, []string{bear})
	radiant := find("Radiant Flames", 0)
	bearID := putCreature(t, e, 1, bear)
	addMana(t, e, 0, "RRG") // {2}{R} paid 2 R + 1 G
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(bearID); o.Zone != state.ZGraveyard {
		t.Fatalf("bear zone=%s damage=%d, want graveyard (took 2 damage from two colours; radiant=%d)",
			o.Zone, o.Damage, e.G.Obj(radiant).Zone)
	}
	replayCheck(t, e, cfg)
}

// TestConvergeCastInfoNotStampedOnPlainCast pins the heads-safety gate from
// the other side: a multicolour cast of a face with NO Count$Converge SVar
// emits no FlagConverged CastInfo, so no game that casts no converge card
// changes an event (this is what keeps the chain heads put).
func TestConvergeCastInfoNotStampedOnPlainCast(t *testing.T) {
	t.Parallel()
	bolt := "Name:Bolt\nManaCost:2 R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n"
	e, cfg, find := etbConfig(t, 139, []string{bolt}, nil) // seed: toss starts seat 0
	boltID := find("Bolt", 0)
	addMana(t, e, 0, "RRGG") // a two-colour payment of a non-converge face
	castFirst(t, e, "cast")
	if d := e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target decision %+v", d)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 20)
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == boltID && events.FlagsFrom(ev.Counter)&state.FlagConverged != 0 {
			t.Fatal("FlagConverged CastInfo stamped on a non-converge face")
		}
	}
	replayCheck(t, e, cfg)
}
