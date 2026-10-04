package rules

// Kernel-era restorations of the W3 converted-ask scenarios
// (resolve_convert_test.go, resolve_convert_park_test.go,
// resolve_convert_unless_test.go, resolve_convert_window_test.go,
// resolve_convert_zone_test.go). The originals ran each scenario on the
// legacy resume machinery and on the tape kernel and required identical
// logs; the legacy arm is gone, so each scenario now runs on the kernel
// alone and keeps what the old tests asserted about the game: the ask it is
// named for is posed, its answer is served from the tape, the outcome the
// legacy run used to be checked for (life, zones, event order) holds, and a
// log-only replay matches.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/resolve"
	"github.com/adams-shaun/gorge/state"
)

// kr8Fixture is newFixtureDeck generalised to n seats: seat 0's library
// holds the fixture cards (moved to hand), every other card is a Mountain.
func kr8Fixture(t *testing.T, seats int, seed uint64, srcs ...string) (*Engine, Config) {
	t.Helper()
	var fixtures []*cards.Card
	for _, s := range srcs {
		fixtures = append(fixtures, card(t, s))
	}
	names := make([]string, seats)
	decks := make([][]*cards.Card, seats)
	for i := range names {
		names[i] = fmt.Sprintf("p%d", i)
		decks[i] = mountainDeck(t, 40)
	}
	decks[0] = append(append([]*cards.Card(nil), fixtures...), mountainDeck(t, 40-len(fixtures))...)
	cfg := seatZeroStart(Config{Seed: seed, Names: names, Decks: decks, Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	for _, f := range fixtures {
		name := f.Faces[0].Name
		inHand := false
		for _, id := range e.G.Zone(state.ZHand, 0) {
			if o := e.G.Obj(id); o.Face().Name == name {
				inHand = true
			}
		}
		if !inHand {
			moveByName(t, e, 0, name, state.ZHand)
		}
	}
	return e, cfg
}

// kr8Run runs scenario on a fresh kernel fixture, requires at least
// minServed answers served from the tape with no legacy ask ending the run,
// and a matching log-only replay; it returns the engine and the counters.
func kr8Run(t *testing.T, seats int, seed uint64, minServed int64, scenario func(t *testing.T, e *Engine), srcs ...string) (*Engine, resolve.Stats) {
	t.Helper()
	before := resolve.ReadStats()
	e, cfg := kr8Fixture(t, seats, seed, srcs...)
	scenario(t, e)
	st := resolve.ReadStats().Sub(before)
	replayCheck(t, e, cfg)
	if st.Served < minServed || st.LegacySwitch != 0 || st.Aborts != 0 {
		t.Fatalf("the asks were not served from the tape: %+v", st)
	}
	return e, st
}

// kr8Drive casts name for mana after setup, then resolves everything,
// answering every non-priority decision with pick and counting the resume
// kinds posed.
func kr8Drive(t *testing.T, e *Engine, name, mana string, pick func(d *decision.Decision) []int) map[string]int {
	t.Helper()
	kinds := map[string]int{}
	addMana(t, e, 0, mana)
	submitChoices(t, e, castOptionFor(t, e, fixtureInHand(t, e, name)).Index)
	for i := 0; i < 400; i++ {
		d := e.Pending()
		if d == nil || e.G.Over {
			return kinds
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				return kinds
			}
			submitChoices(t, e, tapePassIndex(d))
			continue
		}
		kinds[d.ResumeKind]++
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: pick(d)}); err != nil {
			t.Fatalf("submit %s/%s: %v", d.Kind, d.ResumeKind, err)
		}
	}
	t.Fatal("stack never drained")
	return kinds
}

func kr8OnField(e *Engine, p state.PlayerID, name string) bool {
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if e.G.Obj(id).Face().Name == name {
			return true
		}
	}
	return false
}

// --- resolve_convert_test.go ---

// Scry, Surveil and RearrangeTopOfLibrary (one and every player, optional,
// Ponder's may-shuffle) pose their arrange asks and are served from the tape.
func TestKr8ConvertArrange(t *testing.T) {
	cases := []struct {
		name, src string
		served    int64
		life      int32
	}{
		{"Tape Scry", "A:SP$ Scry | ScryNum$ 3 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2", 1, 2},
		{"Tape Surveil", "A:SP$ Surveil | Amount$ 2 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2", 1, 2},
		{"Tape Each Scry", "A:SP$ Scry | ScryNum$ 2 | Defined$ Player", 2, 0},
		{"Tape Maybe Scry", "A:SP$ Scry | ScryNum$ 2 | Optional$ True | Defined$ Player", 2, 0},
		{"Tape Ponder", "A:SP$ RearrangeTopOfLibrary | Defined$ You | NumCards$ 3 | MayShuffle$ True | SubAbility$ DBDraw\nSVar:DBDraw:DB$ Draw | NumCards$ 1", 2, 0},
		{"Tape Rearrange", "A:SP$ RearrangeTopOfLibrary | Defined$ Player | NumCards$ 2", 2, 0},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "Name:" + tc.name + "\nManaCost:U\nTypes:Sorcery\n" + tc.src + "\nOracle:x\n"
			var life int32
			arranges := 0
			e, _ := kr8Run(t, 2, 12000+uint64(i), tc.served, func(t *testing.T, e *Engine) {
				life = e.G.Players[0].Life
				kr8Drive(t, e, tc.name, "U", func(d *decision.Decision) []int {
					if d.Kind == decision.KArrange {
						arranges++
					}
					return tapePick(d)
				})
			}, src)
			if arranges == 0 {
				t.Fatal("no arrange decision was posed")
			}
			if got := e.G.Players[0].Life - life; got != tc.life {
				t.Fatalf("life change %d, want %d (the rider after the arrange)", got, tc.life)
			}
		})
	}
}

// --- resolve_convert_park_test.go ---

// An Attached replacement's election (Sanctuary Blade's colour, Psychic
// Paper's name and creature type) is answered as the Attach happens: the
// Attach applies before the rest of the spell's chain and before the spell
// leaves the stack.
func TestKr8ParkAttached(t *testing.T) {
	for i, eq := range []string{"Tape Blade", "Tape Paper"} {
		t.Run(eq, func(t *testing.T) {
			name := "Tape Equip " + eq
			src := "Name:" + name + "\nManaCost:U\nTypes:Sorcery\n" +
				"A:SP$ Attach | Object$ Valid Equipment.YouCtrl | Defined$ Valid Creature.YouCtrl | SubAbility$ DBGain\n" +
				tapeGainSVar + "\nOracle:x\n"
			var spell, gear, bear state.ObjID
			e, _ := kr8Run(t, 2, 13900+uint64(i), 1, func(t *testing.T, e *Engine) {
				gear = moveByName(t, e, 0, eq, state.ZBattlefield)
				bear = moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield)
				spell = fixtureInHand(t, e, name)
				tapeCastAndResolve(t, e, name, "U")
			}, src, tapeBladeSrc, tapePaperSrc, ptResumeBearSrc)
			ask := -1
			for i, ev := range e.L.Events {
				if ev.Kind == events.DecisionAsk && ev.Text == string(decision.KChoose) {
					ask = i
				}
			}
			att := tapeEventIndex(e, 0, events.Attach, 0)
			left := tapeEventIndex(e, 0, events.MoveZone, spell)
			for left >= 0 && e.L.Events[left].From != state.ZStack {
				left = tapeEventIndex(e, left+1, events.MoveZone, spell)
			}
			if ask < 0 || att < 0 || left < 0 || att > left {
				t.Fatalf("Attach at %d, spell left the stack at %d (last choose ask %d): the election was not answered in place", att, left, ask)
			}
			if e.G.Obj(gear).AttachedTo != bear {
				t.Fatalf("%s is attached to %d, want the bear %d", eq, e.G.Obj(gear).AttachedTo, bear)
			}
		})
	}
}

// An entry an effect makes mid-chain (a mass return from the graveyard)
// asks its as-enters choices -- and its entry replacement body's ask -- in
// place: each permanent enters, answered, before the spell leaves the stack.
func TestKr8ParkMidChainEntry(t *testing.T) {
	name := "Tape Mass Return"
	src := "Name:" + name + "\nManaCost:U\nTypes:Sorcery\n" +
		"A:SP$ ChangeZoneAll | ChangeType$ Creature.YouOwn,Artifact.YouOwn | Origin$ Graveyard | Destination$ Battlefield | SubAbility$ DBGain\n" +
		tapeGainSVar + "\nOracle:x\n"
	var spell, bear, totem state.ObjID
	e, _ := kr8Run(t, 2, 13950, 3, func(t *testing.T, e *Engine) {
		moveByName(t, e, 0, "Mountain", state.ZBattlefield)
		moveByName(t, e, 0, "Mountain", state.ZBattlefield)
		bear = moveByName(t, e, 0, "Tape Prism Bear", state.ZGraveyard)
		totem = moveByName(t, e, 0, "Tape Totem", state.ZGraveyard)
		spell = fixtureInHand(t, e, name)
		tapeCastAndResolve(t, e, name, "U")
	}, src, tapePrismBearSrc, tapeTotemSrc)
	left := -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == spell && ev.From == state.ZStack {
			left = i
		}
	}
	for _, id := range []state.ObjID{bear, totem} {
		in := -1
		for i, ev := range e.L.Events {
			if ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZBattlefield {
				in = i
			}
		}
		if in < 0 || left < 0 || in > left {
			t.Fatalf("%s entered at %d, the spell left the stack at %d: the entry was not answered in place", e.Name(id), in, left)
		}
	}
}

// A CR 616.1 order competition posed mid-resolution (two rewriters of the
// same destroy) is answered in place: the chosen replacement applies.
func TestKr8ParkReplacementOrder(t *testing.T) {
	toHand := "Name:Tape Hand Rewriter\nTypes:Enchantment\n" +
		"R:Event$ Moved | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Creature | ReplaceWith$ Back | Description$ x\n" +
		"SVar:Back:DB$ ChangeZone | Origin$ Battlefield | Destination$ Hand | Defined$ ReplacedCard\nOracle:x\n"
	toExile := "Name:Tape Exile Rewriter\nTypes:Enchantment\n" +
		"R:Event$ Moved | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Creature | ReplaceWith$ Ex | Description$ x\n" +
		"SVar:Ex:DB$ ChangeZone | Origin$ Battlefield | Destination$ Exile | Defined$ ReplacedCard\nOracle:x\n"
	name := "Tape Order Kill"
	src := "Name:" + name + "\nManaCost:U\nTypes:Sorcery\n" +
		"A:SP$ ChangeZone | ValidTgts$ Creature | Origin$ Battlefield | Destination$ Graveyard | SubAbility$ DBGain\n" +
		tapeGainSVar + "\nOracle:x\n"
	tapeCheckpointAll = true
	defer func() { tapeCheckpointAll = false }()
	for i := 0; i < 2; i++ {
		var bear state.ObjID
		e, _ := kr8Run(t, 2, 14000+uint64(i), 1, func(t *testing.T, e *Engine) {
			moveByName(t, e, 0, "Tape Hand Rewriter", state.ZBattlefield)
			moveByName(t, e, 0, "Tape Exile Rewriter", state.ZBattlefield)
			bear = moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield)
			tapeCastAndResolve(t, e, name, "U")
		}, src, toHand, toExile, ptResumeBearSrc)
		if z := e.G.Obj(bear).Zone; z != state.ZHand && z != state.ZExile {
			t.Fatalf("the bear ended in %s, want a replacement's destination", z)
		}
	}
}

// --- resolve_convert_unless_test.go ---

func TestKr8ConvertUnlessPay(t *testing.T) {
	const gain = "\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2 | Defined$ You"
	cases := []struct {
		name, body string
		seats      int
		mana       string
		lands      int
		pay        bool
		served     int64
		window     int // unless mana window asks posed
		life       int32
	}{
		// The pool pays at once.
		{name: "Tape Tithe", seats: 2, mana: "BR", pay: true, served: 1, life: 5,
			body: "A:SP$ GainLife | LifeAmount$ 3 | UnlessCost$ 1 | UnlessPayer$ You | UnlessSwitched$ True | SubAbility$ DBGain" + gain},
		// Declined: the switched body does not run, the sub still does.
		{name: "Tape Tithe Declined", seats: 2, mana: "BR", served: 1, life: 2,
			body: "A:SP$ GainLife | LifeAmount$ 3 | UnlessCost$ 1 | UnlessPayer$ You | UnlessSwitched$ True | SubAbility$ DBGain" + gain},
		// A life payment settles in line too.
		{name: "Tape Blood Tithe", seats: 2, mana: "B", pay: true, served: 1, life: 1,
			body: "A:SP$ GainLife | LifeAmount$ 3 | UnlessCost$ PayLife<2> | UnlessPayer$ You | UnlessSwitched$ True"},
		// Every payer declines in turn: one election per seat.
		{name: "Tape Each Tithe", seats: 4, mana: "B", served: 4, life: -1,
			body: "A:SP$ LoseLife | LifeAmount$ 1 | Defined$ You | UnlessCost$ 1 | UnlessPayer$ Player"},
		// The mana window: an empty pool and two untapped Mountains -- one
		// activation, then Done once the pool covers {1}.
		{name: "Tape Window Tithe", seats: 2, mana: "B", lands: 2, pay: true, served: 3, window: 2, life: 5,
			body: "A:SP$ GainLife | LifeAmount$ 3 | UnlessCost$ 1 | UnlessPayer$ You | UnlessSwitched$ True | SubAbility$ DBGain" + gain},
		// A two-pip cost: two activations, then Done.
		{name: "Tape Wide Window", seats: 2, mana: "B", lands: 3, pay: true, served: 4, window: 3, life: 3,
			body: "A:SP$ GainLife | LifeAmount$ 3 | UnlessCost$ 2 | UnlessPayer$ You | UnlessSwitched$ True"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := tapeUnlessSorcery(tc.name, tc.body)
			var life int32
			window := 0
			pick := tapeUnlessPick(tc.pay)
			e, _ := kr8Run(t, tc.seats, 13100+uint64(i), tc.served, func(t *testing.T, e *Engine) {
				life = e.G.Players[0].Life
				tapeUnlessScenario(tc.name, tc.mana, tc.lands, func(d *decision.Decision) []int {
					if d.ResumeKind == unlessManaKind {
						window++
					}
					return pick(d)
				})(t, e)
			}, src)
			if window != tc.window {
				t.Fatalf("posed %d unless mana windows, want %d", window, tc.window)
			}
			if got := e.G.Players[0].Life - life; got != tc.life {
				t.Fatalf("life change %d, want %d", got, tc.life)
			}
		})
	}
}

// A choice-bearing unless cost: the served election's component step (the
// discard pick) is served from the tape too, and the paid discard happens.
func TestKr8ConvertUnlessComponents(t *testing.T) {
	src := tapeUnlessSorcery("Tape Discard Tithe",
		"A:SP$ GainLife | LifeAmount$ 3 | UnlessCost$ Discard<1/Card> | UnlessPayer$ You | UnlessSwitched$ True")
	var life int32
	var gy int
	e, st := kr8Run(t, 2, 13150, 2, func(t *testing.T, e *Engine) {
		moveByName(t, e, 0, "Mountain", state.ZHand)
		life = e.G.Players[0].Life
		gy = len(e.G.Zone(state.ZGraveyard, 0))
		tapeUnlessScenario("Tape Discard Tithe", "B", 0, tapeUnlessPick(true))(t, e)
	}, src)
	if st.Unservable != 0 {
		t.Fatalf("the component step was not served from the tape: %+v", st)
	}
	if got := e.G.Players[0].Life - life; got != 3 {
		t.Fatalf("life change %d, want 3 (the switched body runs once paid)", got)
	}
	// The sorcery and the discarded card.
	if got := len(e.G.Zone(state.ZGraveyard, 0)) - gy; got != 2 {
		t.Fatalf("graveyard grew by %d, want 2 (the spell and the discard)", got)
	}
}

// --- resolve_convert_window_test.go ---

func TestKr8ConvertTriggerCost(t *testing.T) {
	cases := []struct {
		name, cost, body string
		mana             string
		lands, hand      int
		pay              bool
		served           int64
		life             int32
	}{
		{name: "Tape Chalice", cost: "1", body: "GainLife | LifeAmount$ 3 | Defined$ You", mana: "BR", pay: true, served: 1, life: 3},
		{name: "Tape Chalice Declined", cost: "1", body: "GainLife | LifeAmount$ 3 | Defined$ You", mana: "BR", served: 1},
		{name: "Tape Vault Window", cost: "2", body: "GainLife | LifeAmount$ 3 | Defined$ You", mana: "B", lands: 3, pay: true, served: 4, life: 3},
		{name: "Tape Discard Fee", cost: "Discard<1/Card>", body: "GainLife | LifeAmount$ 3 | Defined$ You", mana: "B", hand: 2, pay: true, served: 2, life: 3},
		{name: "Tape Life Fee", cost: "PayLife<2>", body: "Draw | NumCards$ 1 | Defined$ You", mana: "B", pay: true, served: 1, life: -2},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := tapeWindowETB(tc.name, tc.cost, tc.body)
			asks := 0
			pick := tapeWindowPick(tc.pay)
			var life int32
			e, _ := kr8Run(t, 2, 13300+uint64(i), tc.served, func(t *testing.T, e *Engine) {
				for j := 0; j < tc.hand; j++ {
					moveByName(t, e, 0, "Mountain", state.ZHand)
				}
				life = e.G.Players[0].Life
				tapeUnlessScenario(tc.name, tc.mana, tc.lands, func(d *decision.Decision) []int {
					if d.Kind == decision.KChoose && strings.Contains(d.Prompt, tc.name) {
						asks++
					}
					return pick(d)
				})(t, e)
			}, src)
			if asks == 0 {
				t.Fatal("the trigger cost window was never posed")
			}
			if got := e.G.Players[0].Life - life; got != tc.life {
				t.Fatalf("life change %d, want %d", got, tc.life)
			}
		})
	}
}

func TestKr8ConvertUpkeepWindows(t *testing.T) {
	cases := []struct {
		name, src string
		lands     int
		pay       bool
		served    int64
		gone      bool
	}{
		{name: "Tape Glacier", src: "Name:Tape Glacier\nManaCost:B\nTypes:Enchantment\nK:Cumulative upkeep:1\nOracle:x\n", lands: 2, pay: true, served: 2},
		{name: "Tape Glacier Melts", src: "Name:Tape Glacier Melts\nManaCost:B\nTypes:Enchantment\nK:Cumulative upkeep:1\nOracle:x\n", served: 1, gone: true},
		{name: "Tape Echo Bear", src: "Name:Tape Echo Bear\nManaCost:B\nTypes:Creature Bear\nPT:2/2\nK:Echo:2\nOracle:x\n", lands: 3, pay: true, served: 3},
		{name: "Tape Echo Bear Sac", src: "Name:Tape Echo Bear Sac\nManaCost:B\nTypes:Creature Bear\nPT:2/2\nK:Echo:2\nOracle:x\n", served: 1, gone: true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			asks := 0
			pick := tapeWindowPick(tc.pay)
			e, _ := kr8Run(t, 2, 13400+uint64(i), tc.served, tapeWindowUpkeep(tc.name, tc.lands, func(d *decision.Decision) []int {
				if d.Kind == decision.KChoose && strings.Contains(d.Prompt, tc.name) {
					asks++
				}
				return pick(d)
			}), tc.src)
			if asks == 0 {
				t.Fatal("the upkeep election was never posed")
			}
			if on := kr8OnField(e, 0, tc.name); on == tc.gone {
				t.Fatalf("permanent on battlefield %v, want %v", on, !tc.gone)
			}
		})
	}
}

// An "as this enters" choice is served from the tape and recorded on the
// permanent.
func TestKr8ConvertETBChoice(t *testing.T) {
	srcs := []string{
		"Name:Tape Prism Bear\nManaCost:B\nTypes:Creature Bear\nPT:2/2\nK:ETBReplacement:Other:ChooseColor\nSVar:ChooseColor:DB$ ChooseColor\nOracle:x\n",
		"Name:Tape Totem\nManaCost:B\nTypes:Artifact\nK:ETBReplacement:Other:ChooseCT\nSVar:ChooseCT:DB$ ChooseType | Type$ Creature\nOracle:x\n",
	}
	for i, src := range srcs {
		name := strings.TrimPrefix(strings.SplitN(src, "\n", 2)[0], "Name:")
		t.Run(name, func(t *testing.T) {
			e, _ := kr8Run(t, 2, 13500+uint64(i), 1, tapeUnlessScenario(name, "B", 0, tapePick), src)
			if !kr8OnField(e, 0, name) {
				t.Fatalf("%s did not enter", name)
			}
		})
	}
}

// Ward's pay-or-countered election is served from the tape: paid, the pump
// resolves; declined, it is countered.
func TestKr8ConvertWard(t *testing.T) {
	const wardBear = "Name:Tape Ward Bear\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nK:Ward:1\nOracle:x\n"
	const pump = "Name:Tape Pump\nManaCost:B\nTypes:Instant\nA:SP$ Pump | ValidTgts$ Creature | NumAtt$ +1\nOracle:x\n"
	cases := []struct {
		name, mana string
		lands      int
		pay        bool
		served     int64
	}{
		{name: "pay", mana: "BR", pay: true, served: 1},
		{name: "decline", mana: "BR", served: 1},
		{name: "window", mana: "B", lands: 2, pay: true, served: 3},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := tapeUnlessPick(tc.pay)
			warded := 0
			pick := func(d *decision.Decision) []int {
				if strings.Contains(d.Prompt, "ward") {
					warded++
				}
				if d.ResumeKind == "ward_mana" {
					for _, o := range d.Options {
						if o.Kind == "activate" {
							return []int{o.Index}
						}
					}
					return []int{len(d.Options) - 1}
				}
				return base(d)
			}
			var bear state.ObjID
			e, _ := kr8Run(t, 2, 13600+uint64(i), tc.served, func(t *testing.T, e *Engine) {
				bear = moveByName(t, e, 0, "Tape Ward Bear", state.ZBattlefield)
				e.emit(events.Event{Kind: events.ControlChange, Obj: bear, Player: 1})
				tapeUnlessScenario("Tape Pump", tc.mana, tc.lands, pick)(t, e)
			}, wardBear, pump)
			if warded == 0 {
				t.Fatal("no ward election was posed")
			}
			if got, want := e.Power(bear), int32(2); tc.pay && got != want+1 || !tc.pay && got != want {
				t.Fatalf("bear power %d with ward paid=%v", got, tc.pay)
			}
		})
	}
}

// A resolving permanent spell's own entry replacement body asks after its
// move took it off the stack: served from the tape in line.
func TestKr8ConvertOwnEntryReplacement(t *testing.T) {
	srcs := []string{
		"Name:Tape Sower\nManaCost:B\nTypes:Creature Bear\nPT:2/2\nK:ETBReplacement:Other:DBChoose\nSVar:DBChoose:DB$ ChooseCard | Defined$ You | Choices$ Land.YouCtrl | ChoiceZone$ Battlefield | Mandatory$ True\nOracle:x\n",
		"Name:Tape Charm Gate\nManaCost:B\nTypes:Creature Bear\nPT:2/2\nK:ETBReplacement:Other:DBCharm\nSVar:DBCharm:DB$ Charm | Choices$ DBA,DBB\nSVar:DBA:DB$ GainLife | LifeAmount$ 1 | SpellDescription$ a\nSVar:DBB:DB$ GainLife | LifeAmount$ 2 | SpellDescription$ b\nOracle:x\n",
	}
	for i, src := range srcs {
		name := strings.TrimPrefix(strings.SplitN(src, "\n", 2)[0], "Name:")
		t.Run(name, func(t *testing.T) {
			e, _ := kr8Run(t, 2, 13700+uint64(i), 1, tapeUnlessScenario(name, "B", 2, tapePick), src)
			if !kr8OnField(e, 0, name) {
				t.Fatalf("%s did not enter", name)
			}
		})
	}
}

// Riptide Replicator's shape: an as-enters colour choice, then the entry
// replacement body's own ask; both served in place, and the resolution
// completes with the CR 117.3b priority grant to the active player.
func TestKr8ETBThenEntryReplacement(t *testing.T) {
	src := "Name:Tape Replicator\nManaCost:B\nTypes:Artifact\nK:ETBReplacement:Other:ChooseColor\nSVar:ChooseColor:DB$ ChooseColor\n" +
		"K:ETBReplacement:Other:DBChoose\nSVar:DBChoose:DB$ ChooseCard | Defined$ You | Choices$ Land.YouCtrl | ChoiceZone$ Battlefield | Mandatory$ True\nOracle:x\n"
	e, _ := kr8Run(t, 2, 13750, 2, tapeUnlessScenario("Tape Replicator", "B", 2, tapePick), src)
	last := -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.DecisionMade {
			last = i
		}
	}
	if g := tapeEventIndex(e, last, events.Priority, 0); g < 0 || e.L.Events[g].Player != e.G.Active || e.L.Events[g].Amount != 0 {
		t.Fatalf("no CR 117.3b grant to the active player after the resolution (last answer at %d)", last)
	}
	if !kr8OnField(e, 0, "Tape Replicator") {
		t.Fatal("the replicator did not enter")
	}
}

// A window's mana activation of a multi-ability source poses the ability
// wheel, and an Any source its colour choice: both served from the tape, and
// the paid trigger gains the life.
func TestKr8ConvertWindowManaChoices(t *testing.T) {
	const dual = "Name:Tape Dual\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ R | SpellDescription$ r\nA:AB$ Mana | Cost$ T | Produced$ G | SpellDescription$ g\nOracle:x\n"
	const prism = "Name:Tape Prism\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Any | SpellDescription$ any\nOracle:x\n"
	for i, land := range []string{"Tape Dual", "Tape Prism"} {
		t.Run(land, func(t *testing.T) {
			src := tapeWindowETB("Tape Fee Bear", "1", "GainLife | LifeAmount$ 3 | Defined$ You")
			var life int32
			e, _ := kr8Run(t, 2, 13800+uint64(i), 3, func(t *testing.T, e *Engine) {
				moveByName(t, e, 0, land, state.ZBattlefield)
				life = e.G.Players[0].Life
				tapeUnlessScenario("Tape Fee Bear", "B", 0, tapeWindowPick(true))(t, e)
			}, src, dual, prism)
			if got := e.G.Players[0].Life - life; got != 3 {
				t.Fatalf("life change %d, want 3 (the window paid)", got)
			}
		})
	}
}

// A copy's CR 707.10c new-target choice is served from the tape and its
// continuation resolves the copy in line: two pings land.
func TestKr8ConvertCopyTargets(t *testing.T) {
	for i, rider := range []string{"", " | UnlessCost$ PayLife<1> | UnlessPayer$ Player"} {
		t.Run(fmt.Sprintf("rider%d", i), func(t *testing.T) {
			bolt := "Name:Tape Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1" + rider + "\nOracle:x\n"
			const twin = "Name:Tape Twincast\nManaCost:U\nTypes:Instant\nA:SP$ CopySpellAbility | ValidTgts$ Card | TgtZone$ Stack | TargetType$ Spell | MayChooseTarget$ True\nOracle:x\n"
			copied := false
			kr8Run(t, 2, 13900+uint64(i), 1, func(t *testing.T, e *Engine) {
				addMana(t, e, 0, "RU")
				submitChoices(t, e, castOptionFor(t, e, fixtureInHand(t, e, "Tape Bolt")).Index)
				for i := 0; i < 10; i++ {
					d := e.Pending()
					if d == nil || d.Kind == decision.KPriority {
						break
					}
					submitChoices(t, e, tapePick(d)...)
				}
				submitChoices(t, e, castOptionFor(t, e, fixtureInHand(t, e, "Tape Twincast")).Index)
				tapeUnlessScenarioResolve(t, e, tapePick)
				for _, ev := range e.L.Events {
					if ev.Kind == events.StackCopy {
						copied = true
					}
				}
			}, bolt, twin)
			if !copied {
				t.Fatal("the twincast made no copy")
			}
		})
	}
}

// A Play whose cast commits synchronously (a free creature) is served from
// the tape and begun in line; one whose cast asks (a target) is answered in
// place: the cast's target is answered before the rest of the chain runs.
func TestKr8ConvertPlay(t *testing.T) {
	const bear = "Name:Tape Free Bear\nManaCost:4 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	const bolt = "Name:Tape Free Bolt\nManaCost:4 R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"
	const gain = "\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2 | Defined$ You"
	for i, tc := range []struct {
		target string
		asks   bool
	}{{"Tape Free Bear", false}, {"Tape Free Bolt", true}} {
		t.Run(tc.target, func(t *testing.T) {
			src := tapeUnlessSorcery("Tape Conduit", "A:SP$ Play | Valid$ Card.namedTape Free Bear,Card.namedTape Free Bolt | ValidZone$ Hand | WithoutManaCost$ True | Optional$ True | SubAbility$ DBGain"+gain)
			pick := func(d *decision.Decision) []int {
				if d.ResumeKind == "play" {
					for _, o := range d.Options {
						if strings.Contains(o.Label, tc.target) {
							return []int{o.Index}
						}
					}
				}
				return tapePick(d)
			}
			min := int64(1)
			if tc.asks {
				min = 2
			}
			var played state.ObjID
			e, _ := kr8Run(t, 2, 14000+uint64(i), min, func(t *testing.T, e *Engine) {
				played = fixtureInHand(t, e, tc.target)
				tapeUnlessScenario("Tape Conduit", "B", 0, pick)(t, e)
			}, src, bear, bolt)
			if !tc.asks {
				if e.G.Obj(played).Zone != state.ZBattlefield {
					t.Fatalf("the free bear is in %s, want the battlefield", e.G.Obj(played).Zone)
				}
				return
			}
			if e.G.Obj(played).Zone != state.ZGraveyard {
				t.Fatalf("the free bolt is in %s, want the graveyard (cast and resolved)", e.G.Obj(played).Zone)
			}
			target, life := -1, -1
			for j, ev := range e.L.Events {
				if ev.Kind == events.DecisionMade && strings.HasPrefix(ev.Text, "target") && target < 0 {
					target = j
				}
				if ev.Kind == events.LifeChange && ev.Amount == 2 {
					life = j
				}
			}
			if target < 0 || life < 0 || target > life {
				t.Fatalf("the cast's target answered at %d, the chain's life gain at %d: not answered in place", target, life)
			}
		})
	}
}

// Madness's cast-or-not choice at the MadnessCast ability's resolution is
// posed and served from the tape: declined, the card goes to the graveyard;
// cast, it reaches the battlefield.
func TestKr8ConvertMadness(t *testing.T) {
	const bear = "Name:Tape Mad Bear\nManaCost:3 B\nTypes:Creature Bear\nPT:2/2\nK:Madness:B\nOracle:x\n"
	rot := tapeUnlessSorcery("Tape Rot", "A:SP$ Discard | Defined$ You | NumCards$ 1 | Mode$ TgtChoose")
	for i, yes := range []bool{false, true} {
		t.Run(map[bool]string{true: "cast", false: "decline"}[yes], func(t *testing.T) {
			asked := false
			pick := func(d *decision.Decision) []int {
				switch {
				case d.Kind == decision.KReplacement:
					return []int{0}
				case d.ResumeKind == "madness":
					asked = true
					for _, o := range d.Options {
						if (o.Kind == "yes") == yes {
							return []int{o.Index}
						}
					}
				}
				for _, o := range d.Options {
					if strings.Contains(o.Label, "Tape Mad Bear") && d.Kind != decision.KTriggerOptional {
						return []int{o.Index}
					}
				}
				return tapePick(d)
			}
			// Served: the discard pick, the discard's madness replacement
			// election (answered in place) and the madness choice.
			var mad state.ObjID
			e, _ := kr8Run(t, 2, 14100+uint64(i), 3, func(t *testing.T, e *Engine) {
				mad = fixtureInHand(t, e, "Tape Mad Bear")
				tapeUnlessScenario("Tape Rot", "BB", 0, pick)(t, e)
			}, rot, bear)
			if !asked {
				t.Fatal("the madness cast choice was never posed")
			}
			want := state.ZGraveyard
			if yes {
				want = state.ZBattlefield
			}
			if z := e.G.Obj(mad).Zone; z != want {
				t.Fatalf("the madness card ended in %s, want %s", z, want)
			}
		})
	}
}

// --- resolve_convert_zone_test.go ---

func TestKr8ConvertZone(t *testing.T) {
	gain := "\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2"
	cases := []struct {
		name, src string
		seats     int
		setup     func(t *testing.T, e *Engine)
		served    int64
		kinds     []string
		extra     []string
	}{
		{name: "Tape Tutor", seats: 2, served: 1, kinds: []string{"search"},
			src: tapeZoneSorcery("Tape Tutor", "A:SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Land | ChangeNum$ 2 | SubAbility$ DBGain"+gain)},
		{name: "Tape Each Tutor", seats: 3, served: 3, kinds: []string{"search"},
			src: tapeZoneSorcery("Tape Each Tutor", "A:SP$ ChangeZone | DefinedPlayer$ Player | Origin$ Library | Destination$ Graveyard | ChangeType$ Land | ChangeNum$ 1")},
		{name: "Tape May Shuffle", seats: 2, served: 2, kinds: []string{"search", "search_mayshuffle"},
			src: tapeZoneSorcery("Tape May Shuffle", "A:SP$ ChangeZone | Origin$ Library | Destination$ Library | LibraryPosition$ 0 | ChangeType$ Land | ChangeNum$ 1 | ShuffleNonMandatory$ True | SubAbility$ DBGain"+gain)},
		{name: "Tape Each May Shuffle", seats: 3, served: 6, kinds: []string{"search", "search_mayshuffle"},
			src: tapeZoneSorcery("Tape Each May Shuffle", "A:SP$ ChangeZone | DefinedPlayer$ Player | Origin$ Library | Destination$ Hand | ChangeType$ Land | ChangeNum$ 1 | ShuffleNonMandatory$ True")},
		{name: "Tape Confirm Tutor", seats: 3, served: 3, kinds: []string{"search_confirm"},
			src: tapeZoneSorcery("Tape Confirm Tutor", "A:SP$ ChangeZone | DefinedPlayer$ Player | Origin$ Library | Destination$ Hand | ChangeType$ Land | ChangeNum$ 1 | Optional$ True | SubAbility$ DBGain"+gain)},
		{name: "Tape Limited Tutor", seats: 2, served: 1, kinds: []string{"search"},
			src: tapeZoneSorcery("Tape Limited Tutor", "A:SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Land | ChangeNum$ 1 | MaxRevealed$ 4 | Bogus$ 1")},
		{name: "Tape Top Fetch", seats: 2, served: 1, kinds: []string{"defined_library_optional"},
			src: tapeZoneSorcery("Tape Top Fetch", "A:SP$ ChangeZone | Defined$ TopOfLibrary | Origin$ Library | Destination$ Graveyard | Optional$ True | SubAbility$ DBGain"+gain)},
		{name: "Tape Put Back", seats: 2, served: 1, kinds: []string{"hand_move"}, setup: tapeZoneToHand(3),
			src: tapeZoneSorcery("Tape Put Back", "A:SP$ ChangeZone | Origin$ Hand | Destination$ Library | ChangeNum$ 2 | Mandatory$ True | SubAbility$ DBGain"+gain)},
		{name: "Tape Maybe Put Back", seats: 2, served: 1, kinds: []string{"hand_move_confirm"}, setup: tapeZoneToHand(3),
			src: tapeZoneSorcery("Tape Maybe Put Back", "A:SP$ ChangeZone | Origin$ Hand | Destination$ Library | ChangeNum$ 1 | Optional$ True")},
		{name: "Tape Each Hand", seats: 3, served: 3, kinds: []string{"hand_move"}, setup: tapeZoneToHand(3),
			src: tapeZoneSorcery("Tape Each Hand", "A:SP$ ChangeZone | DefinedPlayer$ Player | Origin$ Hand | Destination$ Graveyard | ChangeNum$ 1 | Mandatory$ True")},
		{name: "Tape Each Maybe Hand", seats: 3, served: 3, kinds: []string{"hand_move_confirm"}, setup: tapeZoneToHand(3),
			src: tapeZoneSorcery("Tape Each Maybe Hand", "A:SP$ ChangeZone | DefinedPlayer$ Player | Origin$ Hand | Destination$ Graveyard | ChangeNum$ 1 | Optional$ True")},
		{name: "Tape Hidden Pick", seats: 2, served: 1, kinds: []string{"hidden_pick"}, setup: tapeZoneToGraveyard(2),
			src: tapeZoneSorcery("Tape Hidden Pick", "A:SP$ ChangeZone | Origin$ Graveyard | Destination$ Hand | Hidden$ True | ChangeType$ Card | ChangeNum$ 2 | SubAbility$ DBGain"+gain)},
		{name: "Tape Each Hidden Pick", seats: 3, served: 3, kinds: []string{"hidden_pick"}, setup: tapeZoneToGraveyard(2),
			src: tapeZoneSorcery("Tape Each Hidden Pick", "A:SP$ ChangeZone | DefinedPlayer$ Player | Origin$ Graveyard | Destination$ Exile | Hidden$ True | ChangeType$ Card | ChangeNum$ 1")},
		{name: "Tape Maybe Hidden Pick", seats: 3, served: 3, kinds: []string{"hidden_pick_confirm"}, setup: tapeZoneToGraveyard(2),
			src: tapeZoneSorcery("Tape Maybe Hidden Pick", "A:SP$ ChangeZone | DefinedPlayer$ Player | Origin$ Graveyard | Destination$ Exile | Hidden$ True | ChangeType$ Card | ChangeNum$ 1 | Optional$ True")},
		{name: "Tape Edict", seats: 3, served: 3, kinds: []string{"sacrifice"}, setup: tapeZoneToBattlefield(3),
			src: tapeZoneSorcery("Tape Edict", "A:SP$ Sacrifice | Defined$ Player | SacValid$ Land | Amount$ 2 | ShowSacrificedCards$ True | SubAbility$ DBGain"+gain)},
		{name: "Tape Strict Edict", seats: 3, served: 3, kinds: []string{"sacrifice_optional"}, setup: tapeZoneToBattlefield(3),
			src: tapeZoneSorcery("Tape Strict Edict", "A:SP$ Sacrifice | Defined$ Player | SacValid$ Land | Amount$ 2 | Optional$ True | StrictAmount$ True | RememberSacrificed$ True")},
		{name: "Tape Self Sac", seats: 2, served: 1, kinds: []string{"sacrifice"}, setup: tapeZoneToBattlefield(1),
			src: tapeZoneSorcery("Tape Self Sac", "A:SP$ Sacrifice | ValidTgts$ Land | TgtPrompt$ x | Optional$ True | SubAbility$ DBGain"+gain)},
		{name: "Tape Imprint", seats: 2, served: 1, kinds: []string{"imprint"}, setup: tapeZoneToHand(3),
			src: tapeZoneSorcery("Tape Imprint", "A:SP$ ChangeZone | Origin$ Hand | Destination$ Exile | ChangeType$ Card | ChangeNum$ 1 | Imprint$ True | SubAbility$ DBGain"+gain)},
		{name: "Tape Vanish", seats: 2, served: 1, kinds: []string{"changezone_alternative"},
			setup: func(t *testing.T, e *Engine) { moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield) },
			extra: []string{ptResumeBearSrc},
			src:   tapeZoneSorcery("Tape Vanish", "A:SP$ ChangeZone | ValidTgts$ Creature | TgtPrompt$ x | AlternativeDecider$ TargetedOwner | Origin$ Battlefield | Destination$ Library | DestinationAlternative$ Library | LibraryPositionAlternative$ -1 | SubAbility$ DBGain"+gain)},
		{name: "Tape Dread", seats: 2, served: 1, kinds: []string{"manifest_dread"},
			src: tapeZoneSorcery("Tape Dread", "A:SP$ ManifestDread | SubAbility$ DBGain"+gain)},
		{name: "Tape Pinger", seats: 2, served: 1, kinds: []string{"tgts"},
			src: tapeZoneETB("Tape Pinger", "SVar:TrigBody:DB$ GainLife | LifeAmount$ 1 | SubAbility$ DBDmg\nSVar:DBDmg:DB$ DealDamage | ValidTgts$ Player | NumDmg$ 1 | SubAbility$ DBGain"+gain)},
		{name: "Tape Raiser", seats: 2, served: 1, kinds: []string{"choice"}, setup: tapeZoneToGraveyard(2),
			src: tapeZoneETB("Tape Raiser", "SVar:TrigBody:DB$ GainLife | LifeAmount$ 1 | SubAbility$ DBReturn\nSVar:DBReturn:DB$ ChangeZone | ValidTgts$ Card.YouOwn | TgtPrompt$ x | TargetMin$ 0 | TargetMax$ 2 | Origin$ Graveyard | Destination$ Hand | SubAbility$ DBGain"+gain)},
		{name: "Tape Reshuffle", seats: 2, served: 2, kinds: []string{"choice", "search_mayshuffle"}, setup: tapeZoneToGraveyard(2),
			src: tapeZoneETB("Tape Reshuffle", "SVar:TrigBody:DB$ GainLife | LifeAmount$ 1 | SubAbility$ DBShuffle\nSVar:DBShuffle:DB$ ChangeZone | ValidTgts$ Card.YouOwn | TgtPrompt$ x | TargetMin$ 1 | TargetMax$ 2 | Origin$ Graveyard | Destination$ Library | Shuffle$ True | ShuffleNonMandatory$ True | SubAbility$ DBGain"+gain)},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srcs := append([]string{tc.src}, tc.extra...)
			var kinds map[string]int
			kr8Run(t, tc.seats, 31000+uint64(i), tc.served, func(t *testing.T, e *Engine) {
				if tc.setup != nil {
					tc.setup(t, e)
				}
				kinds = kr8Drive(t, e, tc.name, "B", tapePick)
			}, srcs...)
			for _, k := range tc.kinds {
				if kinds[k] == 0 {
					t.Fatalf("%s posed no %q ask: %v", tc.name, k, kinds)
				}
			}
		})
	}
}

// Each yes/no election answered both ways: the election named by kind takes
// option `answer`, every other decision tapePick's; both answers are served
// and replay.
func TestKr8ConvertZoneElectionsBothWays(t *testing.T) {
	cases := []struct{ name, src, kind string }{
		{"Tape Sweep Confirm", tapeZoneSorcery("Tape Sweep Confirm", "A:SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Land | ChangeNum$ 1 | Optional$ True"), "search_confirm"},
		{"Tape Sweep Shuffle", tapeZoneSorcery("Tape Sweep Shuffle", "A:SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Land | ChangeNum$ 1 | ShuffleNonMandatory$ True"), "search_mayshuffle"},
		{"Tape Sweep Fetch", tapeZoneSorcery("Tape Sweep Fetch", "A:SP$ ChangeZone | Defined$ TopOfLibrary | Origin$ Library | Destination$ Graveyard | Optional$ True"), "defined_library_optional"},
		{"Tape Sweep Hand", tapeZoneSorcery("Tape Sweep Hand", "A:SP$ ChangeZone | Origin$ Hand | Destination$ Library | ChangeNum$ 1 | Optional$ True"), "hand_move_confirm"},
		{"Tape Sweep Hidden", tapeZoneSorcery("Tape Sweep Hidden", "A:SP$ ChangeZone | Origin$ Graveyard | Destination$ Exile | Hidden$ True | ChangeType$ Card | ChangeNum$ 1 | Optional$ True"), "hidden_pick_confirm"},
		{"Tape Sweep Strict", tapeZoneSorcery("Tape Sweep Strict", "A:SP$ Sacrifice | Defined$ You | SacValid$ Land | Amount$ 1 | Optional$ True | StrictAmount$ True"), "sacrifice_optional"},
	}
	for _, tc := range cases {
		for answer := 0; answer < 2; answer++ {
			t.Run(fmt.Sprintf("%s/%d", tc.name, answer), func(t *testing.T) {
				hit := 0
				kr8Run(t, 2, 32000, 1, func(t *testing.T, e *Engine) {
					tapeZoneToHand(3)(t, e)
					tapeZoneToGraveyard(2)(t, e)
					tapeZoneToBattlefield(2)(t, e)
					kr8Drive(t, e, tc.name, "B", func(d *decision.Decision) []int {
						if d.ResumeKind == tc.kind {
							hit++
							return []int{answer}
						}
						return tapePick(d)
					})
				}, tc.src)
				if hit == 0 {
					t.Fatalf("no %q election was posed", tc.kind)
				}
			})
		}
	}
}

// A roll published before a mid-resolution target ask is still published
// after it (the Numbing Jellyfish shape): the target mills the die's result.
func TestKr8RollSurvivesTargetAsk(t *testing.T) {
	src := tapeZoneETB("Tape Jelly", "SVar:TrigBody:DB$ RollDice | ResultSVar$ Result | SubAbility$ DBMill\nSVar:DBMill:DB$ Mill | ValidTgts$ Player | NumCards$ Result")
	var before int
	e, _ := kr8Run(t, 2, 33001, 1, func(t *testing.T, e *Engine) {
		before = len(e.G.Zone(state.ZGraveyard, 0)) + len(e.G.Zone(state.ZGraveyard, 1))
		tapeCastAndResolve(t, e, "Tape Jelly", "B")
	}, src)
	if milled := len(e.G.Zone(state.ZGraveyard, 0)) + len(e.G.Zone(state.ZGraveyard, 1)) - before; milled == 0 {
		t.Fatal("the rolled result milled nothing")
	}
}

// A second ask inside a RepeatEach iteration still binds the loop's subject
// (the Wave of Vitriol shape): an Optional$ search per sacrificed land,
// fetched by ImprintedController. Every confirmation is followed by its
// search, and every sacrificed land is replaced.
func TestKr8NestedAskKeepsRepeatSubject(t *testing.T) {
	src := tapeZoneSorcery("Tape Vitriol",
		"A:SP$ SacrificeAll | ValidCards$ Land | RememberSacrificed$ True | SubAbility$ DBRepeat\n"+
			"SVar:DBRepeat:DB$ RepeatEach | DefinedCards$ DirectRemembered.Land | UseImprinted$ True | RepeatSubAbility$ DBSearch | ClearRemembered$ True\n"+
			"SVar:DBSearch:DB$ ChangeZone | Origin$ Library | Destination$ Battlefield | ChangeType$ Land.Basic | Tapped$ True | DefinedPlayer$ ImprintedController | Chooser$ ImprintedController | NoShuffle$ True | Optional$ True")
	searches := 0
	e, _ := kr8Run(t, 2, 33101, 8, func(t *testing.T, e *Engine) {
		tapeZoneToBattlefield(2)(t, e)
		kr8Drive(t, e, "Tape Vitriol", "B", func(d *decision.Decision) []int {
			switch d.ResumeKind {
			case "search_confirm":
				return []int{0}
			case "search":
				searches++
				return []int{0}
			}
			return tapePick(d)
		})
	}, src)
	lands := len(e.G.Zone(state.ZBattlefield, 0)) + len(e.G.Zone(state.ZBattlefield, 1))
	if searches != 4 || lands != 4 {
		t.Fatalf("%d searches answered, %d lands back (want 4 and 4)", searches, lands)
	}
}

// A converted ask inside a ReplaceWith$ body is answered in place (the
// Caldera Hellion shape): Devour's sacrifice is asked from the entry
// replacement of a mass return, as the devourer enters, before the next
// creature moves.
func TestKr8ReplacementBodyAskInPlace(t *testing.T) {
	devourer := "Name:Tape Devourer\nManaCost:B\nTypes:Creature Hellion\nPT:1/1\nK:Devour:1\nOracle:x\n"
	raise := tapeZoneSorcery("Tape Mass Raise", "A:SP$ ChangeZoneAll | ChangeType$ Creature | Origin$ Graveyard | Destination$ Battlefield")
	var dev, bear state.ObjID
	e, _ := kr8Run(t, 2, 33201, 1, func(t *testing.T, e *Engine) {
		dev = moveByName(t, e, 0, "Tape Devourer", state.ZGraveyard)
		bear = moveByName(t, e, 0, "ParentLink Bear", state.ZGraveyard)
		moveByName(t, e, 0, "ParentLink Angel", state.ZBattlefield)
		tapeCastAndResolve(t, e, "Tape Mass Raise", "B")
	}, raise, devourer, ptResumeBearSrc, ptResumeAngelSrc)
	made, in := -1, -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.DecisionMade && made < 0 && i > 0 && strings.HasPrefix(ev.Text, "choose") {
			made = i
		}
		if ev.Kind == events.MoveZone && ev.Obj == bear && ev.To == state.ZBattlefield {
			in = i
		}
	}
	if made < 0 || in < 0 || made > in {
		t.Fatalf("%s's sacrifice answered at %d, the next creature entered at %d: not answered in place", e.Name(dev), made, in)
	}
}
