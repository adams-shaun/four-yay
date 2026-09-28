package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CR 601.2h / 106.12, the TRIGGER-relative spelling (task ctms-refhead):
// `TriggeredCard$CastTotalManaSpent` reads the total mana actually spent to
// cast the spell a firing trigger is about. Before the fix evalRefProperty had
// no arm for the property, so every one of the 21 corpus SVars of that exact
// spelling (Aberrant Manawurm, Manaform Hellkite, Muse Seeker, Aetherflux
// Conduit, ...) read the silent zero.
//
// The fixture is the real corpus carrier Aberrant Manawurm, read at runtime
// from .cards/ (never a committed .txt, per the licensing rule): its
// "gets +X/+0 ... where X is the amount of mana spent to cast that spell"
// makes the derived power the exact observable of the head.

const probeOneSrc = "Name:Probe One\nManaCost:R\nTypes:Instant\nOracle:x\n"
const probeThreeSrc = "Name:Probe Three\nManaCost:2 R\nTypes:Instant\nOracle:x\n"

// TestTriggeredCardCastTotalManaSpentPumpsEndToEnd pins the head end to end:
// Aberrant Manawurm on the battlefield pumps +X/+0 where X is the mana spent
// to cast the instant. Two DISTINCT spends are asserted on purpose -- a
// three-mana cast (2/5 -> 5/5) and a one-mana cast (2/5 -> 3/5) -- because the
// defect is a silent zero and a single-value test could pass by coincidence.
func TestTriggeredCardCastTotalManaSpentPumpsEndToEnd(t *testing.T) {
	t.Parallel()
	t.Run("three mana spent", func(t *testing.T) {
		wurmSrc := corpusCardText(t, "a/aberrant_manawurm.txt")
		e, cfg, find := etbConfig(t, seedTossSeat0(161), []string{wurmSrc, probeThreeSrc}, nil)
		wurmID := find("Aberrant Manawurm", 0)
		putCreature(t, e, 0, wurmSrc)
		// Precondition: the trigger's source really is on the battlefield (its
		// TriggerZones$ is Battlefield) and starts at its printed 2/5.
		if o := e.G.Obj(wurmID); o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Aberrant Manawurm zone=%s, want battlefield", o.Zone)
		}
		if got := e.Power(wurmID); got != 2 {
			t.Fatalf("precondition: Aberrant Manawurm power=%d, want printed 2", got)
		}
		addMana(t, e, 0, "RRR") // {2}{R} paid entirely in R = 3 mana spent
		castNamed(t, e, "Probe Three")
		passUntilStackEmpty(t, e, 20)
		if got := e.Power(wurmID); got != 5 {
			t.Fatalf("Aberrant Manawurm power=%d, want 5 (2/5 base +X/+0 with X=3 mana spent)", got)
		}
		replayCheck(t, e, cfg)
	})
	t.Run("one mana spent", func(t *testing.T) {
		wurmSrc := corpusCardText(t, "a/aberrant_manawurm.txt")
		e, cfg, find := etbConfig(t, seedTossSeat0(167), []string{wurmSrc, probeOneSrc}, nil)
		wurmID := find("Aberrant Manawurm", 0)
		putCreature(t, e, 0, wurmSrc)
		if o := e.G.Obj(wurmID); o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Aberrant Manawurm zone=%s, want battlefield", o.Zone)
		}
		addMana(t, e, 0, "R") // {R} = 1 mana spent
		castNamed(t, e, "Probe One")
		passUntilStackEmpty(t, e, 20)
		if got := e.Power(wurmID); got != 3 {
			t.Fatalf("Aberrant Manawurm power=%d, want 3 (2/5 base +X/+0 with X=1 mana spent)", got)
		}
		replayCheck(t, e, cfg)
	})
}

// TestTriggeredCardCastTotalManaSpentNoReaderOutStampsNothing pins the capture
// gate's heads-safety arm: the same cast with the reader still in hand must
// stamp no FlagManaSpent CastInfo (nothing on the battlefield could read the
// value), so a cast no trigger reads changes no event and the chain heads stay
// put for every game that holds no reader out. The cast itself is asserted to
// have happened (the spell resolved to the graveyard), so the absent stamp is
// meaningful rather than a skipped cast.
func TestTriggeredCardCastTotalManaSpentNoReaderOutStampsNothing(t *testing.T) {
	t.Parallel()
	wurmSrc := corpusCardText(t, "a/aberrant_manawurm.txt")
	e, cfg, find := etbConfig(t, seedTossSeat0(173), []string{wurmSrc, probeThreeSrc}, nil)
	wurmID := find("Aberrant Manawurm", 0)
	spellID := find("Probe Three", 0)
	// Precondition: the reader is NOT on the battlefield (it is still in hand),
	// so this cast has no reader out.
	if o := e.G.Obj(wurmID); o.Zone == state.ZBattlefield {
		t.Fatalf("precondition: Aberrant Manawurm zone=%s, want not battlefield", o.Zone)
	}
	addMana(t, e, 0, "RRR")
	castNamed(t, e, "Probe Three")
	passUntilStackEmpty(t, e, 20)
	// Precondition: the cast really happened and resolved.
	if o := e.G.Obj(spellID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: Probe Three did not resolve to the graveyard (%v)", o)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == spellID && events.FlagsFrom(ev.Counter)&state.FlagManaSpent != 0 {
			t.Fatal("FlagManaSpent CastInfo stamped on a cast with no reader out")
		}
	}
	replayCheck(t, e, cfg)
}
