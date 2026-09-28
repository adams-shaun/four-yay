package rules

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// tapEveryManaSource taps each untapped mana source seat p controls, so
// PotentialMana (the colour-aware affordability bound the Spree mode gate
// prices against) sees only the mana the test explicitly floats. Without it a
// fresh deck's untapped lands make every mode "affordable" and the cost tests
// would measure nothing.
func tapEveryManaSource(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o == nil || o.Tapped || o.Face() == nil {
			continue
		}
		if len(o.Face().ManaAbilities()) == 0 {
			continue
		}
		e.emit(events.Event{Kind: events.Tap, Obj: id})
	}
}

// spreeModeCosts proves the compiled face really carries what the test claims
// to exercise: a keyword head (Spree or Tiered) and a per-mode ModeCost$ the
// engine can parse. A test whose whole point is reading ModeCost$ must fail
// loudly if the parameter stops reaching the SVar, not pass because both sides
// quietly read zero.
func spreeModeCosts(t *testing.T, e *Engine, id state.ObjID, want map[string]Cost) {
	spreeModeCostsKeyword(t, e, id, "Spree", want)
}

// spreeModeCostsKeyword is spreeModeCosts with an explicit keyword head, so a
// Tiered card can assert K:Tiered rather than K:Spree while sharing the
// ModeCost$ precondition checks.
func spreeModeCostsKeyword(t *testing.T, e *Engine, id state.ObjID, keyword string, want map[string]Cost) {
	t.Helper()
	f := e.G.Obj(id).Face()
	found := false
	for _, k := range f.Keywords {
		if strings.EqualFold(cards.KeywordHead(k), keyword) {
			found = true
		}
	}
	if !found {
		t.Fatalf("fixture %s carries no K:%s keyword", f.Name, keyword)
	}
	for name, wantCost := range want {
		got, present, ok := modeCost(f, name)
		if !present {
			t.Fatalf("fixture %s mode %s carries no ModeCost$", f.Name, name)
		}
		if !ok {
			t.Fatalf("fixture %s mode %s ModeCost$ is not parseable", f.Name, name)
		}
		if !reflect.DeepEqual(got, wantCost) {
			t.Fatalf("fixture %s mode %s ModeCost$ = %+v, want %+v", f.Name, name, got, wantCost)
		}
	}
}

// targetOptionFor returns the target decision's option selecting obj, failing
// if the target ask is not pending for it.
func targetOptionFor(t *testing.T, e *Engine, obj state.ObjID) int {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	for _, opt := range d.Options {
		if opt.Obj == obj {
			return opt.Index
		}
	}
	t.Fatalf("target option for %d absent from %+v", obj, d.Options)
	return 0
}

// TestRequisitionRaidSpreeChargesEachChosenMode pins the whole Spree family end
// to end on the brief's own card: the total price is the printed {W} PLUS the
// ModeCost$ of every chosen mode. One mode costs {W}{1} (printed {W} + its
// {1}), two modes cost {W}{2}. Before this the modes resolved free for the
// printed {W}.
func TestRequisitionRaidSpreeChargesEachChosenMode(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name      string
		modeTexts []string
		floated   int
		wantPool  int32
		wantModes int
	}{
		{name: "one_mode", modeTexts: []string{"Destroy target artifact."}, floated: 3, wantPool: 1, wantModes: 1},
		{name: "two_modes", modeTexts: []string{"Destroy target artifact.", "Destroy target enchantment."}, floated: 4, wantPool: 1, wantModes: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := crAbortEngine(t, reg, "uw-control", "Requisition Raid", "Sol Ring", "Ghostly Prison")
			id := crAbortMove(t, e, 0, "Requisition Raid", state.ZHand)
			artifact := crAbortMove(t, e, 0, "Sol Ring", state.ZBattlefield)
			enchantment := crAbortMove(t, e, 0, "Ghostly Prison", state.ZBattlefield)
			// Preconditions: the fixture's modes really carry ModeCost$ 1, and
			// both target-bearing modes have a legal target (so a mode's
			// presence or absence below is about cost, never target legality).
			spreeModeCosts(t, e, id, map[string]Cost{
				"DBArtifact":    {Generic: 1},
				"DBEnchantment": {Generic: 1},
			})
			if e.G.Obj(artifact).Zone != state.ZBattlefield || e.G.Obj(enchantment).Zone != state.ZBattlefield {
				t.Fatalf("fixture targets not on the battlefield: artifact=%s enchantment=%s",
					e.G.Obj(artifact).Zone, e.G.Obj(enchantment).Zone)
			}
			tapEveryManaSource(t, e, 0)
			for range tc.floated {
				e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "W", Amount: 1})
			}
			e.askPriority(0)
			crAbortAnswer(t, e, "Requisition Raid", crAbortOption(t, e, "Requisition Raid", "cast", id))

			d := e.Pending()
			choices := make([]int, 0, len(tc.modeTexts))
			for _, text := range tc.modeTexts {
				choices = append(choices, modeOptionContaining(t, d, text))
			}
			crAbortAnswer(t, e, "Requisition Raid", choices...)
			// Each distinct target-bearing mode declares its own CR 601.2c target.
			if tc.wantModes == 1 {
				crAbortAnswer(t, e, "Requisition Raid", targetOptionFor(t, e, artifact))
			} else {
				d = e.Pending()
				if d == nil || d.Kind != decision.KTarget || d.Min != 2 || d.Max != 2 {
					t.Fatalf("pending = %+v, want two target slots", d)
				}
				artIdx, enchantIdx := -1, -1
				for _, o := range d.Options {
					if o.Group == "charm-mode-0" && o.Obj == artifact {
						artIdx = o.Index
					}
					if o.Group == "charm-mode-1" && o.Obj == enchantment {
						enchantIdx = o.Index
					}
				}
				if artIdx < 0 || enchantIdx < 0 || artIdx == enchantIdx {
					t.Fatalf("fixture lacks separate artifact/enchantment targets: %+v", d.Options)
				}
				crAbortAnswer(t, e, "Requisition Raid", artIdx, enchantIdx)
			}

			if got := e.G.Obj(id).Zone; got != state.ZStack {
				t.Fatalf("Requisition Raid zone after the cast = %s, want stack", got)
			}
			if got := e.G.Obj(id).ChosenModes; len(got) != tc.wantModes {
				t.Fatalf("Requisition Raid chosen modes = %v, want %d", got, tc.wantModes)
			}
			// printed {W} + one {1} per chosen mode, paid from the floated W;
			// the pool left is what pins the charged total.
			if got := e.G.Players[0].Pool.Total(); got != tc.wantPool {
				t.Fatalf("Requisition Raid %s left pool %d, want %d (charged %d of %d floated)",
					tc.name, got, tc.wantPool, int32(tc.floated)-tc.wantPool, tc.floated)
			}
		})
	}
}

// TestMetamorphicBlastSpreeUnaffordableModeNotOffered pins the cost-aware mode
// offer: with only {U}{U} floated, Metamorphic Blast's {3} "target player
// draws two cards" mode cannot be paid on top of the printed {U}, so it is not
// offered, while its {1} Rabbit mode (same target legality: a creature and a
// player are both present) is. A mode declined for cost is not selected.
func TestMetamorphicBlastSpreeUnaffordableModeNotOffered(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "uw-control", "Metamorphic Blast", "Grizzly Bears")
	id := crAbortMove(t, e, 0, "Metamorphic Blast", state.ZHand)
	creature := crAbortMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	spreeModeCosts(t, e, id, map[string]Cost{
		"DBAnimate": {Generic: 1},
		"DBDraw":    {Generic: 3},
	})
	if e.G.Obj(creature).Zone != state.ZBattlefield {
		t.Fatalf("fixture creature not on the battlefield")
	}
	// Prove the {3} mode is TARGET-legal here (a player exists), so its absence
	// from the options is the cost gate and not the target filter.
	drawSA := cards.ResolveSVar(e.G.Obj(id).Face().SVars, "DBDraw")
	if drawSA == nil || len(e.legalTargetCandidates(0, id, id, drawSA)) < 1 {
		t.Fatalf("fixture: DB$ Draw has no legal player target, cannot attribute its absence to cost")
	}
	tapEveryManaSource(t, e, 0)
	for range 2 {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 1})
	}
	e.askPriority(0)
	crAbortAnswer(t, e, "Metamorphic Blast", crAbortOption(t, e, "Metamorphic Blast", "cast", id))

	d := e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("after cast, next decision = %+v, want modes", d)
	}
	hasRabbit, hasDraw := false, false
	for _, opt := range d.Options {
		hasRabbit = hasRabbit || strings.Contains(opt.Label, "Rabbit")
		hasDraw = hasDraw || strings.Contains(opt.Label, "draws two cards")
	}
	if !hasRabbit {
		t.Fatalf("the {1} Rabbit mode was not offered: %+v", d.Options)
	}
	if hasDraw {
		t.Fatalf("the unaffordable {3} draw mode was offered with only {U}{U} floated: %+v", d.Options)
	}
	crAbortAnswer(t, e, "Metamorphic Blast", modeOptionContaining(t, d, "Rabbit"))
	crAbortAnswer(t, e, "Metamorphic Blast", targetOptionFor(t, e, creature))
	if got := e.G.Obj(id).Zone; got != state.ZStack {
		t.Fatalf("Metamorphic Blast zone = %s, want stack", got)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("Metamorphic Blast left pool %d, want 0 (printed {U} + the {1} mode)", got)
	}
}

// TestRequisitionRaidSpreeZeroModeCastNotOffered pins the family's lower
// bound: a Spree cast must choose at least MinCharmNum$ modes, so with only
// the printed {W} affordable no mode is payable and the cast is not offered --
// it reverses to hand (CR 733.1) instead of silently resolving a free mode.
func TestRequisitionRaidSpreeZeroModeCastNotOffered(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "uw-control", "Requisition Raid", "Sol Ring", "Ghostly Prison")
	id := crAbortMove(t, e, 0, "Requisition Raid", state.ZHand)
	crAbortMove(t, e, 0, "Sol Ring", state.ZBattlefield)
	crAbortMove(t, e, 0, "Ghostly Prison", state.ZBattlefield)
	spreeModeCosts(t, e, id, map[string]Cost{"DBArtifact": {Generic: 1}})
	tapEveryManaSource(t, e, 0)
	// Only {W}: the printed cost is payable, not one mode's {1} on top.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "W", Amount: 1})
	e.askPriority(0)
	crAbortAnswer(t, e, "Requisition Raid", crAbortOption(t, e, "Requisition Raid", "cast", id))

	if got := e.G.Obj(id).Zone; got != state.ZHand {
		t.Fatalf("zero-mode Requisition Raid zone = %s, want hand (the cast must reverse)", got)
	}
	if e.cast != nil {
		t.Fatalf("zero-mode Requisition Raid left a pending cast")
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.ModeChosen && ev.Obj == id {
			t.Fatalf("zero-mode cast recorded a mode announcement at seq %d", ev.Seq)
		}
	}
}

// TestRequisitionRaidSpreeUnaffordableCombinationReverses pins the other side
// of the per-mode gate: each mode is individually affordable ({W}{1}), but the
// SUM ({W}{2}) exceeds {W}{W}, so the whole proposal reverses (CR 733.1) and
// the cast never pays for modes it cannot afford.
func TestRequisitionRaidSpreeUnaffordableCombinationReverses(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "uw-control", "Requisition Raid", "Sol Ring", "Ghostly Prison")
	id := crAbortMove(t, e, 0, "Requisition Raid", state.ZHand)
	crAbortMove(t, e, 0, "Sol Ring", state.ZBattlefield)
	crAbortMove(t, e, 0, "Ghostly Prison", state.ZBattlefield)
	spreeModeCosts(t, e, id, map[string]Cost{"DBArtifact": {Generic: 1}, "DBEnchantment": {Generic: 1}})
	tapEveryManaSource(t, e, 0)
	for range 2 {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "W", Amount: 1})
	}
	e.askPriority(0)
	crAbortAnswer(t, e, "Requisition Raid", crAbortOption(t, e, "Requisition Raid", "cast", id))

	d := e.Pending()
	crAbortAnswer(t, e, "Requisition Raid",
		modeOptionContaining(t, d, "Destroy target artifact."),
		modeOptionContaining(t, d, "Destroy target enchantment."))
	// Each mode is affordable alone, so the proposal advances past the mode
	// answer; the unaffordable {W}{2} total reverses it (CR 733.1) before any
	// target is committed or mana is spent.
	if got := e.G.Obj(id).Zone; got != state.ZHand {
		t.Fatalf("over-budget Requisition Raid zone = %s, want hand", got)
	}
	if got := e.G.Players[0].Pool.Total(); got != 2 {
		t.Fatalf("over-budget Requisition Raid pool = %d, want 2 (nothing charged)", got)
	}
	if e.cast != nil {
		t.Fatalf("over-budget Requisition Raid left a pending cast")
	}
}

// TestMetamorphicBlastSpreeModeGateAppliesCostModifiers pins the modifier-aware
// half of the per-mode cost gate: a mode priced ABOVE the floated mana by its
// raw ModeCost$ may still be payable once the cast's live ReduceCost$ statics
// are composed, and the gate must see that. Baral, Chief of Compliance's
// "instant and sorcery spells you cast cost {1} less" makes Metamorphic Blast's
// {1} Rabbit mode cost {U} total -- payable from the single floated Island --
// even though its raw price is {U}{1}. A modifier-blind gate would withhold
// every mode and drive the whole cast through the min > len(legal) no-progress
// abort, denying a legal cast.
func TestMetamorphicBlastSpreeModeGateAppliesCostModifiers(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "uw-control", "Metamorphic Blast", "Baral, Chief of Compliance", "Grizzly Bears")
	id := crAbortMove(t, e, 0, "Metamorphic Blast", state.ZHand)
	baral := crAbortMove(t, e, 0, "Baral, Chief of Compliance", state.ZBattlefield)
	creature := crAbortMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	spreeModeCosts(t, e, id, map[string]Cost{
		"DBAnimate": {Generic: 1},
		"DBDraw":    {Generic: 3},
	})
	// Preconditions: Baral is really on the battlefield (the reducer source)
	// and the Rabbit mode's target is present, so a withheld mode below is
	// attributable to the cost gate, never to a missing target or static.
	if e.G.Obj(baral).Zone != state.ZBattlefield || e.G.Obj(creature).Zone != state.ZBattlefield {
		t.Fatalf("fixture not on the battlefield: baral=%s creature=%s",
			e.G.Obj(baral).Zone, e.G.Obj(creature).Zone)
	}
	tapEveryManaSource(t, e, 0)
	// One Island's worth of {U}: the raw {U}{1} Rabbit mode is unaffordable,
	// but {1}-less makes it {U}.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 1})
	e.askPriority(0)
	crAbortAnswer(t, e, "Metamorphic Blast", crAbortOption(t, e, "Metamorphic Blast", "cast", id))

	d := e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("after cast, next decision = %+v, want modes", d)
	}
	hasRabbit := false
	for _, opt := range d.Options {
		hasRabbit = hasRabbit || strings.Contains(opt.Label, "Rabbit")
	}
	if !hasRabbit {
		t.Fatalf("the {1} Rabbit mode was withheld under a {1}-less reducer: %+v", d.Options)
	}
	crAbortAnswer(t, e, "Metamorphic Blast", modeOptionContaining(t, d, "Rabbit"))
	crAbortAnswer(t, e, "Metamorphic Blast", targetOptionFor(t, e, creature))
	if got := e.G.Obj(id).Zone; got != state.ZStack {
		t.Fatalf("Metamorphic Blast zone = %s, want stack (the {U} total was payable)", got)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("Metamorphic Blast left pool %d, want 0 (printed {U}, {1} mode reduced to 0)", got)
	}
}

// TestFireMagicTieredChargesTheChosenMode pins the kw:Tiered half of the same
// machinery on a real Tiered card: Fire Magic's printed {R} plus the chosen
// mode's ModeCost$ (Fira {2} -> {R}{2}). Tiered shares the Spree
// SP$ Charm + ModeCost$ + default CharmNum$ 1 shape, so it must charge
// identically; this test would fail if the per-mode charge were wired to the
// Spree keyword instead of to ModeCost$ itself.
func TestFireMagicTieredChargesTheChosenMode(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "uw-control", "Fire Magic")
	id := crAbortMove(t, e, 0, "Fire Magic", state.ZHand)
	spreeModeCostsKeyword(t, e, id, "Tiered", map[string]Cost{
		"DBFire":   {},
		"DBFira":   {Generic: 2},
		"DBFiraga": {Generic: 5},
	})
	f := e.G.Obj(id).Face()
	found := false
	for _, k := range f.Keywords {
		if strings.EqualFold(cards.KeywordHead(k), "Tiered") {
			found = true
		}
	}
	if !found {
		t.Fatalf("fixture %s carries no K:Tiered keyword", f.Name)
	}
	tapEveryManaSource(t, e, 0)
	// {R}{R}{R}{R}: printed {R} plus the {2} Fira mode is 3 mana total, so
	// exactly one R must remain -- a free mode would leave 3 and a
	// printed-only charge 3 as well, so the residual discriminates the {2}.
	for range 4 {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 1})
	}
	e.askPriority(0)
	crAbortAnswer(t, e, "Fire Magic", crAbortOption(t, e, "Fire Magic", "cast", id))

	d := e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("after Tiered cast, next decision = %+v, want modes", d)
	}
	crAbortAnswer(t, e, "Fire Magic", modeOptionContaining(t, d, "deals 2 damage"))
	if got := e.G.Obj(id).Zone; got != state.ZStack {
		t.Fatalf("Fire Magic zone = %s, want stack", got)
	}
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("Fire Magic left pool %d, want 1 (printed {R} + the {2} Fira mode of 3 floated)", got)
	}
	if got := e.G.Obj(id).ChosenModes; len(got) != 1 {
		t.Fatalf("Fire Magic chosen modes = %v, want 1", got)
	}
}

// TestSpreeUnparseableModeCostWithheld pins the present-vs-absent distinction
// in modeCost: a mode that PRINTS a ModeCost$ this build cannot price must not
// read as a free mode. The corpus carries only parsable ModeCost$ tokens, so
// this authors a fixture carrying an unmodelled token ("Xyzzy", which ParseCost
// records in Cost.Unknown) beside a real {1} mode: the unparseable mode is
// withheld from the cast's legal set while the {1} mode is offered, and
// modeCostUnparseable names the shape.
func TestSpreeUnparseableModeCostWithheld(t *testing.T) {
	t.Parallel()
	const src = "Name:Bad Spree\nManaCost:U\nTypes:Sorcery\nK:Spree\n" +
		"A:SP$ Charm | Choices$ DBGood,DBBad | MinCharmNum$ 1\n" +
		"SVar:DBGood:DB$ Draw | ModeCost$ 1 | Defined$ You | NumCards$ 1 | SpellDescription$ Good mode.\n" +
		"SVar:DBBad:DB$ Draw | ModeCost$ Xyzzy | Defined$ You | NumCards$ 1 | SpellDescription$ Bad mode.\n" +
		"Oracle:x\n"
	f := card(t, src).Faces[0]
	// Precondition: the fixture really carries the two distinct shapes.
	if _, present, ok := modeCost(f, "DBGood"); !present || !ok {
		t.Fatalf("fixture DBGood ModeCost$ not present/parseable")
	}
	if !modeCostUnparseable(f, "DBBad") {
		t.Fatalf("fixture DBBad ModeCost$ should be present but unparseable")
	}
	if _, present, ok := modeCost(f, "DBAbsent"); present || ok {
		t.Fatalf("a mode with no ModeCost$ must read absent, not present")
	}

	e, cfg, id := newFixtureDeck(t, 9901, src)
	// {U} only: the {1} good mode is unaffordable (so it is absent for COST),
	// and the unparseable bad mode must be absent because it cannot be priced
	// -- never offered as a free mode. min 1 > 0 legal modes reverses the cast.
	toMain1(t, e)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 1})
	e.pending = nil
	e.askPriority(0)
	var opt *decision.Option
	opts := castOptions(t, e)
	for i := range opts {
		if opts[i].Obj == id {
			opt = &opts[i]
		}
	}
	if opt == nil {
		t.Fatalf("fixture cast not offered at all before the mode answer")
	}
	submitChoices(t, e, opt.Index)
	if e.cast != nil {
		t.Fatalf("unpayable/unparseable-mode cast left a pending cast")
	}
	if got := e.G.Obj(id).Zone; got != state.ZHand {
		t.Fatalf("cast zone = %s, want hand (the min>len(legal) abort)", got)
	}
	replayCheck(t, e, cfg)
}
