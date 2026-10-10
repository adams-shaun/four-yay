package rules

// Task agent-20261009T085207Z-2ef0d22c: MKM Cryptex ("{T}, Collect evidence
// 3: Add one mana of any color. Put an unlock counter on Cryptex") is an
// AB$ Mana whose cost carries a CollectEvidence<3> part. The off-stack mana
// path had no settle for the part, so manaAbilityPayable refused the whole
// ability and the oracle comparison could not generate it. The path now
// elects the evidence (the cast flow's evidenceAsk shape: any combination of
// the payer's graveyard whose mana values total 3 or more, mana value
// descending, Min the greedy count) and exiles it with the
// CollectEvidenceAction marker.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// evidenceBoard seeds seat 0 with Cryptex on the battlefield and the named
// cards in its graveyard, at main-phase priority.
func evidenceBoard(t *testing.T, seed uint64, grave ...string) (*Engine, Config, state.ObjID, map[string]state.ObjID) {
	t.Helper()
	moves := map[string]state.Zone{"Cryptex": state.ZBattlefield}
	for _, g := range grave {
		moves[g] = state.ZGraveyard
	}
	e, cfg, ids := edrBoard(t, testutil.CorpusRegistry(t), seed, moves)
	cx := ids["Cryptex"]
	if o := e.G.Obj(cx); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Cryptex is not on the battlefield: %+v", o)
	}
	for _, g := range grave {
		if o := e.G.Obj(ids[g]); o == nil || o.Zone != state.ZGraveyard || o.Owner != 0 {
			t.Fatalf("precondition: %s is not in seat 0's graveyard: %+v", g, o)
		}
	}
	return e, cfg, cx, ids
}

// evidencePending asserts the pending decision is the evidence election and
// returns it.
func evidencePending(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "evidence" {
		t.Fatalf("expected the evidence election, got %+v", d)
	}
	return d
}

// evidenceChoices maps card ids to the option indexes of d.
func evidenceChoices(t *testing.T, d *decision.Decision, want ...state.ObjID) []int {
	t.Helper()
	var out []int
	for _, w := range want {
		found := false
		for _, o := range d.Options {
			if o.Obj == w {
				out = append(out, o.Index)
				found = true
			}
		}
		if !found {
			t.Fatalf("object %d is not an evidence option: %+v", w, d.Options)
		}
	}
	return out
}

func evidenceEventCount(e *Engine, kind events.Kind) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

// TestManaAbilityCollectEvidencePaysAndExiles is the positive half: Cryptex is
// offered, the seat elects 2+1 of the three graveyard cards, exactly those
// two leave for exile, the evidence action is recorded once, and the mana and
// unlock counter arrive.
func TestManaAbilityCollectEvidencePaysAndExiles(t *testing.T) {
	t.Parallel()
	e, cfg, cx, ids := evidenceBoard(t, 91, "Grizzly Bears", "Llanowar Elves", "Birds of Paradise")
	bears, elves, birds := ids["Grizzly Bears"], ids["Llanowar Elves"], ids["Birds of Paradise"]
	if cmc := e.G.Obj(bears).Face().Cmc(); cmc != 2 {
		t.Fatalf("precondition: Grizzly Bears mana value = %d, want 2", cmc)
	}
	if evidenceEventCount(e, events.CollectEvidenceAction) != 0 {
		t.Fatal("precondition: a collect-evidence action was already logged")
	}
	submitChoices(t, e, activateOption(t, e, cx))
	d := evidencePending(t, e)
	if d.Min != 2 || d.Max != 3 || len(d.Options) != 3 {
		t.Fatalf("evidence ask Min/Max/options = %d/%d/%d, want 2/3/3 (greedy 2+1 reaches 3)", d.Min, d.Max, len(d.Options))
	}
	if d.Options[0].Obj != bears {
		t.Fatalf("first option = %d, want Grizzly Bears %d (mana value descending)", d.Options[0].Obj, bears)
	}
	submitChoices(t, e, evidenceChoices(t, d, bears, elves)...)
	answerManaChoose(t, e, "Add G")
	for _, id := range []state.ObjID{bears, elves} {
		if z := e.G.Obj(id).Zone; z != state.ZExile {
			t.Fatalf("elected evidence %d is in %s, want exile", id, z)
		}
	}
	if z := e.G.Obj(birds).Zone; z != state.ZGraveyard {
		t.Fatalf("unelected card is in %s, want graveyard", z)
	}
	if got := evidenceEventCount(e, events.CollectEvidenceAction); got != 1 {
		t.Fatalf("CollectEvidenceAction events = %d, want 1", got)
	}
	if o := e.G.Obj(cx); !o.Tapped || o.Counter("UNLOCK") != 1 {
		t.Fatalf("Cryptex tapped=%v unlock=%d, want tapped with one unlock counter", o.Tapped, o.Counter("UNLOCK"))
	}
	if pool := e.G.Players[0].Pool; pool[state.MG] != 1 || pool.Total() != 1 {
		t.Fatalf("pool %v, want exactly {G}", pool)
	}
	replayCheck(t, e, cfg)
}

// TestManaAbilityCollectEvidenceRejectsShortSelection: a selection with the
// right card COUNT but too little mana value (1+1 < 3) is rejected, nothing is
// exiled, and the election is re-posed; a valid answer then pays.
func TestManaAbilityCollectEvidenceRejectsShortSelection(t *testing.T) {
	t.Parallel()
	e, cfg, cx, ids := evidenceBoard(t, 92, "Grizzly Bears", "Llanowar Elves", "Birds of Paradise")
	bears, elves, birds := ids["Grizzly Bears"], ids["Llanowar Elves"], ids["Birds of Paradise"]
	submitChoices(t, e, activateOption(t, e, cx))
	d := evidencePending(t, e)
	submitChoices(t, e, evidenceChoices(t, d, elves, birds)...)
	again := evidencePending(t, e)
	if again.Seq == d.Seq {
		t.Fatalf("the short selection was not re-posed as a fresh decision: %+v", again)
	}
	for _, id := range []state.ObjID{bears, elves, birds} {
		if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
			t.Fatalf("card %d left the graveyard (%s) on a rejected selection", id, z)
		}
	}
	if o := e.G.Obj(cx); o.Tapped {
		t.Fatal("Cryptex was tapped by a rejected evidence selection")
	}
	if got := evidenceEventCount(e, events.CollectEvidenceAction); got != 0 {
		t.Fatalf("CollectEvidenceAction events = %d after a rejected selection, want 0", got)
	}
	submitChoices(t, e, evidenceChoices(t, again, bears, birds)...)
	answerManaChoose(t, e, "Add U")
	if e.G.Obj(bears).Zone != state.ZExile || e.G.Obj(birds).Zone != state.ZExile || e.G.Obj(elves).Zone != state.ZGraveyard {
		t.Fatalf("zones after the valid selection: bears=%s birds=%s elves=%s",
			e.G.Obj(bears).Zone, e.G.Obj(birds).Zone, e.G.Obj(elves).Zone)
	}
	replayCheck(t, e, cfg)
}

// TestManaAbilityCollectEvidenceBotAnswerValidatesAndPays runs the
// deterministic bot's own answer through the validator on the board where the
// total-value rule binds (the first Min options by descending value reach 3,
// a naive two-card pick does not), then lets it finish the activation.
func TestManaAbilityCollectEvidenceBotAnswerValidatesAndPays(t *testing.T) {
	t.Parallel()
	e, _, cx, ids := evidenceBoard(t, 93, "Grizzly Bears", "Llanowar Elves", "Birds of Paradise")
	submitChoices(t, e, activateOption(t, e, cx))
	d := evidencePending(t, e)
	in := newTestBot(1).answer(e, d)
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot evidence answer fails Decision.Validate: %v (choices %v)", err, in.Choices)
	}
	submitChoices(t, e, in.Choices...)
	if e.Pending() != nil && e.Pending().Seq == d.Seq {
		t.Fatalf("bot answer left the identical evidence ask pending (livelock): %+v", e.Pending())
	}
	if z := e.G.Obj(ids["Grizzly Bears"]).Zone; z != state.ZExile {
		t.Fatalf("the bot's greedy answer kept Grizzly Bears in %s, want exiled evidence", z)
	}
	if got := evidenceEventCount(e, events.CollectEvidenceAction); got != 1 {
		t.Fatalf("CollectEvidenceAction events = %d, want 1", got)
	}
}

// TestManaAbilityCollectEvidenceNotOfferedWhenGraveyardTooLow: with the same
// board minus the 1-drops (total mana value 2 < 3) the ability is neither
// payable nor offered; the control board above proves the fixture otherwise
// offers it.
func TestManaAbilityCollectEvidenceNotOfferedWhenGraveyardTooLow(t *testing.T) {
	t.Parallel()
	ctl, _, ctlCx, _ := evidenceBoard(t, 94, "Grizzly Bears", "Llanowar Elves")
	if !offersActivate(ctl, ctlCx) {
		t.Fatalf("control: Cryptex with mana value 3 in the graveyard is not offered: %+v", ctl.Pending().Options)
	}
	e, _, cx, _ := evidenceBoard(t, 95, "Grizzly Bears")
	if offersActivate(e, cx) {
		t.Fatalf("Cryptex offered with only mana value 2 in the graveyard: %+v", e.Pending().Options)
	}
	for _, ma := range e.G.Obj(cx).Face().ManaAbilities() {
		if e.manaAbilityPayable(0, cx, ma) {
			t.Fatalf("mana ability %q is payable with mana value 2 in the graveyard", ma.Params["Cost"])
		}
	}
}

func offersActivate(e *Engine, id state.ObjID) bool {
	d := e.Pending()
	if d == nil {
		return false
	}
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == id {
			return true
		}
	}
	return false
}

// TestManaAbilityCollectEvidenceNonInteractiveTakesTheGreedyPrefix: a caller
// that cannot ask (resolveManaAbility, the attack-cost tap window's shape)
// settles the election deterministically with the mana-value-descending
// prefix instead of posing the ask or paying nothing.
func TestManaAbilityCollectEvidenceNonInteractiveTakesTheGreedyPrefix(t *testing.T) {
	t.Parallel()
	e, cfg, cx, ids := evidenceBoard(t, 96, "Grizzly Bears", "Llanowar Elves", "Birds of Paradise")
	mas := e.G.Obj(cx).Face().ManaAbilities()
	if len(mas) != 1 || len(e.parseCost(mas[0].Params["Cost"]).Evidence) != 1 {
		t.Fatalf("precondition: Cryptex mana abilities = %d, want one carrying CollectEvidence", len(mas))
	}
	e.resolveManaAbility(0, cx, mas[0], false)
	answerManaChoose(t, e, "Add R")
	bears := ids["Grizzly Bears"]
	if z := e.G.Obj(bears).Zone; z != state.ZExile {
		t.Fatalf("Grizzly Bears (the highest mana value) is in %s, want exiled as evidence", z)
	}
	exiled := 0
	for _, g := range []string{"Grizzly Bears", "Llanowar Elves", "Birds of Paradise"} {
		if e.G.Obj(ids[g]).Zone == state.ZExile {
			exiled++
		}
	}
	if exiled != 2 {
		t.Fatalf("exiled evidence cards = %d, want the greedy two (2+1 reaches 3)", exiled)
	}
	if got := evidenceEventCount(e, events.CollectEvidenceAction); got != 1 {
		t.Fatalf("CollectEvidenceAction events = %d, want 1", got)
	}
	replayCheck(t, e, cfg)
}
