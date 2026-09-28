package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestTargetingPlayerControls(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card := mustCorpusCard(t, reg, "Evangelize")
	var sa *cards.SA
	for _, candidate := range card.Faces[0].Abilities {
		if candidate.Kind == "SP" && candidate.Params["TargetingPlayer"] == "Player.Opponent" && strings.EqualFold(strings.TrimSpace(candidate.Params["TargetingPlayerControls"]), "True") {
			sa = candidate
			break
		}
	}
	if sa == nil {
		t.Fatal("Evangelize fixture must have an SP with TargetingPlayer$ Player.Opponent and TargetingPlayerControls$ True")
	}
	e, _ := combatTriggerBoard(t, reg, []string{"Evangelize"}, []string{"Name:Own Creature\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"}, nil, []string{"Name:Opponent Creature\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"})
	var source state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Card == card && o.Zone == state.ZBattlefield {
			source = o.ID
			break
		}
	}
	if source == 0 {
		t.Fatal("Evangelize source precondition: source is not on battlefield")
	}
	own := findBattlefield(t, e, 0, "Own Creature", 0)
	opponent := findBattlefield(t, e, 1, "Opponent Creature", 0)
	ownObj, oppObj := e.G.Obj(own), e.G.Obj(opponent)
	if ownObj == nil || oppObj == nil || ownObj.Zone != state.ZBattlefield || oppObj.Zone != state.ZBattlefield || ownObj.Controller == oppObj.Controller || ownObj.Controller != 0 || oppObj.Controller != 1 {
		t.Fatalf("candidate preconditions differ: own=%+v opponent=%+v", ownObj, oppObj)
	}
	e.askTarget(0, source, sa)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Player != 1 {
		t.Fatalf("target ask = %+v, want opponent seat 1", d)
	}
	if targetOptionContains(d.Options, own) || !targetOptionContains(d.Options, opponent) {
		t.Fatalf("TargetingPlayerControls offer = %+v, want only opponent's creature among these two", d.Options)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Obj == opponent {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatal("answering opponent's target absent from offered options")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit offered target: %v", err)
	}
	chosen := []state.Target{{Obj: opponent}}
	if got := e.legalTargets(chosen, sa, targetZones(sa), 0, source, source); len(got) != 1 || got[0].Obj != opponent {
		t.Fatalf("CR 608.2b recheck before control change = %v, want chosen opponent permanent", got)
	}
	// Control changes are event-folded; after the target leaves the answering
	// seat's control, the same recorded answer must fail the resolution recheck.
	e.emit(events.Event{Kind: events.ControlChange, Obj: opponent, Player: 0})
	if got := e.legalTargets(chosen, sa, targetZones(sa), 0, source, source); len(got) != 0 {
		t.Fatalf("CR 608.2b recheck after control change = %v, want no legal target", got)
	}

	// The neighboring ask with the flag removed remains caster-relative and
	// admits both creatures; ValidTgts$ is not reinterpreted from the chooser.
	without := *sa
	without.Params = make(map[string]string, len(sa.Params))
	for k, v := range sa.Params {
		if k != "TargetingPlayerControls" {
			without.Params[k] = v
		}
	}
	candidates := e.legalTargetCandidates(0, source, source, &without)
	seenOwn, seenOpp := false, false
	for _, c := range candidates {
		seenOwn = seenOwn || c.obj == own
		seenOpp = seenOpp || c.obj == opponent
	}
	if !seenOwn || !seenOpp {
		t.Fatalf("non-flagged control candidate census own=%t opponent=%t, want both", seenOwn, seenOpp)
	}
}

func TestTargetingPlayerControlsArenaChainedAsk(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Arena", "Magus of the Arena"} {
		t.Run(name, func(t *testing.T) {
			card := mustCorpusCard(t, reg, name)
			var root, child *cards.SA
			for _, candidate := range card.Faces[0].Abilities {
				if candidate.Kind == "AB" && candidate.Sub != nil {
					root, child = candidate, candidate.Sub
					break
				}
			}
			if root == nil || child == nil || root.Params["ValidTgts"] != "Creature.YouCtrl" || !strings.EqualFold(strings.TrimSpace(child.Params["TargetingPlayerControls"]), "True") {
				t.Fatalf("%s fixture must have root YouCtrl target and flagged chained DB ask: root=%+v child=%+v", name, root, child)
			}
			e, _ := combatTriggerBoard(t, reg, []string{name}, []string{"Name:Own Creature\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"}, nil, []string{"Name:Opponent Creature\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"})
			var source state.ObjID
			for i := range e.G.Objs {
				o := &e.G.Objs[i]
				if o.Owner == 0 && o.Card == card && o.Zone == state.ZBattlefield {
					source = o.ID
					break
				}
			}
			own := findBattlefield(t, e, 0, "Own Creature", 0)
			opponent := findBattlefield(t, e, 1, "Opponent Creature", 0)
			if source == 0 || e.G.Obj(own).Controller != 0 || e.G.Obj(opponent).Controller != 1 {
				t.Fatalf("fixture precondition source=%d own=%+v opponent=%+v", source, e.G.Obj(own), e.G.Obj(opponent))
			}
			rootCandidates := e.legalTargetCandidates(0, source, source, root)
			childCandidates := e.legalTargetCandidates(0, source, source, child)
			contains := func(cs []targetCandidate, id state.ObjID) bool {
				for _, candidate := range cs {
					if candidate.obj == id {
						return true
					}
				}
				return false
			}
			if !contains(rootCandidates, own) || contains(rootCandidates, opponent) {
				t.Fatalf("root AB$ YouCtrl candidates=%+v, want only caster's own creature", rootCandidates)
			}
			if contains(childCandidates, own) || !contains(childCandidates, opponent) {
				t.Fatalf("chained DB$ candidates=%+v, want only opponent-controlled creature", childCandidates)
			}
		})
	}
}
