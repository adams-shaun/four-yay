package rules

import (
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func teamworkAskOptions(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || len(d.Options) < 2 {
		t.Fatalf("expected Teamwork KChoose, got %+v", d)
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
			need := d.Min
			if need < 1 {
				need = 1
			}
			choices := make([]int, 0, need)
			groups := map[string]bool{}
			for _, option := range d.Options {
				if option.Group != "" && groups[option.Group] {
					continue
				}
				choices = append(choices, option.Index)
				if option.Group != "" {
					groups[option.Group] = true
				}
				if len(choices) == need {
					break
				}
			}
			submitChoices(t, e, choices...)
			continue
		}
		return
	}
}

// Resolve the actual ConditionPresent/ConditionCompare gate against the cast
// spell on the stack. The life change proves the condition ran rather than
// just its underlying object-filter predicate.
func teamworkConditionResult(t *testing.T, e *Engine, spell state.ObjID, wantPaid bool) {
	t.Helper()
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack {
		t.Fatalf("precondition: spell %d must be on the stack, got %+v", spell, o)
	}
	gate := &cards.SA{Kind: "DB", API: "GainLife", Params: map[string]string{
		"Defined": "You", "LifeAmount": "2", "ConditionDefined": "Self",
		"ConditionPresent": "Card.Self+Teamwork", "ConditionCompare": "EQ1",
	}}
	before := e.G.Players[0].Life
	effects.Resolve(e, &effects.Ctx{Source: spell, Controller: 0}, gate)
	want := before
	if wantPaid {
		want += 2
	}
	if got := e.G.Players[0].Life; got != want {
		t.Fatalf("ConditionPresent$ Card.Self+Teamwork EQ1: paid=%v life %d -> %d, want %d", wantPaid, before, got, want)
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
	e, cfg, reg := conspireEngine(t, "Go Nuts!", "Grizzly Bears")
	a := seedBattlefield(t, e, reg, "Goblin Piker")
	b := seedBattlefield(t, e, reg, "Grizzly Bears")
	spare := seedBattlefield(t, e, reg, "Grizzly Bears")
	moveSeededCard(t, e, 1, searchCorpusCard(t, reg, "Grizzly Bears"), state.ZBattlefield)
	hero := searchMoveByName(t, e, "Go Nuts!", state.ZHand)
	addMana(t, e, 0, "G")
	cast := castOptMode(t, e.Pending().Options, hero, "teamworked")
	submitChoices(t, e, cast.Index)
	d := teamworkAskOptions(t, e)
	if d.MinSum != 3 {
		t.Fatalf("Teamwork threshold = %d, want 3", d.MinSum)
	}
	// Go Nuts!'s decline is the empty answer, while the actual decision also
	// offers creatures. A legacy synthetic decline option here could combine
	// its threshold value with an under-threshold creature during repair.
	below := teamworkOption(t, d, a)
	if !d.AllowNone || len(d.Options) < 1 {
		t.Fatalf("precondition: Go Nuts! Teamwork decision must offer decline and creatures: %+v", d)
	}
	for _, option := range d.Options {
		if option.Kind != "teamwork" {
			t.Fatalf("precondition: decline must not be a power-bearing option: %+v", option)
		}
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player}); err != nil {
		t.Fatalf("empty Teamwork decline rejected: %v", err)
	}
	botAnswer := botpolicy.Decide(botpolicy.Board{}, d, rand.New(rand.NewPCG(1, 2)))
	if err := d.Validate(botAnswer); err != nil {
		t.Fatalf("bot's default Teamwork answer violates the shared threshold rule: %v (answer=%+v, fit=%v, max=%d options=%+v)", err, botAnswer, d.FitRequired(botAnswer.Choices), d.Max, d.Options)
	}
	// A partial client answer must be repaired from the SAME floor rule
	// Validate enforces. There is no power-bearing decline option for the
	// repair to combine with a creature (the former livelock).
	for _, intent := range []decision.Intent{
		{Seq: d.Seq, Player: d.Player, Choices: d.FitRequired([]int{below})},
		botpolicy.Clamp(d, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{below}}),
	} {
		if err := d.Validate(intent); err != nil {
			t.Fatalf("repair returned invalid Teamwork answer %+v: %v", intent, err)
		}
		if len(intent.Choices) > 0 && len(intent.Choices) < 2 {
			t.Fatalf("repair returned below-threshold subset: %+v", intent)
		}
	}
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
	teamworkConditionResult(t, e, hero, true)
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
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player}); err != nil {
		t.Fatalf("empty Teamwork decline rejected by shared decision rule: %v", err)
	}
	submitChoices(t, e)
	finishTeamworkAnnouncement(t, e)
	if e.G.Obj(hero).Zone != state.ZStack {
		t.Fatalf("declined test did not finish casting; zone=%s pending=%+v", e.G.Obj(hero).Zone, e.Pending())
	}
	if e.G.Obj(hero).TeamworkPaid || e.G.Obj(a).Tapped {
		t.Fatalf("declined Teamwork has paid=%v, creature tapped=%v", e.G.Obj(hero).TeamworkPaid, e.G.Obj(a).Tapped)
	}
	teamworkConditionResult(t, e, hero, false)
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
