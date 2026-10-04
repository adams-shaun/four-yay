package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestReplEventGateCensus holds replEventGates complete: every replaceable
// event kind carries exactly one decision (a board gate, or the reason no
// ask-free resolution proposes it), every event an allowlisted API can
// propose is gated, and no gate is declared unreachable while an API reaches
// it. A new ReplEvent kind or a new allowlisted API fails here until it is
// decided.
func TestReplEventGateCensus(t *testing.T) {
	reached := cards.AskFreeAPIGates()
	for k := cards.ReplEvent(1); k < cards.ReplEventCount; k++ {
		g := replEventGates[k]
		switch {
		case g.ask == nil && g.unreachable == "":
			t.Errorf("ReplEvent %s has no gate decision", k)
		case g.ask != nil && g.unreachable != "":
			t.Errorf("ReplEvent %s is both gated and unreachable", k)
		case g.ask == nil && reached.Has(k):
			t.Errorf("ReplEvent %s is declared unreachable (%q) but an allowlisted API proposes it", k, g.unreachable)
		}
	}
	if reached.Has(0) {
		t.Error("an allowlisted API names the out-of-vocabulary event 0")
	}
	names := cards.AskFreeAPINames()
	for i, n := range names {
		if i > 0 && names[i-1] >= n {
			t.Fatalf("the allowlist is not sorted at %q (AskFreeAPIEvents binary searches it)", n)
		}
		if _, ok := cards.AskFreeAPIEvents(n); !ok {
			t.Errorf("AskFreeAPIEvents misses allowlisted %q", n)
		}
	}
}

// gateProbe is one newly gated event kind: a board of two competing
// replacements for the event, and an ask-free spell that proposes it.
type gateProbe struct {
	name   string
	board  []string // replacement sources moved onto seat 0's battlefield
	extra  []string // other seat-0 permanents (targets)
	hand   []string // seat-0 spells cast (held on the stack) before spell
	spell  string   // the ask-free spell cast from seat 0's hand
	act    string   // or: the extra permanent whose ability is activated
	target string   // the extra permanent its target ask picks ("" = first option)
	pre    func(t *testing.T, e *Engine, ids map[string]state.ObjID)
}

func gateRepl(name, line string, svars ...string) string {
	s := "Name:" + name + "\nTypes:Enchantment\nR:" + line + "\n"
	for _, v := range svars {
		s += "SVar:" + v + "\n"
	}
	return s + "Oracle:x\n"
}

// TestBoardGateCompetingReplacementsAsk: for each event kind the census
// newly gated, a resolution whose text is ask-free proposes the event while
// two replacements compete for it, so the CR 616.1 order choice must be
// posed (the predicate must not exempt the resolution: an exempted ask
// panics "the ask-free predicate missed an ask").
func TestBoardGateCompetingReplacementsAsk(t *testing.T) {
	t.Parallel()
	bear := "Name:Gate Bear\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	lossRepl := func(name, op string) string {
		return gateRepl(name, "Event$ LifeReduced | ActiveZones$ Battlefield | ValidPlayer$ You | ReplaceWith$ X | Description$ x",
			"X:DB$ ReplaceEffect | VarName$ Amount | VarValue$ Y", "Y:ReplaceCount$Amount/"+op)
	}
	probes := []gateProbe{{
		name: "Moved/Destroy",
		board: []string{
			gateRepl("Gate Exiler", "Event$ Moved | ActiveZones$ Battlefield | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Creature | ReplaceWith$ Ex | Description$ x",
				"Ex:DB$ ChangeZone | Origin$ Battlefield | Destination$ Exile | Defined$ ReplacedCard"),
			gateRepl("Gate Bouncer", "Event$ Moved | ActiveZones$ Battlefield | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Creature | ReplaceWith$ Bo | Description$ x",
				"Bo:DB$ ChangeZone | Origin$ Battlefield | Destination$ Hand | Defined$ ReplacedCard"),
		},
		extra:  []string{bear},
		spell:  "Name:Gate Destroy\nManaCost:0\nTypes:Sorcery\nA:SP$ Destroy | ValidTgts$ Creature | SpellDescription$ x\nOracle:x\n",
		target: "Gate Bear",
	}, {
		name: "Untap/UntapAll",
		board: []string{
			gateRepl("Gate Frost A", "Event$ Untap | ActiveZones$ Battlefield | ValidCard$ Creature | Layer$ CantHappen | Description$ x"),
			gateRepl("Gate Frost B", "Event$ Untap | ActiveZones$ Battlefield | ValidCard$ Creature | Layer$ CantHappen | Description$ x"),
		},
		extra: []string{bear},
		spell: "Name:Gate Untap\nManaCost:0\nTypes:Sorcery\nA:SP$ UntapAll | ValidCards$ Creature | SpellDescription$ x\nOracle:x\n",
		pre: func(t *testing.T, e *Engine, ids map[string]state.ObjID) {
			e.emit(events.Event{Kind: events.Tap, Obj: ids["Gate Bear"]})
		},
	}, {
		name:  "LifeReduced/LoseLife",
		board: []string{lossRepl("Gate Loss Twice", "Twice"), lossRepl("Gate Loss Plus", "Plus.1")},
		spell: "Name:Gate Lose\nManaCost:0\nTypes:Sorcery\nA:SP$ LoseLife | Defined$ You | LifeAmount$ 2 | SpellDescription$ x\nOracle:x\n",
	}, {
		name:  "LifeReduced/DealDamage",
		board: []string{lossRepl("Gate Loss Twice", "Twice"), lossRepl("Gate Loss Plus", "Plus.1")},
		spell: "Name:Gate Shock\nManaCost:0\nTypes:Sorcery\nA:SP$ DealDamage | Defined$ You | NumDmg$ 2 | SpellDescription$ x\nOracle:x\n",
	}, {
		name: "Counter/Counter",
		board: []string{
			gateRepl("Gate Shield A", "Event$ Counter | ActiveZones$ Battlefield | ValidCard$ Card | ValidSA$ Spell | Layer$ CantHappen | Description$ x"),
			gateRepl("Gate Shield B", "Event$ Counter | ActiveZones$ Battlefield | ValidCard$ Card | ValidSA$ Spell | Layer$ CantHappen | Description$ x"),
		},
		hand:  []string{"Name:Gate Held Bear\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"},
		spell: "Name:Gate Cancel\nManaCost:0\nTypes:Instant\nA:SP$ Counter | TargetType$ Spell | ValidTgts$ Card | SpellDescription$ x\nOracle:x\n",
	}, {
		name: "Draw/Draw",
		board: []string{
			gateRepl("Gate Draw Two", "Event$ Draw | ActiveZones$ Battlefield | ValidPlayer$ You | ReplaceWith$ D | Description$ x",
				"D:DB$ Draw | Defined$ You | NumCards$ 2"),
			gateRepl("Gate Draw Life", "Event$ Draw | ActiveZones$ Battlefield | ValidPlayer$ You | ReplaceWith$ G | Description$ x",
				"G:DB$ GainLife | Defined$ You | LifeAmount$ 3"),
		},
		spell: "Name:Gate Divination\nManaCost:0\nTypes:Sorcery\nA:SP$ Draw | Defined$ You | NumCards$ 1 | SpellDescription$ x\nOracle:x\n",
	}, {
		name:  "GainLife/DealDamage-lifelink",
		board: []string{lifeReplSrc("Gate Gain Twice", "Twice"), lifeReplSrc("Gate Gain Plus", "Plus.1")},
		extra: []string{"Name:Gate Linker\nManaCost:0\nTypes:Creature Cleric\nPT:1/1\nK:Lifelink\n" +
			"A:AB$ DealDamage | Cost$ 0 | Defined$ Opponent | NumDmg$ 1 | SpellDescription$ x\nOracle:x\n"},
		act: "Gate Linker",
	}}
	for _, p := range probes {
		p := p
		t.Run(p.name, func(t *testing.T) {
			var cs []*cards.Card
			byName := map[string]*cards.Card{}
			all := append(append(append([]string{}, p.board...), p.extra...), p.hand...)
			if p.spell != "" {
				all = append(all, p.spell)
			}
			for _, src := range all {
				c := card(t, src)
				cs = append(cs, c)
				byName[c.Faces[0].Name] = c
			}
			e, cfg := tokenReplGame(t, 4242, cs...)
			ids := map[string]state.ObjID{}
			for _, src := range append(append([]string{}, p.board...), p.extra...) {
				c := card(t, src)
				ids[c.Faces[0].Name] = moveSeededCard(t, e, 0, byName[c.Faces[0].Name], state.ZBattlefield)
			}
			for _, src := range p.hand {
				moveSeededCard(t, e, 0, byName[card(t, src).Faces[0].Name], state.ZHand)
			}
			var spellName string
			if p.spell != "" {
				spellName = card(t, p.spell).Faces[0].Name
				moveSeededCard(t, e, 0, byName[spellName], state.ZHand)
			}
			if p.pre != nil {
				p.pre(t, e, ids)
			}
			for _, src := range p.hand {
				addMana(t, e, 0, "")
				castSpellOption(t, e, card(t, src).Faces[0].Name)
			}
			addMana(t, e, 0, "")
			if p.act != "" {
				activateFirst(t, e, ids[p.act])
			} else {
				castSpellOption(t, e, spellName)
			}
			if asks := driveCountingReplacementAsks(t, e, ids[p.target]); asks == 0 {
				t.Fatal("no CR 616.1 replacement-order ask was posed")
			}
			replayCheck(t, e, cfg)
		})
	}
}

// driveCountingReplacementAsks resolves the stack, answering every ask with
// its first option (a target ask picks target when set), and counts the
// replacement-order asks.
func driveCountingReplacementAsks(t *testing.T, e *Engine, target state.ObjID) int {
	t.Helper()
	asks := 0
	for i := 0; i < 60; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while resolving")
		}
		pick := 0
		switch d.Kind {
		case decision.KPriority:
			if len(e.G.Stack) == 0 {
				return asks
			}
			passPriorityOnce(t, e)
			continue
		case decision.KReplacement:
			asks++
		case decision.KTarget:
			if target != 0 {
				if pick = optionForObj(d, target); pick < 0 {
					t.Fatalf("target ask does not offer %d: %+v", target, d.Options)
				}
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("the stack never emptied")
	return asks
}

// activateFirst submits the pending priority decision's first activate
// option of obj.
func activateFirst(t *testing.T, e *Engine, obj state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == obj {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no activate option for %d: %+v", obj, d.Options)
}
