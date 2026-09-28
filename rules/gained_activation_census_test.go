package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestGainedActivationCountsOnTheRecipientsCensus pins the per-source
// activation census (state.Object.ActivatedThisTurn) for a has-all-abilities-of
// activation on the real corpus pair from fuzz batch4 line 5: Myr Welder
// imprints a Knowledge Vault from the graveyard and so gains its
// "{0}: Sacrifice CARDNAME ..." ability. The GainedAbilityPush mint never
// folded onto the census AbilityPush keeps, so the bot's repeatability
// budget (botpolicy A5, which reads exactly this count) never closed on a
// free gained ability and the seat re-activated it forever with the stack
// growing (a livelock). The gained activation must count like any other
// activation of the recipient.
func TestGainedActivationCountsOnTheRecipientsCensus(t *testing.T) {
	t.Parallel()
	welder := tokenReplCorpusCard(t, "Myr Welder")
	vault := tokenReplCorpusCard(t, "Knowledge Vault")
	e, cfg := tokenReplGame(t, 9131, welder, vault)
	welderID := moveSeededCard(t, e, 0, welder, state.ZBattlefield)
	vaultID := moveSeededCard(t, e, 0, vault, state.ZGraveyard)
	e.priorityRound()

	// Turn 3: the Welder is no longer summoning-sick, so its own {T}
	// imprint is offered.
	gainsDriveToStep(t, e, 3, 0, state.StepMain1)
	d := e.Pending()
	imprint := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == welderID && o.GainedSource == 0 {
			imprint = o.Index
		}
	}
	if imprint < 0 {
		t.Fatalf("Myr Welder offers no imprint ability: %+v", d.Options)
	}
	submitChoices(t, e, imprint)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		idx := -1
		for _, o := range d.Options {
			if o.Obj == vaultID {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("imprint does not offer the graveyard Knowledge Vault: %+v", d.Options)
		}
		submitChoices(t, e, idx)
	}
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(vaultID); o == nil || o.Zone != state.ZExile || o.ExiledWith != welderID {
		t.Fatalf("Knowledge Vault = %+v, want exiled with the Welder", o)
	}
	before := e.G.Obj(welderID).ActivatedThisTurn
	if before != 1 {
		t.Fatalf("Welder census after its own imprint = %d, want 1", before)
	}

	// The gained "{0}: Sacrifice" is offered on the Welder, anchored on the
	// exiled Vault. Activate it twice (it is free and needs no tap): every
	// activation must land on the Welder's census.
	for i := int32(1); i <= 2; i++ {
		d = e.Pending()
		if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
			t.Fatalf("pending = %+v, want seat 0 priority", d)
		}
		gained := -1
		for _, o := range d.Options {
			if o.Kind == "ability" && o.Obj == welderID && o.GainedSource == vaultID {
				gained = o.Index
			}
		}
		if gained < 0 {
			t.Fatalf("Myr Welder offers no gained Knowledge Vault ability: %+v", d.Options)
		}
		submitChoices(t, e, gained)
		if got := e.G.Obj(welderID).ActivatedThisTurn; got != before+i {
			t.Fatalf("Welder census after gained activation %d = %d, want %d", i, got, before+i)
		}
	}
	if n := countKind(e.L.Events, events.GainedAbilityPush, welderID); n != 2 {
		t.Fatalf("GainedAbilityPush count = %d, want 2", n)
	}
	replayCheck(t, e, cfg)
}
