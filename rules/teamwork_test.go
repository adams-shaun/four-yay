package rules

import (
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func teamworkAskOptions(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || len(d.Options) < 2 || d.Options[0].Kind != "teamwork_decline" {
		t.Fatalf("expected optional Teamwork KChoose, got %+v", d)
	}
	return d
}

func finishTeamworkAnnouncement(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 6; i++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			return
		}
		if len(d.Options) > 0 && (d.Kind == decision.KTarget || d.Kind == decision.KModes || d.Kind == decision.KChoose) {
			submitChoices(t, e, d.Options[0].Index)
			continue
		}
		return
	}
}

func teamworkOption(t *testing.T, d *decision.Decision, id state.ObjID) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == "teamwork" && o.Obj == id {
			return o.Index
		}
	}
	t.Fatalf("Teamwork creature %d not offered: %+v", id, d.Options)
	return -1
}

// The real Go Nuts! corpus card has Count$Teamwork.2.1 and Teamwork 3.
// This cast pays with both two-power creatures, leaving a third untapped.
func TestTeamworkCastCostAndPaidProvenance(t *testing.T) {
	t.Parallel()
	e, cfg, reg := conspireEngine(t, "Go Nuts!")
	a := seedBattlefield(t, e, reg, "Goblin Piker")
	b := seedBattlefield(t, e, reg, "Grizzly Bears")
	spare := seedBattlefield(t, e, reg, "Grizzly Bears")
	hero := searchMoveByName(t, e, "Go Nuts!", state.ZHand)
	addMana(t, e, 0, "G")
	cast := castOptMode(t, e.Pending().Options, hero, "teamworked")
	submitChoices(t, e, cast.Index)
	d := teamworkAskOptions(t, e)
	if d.MinSum != 3 {
		t.Fatalf("Teamwork threshold = %d, want 3", d.MinSum)
	}
	botAnswer := botpolicy.Decide(botpolicy.Board{}, d, rand.New(rand.NewPCG(1, 2)))
	if err := d.Validate(botAnswer); err != nil {
		t.Fatalf("bot's default Teamwork answer violates the shared threshold rule: %v (answer=%+v)", err, botAnswer)
	}
	below := teamworkOption(t, d, a)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{below}}); err == nil {
		t.Fatal("accepted one power-2 creature below Teamwork 3 threshold")
	}
	// The rejected decision remains pending; choose the legal power-4 subset.
	d = teamworkAskOptions(t, e)
	submitChoices(t, e, teamworkOption(t, d, a), teamworkOption(t, d, b))
	// Resolve the spell's mode and target announcements until payment pushes it.
	finishTeamworkAnnouncement(t, e)
	if !e.G.Obj(a).Tapped || !e.G.Obj(b).Tapped || e.G.Obj(spare).Tapped {
		t.Fatalf("tapped a=%v b=%v spare=%v; pending=%+v; cast=%+v; only selected creatures should tap", e.G.Obj(a).Tapped, e.G.Obj(b).Tapped, e.G.Obj(spare).Tapped, e.Pending(), e.cast)
	}
	if !e.G.Obj(hero).TeamworkPaid {
		t.Fatal("paid Teamwork provenance not folded onto the spell")
	}
	paidBranch := effects.EvalCount(e, &effects.Ctx{Source: hero}, "Count$Teamwork.2.1")
	if paidBranch != 2 {
		t.Fatalf("paid Count$Teamwork branch=%d, want 2", paidBranch)
	}
	if !tappedByCost(e, a) || !tappedByCost(e, b) {
		t.Fatal("selected creatures were not tapped through payment events")
	}
	foundFlag := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == hero && events.FlagsFrom(ev.Counter)&state.FlagTeamworkPaid != 0 {
			foundFlag = true
		}
	}
	if !foundFlag {
		t.Fatal("paid cast emitted no FlagTeamworkPaid CastInfo")
	}
	_ = cfg // This test pins the paid event/state fold; resolution is covered elsewhere.
}

func TestTeamworkDeclinedIsUnpaid(t *testing.T) {
	t.Parallel()
	e, cfg, reg := conspireEngine(t, "Go Nuts!")
	a := seedBattlefield(t, e, reg, "Goblin Piker")
	seedBattlefield(t, e, reg, "Grizzly Bears")
	hero := searchMoveByName(t, e, "Go Nuts!", state.ZHand)
	addMana(t, e, 0, "G")
	cast := castOptMode(t, e.Pending().Options, hero, "teamworked")
	submitChoices(t, e, cast.Index)
	d := teamworkAskOptions(t, e)
	submitChoices(t, e, d.Options[0].Index)
	finishTeamworkAnnouncement(t, e)
	if e.G.Obj(hero).Zone != state.ZStack {
		t.Fatalf("declined test did not finish casting; zone=%s pending=%+v", e.G.Obj(hero).Zone, e.Pending())
	}
	if e.G.Obj(hero).TeamworkPaid || e.G.Obj(a).Tapped {
		t.Fatalf("declined Teamwork has paid=%v, creature tapped=%v", e.G.Obj(hero).TeamworkPaid, e.G.Obj(a).Tapped)
	}
	unpaidBranch := effects.EvalCount(e, &effects.Ctx{Source: hero}, "Count$Teamwork.2.1")
	if paidBranch := int32(2); paidBranch == unpaidBranch {
		t.Fatalf("precondition: paid/unpaid branch values equal: %d", unpaidBranch)
	}
	if unpaidBranch != 1 {
		t.Fatalf("unpaid Count$Teamwork branch=%d, want 1", unpaidBranch)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == hero && events.FlagsFrom(ev.Counter)&state.FlagTeamworkPaid != 0 {
			t.Fatal("declined cast emitted paid Teamwork provenance")
		}
	}
	_ = cfg // The assertion is the absence of a paid provenance event.
}
