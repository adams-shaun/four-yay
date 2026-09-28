package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// UnlessResolveSubs$ gates the SubAbility$ walk on the unless-pay outcome
// (Forge's AbilityUtils.handleUnlessCost): absent/'Always' resolves the subs
// either way — every pre-existing UnlessCost$ behaviour — WhenPaid only when
// the cost was paid, WhenNotPaid only when it was not. Rhystic Syphon is the
// clean corpus carrier: "Unless target player pays {3}, that player loses 5
// life and you gain 5 life", whose DBGainLife rides WhenNotPaid.

// castRhysticSyphon casts the sorcery at seat 1 and leaves the unless-pay
// ask pending. The {3} the payer needs is put in their pool by the caller
// when the pay branch is wanted.
func castRhysticSyphon(t *testing.T) *Engine {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Rhystic Syphon"))
	addMana(t, e, 0, "BBBBB")
	addMana(t, e, 1, "CCC") // the payer's {3}, for the pay-branch case
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, handIDsByFace(e)["Rhystic Syphon"]))
	d := passUntilNonPriority(t, e, 8)
	if d == nil || d.Kind != decision.KTarget || d.Player != 0 {
		t.Fatalf("pending = %+v, want the target decision", d)
	}
	opp := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			opp = o.Index
		}
	}
	if opp < 0 {
		t.Fatalf("no seat-1 option in the target ask %+v", d.Options)
	}
	submitChoices(t, e, opp)
	if d := passUntilNonPriority(t, e, 8); d == nil || d.ResumeKind != "unless_pay" {
		t.Fatalf("pending = %+v, want the unless-pay ask", d)
	}
	return e
}

// TestUnlessResolveSubsWhenNotPaidDeclined drives the decline branch: the
// targeted player declines, the LoseLife body runs, and DBGainLife resolves
// with it — five life each way.
func TestUnlessResolveSubsWhenNotPaidDeclined(t *testing.T) {
	t.Parallel()
	e := castRhysticSyphon(t)
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	answerUnlessPay(t, e, false)
	if got := e.G.Players[1].Life; got != life1-5 {
		t.Fatalf("target life = %d, want %d (declined, body ran)", got, life1-5)
	}
	if got := e.G.Players[0].Life; got != life0+5 {
		t.Fatalf("caster life = %d, want %d (DBGainLife ran on WhenNotPaid)", got, life0+5)
	}
}

// TestUnlessResolveSubsWhenNotPaidPaid drives the pay branch: the targeted
// player pays {3}, the LoseLife body is prevented, and the WhenNotPaid
// DBGainLife chain is skipped with it — only the payer's pool moves.
func TestUnlessResolveSubsWhenNotPaidPaid(t *testing.T) {
	t.Parallel()
	e := castRhysticSyphon(t)
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	answerUnlessPay(t, e, true)
	if got := e.G.Players[1].Life; got != life1 {
		t.Fatalf("payer life = %d, want %d (paid, body prevented)", got, life1)
	}
	if got := e.G.Players[0].Life; got != life0 {
		t.Fatalf("caster life = %d, want %d (WhenNotPaid chain skipped)", got, life0)
	}
	if got := e.G.Players[1].Pool[state.MC]; got != 0 {
		t.Fatalf("payer colourless pool = %d, want 0 (the {3} was charged)", got)
	}
}
