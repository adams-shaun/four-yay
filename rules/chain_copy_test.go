package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// nextCopyElection passes priority and answers every intervening decision
// (targets via pickTarget, other asks with their first Min options) until a
// copy_optional election is pending, and returns it; nil once the stack is
// empty with no election posed.
func nextCopyElection(t *testing.T, e *Engine, pickTarget func(*decision.Decision) []int) *decision.Decision {
	t.Helper()
	for i := 0; i < 60 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision pending")
		}
		switch {
		case d.Kind == decision.KChoose && d.ResumeKind == "copy_optional":
			return d
		case d.Kind == decision.KPriority:
			if len(e.G.Stack) == 0 {
				return nil
			}
			passPriority(t, e)
		case d.Kind == decision.KTarget:
			submitChoices(t, e, pickTarget(d)...)
		default:
			var cs []int
			for j := 0; j < d.Min && j < len(d.Options); j++ {
				cs = append(cs, d.Options[j].Index)
			}
			submitChoices(t, e, cs...)
		}
	}
	t.Fatal("no copy election within 60 steps")
	return nil
}

// TestChainOfSmogMarksCopyOfCopyElection: Chain of Smog's "That player may
// copy this spell" (CopySpellAbility | Defined$ Parent | Controller$
// TargetedPlayer | Optional$ True) poses the election to the targeted
// player. The first election (the ORIGINAL spell) is an ordinary election;
// the election the copy then poses back to the caster copies a COPY, and the
// engine marks it CopyOfCopy -- the runtime fact the unattended bot declines
// so two bots cannot hand the chain back and forth forever (cardfuzz batch1
// lines 13/15/20: 20000 intents of Chain of Smog on empty hands).
func TestChainOfSmogMarksCopyOfCopyElection(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := edrBoard(t, reg, 83, map[string]state.Zone{"Chain of Smog": state.ZHand})
	addMana(t, e, 0, "B1")
	edrSeatZeroPriority(t, e)
	d := e.Pending()
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == ids["Chain of Smog"] {
			cast = o.Index
		}
	}
	if cast < 0 {
		t.Fatalf("Chain of Smog not castable: %+v", d.Options)
	}
	submitChoices(t, e, cast)
	target := func(p state.PlayerID) func(*decision.Decision) []int {
		return func(d *decision.Decision) []int {
			for _, o := range d.Options {
				if o.Kind == "player" && o.Player == p {
					return []int{o.Index}
				}
			}
			t.Fatalf("seat %d not offered as a target: %+v", p, d.Options)
			return nil
		}
	}
	first := nextCopyElection(t, e, target(1))
	if first == nil || first.Player != 1 || first.CopyOfCopy {
		t.Fatalf("first election = %+v, want seat 1's election on the original (CopyOfCopy false)", first)
	}
	submitChoices(t, e, first.Options[0].Index) // yes: seat 1 copies, targeting seat 0
	second := nextCopyElection(t, e, target(0))
	if second == nil || second.Player != 0 || !second.CopyOfCopy {
		t.Fatalf("second election = %+v, want seat 0's election on the copy (CopyOfCopy true)", second)
	}
	submitChoices(t, e, second.Options[1].Index) // no: the chain ends
	if again := nextCopyElection(t, e, target(1)); again != nil {
		t.Fatalf("a declined chain still posed an election: %+v", again)
	}
	replayCheck(t, e, cfg)
}

// TestBarroomBrawlCopyGoesToNextOpponent: Barroom Brawl's "Then that player
// [the opponent to your left] may copy this spell" (Controller$
// NextOpponentToYourLeft). The selector was unknown to the copy-controller
// resolver, which fell back to the resolving controller: the CASTER was
// offered its own copy, and each copy's copy, forever (cardfuzz batch1
// line 1). The election belongs to the next seat.
func TestBarroomBrawlCopyGoesToNextOpponent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := edrBoard(t, reg, 89, map[string]state.Zone{
		"Barroom Brawl": state.ZHand,
		"Grizzly Bears": state.ZBattlefield,
	})
	addMana(t, e, 0, "G1")
	edrSeatZeroPriority(t, e)
	d := e.Pending()
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == ids["Barroom Brawl"] {
			cast = o.Index
		}
	}
	if cast < 0 {
		t.Fatalf("Barroom Brawl not castable: %+v", d.Options)
	}
	submitChoices(t, e, cast)
	first := nextCopyElection(t, e, func(d *decision.Decision) []int {
		var cs []int
		for j := 0; j < d.Min && j < len(d.Options); j++ {
			cs = append(cs, d.Options[j].Index)
		}
		return cs
	})
	if first == nil {
		t.Fatal("Barroom Brawl posed no copy election")
	}
	if first.Player != 1 {
		t.Fatalf("copy election posed to seat %d, want the opponent to the caster's left (seat 1)", first.Player)
	}
	submitChoices(t, e, first.Options[1].Index)
	replayCheck(t, e, cfg)
}
