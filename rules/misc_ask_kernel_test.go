package rules

// Restored from effects/cmc_budget_test.go, copy_test.go,
// counter_pick_test.go, defined_remembered_consumers_test.go and
// dice_test.go (W3 legacy removal): asks whose answers drive a game outcome,
// answered through the resolution kernel on a real engine.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestLivelyDirgeReturnBudgetCapsCumulativeKernel: the WithTotalCMC$ budget
// is cumulative -- graveyard [3,3,2], budget 5, ChangeNum 2: the ask's
// MaxSum refuses the 3+3 pair, accepts 3+2, and the answer moves exactly
// those two.
func TestLivelyDirgeReturnBudgetCapsCumulativeKernel(t *testing.T) {
	t.Parallel()
	e := kr0Engine(t, 2)
	src := kr0Src(t, e, 0, "Name:Dirge\nTypes:Sorcery\nOracle:x\n", state.ZBattlefield)
	var ids []state.ObjID
	for i, mv := range []int{3, 3, 2} {
		ids = append(ids, kr0Src(t, e, 0, "Name:Creature"+string(rune('A'+i))+"\nTypes:Creature\nManaCost:"+
			strings.Repeat("{W}", mv)+"\nPT:1/1\nOracle:x\n", state.ZGraveyard))
	}
	d := kr0Run(t, e, kr0SA(t, "DB$ ChangeZone | Origin$ Graveyard | Destination$ Battlefield | "+
		"WithTotalCMC$ 5 | ChangeNum$ 2 | Hidden$ True | ChangeType$ Creature.YouOwn"),
		func() *effects.Ctx { return &effects.Ctx{Source: src, Controller: 0} }, nil)
	if d == nil {
		t.Fatal("no decision posed")
	}
	if d.MaxSum != 5 {
		t.Fatalf("MaxSum = %d, want 5", d.MaxSum)
	}
	a, b, c := kr0Opt(t, d, ids[0]), kr0Opt(t, d, ids[1]), kr0Opt(t, d, ids[2])
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{a, b}}); err == nil {
		t.Fatal("a 3+3 pair exceeding the budget passed Validate")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{a, b}}); err == nil {
		t.Fatal("the engine accepted an over-budget 3+3 answer")
	}
	kr0Answer(t, e, a, c)
	kr0Zone(t, e, ids[0], state.ZBattlefield)
	kr0Zone(t, e, ids[2], state.ZBattlefield)
	kr0Zone(t, e, ids[1], state.ZGraveyard)
}

// kr0CopySA finds a corpus card's CopySpellAbility SA (abilities and
// trigger bodies, following sub-ability chains).
func kr0CopySA(t *testing.T, name string) *cards.SA {
	t.Helper()
	c := kr0Corpus(t, name)
	var found *cards.SA
	walk := func(sa *cards.SA) {
		for ; sa != nil && found == nil; sa = sa.Sub {
			if sa.API == "CopySpellAbility" {
				found = sa
			}
		}
	}
	for _, f := range c.Faces {
		for _, a := range f.Abilities {
			walk(a)
		}
		for _, tr := range f.Triggers {
			walk(tr.Effect)
		}
	}
	if found == nil {
		t.Fatalf("corpus card %q has no CopySpellAbility", name)
	}
	return found
}

// TestCopySpellAbilityUnswitchedShapePayingStopsTheCopiesKernel: Wandering
// Archaic's real copy ("they may pay {2}; if they don't, you may copy that
// spell"): the pay KModes goes to the payer with the no-copy label first and
// nothing copies before it; paying makes NO copy; declining poses the
// Optional$ may-copy election, whose "no" copies nothing and "yes" exactly
// one.
func TestCopySpellAbilityUnswitchedShapePayingStopsTheCopiesKernel(t *testing.T) {
	t.Parallel()
	s := kr0CopySA(t, "Wandering Archaic")
	if _, ok := s.Params["UnlessSwitched"]; ok {
		t.Fatalf("Wandering Archaic copy SA unexpectedly carries UnlessSwitched$: %+v", s.Params)
	}
	if !strings.EqualFold(strings.TrimSpace(s.Params["Optional"]), "True") {
		t.Fatalf("Wandering Archaic copy SA unexpectedly lacks Optional$ True: %+v", s.Params)
	}
	type leg struct {
		pay, elect string
		copies     int
	}
	for _, l := range []leg{{"pay", "", 0}, {"decline", "no", 0}, {"decline", "yes", 1}} {
		e := kr0Engine(t, 2)
		e.G.Players[0].Pool[state.MC] = 2
		spell := e.G.AddObject(card(t, "Name:Blast\nManaCost:R\nTypes:Sorcery\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"), 1)
		spell.Zone = state.ZStack
		spell.Targets = []state.Target{{Player: 0, IsPlayer: true}}
		id := spell.ID
		e.G.Stack = append(e.G.Stack, id)
		start := len(e.L.Events)
		d := kr0Run(t, e, s, func() *effects.Ctx {
			return &effects.Ctx{Source: id, Controller: 0, Remembered: []state.Target{{Obj: id}}}
		}, nil)
		if d == nil || d.Kind != decision.KModes || d.Player != 0 || d.Min != 1 || d.Max != 1 {
			t.Fatalf("%v: pay decision = %+v, want a 1/1 KModes posed to seat 0", l, d)
		}
		if len(d.Options) != 2 || d.Options[0].Label != "Pay 2 — no copy" || d.Options[1].Label != "Don't pay — make a copy" {
			t.Fatalf("%v: options = %+v, want the inverted pay/decline pair", l, d.Options)
		}
		if n := kr0Count(kr0Since(e, start), events.StackCopy); n != 0 {
			t.Fatalf("%v: %d copies before the pay decision was answered", l, n)
		}
		idx := 1
		if l.pay == "pay" {
			idx = 0
		}
		d = kr0Answer(t, e, idx)
		if l.elect != "" {
			if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "copy_optional" || d.Player != 0 ||
				len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
				t.Fatalf("%v: may-copy election = %+v, want seat 0's yes/no copy_optional", l, d)
			}
			if n := kr0Count(kr0Since(e, start), events.StackCopy); n != 0 {
				t.Fatalf("%v: %d copies before the election was answered", l, n)
			}
			d = kr0Answer(t, e, kr0Kind(t, d, l.elect))
		}
		if d != nil {
			t.Fatalf("%v: unexpected further decision %+v", l, d)
		}
		if n := kr0Count(kr0Since(e, start), events.StackCopy); n != l.copies {
			t.Fatalf("%v: %d copies, want %d", l, n, l.copies)
		}
	}
}

// TestPutCounterChoicesPickAsksAndPlacesTheAnswerKernel: a bare-Choices$
// PutCounter over three creatures asks the controller (KChoose 1/1, zone
// order, ResumeKind counter_pick), places nothing before the answer, puts
// the counter on the answered (second) creature only, and RememberCards$
// records exactly it.
func TestPutCounterChoicesPickAsksAndPlacesTheAnswerKernel(t *testing.T) {
	t.Parallel()
	e := kr0Engine(t, 2)
	src := kr0Src(t, e, 0, "Name:Asker\nTypes:Sorcery\nOracle:x\n", state.ZHand)
	var ids []state.ObjID
	for _, n := range []string{"Bear A", "Bear B", "Bear C"} {
		ids = append(ids, kr0Src(t, e, 0, "Name:"+n+"\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", state.ZBattlefield))
	}
	var last *effects.Ctx
	d := kr0Run(t, e, kr0SA(t, "DB$ PutCounter | Choices$ Creature.YouCtrl | ChoiceTitle$ Choose a creature you control | Chooser$ You | CounterType$ VOW | CounterNum$ 1 | RememberCards$ True"),
		func() *effects.Ctx { return &effects.Ctx{Source: src, Controller: 0} }, &last)
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 || d.Player != 0 || d.ResumeKind != "counter_pick" {
		t.Fatalf("decision = %+v, want seat 0's 1/1 counter_pick KChoose", d)
	}
	kr0OfferSet(t, d, ids...)
	for _, id := range ids {
		if e.G.Obj(id).Counter("VOW") != 0 {
			t.Fatal("a counter was placed before the choice was made")
		}
	}
	kr0Answer(t, e, kr0Opt(t, d, ids[1]))
	for i, id := range ids {
		want := int32(0)
		if i == 1 {
			want = 1
		}
		if got := e.G.Obj(id).Counter("VOW"); int32(got) != want {
			t.Fatalf("bear %d VOW = %d, want %d", i, got, want)
		}
	}
	if len(last.Remembered) != 1 || last.Remembered[0].Obj != ids[1] {
		t.Fatalf("Remembered = %+v, want exactly the answered bear", last.Remembered)
	}
}

// TestTapOrUntapTapperPlainRememberedExcludesCardControllerKernel: a
// TapOrUntap with Tapper$ Remembered over a remembered set [a seat-1 card,
// seat 2] taps the target on the "tap" answer with seat 2 -- not the
// remembered card's controller -- as the tapper, observed through each
// seat's "whenever you tap a creature" watcher.
func TestTapOrUntapTapperPlainRememberedExcludesCardControllerKernel(t *testing.T) {
	t.Parallel()
	e := kr0Engine(t, 3)
	remembered := kr0Src(t, e, 1, "Name:Remembered Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", state.ZBattlefield)
	target := kr0Src(t, e, 0, "Name:Tap Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", state.ZBattlefield)
	const watcher = "Types:Enchantment\nT:Mode$ Taps | ValidCard$ Creature | ValidPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigGain | TriggerDescription$ x\nSVar:TrigGain:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	w1 := kr0Src(t, e, 1, "Name:Watcher One\n"+watcher, state.ZBattlefield)
	w2 := kr0Src(t, e, 2, "Name:Watcher Two\n"+watcher, state.ZBattlefield)
	d := kr0Run(t, e, &cards.SA{Kind: "DB", API: "TapOrUntap", Params: map[string]string{"Defined": "Self", "Tapper": "Remembered"}},
		func() *effects.Ctx {
			return &effects.Ctx{Source: target, Controller: 0,
				Remembered: []state.Target{{Obj: remembered}, {Player: 2, IsPlayer: true}}}
		}, nil)
	if d == nil || d.ResumeKind != "taporuntap" {
		t.Fatalf("TapOrUntap posed %+v, want the tap/untap election", d)
	}
	kr0Answer(t, e, kr0Kind(t, d, "tap"))
	if !e.G.Obj(target).Tapped {
		t.Fatal("the \"tap\" answer did not tap the target")
	}
	fired := map[state.ObjID]bool{}
	for _, pt := range e.pendingTriggers {
		fired[pt.Source] = true
	}
	if !fired[w2] || fired[w1] {
		t.Fatalf("Taps watchers fired = %v, want only seat 2's (%d): the tapper is the remembered player 2, not the card's controller 1 (%d)", fired, w2, w1)
	}
}

// TestValiantEndeavorAsksThenPublishesChosenAndOtherKernel: Valiant
// Endeavor's real two-die roll poses a 1/1 KChoose over the rolled results
// (one option per die) with nothing resolved before the answer; picking the
// SECOND die publishes it as X (DBDestroy's Creature.powerGEX) and the other
// as Y (DBToken's TokenAmount$).
func TestValiantEndeavorAsksThenPublishesChosenAndOtherKernel(t *testing.T) {
	t.Parallel()
	ve := kr0Corpus(t, "Valiant Endeavor")
	var roll *cards.SA
	for _, a := range ve.Faces[0].Abilities {
		if a.API == "RollDice" {
			roll = a
		}
	}
	if roll == nil {
		t.Fatal("corpus fixture: Valiant Endeavor has no RollDice ability")
	}
	e := kr0EngineCorpus(t, 2)
	src := kr0Place(t, e, 0, ve, state.ZBattlefield)
	powers := []int{1, 2, 4, 6}
	cre := make([]state.ObjID, len(powers))
	for i, p := range powers {
		cre[i] = kr0Src(t, e, state.PlayerID(i%2), "Name:C"+strconv.Itoa(i)+"\nTypes:Creature\nPT:"+strconv.Itoa(p)+"/1\nOracle:x\n", state.ZBattlefield)
	}
	var last *effects.Ctx
	d := kr0Run(t, e, roll, func() *effects.Ctx {
		return &effects.Ctx{Controller: 0, Source: src, SVars: ve.Faces[0].SVars}
	}, &last)
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 || d.Min != 1 || d.Max != 1 || d.ResumeKind != "roll" {
		t.Fatalf("decision = %+v, want seat 0's 1/1 roll KChoose", d)
	}
	if len(d.Rolls) != 2 || len(d.Options) != 2 || d.Options[0].Kind != "roll" ||
		!strings.Contains(d.Options[0].Label, strconv.Itoa(int(d.Rolls[0]))) || !strings.Contains(d.Options[1].Label, strconv.Itoa(int(d.Rolls[1]))) {
		t.Fatalf("options = %+v rolls %v, want one roll option per die naming its result", d.Options, d.Rolls)
	}
	for _, id := range cre {
		if e.G.Obj(id).Zone != state.ZBattlefield {
			t.Fatal("a creature died before the answer")
		}
	}
	x, y := int(d.Rolls[1]), int(d.Rolls[0])
	kr0Answer(t, e, 1)
	if last.Roll.LastName != "X" || int(last.Roll.Last) != x {
		t.Fatalf("chosen publication = %s/%d, want X/%d", last.Roll.LastName, last.Roll.Last, x)
	}
	for i, p := range powers {
		dead := p >= x
		if z := e.G.Obj(cre[i]).Zone; dead == (z == state.ZBattlefield) {
			t.Fatalf("creature with power %d in %v; X = %d", p, z, x)
		}
	}
	knights := 0
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			if o := e.G.Obj(id); o != nil && o.IsToken && o.Face() != nil && strings.Contains(o.Face().Name, "Knight") {
				knights++
			}
		}
	}
	if knights != y {
		t.Fatalf("%d Knight tokens, want Y = %d (the other die)", knights, y)
	}
}
