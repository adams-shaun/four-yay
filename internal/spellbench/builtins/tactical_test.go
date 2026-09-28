package builtins

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// tacticalFixture is a two-player board the tactical tests build on: seat 0
// (us) and seat 1, with named cards resolved through the real corpus.
type tacticalFixture struct {
	t      *testing.T
	lookup CardLookup
	v      view.View
	next   state.ObjID
}

func newTacticalFixture(t *testing.T) *tacticalFixture {
	reg := testutil.CorpusRegistry(t)
	f := &tacticalFixture{t: t, lookup: NewRegistryLookup(reg), next: 100}
	f.v = view.View{Viewer: 0, Turn: 9, Step: "main1", Phase: "main1", Active: 0, Priority: 0,
		Players: []view.PlayerView{{ID: 0, Life: 20, HandSize: 0}, {ID: 1, Life: 20, HandSize: 3}}}
	return f
}

// card builds a CardView for name from its printed face.
func (f *tacticalFixture) card(name string, ctl state.PlayerID) view.CardView {
	c := f.lookup(name)
	if c == nil {
		f.t.Fatalf("no card %q in the corpus", name)
	}
	fc := c.Faces[0]
	f.next++
	cv := view.CardView{ID: f.next, Name: fc.Name, ManaCost: fc.ManaCost, Controller: ctl, Owner: ctl,
		Power: int32(fc.Power()), Toughness: int32(fc.Toughness()), Keywords: fc.Keywords}
	for i, ty := range fc.Types {
		if i > 0 {
			cv.Types += " "
		}
		cv.Types += ty
	}
	if sa := fc.SpellAbility(); sa != nil {
		cv.SpellAPI = sa.API
	}
	if fc.IsLand() {
		mp := fc.ManaProduction()
		cv.Produces = &mp
	}
	return cv
}

func (f *tacticalFixture) battlefield(p state.PlayerID, name string) view.CardView {
	cv := f.card(name, p)
	f.v.Players[p].Battlefield = append(f.v.Players[p].Battlefield, cv)
	return cv
}

func (f *tacticalFixture) hand(name string) view.CardView {
	cv := f.card(name, 0)
	f.v.Players[0].Hand = append(f.v.Players[0].Hand, cv)
	f.v.Players[0].HandSize++
	return cv
}

func (f *tacticalFixture) seat() *Seat {
	return NewTactical(AutoPay, 1, f.lookup, DefaultTacticalWeights())
}

func (f *tacticalFixture) chose(in decision.Intent, d decision.Decision) decision.Option {
	f.t.Helper()
	if len(in.Choices) != 1 {
		f.t.Fatalf("answer %+v: want one choice", in)
	}
	return d.Options[in.Choices[0]]
}

// TestTacticalProfileLabels pins the IR reading on the benchmark's cards.
func TestTacticalProfileLabels(t *testing.T) {
	f := newTacticalFixture(t)
	get := func(name string) *tProfile { return profileOf(f.lookup(name)) }
	if p := get("Counterspell"); !p.has(effCounter) || !p.reactive() {
		t.Fatalf("Counterspell: %+v", p)
	}
	if p := get("Lightning Bolt"); len(p.spell) == 0 || p.spell[0].class != effDamage || p.spell[0].amount != 3 || !p.spell[0].players {
		t.Fatalf("Lightning Bolt: %+v", p.spell)
	}
	if p := get("Journey to Nowhere"); !p.has(effRemoval) || p.reactive() {
		t.Fatalf("Journey to Nowhere: etb %+v", p.etb)
	}
	if p := get("Timberwatch Elf"); len(p.abilities) == 0 || p.abilities[0].effects[0].class != effPump || !p.abilities[0].tapCost {
		t.Fatalf("Timberwatch Elf: %+v", p.abilities)
	}
	if p := get("Humbling Elder"); !p.flash || !p.creature || p.etb[0].class != effDebuff {
		t.Fatalf("Humbling Elder: %+v", p)
	}
	p := get("Cleansing Wildfire")
	if p.spell[0].class != effRemoval || !p.spell[1].rider || p.spell[1].class != effRamp || p.spell[2].class != effDraw {
		t.Fatalf("Cleansing Wildfire: %+v", p.spell)
	}
	if p := get("Goblin Bushwhacker"); len(p.etb) == 0 || !p.etb[0].kicked || p.etb[0].class != effPumpAll {
		t.Fatalf("Goblin Bushwhacker: %+v", p.etb)
	}
	if p := get("Llanowar Elves"); !p.manaSource {
		t.Fatalf("Llanowar Elves is a mana source: %+v", p)
	}
}

// TestTacticalCounterNeedsAForeignSpell: a counterspell is never cast at an
// empty stack (it could only hit our own spell), and is cast at the
// opponent's spell.
func TestTacticalCounterNeedsAForeignSpell(t *testing.T) {
	f := newTacticalFixture(t)
	f.v.Active, f.v.Turn, f.v.Step, f.v.Phase = 1, 10, "main1", "main1"
	f.battlefield(0, "Island")
	f.battlefield(0, "Island")
	cs := f.hand("Counterspell")
	d := prio(opt(0, "pass", 0), opt(1, "cast", cs.ID))
	s := f.seat()
	if o := f.chose(decide(t, s, f.v, d), d); o.Kind != "pass" {
		t.Fatalf("empty stack: cast %q", o.Label)
	}
	threat := f.card("Myr Enforcer", 1)
	f.v.Stack = []view.StackView{{ID: threat.ID, Kind: "spell", Name: threat.Name, Controller: 1, Card: &threat}}
	if o := f.chose(decide(t, s, f.v, d), d); o.Kind != "cast" {
		t.Fatalf("foreign spell on the stack: answered %q", o.Label)
	}
}

// TestTacticalHoldsInstantRemoval: in our own main phase an instant burn is
// held (pass); at the opponent's end step it is cast.
func TestTacticalHoldsInstantRemoval(t *testing.T) {
	f := newTacticalFixture(t)
	f.battlefield(0, "Mountain")
	f.battlefield(1, "Burning-Tree Emissary")
	bolt := f.hand("Lightning Bolt")
	d := prio(opt(0, "pass", 0), opt(1, "cast", bolt.ID))
	s := f.seat()
	f.v.Step, f.v.Phase = "main2", "main2"
	if o := f.chose(decide(t, s, f.v, d), d); o.Kind != "pass" {
		t.Fatalf("own main phase: cast %q instead of holding", o.Label)
	}
	f.v.Active, f.v.Turn, f.v.Step, f.v.Phase = 1, 10, "end", "ending"
	if o := f.chose(decide(t, s, f.v, d), d); o.Kind != "cast" {
		t.Fatalf("opponent's end step: answered %q", o.Label)
	}
	// Without the timing group the bolt is simply cast.
	w := DefaultTacticalWeights()
	w.Timing = false
	s2 := NewTactical(AutoPay, 1, f.lookup, w)
	f.v.Active, f.v.Turn, f.v.Step, f.v.Phase = 0, 9, "main2", "main2"
	if o := f.chose(decide(t, s2, f.v, d), d); o.Kind != "cast" {
		t.Fatalf("timing off: answered %q", o.Label)
	}
}

// TestTacticalTargetDirection: help goes to our side, harm to theirs, and a
// harmful effect whose rider benefits the target's controller is pointed at
// our own indestructible permanent.
func TestTacticalTargetDirection(t *testing.T) {
	f := newTacticalFixture(t)
	elf := f.battlefield(0, "Timberwatch Elf")
	mine := f.battlefield(0, "Llanowar Elves")
	theirs := f.battlefield(1, "Llanowar Elves")
	bridge := f.battlefield(0, "Slagwoods Bridge")
	oppLand := f.battlefield(1, "Forest")
	s := f.seat()
	tgt := func(src state.ObjID, api string, dmg *int, opts ...decision.Option) decision.Decision {
		return decision.Decision{Seq: 3, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1, Source: src,
			TargetEffect: &decision.TargetEffect{API: api, Damage: func() *decision.DamageEffect {
				if dmg == nil {
					return nil
				}
				return &decision.DamageEffect{Amount: dmg}
			}()}, Options: opts}
	}
	obj := func(i int, id state.ObjID, ctl state.PlayerID) decision.Option {
		return decision.Option{Index: i, Kind: "card", Obj: id, Player: ctl, Controller: ctl}
	}
	// A pump: our creature, not theirs.
	d := tgt(elf.ID, "Pump", nil, obj(0, theirs.ID, 1), obj(1, mine.ID, 0))
	if o := f.chose(decide(t, s, f.v, d), d); o.Obj != mine.ID {
		t.Fatalf("pump aimed at %d, want our %d", o.Obj, mine.ID)
	}
	// Burn: their creature (a mana elf) over our own.
	bolt := f.hand("Lightning Bolt")
	three := 3
	d = tgt(bolt.ID, "DealDamage", &three, obj(0, mine.ID, 0), obj(1, theirs.ID, 1))
	if o := f.chose(decide(t, s, f.v, d), d); o.Obj != theirs.ID {
		t.Fatalf("burn aimed at %d, want their %d", o.Obj, theirs.ID)
	}
	// Cleansing Wildfire: our indestructible Bridge (we ramp and draw).
	wf := f.hand("Cleansing Wildfire")
	d = tgt(wf.ID, "Destroy", nil, obj(0, oppLand.ID, 1), obj(1, bridge.ID, 0))
	if o := f.chose(decide(t, s, f.v, d), d); o.Obj != bridge.ID {
		t.Fatalf("wildfire aimed at %d, want our bridge %d", o.Obj, bridge.ID)
	}
}

// TestTacticalBlocksToSurvive: facing lethal, the seat chump-blocks.
func TestTacticalBlocksToSurvive(t *testing.T) {
	f := newTacticalFixture(t)
	f.v.Active, f.v.Turn, f.v.Step, f.v.Phase = 1, 10, "declare-attackers", "combat"
	f.v.Players[0].Life = 3
	atk := f.battlefield(1, "Myr Enforcer")
	f.v.Players[1].Battlefield[0].Attacking = true
	blk := f.battlefield(0, "Llanowar Elves")
	d := decision.Decision{Seq: 4, Player: 0, Kind: decision.KBlockers, Min: 0, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "block", Obj: blk.ID, Attacker: atk.ID}}}
	in := decide(t, f.seat(), f.v, d)
	if !reflect.DeepEqual(in.Choices, []int{0}) {
		t.Fatalf("at 3 life facing a 4/4: blocks %v, want the chump", in.Choices)
	}
}

// TestTacticalDeterminism: the same seed and inputs give the same answers.
func TestTacticalDeterminism(t *testing.T) {
	f := newTacticalFixture(t)
	f.battlefield(0, "Mountain")
	f.battlefield(1, "Llanowar Elves")
	bolt := f.hand("Lightning Bolt")
	gob := f.hand("Goblin Bushwhacker")
	d := prio(opt(0, "pass", 0), opt(1, "cast", bolt.ID), opt(2, "cast", gob.ID))
	run := func() []decision.Intent {
		s := f.seat()
		var out []decision.Intent
		for i := 0; i < 20; i++ {
			out = append(out, decide(t, s, f.v, d), decide(t, s, f.v, attackersDecision()))
		}
		return out
	}
	if a, b := run(), run(); !reflect.DeepEqual(a, b) {
		t.Fatal("same seed, different answers")
	}
}

var _ = cards.NormalizeName
