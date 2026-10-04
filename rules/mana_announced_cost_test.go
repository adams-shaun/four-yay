package rules

// The mana-activation path's announced-SubCounter, Forage and untapYType
// cost support. Each test drives the REAL corpus card (Haruspex, Rasputin,
// Thornvault Forager, Benthic Explorers) end to end and asserts its own
// precondition (the counters/fodder/tapped land really present, the compared
// values really differ), so it cannot pass vacuously.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// pickManaXOption submits the pending X ask's option carrying the given
// amount.
func pickManaXOption(t *testing.T, e *Engine, amount int) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected an X KChoose, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount == amount {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no X=%d option in %+v", amount, d.Options)
}

// TestParseUntapYTypeCost pins the parser arm: untapYType<N/Spec> is a real
// Cost.UntapPermanent part, never Cost.Unknown (which priced one phantom
// generic mana and refused the whole mana ability at the offer gate).
func TestParseUntapYTypeCost(t *testing.T) {
	t.Parallel()
	c := ParseCost("T untapYType<1/Land.OppCtrl/land>")
	if len(c.Unknown) != 0 {
		t.Fatalf("untapYType parsed into Cost.Unknown: %v", c.Unknown)
	}
	if len(c.UntapPermanent) != 1 {
		t.Fatalf("untapYType parsed %d UntapPermanent parts, want 1 (%+v)", len(c.UntapPermanent), c)
	}
	p := c.UntapPermanent[0]
	if p.N != 1 || p.Spec != "Land.OppCtrl" {
		t.Fatalf("untapYType part = N:%d Spec:%q, want N:1 Spec:Land.OppCtrl", p.N, p.Spec)
	}
	if !c.Tap {
		t.Fatal("untapYType cost lost its {T} tap component")
	}
	if got := ParseCost("untapYType<2/Creature.Blue+YouCtrl/blue creature>").UntapPermanent[0]; got.N != 2 || got.Spec != "Creature.Blue+YouCtrl" {
		t.Fatalf("multi-count parse = N:%d Spec:%q", got.N, got.Spec)
	}
	// A malformed count stays the reported one-generic fallback.
	if bad := ParseCost("untapYType<0/Land/land>"); len(bad.Unknown) != 1 || bad.Generic != 1 {
		t.Fatalf("malformed untapYType = Unknown:%v Generic:%d, want [untapYType] 1", bad.Unknown, bad.Generic)
	}
}

// TestManaAnnouncedSubCounterX is Haruspex's "Remove X +1/+1 counters: Add X
// mana of any one color" on the mana-activation path: the offer is present
// with counters on the source, the X ask offers the announced values
// (maximum first), X=2 removes two counters and adds two mana.
func TestManaAnnouncedSubCounterX(t *testing.T) {
	t.Parallel()
	e, cfg, ids := manaTapBoard(t, 7801, "Haruspex")
	h := ids["Haruspex"]
	e.emit(events.Event{Kind: events.CounterChange, Obj: h, Counter: "P1P1", Amount: 2})
	reprioritize(t, e)

	// Preconditions: the source is really on the battlefield carrying two
	// counters, untapped, and the pool is empty, so "two counters removed and
	// two mana added" is a real comparison.
	if o := e.G.Obj(h); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("fixture: Haruspex not on the battlefield (%+v)", e.G.Obj(h))
	}
	if got := e.G.Obj(h).Counter("P1P1"); got != 2 {
		t.Fatalf("fixture: Haruspex carries %d +1/+1 counters, want 2", got)
	}
	if e.G.Obj(h).Tapped {
		t.Fatal("fixture: Haruspex must start untapped to pay its {T}")
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("fixture: pool starts at %d, want 0", e.G.Players[0].Pool.Total())
	}
	if !hasActivateOption(e, h) {
		t.Fatalf("Haruspex mana ability not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, activateOption(t, e, h))

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("Haruspex X ask = %+v, want a KChoose", d)
	}
	if d.Options[0].Kind != "x" || d.Options[0].Amount != 2 {
		t.Fatalf("X ask first option = %+v, want the maximum X=2 first", d.Options[0])
	}
	pickManaXOption(t, e, 2)
	if e.Pending() != nil && e.Pending().Kind == decision.KChoose {
		answerManaChoose(t, e, "Add W")
	}
	if got := e.G.Obj(h).Counter("P1P1"); got != 0 {
		t.Errorf("after X=2: Haruspex carries %d +1/+1 counters, want 0", got)
	}
	if got := e.G.Players[0].Pool.Total(); got != 2 {
		t.Errorf("after X=2: pool total = %d, want 2", got)
	}
	replayCheck(t, e, cfg)
}

// TestManaAnnouncedSubCounterXMinFloor is Rasputin, the Oneiromancer's
// "T, Remove X dream counters: Add X {C}" with XMin$ 1: the X ask must not
// offer X=0.
func TestManaAnnouncedSubCounterXMinFloor(t *testing.T) {
	t.Parallel()
	e, _, ids := manaTapBoard(t, 7802, "Rasputin, the Oneiromancer")
	r := ids["Rasputin, the Oneiromancer"]
	e.emit(events.Event{Kind: events.CounterChange, Obj: r, Counter: "DREAM", Amount: 2})
	reprioritize(t, e)
	before := e.G.Obj(r).Counter("DREAM")
	if before < 1 {
		t.Fatalf("fixture: Rasputin carries %d dream counters, want at least 1", before)
	}
	if !hasActivateOption(e, r) {
		t.Fatalf("Rasputin mana ability not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, activateOption(t, e, r))
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 {
		t.Fatalf("Rasputin X ask = %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount < 1 {
			t.Fatalf("Rasputin offered X=%d below its XMin$ 1: %+v", o.Amount, d.Options)
		}
	}
}

// TestManaAnnouncedSubCounterXMinAutoPays covers the single-value XMin path:
// Rasputin with exactly one dream counter must remove it when X=1 is implied.
func TestManaAnnouncedSubCounterXMinAutoPays(t *testing.T) {
	t.Parallel()
	e, cfg, ids := manaTapBoard(t, 7806, "Rasputin, the Oneiromancer")
	r := ids["Rasputin, the Oneiromancer"]
	e.emit(events.Event{Kind: events.CounterChange, Obj: r, Counter: "DREAM", Amount: 1})
	reprioritize(t, e)
	if o := e.G.Obj(r); o == nil || o.Zone != state.ZBattlefield || o.Counter("DREAM") != 1 {
		t.Fatalf("fixture: Rasputin must be on the battlefield with exactly one dream counter: %+v", o)
	}
	if !hasActivateOption(e, r) {
		t.Fatalf("Rasputin not offered with its XMin payment available: %+v", e.Pending().Options)
	}
	submitChoices(t, e, activateOption(t, e, r))
	if got := e.G.Obj(r).Counter("DREAM"); got != 0 {
		t.Fatalf("after implied X=1 activation, Rasputin has %d dream counters, want 0", got)
	}
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("after implied X=1 activation, pool total = %d, want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestManaForageCost is Thornvault Forager's "{T}, Forage: Add two mana in
// any combination of colors": with a Food on the battlefield the ability is
// offered, the forage ask offers the sacrifice arm, and answering it
// sacrifices the Food and adds two mana.
func TestManaForageCost(t *testing.T) {
	t.Parallel()
	e, cfg, ids := manaTapBoard(t, 7803, "Thornvault Forager", "Krovod Haunch", "Bagel and Schmear")
	tf := ids["Thornvault Forager"]

	// Preconditions: two Foods controlled (so the sacrifice arm is a real
	// election, not the single-option auto-pick) and <3 graveyard cards, so
	// the sacrifice arm is the only payable one.
	foods := pay.CostCandidates(asPayer(e), 0, tf, state.ZBattlefield, "Food.YouCtrl", false, false)
	if len(foods) < 2 {
		t.Fatalf("fixture: %d Foods on the battlefield, want 2 (Krovod Haunch + Bagel and Schmear)", len(foods))
	}
	if len(e.G.Zone(state.ZGraveyard, 0)) >= 3 {
		t.Fatal("fixture: graveyard holds 3+ cards, the exile arm would also be legal")
	}
	if !hasActivateOption(e, tf) {
		t.Fatalf("Thornvault Forager not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, activateOption(t, e, tf))
	// Thornvault has two mana abilities ({T} and {T}, Forage): the activation
	// poses the ability pick as priority-style `mana` options, so elect the
	// one carrying Ability index 1 (the forage one).
	pick := e.Pending()
	if pick == nil || pick.Kind != decision.KChoose {
		t.Fatalf("ability pick = %+v", pick)
	}
	pickAbility := -1
	for _, o := range pick.Options {
		if o.Kind == "mana" && o.Ability == 1 {
			pickAbility = o.Index
			break
		}
	}
	if pickAbility < 0 {
		t.Fatalf("no Forage ability option: %+v", pick.Options)
	}
	submitChoices(t, e, pickAbility)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("forage ask = %+v, want a KChoose", d)
	}
	picked, food := -1, state.ObjID(0)
	for _, o := range d.Options {
		if o.Kind == "forage_food" {
			picked, food = o.Index, o.Obj
			break
		}
	}
	if picked < 0 {
		t.Fatalf("forage ask offered no forage_food arm: %+v", d.Options)
	}
	submitChoices(t, e, picked)
	// Resolve any remaining colour allocation with the first option(s).
	for i := 0; i < 8 && e.Pending() != nil && e.Pending().Kind == decision.KChoose; i++ {
		d = e.Pending()
		n := d.Min
		if n < 1 {
			n = 1
		}
		if d.Max > 0 && n > d.Max {
			n = d.Max
		}
		var cs []int
		for j := 0; j < n && j < len(d.Options); j++ {
			cs = append(cs, d.Options[j].Index)
		}
		submitChoices(t, e, cs...)
	}
	if o := e.G.Obj(food); o != nil && o.Zone == state.ZBattlefield {
		t.Errorf("the elected Food %d was not sacrificed: zone %v", food, o.Zone)
	}
	if got := e.G.Players[0].Pool.Total(); got != 2 {
		t.Errorf("after forage: pool total = %d, want 2", got)
	}
	replayCheck(t, e, cfg)
}

// TestManaUntapYTypeCost is Benthic Explorers' "{T}, Untap a tapped land an
// opponent controls: Add one mana of any type that land could produce". It
// asserts the offer, the exact elected target, and the full untap/reflection
// settle end to end.
func TestManaUntapYTypeCost(t *testing.T) {
	t.Parallel()
	e, cfg, ids := manaTapBoard(t, 7804, "Benthic Explorers")
	b := ids["Benthic Explorers"]
	// Put one of seat 1's Mountains on its battlefield and tap it.
	mnt := findByName(e, "Mountain", 1)
	if mnt == 0 {
		t.Fatal("fixture: seat 1 has no Mountain")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: mnt, From: state.ZLibrary, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.Tap, Obj: mnt})
	reprioritize(t, e)

	m := e.G.Obj(mnt)
	if m == nil || m.Zone != state.ZBattlefield {
		t.Fatalf("fixture: opponent Mountain not on the battlefield (%+v)", m)
	}
	if m.Controller != 1 {
		t.Fatalf("fixture: Mountain controller = %d, want seat 1", m.Controller)
	}
	if !m.Tapped {
		t.Fatal("fixture: opponent Mountain must be tapped (an untap cost needs a tapped permanent)")
	}
	if e.G.Obj(b).Tapped {
		t.Fatal("fixture: Benthic must start untapped")
	}
	// Precondition for a NON-VACUOUS reflection assertion: seat 0 controls no
	// untapped permanents of its own, so reflected mana can only come from the
	// elected opponent land. Without this, the old controller-untapped fallback
	// (effects/mana_reflected.go reflectedDefinedExtras "Untapped") could
	// satisfy the pool check from the wrong permanent.
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if id == b { // the source is necessarily untapped before activation, then taps as a cost.
			continue
		}
		if o := e.G.Obj(id); o != nil && !o.Tapped {
			t.Fatalf("fixture: seat 0 controls untapped permanent %d (%s); the reflection assertion would be ambiguous", id, o.Face().Name)
		}
	}
	// The landed machinery: the cost parses (no Cost.Unknown), the offer-gate
	// read finds the tapped opponent land, and the settle's candidate walk
	// offers exactly it.
	c := ParseCost("T untapYType<1/Land.OppCtrl/land>")
	if len(c.Unknown) != 0 {
		t.Fatalf("untapYType parsed into Cost.Unknown: %v", c.Unknown)
	}
	if !e.manaUntapPayable(0, b, c) {
		t.Fatalf("manaUntapPayable = false with a tapped opponent Mountain %d on the board", mnt)
	}
	cands := e.manaUntapCandidates(0, b, "Land.OppCtrl", nil)
	if len(cands) != 1 || cands[0] != mnt {
		t.Fatalf("manaUntapCandidates = %v, want exactly the tapped Mountain %d", cands, mnt)
	}
	if !hasActivateOption(e, b) {
		t.Fatalf("Benthic Explorers not offered with tapped opponent Mountain: %+v", e.Pending().Options)
	}
	submitChoices(t, e, activateOption(t, e, b))
	if o := e.G.Obj(mnt); o == nil || o.Tapped {
		t.Fatalf("elected opponent Mountain was not untapped as the cost: %+v", o)
	}
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("Benthic reflected mana pool total = %d, want 1 from elected Mountain", got)
	}
	// The opponent Mountain produces R, so binding the reflection to the
	// ELECTED permanent (Ctx.CostUntapped) adds a red unit; the pre-fix
	// fallback read the controller's own untapped permanents (none here) and
	// withheld the ability entirely.
	if got := e.G.Players[0].Pool[state.ManaIndex('R')]; got != 1 {
		t.Fatalf("Benthic reflected mana red slot = %d, want 1 (the elected Mountain's colour)", got)
	}
	replayCheck(t, e, cfg)
}

// TestManaUntapYTypeNotOfferedWithoutTappedOpponentLand asserts the negation:
// with no tapped opponent land the ability is refused at the offer gate. The
// positive control (the test above) proves the same card is otherwise
// offered, so this cannot pass because the card is unknown/unparsed.
func TestManaUntapYTypeNotOfferedWithoutTappedOpponentLand(t *testing.T) {
	t.Parallel()
	e, _, ids := manaTapBoard(t, 7805, "Benthic Explorers")
	b := ids["Benthic Explorers"]
	if hasActivateOption(e, b) {
		t.Fatalf("Benthic Explorers offered with no tapped opponent land: %+v", e.Pending().Options)
	}
	if abs := e.availableManaAbilities(0, b); len(abs) != 0 {
		t.Fatalf("availableManaAbilities = %d, want 0", len(abs))
	}
	// The ability parses to a real part (not Cost.Unknown): the refusal is the
	// offer gate, not a parser miss.
	if c := ParseCost("T untapYType<1/Land.OppCtrl/land>"); len(c.Unknown) != 0 {
		t.Fatalf("untapYType must not parse into Cost.Unknown: %v", c.Unknown)
	}
}
