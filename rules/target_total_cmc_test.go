package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const totalCMCCapAbility = "Name:CMC Reclaimer\nManaCost:2\nTypes:Creature\n" +
	"A:AB$ ChangeZone | Cost$ 0 | Origin$ Graveyard | Destination$ Hand | TargetMin$ 0 | TargetMax$ 3 | MaxTotalTargetCMC$ 3 | ValidTgts$ Creature.YouOwn\n" +
	"Oracle:x\n"

// TestSetAudit_eoe_ScoutForSurvivors_TotalManaValueCap pins the selection
// budget from Scout for Survivors' ChangeZone shape. The three graveyard
// creatures are real legal candidates individually, but their combined MV 9
// must not be accepted under the shared MV-3 cap.
func TestSetAudit_eoe_ScoutForSurvivors_TotalManaValueCap(t *testing.T) {
	t.Parallel()
	bear := "Name:MV Three Bear\nManaCost:2 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, source := newFixtureDeck(t, 92801, totalCMCCapAbility, bear, bear, bear)
	toMain1(t, e)
	ids := make([]state.ObjID, 0, 3)
	for _, id := range append(append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...), e.G.Zone(state.ZLibrary, 0)...) {
		o := e.G.Obj(id)
		if o != nil && o.Face() != nil && o.Face().Name == "MV Three Bear" && o.Zone != state.ZGraveyard {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZGraveyard})
			ids = append(ids, id)
		}
	}
	if len(ids) != 3 {
		t.Fatalf("precondition: moved %d MV-3 bears to graveyard, want 3", len(ids))
	}
	if o := e.G.Obj(source); o == nil {
		t.Fatal("precondition: source object is missing")
	} else if o.Zone != state.ZBattlefield {
		e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: o.Zone, To: state.ZBattlefield})
	}
	e.pending = nil
	e.priorityRound()
	submitChoices(t, e, abilityOption(t, e, source, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.MaxSum != 3 || !d.HasBudget() {
		t.Fatalf("target ask = %+v, want a cumulative MV-3 target budget", d)
	}
	if len(d.Options) < 3 {
		t.Fatalf("precondition: target ask offered %d of 3 legal MV-3 candidates", len(d.Options))
	}
	oversize := []int{d.Options[0].Index, d.Options[1].Index}
	if d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: oversize}) == nil {
		t.Fatalf("oversize target set %v (total MV 6) passed decision validation", oversize)
	}
	clamped := newTestBot(1).answer(e, d)
	if err := d.Validate(clamped); err != nil {
		t.Fatalf("bot answer %v violates the CMC cap: %v", clamped.Choices, err)
	}
	if len(clamped.Choices) != 1 {
		t.Fatalf("bot selected %d targets, want one under MV-3 cap: %v", len(clamped.Choices), clamped.Choices)
	}
	if err := e.Submit(clamped); err != nil {
		t.Fatalf("submit clamped answer: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	returned := 0
	for _, id := range ids {
		if o := e.G.Obj(id); o != nil && o.Zone == state.ZHand {
			returned++
		}
	}
	if returned != 1 {
		t.Fatalf("returned %d MV-3 creatures (total %d) under total-MV cap 3, want one", returned, returned*3)
	}
	replayCheck(t, e, cfg)
}

// TestMaxTotalTargetCMCCorpusCensus keeps the mechanism class visible and
// names every current Forge script carrying the parameter.
func TestMaxTotalTargetCMCCorpusCensus(t *testing.T) {
	t.Parallel()
	var files []string
	err := filepath.WalkDir("../.cards/cardsfolder", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".txt") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "MaxTotalTargetCMC$") {
			files = append(files, filepath.Base(path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 14 {
		t.Fatalf("MaxTotalTargetCMC$ corpus carriers = %d, want 14: %v", len(files), files)
	}
	t.Logf("MaxTotalTargetCMC$ carriers (%d): %s", len(files), strings.Join(files, ", "))
}
