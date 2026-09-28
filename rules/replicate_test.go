package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kw:Replicate (CR 702.55a) pinned end to end on the REAL corpus carriers.
// The deck is built from compiled corpus cards only, so no Forge script text
// is committed. None of the three carriers (Changing Loyalty, Pyromatics —
// and the declined shape reuses Changing Loyalty) is in any legacy golden
// deck, so no chain head depends on these cards.
//
// The helpers come from search_library_test.go (searchTestRegistry,
// searchCorpusCard, searchMoveByName), cast_test.go (addMana, submitChoices,
// castObj) and replacement_updated_test.go (passUntilStackEmpty) — all the
// same package.

// replicateEngine deals seat 0 a 40-card deck whose first card is the named
// replicate spell, then Mountains, Forests and Grizzly Bears (the creature
// the Changing Loyalty tests enchant); the opponent's deck is all Mountains.
// The seed is advanced to start seat 0.
func replicateEngine(t *testing.T, hero string) (*Engine, Config, *cards.Registry) {
	t.Helper()
	reg := searchTestRegistry(t)
	mountain := searchCorpusCard(t, reg, "Mountain")
	forest := searchCorpusCard(t, reg, "Forest")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{searchCorpusCard(t, reg, hero)}
	for i := 0; i < 6; i++ {
		deck = append(deck, mountain)
	}
	for i := 0; i < 6; i++ {
		deck = append(deck, forest)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 9311, Names: []string{"rep", "opp"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg, reg
}

// castBear puts a Grizzly Bears on the battlefield under seat 0 the ordinary
// way — moved to hand, funded, cast through the pending decision — so every
// event in the log is engine-produced and replayCheck stays honest.
func castBear(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
	addMana(t, e, 0, "GG") // Grizzly Bears is {1}{G}
	castObj(t, e, bear)
	if e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatalf("bear did not enter the battlefield: %s", e.G.Obj(bear).Zone)
	}
	return bear
}

// replicateOption returns the priority option casting id in the given mode.
func replicateOption(t *testing.T, e *Engine, id state.ObjID, mode string) decision.Option {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id && o.Mode == mode {
			return o
		}
	}
	t.Fatalf("no %q cast option for %d: %+v", mode, id, d.Options)
	return decision.Option{}
}

// chooseReplicate submits the count decision's option whose Amount is want.
func chooseReplicate(t *testing.T, e *Engine, want int) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected a KChoose replicate-count decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "replicate" && o.Amount == want {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no replicate option for %d: %+v", want, d.Options)
}

// chooseTargetObject answers a pending target decision with the option whose
// Obj is want.
func chooseTargetObject(t *testing.T, e *Engine, want state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == want {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("target %d not offered: %+v", want, d.Options)
}

// logHasFlag reports whether the log carries a CastInfo for obj whose flag
// list names name.
func logHasFlag(e *Engine, obj state.ObjID, name string) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == obj {
			for _, part := range splitCSV(ev.Counter) {
				if part == name {
					return true
				}
			}
		}
	}
	return false
}

func splitCSV(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}

// TestReplicateChangingLoyaltyPaidOnceAttachesTheCopy is the Changing Loyalty
// carrier (K:Replicate:2, CR 702.55a): the replicated cast option is offered,
// the count ask is answered once, exactly one copy stack object resolves on
// top of the original, and the copy — a permanent-spell copy — enters the
// battlefield attached to the SAME creature the original enchants. Per
// CR 707.10g a copy of a permanent spell becomes a token: the resolved copy
// arrives as a token object (IsToken, not IsCopy), not a card copy.
func TestReplicateChangingLoyaltyPaidOnceAttachesTheCopy(t *testing.T) {
	t.Parallel()
	e, cfg, _ := replicateEngine(t, "Changing Loyalty")
	bear := castBear(t, e)
	hero := searchMoveByName(t, e, "Changing Loyalty", state.ZHand)
	addMana(t, e, 0, "BCCC") // base {1}{B} plus one {2}: the B pip and 3 generic

	// The plain and replicated cast options are both offered.
	replicateOption(t, e, hero, "")
	opt := replicateOption(t, e, hero, "replicated")
	submitChoices(t, e, opt.Index)

	// The count ask: ascending 0..max, here exactly 0..1 (the pool affords
	// exactly one payment).
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 ||
		d.Options[0].Kind != "replicate" || d.Options[0].Amount != 0 ||
		d.Options[1].Amount != 1 {
		t.Fatalf("replicate count decision %+v", d)
	}
	chooseReplicate(t, e, 1)

	// The target decision, answered at the bear; then both auras resolve.
	chooseTargetObject(t, e, bear)
	passUntilStackEmpty(t, e, 30)

	o := e.G.Obj(hero)
	if o.Zone != state.ZBattlefield || o.AttachedTo != bear {
		t.Fatalf("original: zone=%s attachedTo=%d", o.Zone, o.AttachedTo)
	}
	copies := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		c := e.G.Obj(id)
		if id != hero && c.Face() != nil && c.Face().Name == "Changing Loyalty" {
			copies++
			// CR 707.10g: the copy of the permanent spell became a token.
			if !c.IsToken || c.IsCopy {
				t.Fatalf("copy %d: IsToken=%v IsCopy=%v, want a token", id, c.IsToken, c.IsCopy)
			}
			if c.AttachedTo != bear {
				t.Fatalf("copy %d attached to %d, want the same bear %d", id, c.AttachedTo, bear)
			}
			if c.Zone != state.ZBattlefield {
				t.Fatalf("copy in %s", c.Zone)
			}
		}
	}
	if copies != 1 {
		t.Fatalf("%d copies on the battlefield, want 1", copies)
	}
	if !logHasFlag(e, hero, "replicated") {
		t.Fatal("the pay-time CastInfo carries no replicated flag")
	}
	replayCheck(t, e, cfg)
}

// TestReplicateDeclinedIsAPlainCast: the count ask answered 0 declines — the
// cast resolves exactly like the pre-existing plain cast: no copies, no
// replicated flag, no CastInfo event at all (the declined shape emits nothing
// the plain cast would not).
func TestReplicateDeclinedIsAPlainCast(t *testing.T) {
	t.Parallel()
	e, cfg, _ := replicateEngine(t, "Changing Loyalty")
	bear := castBear(t, e)
	hero := searchMoveByName(t, e, "Changing Loyalty", state.ZHand)
	addMana(t, e, 0, "BCCC")
	opt := replicateOption(t, e, hero, "replicated")
	submitChoices(t, e, opt.Index)
	chooseReplicate(t, e, 0)
	chooseTargetObject(t, e, bear)
	passUntilStackEmpty(t, e, 30)

	o := e.G.Obj(hero)
	if o.Zone != state.ZBattlefield || o.AttachedTo != bear {
		t.Fatalf("original: zone=%s attachedTo=%d", o.Zone, o.AttachedTo)
	}
	copies := 0
	for _, id := range e.G.Objs {
		if id.IsCopy {
			copies++
		}
	}
	if copies != 0 {
		t.Fatalf("%d copies, want 0", copies)
	}
	if logHasFlag(e, hero, "replicated") {
		t.Fatal("a declined replicate must not carry the flag")
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == hero {
			t.Fatalf("declined replicate emitted a CastInfo: %+v", ev)
		}
	}
	replayCheck(t, e, cfg)
}

// TestReplicatePyromaticsPaidTwiceBoundedByAffordability: the pool affords
// exactly two {1}{R} payments, so the count ask offers exactly 0..2 — not
// more — and the answered 2 yields two copies that resolve alongside the
// original (3 damage instances to the targeted opponent), the instant copies
// resting in exile.
func TestReplicatePyromaticsPaidTwiceBoundedByAffordability(t *testing.T) {
	t.Parallel()
	e, cfg, _ := replicateEngine(t, "Pyromatics")
	hero := searchMoveByName(t, e, "Pyromatics", state.ZHand)
	addMana(t, e, 0, "RRRCCC") // base {1}{R} + two {1}{R}: 3 R pips + 3 generic
	life1 := e.G.Players[1].Life

	opt := replicateOption(t, e, hero, "replicated")
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the replicate count decision, got %+v", d)
	}
	if len(d.Options) != 3 || d.Options[0].Amount != 0 ||
		d.Options[1].Amount != 1 || d.Options[2].Amount != 2 {
		t.Fatalf("count ask not bounded by affordability (want 0..2): %+v", d)
	}
	chooseReplicate(t, e, 2)

	// Pyromatics targets any: aim the opponent.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	tIdx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("seat 1 not offered: %+v", d.Options)
	}
	submitChoices(t, e, tIdx)
	passUntilStackEmpty(t, e, 30)

	if got := life1 - e.G.Players[1].Life; got != 3 {
		t.Fatalf("seat 1 lost %d life, want 3 (original + two copies)", got)
	}
	copies := 0
	for _, o := range e.G.Objs {
		if o.IsCopy {
			copies++
			if o.Zone != state.ZExile {
				t.Fatalf("a resolved instant copy sits in %s", o.Zone)
			}
		}
	}
	if copies != 2 {
		t.Fatalf("%d copies, want 2", copies)
	}
	if !logHasFlag(e, hero, "replicated") {
		t.Fatal("the pay-time CastInfo carries no replicated flag")
	}
	replayCheck(t, e, cfg)
}

// replicateTapEngine is the tapXType/energy replicate carriers' engine: the
// hero first, the named extras after it (before the mountain filler), the
// rest Grizzly Bears; the opponent's deck leads with one bear so a creature
// target exists. All corpus cards, no Forge script text committed; no
// carrier is in any legacy golden deck.
func replicateTapEngine(t *testing.T, hero string, extras ...string) (*Engine, Config, *cards.Registry) {
	t.Helper()
	reg := searchTestRegistry(t)
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{searchCorpusCard(t, reg, hero)}
	for _, name := range extras {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for len(deck) < 9 {
		deck = append(deck, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := []*cards.Card{bear}
	for len(opp) < 40 {
		opp = append(opp, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 9417, Names: []string{"rep", "opp"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg, reg
}

// chooseTapCost answers a pending tap-cost KChoose with the option whose Obj
// is want.
func chooseTapCost(t *testing.T, e *Engine, want state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected a tap-cost KChoose, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "tapcost" && o.Obj == want {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("tapcost option %d not offered: %+v", want, d.Options)
}

// drainStackDecliningPlays drains the stack like passUntilStackEmpty but also
// declines the optional mid-resolution "Play a card" ask (Psionic Ritual's
// DB$ Play Optional$ True: this test's engine has no host that wants the free
// cast, so every ask is declined with the empty answer its Min 0 permits).
func drainStackDecliningPlays(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for n := 0; n < limit && !e.G.Over && len(e.G.Stack) > 0; n++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining the stack (stack depth %d)", len(e.G.Stack))
		}
		if d.Kind == decision.KModes && d.ResumeKind == "play" {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
				t.Fatalf("decline optional play ask: %v", err)
			}
			continue
		}
		if d.Kind == decision.KTarget && d.ResumeKind == "copy_targets" {
			// CR 707.10c: keep the copy's inherited targets (the leading
			// d.Min options, placed first) -- one for a single-target copy,
			// all of them for a multi-target copy.
			choices := make([]int, 0, d.Min)
			keep := d.Min
			if keep < 1 {
				keep = 1
			}
			for i := 0; i < keep && i < len(d.Options); i++ {
				choices = append(choices, d.Options[i].Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
				t.Fatalf("submit copy target: %v", err)
			}
			continue
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %+v while draining the stack", d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	if len(e.G.Stack) > 0 {
		t.Fatalf("stack did not drain within %d steps (depth %d)", limit, len(e.G.Stack))
	}
}

// TestReplicateTapCostAskBoundedByUntappedCandidates is the r2 MAJOR's
// regression pin on the clean carrier: Exterminate! (K:Replicate:tapXType<1/Dalek>,
// {2}{B}) with TWO untapped Daleks. The count ask's max is the largest N the
// composed cost can still pay, and the composed cost's repeated tap parts
// draw down ONE shared pool of untapped Daleks — the pre-fix engine checked
// each part independently, offered 0..64 (the hard cap), and answering above
// 2 aborted the whole cast at the payment stage. Here the ask must offer
// exactly 0..2, and answering the true max 2 completes the cast end to end:
// both Daleks tapped, the victim destroyed, two IsCopy spell copies resolved.
func TestReplicateTapCostAskBoundedByUntappedCandidates(t *testing.T) {
	t.Parallel()
	e, cfg, reg := replicateTapEngine(t, "Exterminate!", "Dalek Squadron", "Dalek Squadron")
	dalekCard := searchCorpusCard(t, reg, "Dalek Squadron")
	dalek1 := moveSeededCard(t, e, 0, dalekCard, state.ZBattlefield)
	dalek2 := moveSeededCard(t, e, 0, dalekCard, state.ZBattlefield)
	victim := moveSeededCard(t, e, 1, searchCorpusCard(t, reg, "Grizzly Bears"), state.ZBattlefield)
	life1 := e.G.Players[1].Life

	addMana(t, e, 0, "BCC") // Exterminate! is {2}{B}
	hero := searchMoveByName(t, e, "Exterminate!", state.ZHand)
	opt := replicateOption(t, e, hero, "replicated")
	submitChoices(t, e, opt.Index)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the replicate count decision, got %+v", d)
	}
	if len(d.Options) != 3 || d.Options[0].Amount != 0 ||
		d.Options[1].Amount != 1 || d.Options[2].Amount != 2 {
		t.Fatalf("tap replicate count ask not bounded by the shared Dalek pool (want 0..2): %+v", d)
	}
	chooseReplicate(t, e, 2)

	// The composed cost carries two tapXType<1/Dalek> parts: the first poses
	// a real ask over both Daleks, the second finds exactly one candidate
	// left and taps it without asking.
	chooseTapCost(t, e, dalek1)
	if d = e.Pending(); d != nil && d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "tapcost" {
		t.Fatalf("the second tap part should have auto-selected the last Dalek: %+v", d)
	}
	if e.G.Obj(dalek1).Tapped || e.G.Obj(dalek2).Tapped {
		// Taps settle at payCast, after the target ask; assert below.
		_ = d
	}

	// Exterminate! targets a creature: aim the opponent's bear.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	chooseTargetObject(t, e, victim)
	passUntilStackEmpty(t, e, 30)

	if !e.G.Obj(dalek1).Tapped || !e.G.Obj(dalek2).Tapped {
		t.Fatalf("replicate payments did not tap both Daleks (%v, %v)",
			e.G.Obj(dalek1).Tapped, e.G.Obj(dalek2).Tapped)
	}
	if o := e.G.Obj(victim); o.Zone == state.ZBattlefield {
		t.Fatalf("the victim survived: %+v", o)
	}
	if lost := life1 - e.G.Players[1].Life; lost < 3 {
		t.Fatalf("seat 1 lost %d life, want at least the one resolving copy's 3", lost)
	}
	copies := 0
	for _, o := range e.G.Objs {
		if o.IsCopy {
			copies++
			if o.Zone != state.ZExile {
				t.Fatalf("a resolved sorcery copy sits in %s", o.Zone)
			}
		}
	}
	if copies != 2 {
		t.Fatalf("%d copies, want 2", copies)
	}
	if !logHasFlag(e, hero, "replicated") {
		t.Fatal("the pay-time CastInfo carries no replicated flag")
	}
	replayCheck(t, e, cfg)
}

// TestReplicateTapCostPsionicRitualBoundReservesCandidates is the r2 MAJOR's
// measured shape verbatim: Psionic Ritual (K:Replicate:tapXType<1/Horror>,
// {4}{U}{U}) with two untapped Kederekt Creepers (real corpus Horrors) and a
// legal graveyard target. The pre-fix engine offered the count 0..64 — the
// hard cap, every part priced against the whole unreserved candidate list —
// and answering above the true count aborted the entire cast ("tap cost no
// longer payable", the spell back in hand). The fixed bound is exactly the
// board's: 0..2, and answering 2 pays, taps both Horrors and puts three
// spell objects on the stack (original + two IsCopy copies).
func TestReplicateTapCostPsionicRitualBoundReservesCandidates(t *testing.T) {
	t.Parallel()
	e, cfg, reg := replicateTapEngine(t, "Psionic Ritual",
		"Kederekt Creeper", "Kederekt Creeper", "Giant Growth")
	creeperCard := searchCorpusCard(t, reg, "Kederekt Creeper")
	creeper1 := moveSeededCard(t, e, 0, creeperCard, state.ZBattlefield)
	creeper2 := moveSeededCard(t, e, 0, creeperCard, state.ZBattlefield)
	gravy := moveSeededCard(t, e, 0, searchCorpusCard(t, reg, "Giant Growth"), state.ZGraveyard)

	addMana(t, e, 0, "UUCCCC") // Psionic Ritual is {4}{U}{U}
	hero := searchMoveByName(t, e, "Psionic Ritual", state.ZHand)
	opt := replicateOption(t, e, hero, "replicated")
	submitChoices(t, e, opt.Index)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the replicate count decision, got %+v", d)
	}
	if len(d.Options) != 3 || d.Options[0].Amount != 0 ||
		d.Options[1].Amount != 1 || d.Options[2].Amount != 2 {
		t.Fatalf("Psionic Ritual's count ask not bounded by the two Horrors (want 0..2): %+v", d)
	}
	chooseReplicate(t, e, 2)

	chooseTapCost(t, e, creeper1)
	// Target the graveyard instant.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	chooseTargetObject(t, e, gravy)

	// The cast COMPLETED — answering the true max must never abort at the
	// tap cost: the settle tapped both Horrors and the spell sits on the
	// stack awaiting its (two-copy) replicate trigger.
	if !e.G.Obj(creeper1).Tapped || !e.G.Obj(creeper2).Tapped {
		t.Fatalf("replicate payments did not tap both Horrors at the settle (%v, %v)",
			e.G.Obj(creeper1).Tapped, e.G.Obj(creeper2).Tapped)
	}
	if o := e.G.Obj(hero); o.Zone != state.ZStack {
		t.Fatalf("the 2-payment cast did not complete: the spell sits in %s", o.Zone)
	}
	drainStackDecliningPlays(t, e, 40)
	if !e.G.Obj(creeper1).Tapped || !e.G.Obj(creeper2).Tapped {
		t.Fatalf("replicate payments did not tap both Horrors (%v, %v)",
			e.G.Obj(creeper1).Tapped, e.G.Obj(creeper2).Tapped)
	}
	copies := 0
	for _, o := range e.G.Objs {
		if o.IsCopy {
			copies++
		}
	}
	if copies != 2 {
		t.Fatalf("%d copies, want 2", copies)
	}
	if !logHasFlag(e, hero, "replicated") {
		t.Fatal("the pay-time CastInfo carries no replicated flag")
	}
	replayCheck(t, e, cfg)
}

// TestReplicateEnergyCostAskBoundedByEnergyPool pins the same class on the
// non-mana repeatable resource: Reiterating Bolt (K:Replicate:PayEnergy<3>,
// {1}{R}) with exactly 6 energy. The pre-fix engine priced each composed
// PayEnergy part against the whole counter total independently, so any pool
// of 3+ offered the count 0..64; the fixed bound sums the composed parts and
// offers exactly 0..2, and answering 2 drains all 6 energy and resolves two
// copies.
func TestReplicateEnergyCostAskBoundedByEnergyPool(t *testing.T) {
	t.Parallel()
	e, cfg, _ := replicateTapEngine(t, "Reiterating Bolt")
	bear := castBear(t, e)
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "ENERGY", Amount: 6})
	e.pending = nil
	e.priorityRound()

	addMana(t, e, 0, "RC") // Reiterating Bolt is {1}{R}; the replicate is energy-only
	hero := searchMoveByName(t, e, "Reiterating Bolt", state.ZHand)
	opt := replicateOption(t, e, hero, "replicated")
	submitChoices(t, e, opt.Index)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the replicate count decision, got %+v", d)
	}
	if len(d.Options) != 3 || d.Options[0].Amount != 0 ||
		d.Options[1].Amount != 1 || d.Options[2].Amount != 2 {
		t.Fatalf("energy replicate count ask not bounded by the 6-energy pool (want 0..2): %+v", d)
	}
	chooseReplicate(t, e, 2)

	// Reiterating Bolt targets a creature: the bear.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	chooseTargetObject(t, e, bear)
	passUntilStackEmpty(t, e, 30)

	if n := e.G.Players[0].Counter("ENERGY"); n != 0 {
		t.Fatalf("energy pool at %d after two PayEnergy<3> payments, want 0", n)
	}
	copies := 0
	for _, o := range e.G.Objs {
		if o.IsCopy {
			copies++
			if o.Zone != state.ZExile {
				t.Fatalf("a resolved sorcery copy sits in %s", o.Zone)
			}
		}
	}
	if copies != 2 {
		t.Fatalf("%d copies, want 2", copies)
	}
	if !logHasFlag(e, hero, "replicated") {
		t.Fatal("the pay-time CastInfo carries no replicated flag")
	}
	replayCheck(t, e, cfg)
}
