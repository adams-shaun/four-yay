package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// kr4BlightCtx is a fresh resolution context for a corpus card's own SVars.
func kr4BlightCtx(t *testing.T, reg *cards.Registry, name string, src state.ObjID) func() *effects.Ctx {
	svars := searchCorpusCard(t, reg, name).Faces[0].SVars
	return func() *effects.Ctx { return &effects.Ctx{Source: src, Controller: 0, SVars: svars} }
}

// TestBlightShadowUchinAsksOnlyWithTwoCreaturesKernel pins the strict-supersets
// leaf on the real trigger body (Defined$ You / Num$ 1): with one controlled
// creature the counter is placed silently; with two a blight KChoose is posed
// and the answered creature gets the counter.
func TestBlightShadowUchinAsksOnlyWithTwoCreaturesKernel(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	blight := blightSVar(t, reg, "Shadow Urchin", "DBBlight")

	e, _ := blightEngine(t, reg, 2, "Shadow Urchin")
	urchin := blightMove(t, e, 0, "Shadow Urchin", state.ZBattlefield)
	kr4Resolve(e, kr4BlightCtx(t, reg, "Shadow Urchin", urchin), blight)
	if d := pendingBlightAsk(e); d != nil {
		t.Fatalf("ask posed %+v with a single controlled creature", d)
	}
	if got := blightCounters(t, e); got[urchin] != 1 {
		t.Fatalf("counters = %v, want 1 silent M1M1 on the urchin (%d)", got, urchin)
	}

	e2, _ := blightEngine(t, reg, 2, "Shadow Urchin")
	urchin2 := blightMove(t, e2, 0, "Shadow Urchin", state.ZBattlefield)
	bear2 := blightMove(t, e2, 0, "Grizzly Bears", state.ZBattlefield)
	kr4Resolve(e2, kr4BlightCtx(t, reg, "Shadow Urchin", urchin2), blight)
	d := blightAnswerBlight(t, e2)
	got := blightCounters(t, e2)
	want := d.Options[0].Obj
	if got[want] != 1 || len(got) != 1 || (want != urchin2 && want != bear2) {
		t.Fatalf("counters = %v, want 1 on the answered %d (urchin %d, bear %d)", got, want, urchin2, bear2)
	}
}

// TestBlightHighPerfectMorcantAsksEachOpponentKernel pins the multi-player
// walk: Defined$ Opponent asks seat 1 (two creatures), leaves seat 2 (no
// creatures) unharmed, and never touches the Elf's own controller.
func TestBlightHighPerfectMorcantAsksEachOpponentKernel(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := blightEngine(t, reg, 3, "High Perfect Morcant")
	morcant := blightMove(t, e, 0, "High Perfect Morcant", state.ZBattlefield)
	opp1a := blightMove(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	opp1b := blightMove(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	blightMove(t, e, 2, "Grizzly Bears", state.ZGraveyard)
	blightMove(t, e, 2, "Grizzly Bears", state.ZGraveyard)
	kr4Resolve(e, kr4BlightCtx(t, reg, "High Perfect Morcant", morcant),
		blightSVar(t, reg, "High Perfect Morcant", "TrigBlight"))
	d := e.Pending()
	if d == nil || d.ResumeKind != "blight" || d.Player != 1 {
		t.Fatalf("pending %+v, want seat 1's blight ask", d)
	}
	blightAnswerBlight(t, e)
	got := blightCounters(t, e)
	if len(got) != 1 || got[opp1a]+got[opp1b] != 1 {
		t.Fatalf("counters = %v, want exactly 1 on one of seat 1's bears (%d/%d)", got, opp1a, opp1b)
	}
	if got[morcant] != 0 {
		t.Fatalf("the Elf's controller was blighted: %v", got)
	}
	if d := pendingBlightAsk(e); d != nil {
		t.Fatalf("a second blight ask was left pending: %+v", d)
	}
}

// TestBlightChaosSpewerUnlessGateKernel pins the unless gate around the blight
// body: paying {2} spares the blight; declining runs it and blights 2 on one
// of the controlled creatures.
func TestBlightChaosSpewerUnlessGateKernel(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	trig := blightSVar(t, reg, "Chaos Spewer", "TrigBlight")

	e, _ := blightEngine(t, reg, 2, "Chaos Spewer")
	spewer := blightMove(t, e, 0, "Chaos Spewer", state.ZBattlefield)
	blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	addMana(t, e, 0, "CC")
	kr4Resolve(e, kr4BlightCtx(t, reg, "Chaos Spewer", spewer), trig)
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "unless_pay" {
		t.Fatalf("pending %+v, want the unless-pay offer", d)
	}
	before := lifeOf(t, e, 0)
	submitChoices(t, e, d.Options[0].Index) // pay
	if got := blightCounters(t, e); len(got) != 0 {
		t.Fatalf("counters = %v after paying, want none", got)
	}
	if lifeOf(t, e, 0) != before {
		t.Fatal("life changed on a mana payment")
	}
	if n := e.G.Players[0].Pool.Total(); n != 0 {
		t.Fatalf("pool = %d after paying {2}, want 0", n)
	}

	e2, _ := blightEngine(t, reg, 2, "Chaos Spewer")
	spewer2 := blightMove(t, e2, 0, "Chaos Spewer", state.ZBattlefield)
	bear2 := blightMove(t, e2, 0, "Grizzly Bears", state.ZBattlefield)
	addMana(t, e2, 0, "CC")
	kr4Resolve(e2, kr4BlightCtx(t, reg, "Chaos Spewer", spewer2), trig)
	d2 := e2.Pending()
	if d2 == nil || d2.ResumeKind != "unless_pay" || len(d2.Options) != 2 {
		t.Fatalf("pending %+v, want the pay/decline unless offer", d2)
	}
	submitChoices(t, e2, d2.Options[1].Index) // decline
	if d3 := pendingBlightAsk(e2); d3 != nil {
		submitChoices(t, e2, d3.Options[0].Index)
	}
	got := blightCounters(t, e2)
	if total := got[spewer2] + got[bear2]; total != 2 || len(got) != 1 {
		t.Fatalf("counters = %v, want 2 M1M1 on ONE of spewer %d / bear %d", got, spewer2, bear2)
	}
}
