package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestTriggerTargetSpecContextResolvesSourceXShapes(t *testing.T) {
	e := New(Config{Seed: 991, Names: []string{"a", "b"}, Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	source := e.G.AddObject(card(t, "Name:Resolver Source\nTypes:Creature\nSVar:X:Count$xPaid\nOracle:x\n"), 0)
	stack := e.G.AddObject(card(t, "Name:Trigger Wrapper\nTypes:Creature\nOracle:x\n"), 0)
	stack.X = 3

	resolve := func(body string) (int32, bool) {
		source.Face().SVars["X"] = body
		return e.targetSpecContext(source.ID, stack.ID, 0).Resolve("X")
	}
	if got, ok := resolve("Count$xPaid"); !ok || got != 3 {
		t.Fatalf("Count$xPaid X = (%d, %v), want (3, true)", got, ok)
	}
	if got, ok := resolve("4"); !ok || got != 4 {
		t.Fatalf("fixed SVar:X = (%d, %v), want (4, true)", got, ok)
	}
	if got, ok := resolve("Count$NoSuchHead"); ok {
		t.Fatalf("unresolvable SVar:X = (%d, true), want fail closed", got)
	}
}

func TestHammerheadTyrantTargetsAtMostTheCausingSpellManaValue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		spellName string
		manaValue int
		allowed   map[string]bool
	}{
		{spellName: "Four Mana Test Spell", manaValue: 4, allowed: map[string]bool{"One Drop": true, "Two Drop": true, "Four Drop": true}},
		{spellName: "Two Mana Test Spell", manaValue: 2, allowed: map[string]bool{"One Drop": true, "Two Drop": true}},
	} {
		t.Run(tc.spellName, func(t *testing.T) {
			hammerhead := choiceCorpusCard(t, "Hammerhead Tyrant")
			spellCost := "4"
			if tc.manaValue == 2 {
				spellCost = "2"
			}
			spell := card(t, "Name:"+tc.spellName+"\nManaCost:"+spellCost+"\nTypes:Sorcery\nOracle:x\n")
			one := card(t, "Name:One Drop\nManaCost:1\nTypes:Creature\nPT:1/1\nOracle:x\n")
			two := card(t, "Name:Two Drop\nManaCost:2\nTypes:Creature\nPT:2/2\nOracle:x\n")
			four := card(t, "Name:Four Drop\nManaCost:4\nTypes:Creature\nPT:4/4\nOracle:x\n")
			five := card(t, "Name:Five Drop\nManaCost:5\nTypes:Creature\nPT:5/5\nOracle:x\n")
			deck0 := append([]*cards.Card{hammerhead, spell}, mountainDeck(t, 38)...)
			deck1 := append([]*cards.Card{one, two, four, five}, mountainDeck(t, 36)...)
			e := New(seatZeroStart(Config{Seed: 992, Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck0, deck1}}))
			e.Advance()
			driveToStep(t, e, 1, 0, state.StepMain1)
			for _, name := range []string{"Hammerhead Tyrant", tc.spellName} {
				id := findByName(e, name, 0)
				if id == 0 {
					t.Fatalf("missing seat 0 card %q", name)
				}
				zone := state.ZHand
				if name == "Hammerhead Tyrant" {
					zone = state.ZBattlefield
				}
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: zone})
			}
			for _, name := range []string{"One Drop", "Two Drop", "Four Drop", "Five Drop"} {
				id := findByName(e, name, 1)
				if id == 0 {
					t.Fatalf("missing seat 1 permanent %q", name)
				}
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZBattlefield})
			}
			e.pending = nil
			for i := 0; i < tc.manaValue; i++ {
				e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
			}
			e.Advance()
			castFirst(t, e, "cast")
			d := e.Pending()
			if d == nil || d.Kind != decision.KTarget {
				t.Fatalf("Hammerhead trigger target ask = %+v; trigger must offer targets", d)
			}
			got := make(map[string]bool)
			for _, option := range d.Options {
				if option.Kind != "permanent" {
					t.Fatalf("unexpected non-permanent option: %+v", option)
				}
				obj := e.G.Obj(option.Obj)
				if obj == nil || obj.Face() == nil {
					t.Fatalf("target option has no live face: %+v", option)
				}
				got[obj.Face().Name] = true
			}
			if len(got) != len(tc.allowed) {
				t.Fatalf("mana value %d target options = %v, want exactly %v", tc.manaValue, got, tc.allowed)
			}
			for name := range tc.allowed {
				if !got[name] {
					t.Errorf("mana value %d did not offer %q; options=%v", tc.manaValue, name, got)
				}
			}
			if got["Five Drop"] {
				t.Errorf("mana value %d incorrectly offered cmc-5 permanent", tc.manaValue)
			}
		})
	}
}
