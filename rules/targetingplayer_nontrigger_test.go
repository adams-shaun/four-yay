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

// Opponent specifications in non-triggered asks let the ability controller
// select which living opponent answers when more than one is available.
func TestTargetingPlayerOpponentSpellAndActivation(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, ability string
		kind          string
	}{
		{name: "Evangelize", ability: "SP", kind: "spell"},
		{name: "Echo Chamber", ability: "AB", kind: "activation"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			e, _ := combatTriggerBoard(t, reg,
				[]string{tc.name},
				[]string{"Name:Own Creature\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"},
				nil,
				[]string{"Name:Opponent Creature\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"})
			carrier := mustCorpusCard(t, reg, tc.name)
			var sa *cards.SA
			for _, candidate := range carrier.Faces[0].Abilities {
				if candidate.Kind == tc.ability && candidate.Params["TargetingPlayer"] == "Player.Opponent" && strings.EqualFold(strings.TrimSpace(candidate.Params["TargetingPlayerControls"]), "True") {
					sa = candidate
					break
				}
			}
			if sa == nil {
				t.Fatalf("%s compiled %s ability with TargetingPlayer$ Player.Opponent not found", tc.name, tc.ability)
			}
			var source state.ObjID
			for i := range e.G.Objs {
				o := &e.G.Objs[i]
				if o.Owner == 0 && o.Card == carrier && o.Zone == state.ZBattlefield {
					source = o.ID
					break
				}
			}
			if source == 0 {
				t.Fatalf("%s source is not on the battlefield", tc.name)
			}
			own := findBattlefield(t, e, 0, "Own Creature", 0)
			opponent := findBattlefield(t, e, 1, "Opponent Creature", 0)
			if e.G.Obj(own).Controller == e.G.Obj(opponent).Controller {
				t.Fatal("candidate fixture does not contain different controllers")
			}

			// This directly drives the shared cast/activation target-ask choke
			// point with the real compiled card ability; target legality remains
			// calculated from controller 0, while only the answering seat moves.
			e.askTarget(0, source, sa)
			d := e.Pending()
			if d == nil || d.Kind != decision.KTarget {
				t.Fatalf("pending ask = %+v, want target decision", d)
			}
			if d.Player != 1 {
				t.Fatalf("chooser = %d, want first living opponent seat 1", d.Player)
			}
			if targetOptionContains(d.Options, own) || !targetOptionContains(d.Options, opponent) {
				t.Fatalf("TargetingPlayerControls$ True candidates = %+v, want only the answering opponent's creature", d.Options)
			}
			bad := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{len(d.Options) + 10}}
			if err := d.Validate(bad); err == nil {
				t.Fatal("chooser can submit a target not present in the legal option set")
			}
		})
	}
}

// TestTargetingPlayerOpponentMultiSeatIsDeterministic verifies a controller
// selection when two opponents live and direct routing when only one remains.
func TestTargetingPlayerOpponentMultiSeatIsDeterministic(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	evangelize := mustCorpusCard(t, reg, "Evangelize")
	var sa *cards.SA
	for _, f := range evangelize.Faces {
		for _, cand := range f.Abilities {
			if cand.Kind == "SP" && strings.TrimSpace(cand.Params["TargetingPlayer"]) == "Player.Opponent" && strings.EqualFold(strings.TrimSpace(cand.Params["TargetingPlayerControls"]), "True") {
				sa = cand
			}
		}
	}
	if sa == nil {
		t.Fatal("Evangelize's compiled SP ability no longer carries TargetingPlayer$ Player.Opponent -- fixture premise broken")
	}
	for _, tc := range []struct {
		name       string
		lost       state.PlayerID // seat marked lost before the ask (0 = none)
		want       state.PlayerID // seat that must answer
		aliveOrder []state.PlayerID
	}{
		{name: "controller's first-option answer selects seat 1", want: 1, aliveOrder: []state.PlayerID{0, 1, 2}},
		{name: "dead first opponent fails over", lost: 1, want: 2, aliveOrder: []state.PlayerID{0, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := New(Config{Seed: 9311, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{
				mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40),
			}})
			got := e.G.AliveFrom(0)
			if len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
				t.Fatalf("turn order = %v, want [0 1 2]", got)
			}
			src := e.G.AddObject(evangelize, 0)
			e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: src.Zone, To: state.ZBattlefield})
			if src.Zone != state.ZBattlefield || src.Controller != 0 {
				t.Fatalf("Evangelize precondition: %+v (want on battlefield under seat 0)", src)
			}
			addCreature := func(name string, owner state.PlayerID) state.ObjID {
				o := e.G.AddObject(card(t, "Name:"+name+"\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), owner)
				e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
				return o.ID
			}
			own := addCreature("Alpha", 0)
			opp1 := addCreature("Beta", 1)
			opp2 := addCreature("Gamma", 2)
			for _, c := range []struct {
				id   state.ObjID
				want state.PlayerID
			}{{own, 0}, {opp1, 1}, {opp2, 2}} {
				o := e.G.Obj(c.id)
				if o == nil || o.Zone != state.ZBattlefield || o.Controller != c.want {
					t.Fatalf("seat %d creature precondition: %+v (want battlefield under seat %d)", c.want, o, c.want)
				}
			}
			if tc.lost > 0 {
				e.emit(events.Event{Kind: events.PlayerLost, Player: tc.lost, Text: "conceded"})
				if got := e.G.AliveFrom(0); len(got) != 2 || got[0] != 0 || got[1] != tc.want {
					t.Fatalf("post-loss turn order = %v, want %v", got, tc.aliveOrder)
				}
			}

			e.askTarget(0, src.ID, sa)
			d := e.Pending()
			if tc.lost == 0 {
				if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "opp_pick" || d.Player != 0 {
					t.Fatalf("pending ask = %+v, want controller's opponent selection", d)
				}
				if len(d.Options) != 2 || d.Options[0].Player == d.Options[1].Player {
					t.Fatalf("selection options = %+v, want two distinct opponents", d.Options)
				}
				if err := d.Validate(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{len(d.Options)}}); err == nil {
					t.Fatal("selection accepted an option outside its offered set")
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0}}); err != nil {
					t.Fatalf("submit opponent selection: %v", err)
				}
				d = e.Pending()
			}
			if d == nil || d.Kind != decision.KTarget || d.Player != tc.want {
				t.Fatalf("target ask = %+v, want seat %d", d, tc.want)
			}
			for _, c := range []struct {
				id    state.ObjID
				owner state.PlayerID
			}{{own, 0}, {opp1, 1}, {opp2, 2}} {
				onOffer := targetOptionContains(d.Options, c.id)
				lostSeat := tc.lost > 0 && c.owner == tc.lost
				if lostSeat && onOffer {
					t.Fatalf("lost seat %d's creature %d still offered", c.owner, c.id)
				}
				selectedSeat := tc.want
				if !lostSeat && c.owner == selectedSeat && !onOffer {
					t.Fatalf("selected opponent's legal candidate %d missing from options %+v", c.id, d.Options)
				}
				if !lostSeat && c.owner != selectedSeat && onOffer {
					t.Fatalf("non-answering seat %d's creature %d offered to chooser seat %d: %+v", c.owner, c.id, selectedSeat, d.Options)
				}
			}
			bad := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{len(d.Options) + 10}}
			if err := d.Validate(bad); err == nil {
				t.Fatal("chooser submitted an out-of-set target and the decision accepted it")
			}
		})
	}
}

func targetOptionContains(options []decision.Option, obj state.ObjID) bool {
	for _, o := range options {
		if o.Obj == obj {
			return true
		}
	}
	return false
}
