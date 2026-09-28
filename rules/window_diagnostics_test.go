package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// windowReasonFor returns the WindowReasons entry for (obj, kind), and ok.
func windowReasonFor(d *decision.Decision, obj state.ObjID, kind string) (decision.WindowReason, bool) {
	if d == nil {
		return decision.WindowReason{}, false
	}
	for _, r := range d.WindowReasons {
		if r.Obj == obj && r.Kind == kind {
			return r, true
		}
	}
	return decision.WindowReason{}, false
}

// hasOptionFor reports whether the decision offers any option for obj.
func hasOptionFor(d *decision.Decision, obj state.ObjID) bool {
	if d == nil {
		return false
	}
	for _, o := range d.Options {
		if o.Obj == obj {
			return true
		}
	}
	return false
}

// windowSweep plays a whole game with a deterministic naive bot and returns
// the event log slice length and the chain head. It answers every decision
// with the last "pass" option when present, else the first option.
func windowSweep(t *testing.T, seed uint64, window bool) (int, string) {
	t.Helper()
	names := []string{"a", "b"}
	decks := [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}
	e := New(seatZeroStart(Config{Seed: seed, Names: names, Decks: decks, WindowDiagnostics: window}))
	e.Advance()
	for i := 0; i < 50000 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		pick := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pick = o.Index
			}
		}
		if pick < 0 {
			if len(d.Options) == 0 {
				t.Fatalf("decision with no options: %+v", d)
			}
			// Choose the first option that is neither pass nor concede.
			for _, o := range d.Options {
				if o.Kind != "concede" {
					pick = o.Index
					break
				}
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	return len(e.L.Events), e.L.Head()
}

// TestWindowDiagnosticsReplayIdentity is the mandatory observer test: enabling
// the diagnostic changes no event and no chain head, because the collector
// emits nothing.
func TestWindowDiagnosticsReplayIdentity(t *testing.T) {
	t.Parallel()
	nOff, headOff := windowSweep(t, 7, false)
	nOn, headOn := windowSweep(t, 7, true)
	if nOff != nOn {
		t.Fatalf("event count differs: off=%d on=%d", nOff, nOn)
	}
	if headOff != headOn {
		t.Fatalf("chain head differs: off=%s on=%s", headOff, headOn)
	}
	if nOff == 0 {
		t.Fatal("precondition failed: the sweep emitted no events")
	}
	// And the sidecar is actually produced when enabled, so the test's
	// subject is live (a window-less engine would trivially match).
	if !windowProduced(t, 7) {
		t.Fatal("enabled game produced no WindowReasons on any priority decision")
	}
}

// windowProduced reports whether some priority decision in a short enabled
// game carried a non-nil WindowReasons sidecar.
func windowProduced(t *testing.T, seed uint64) bool {
	t.Helper()
	names := []string{"a", "b"}
	decks := [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}
	cfg := seatZeroStart(Config{Seed: seed, Names: names, Decks: decks, WindowDiagnostics: true})
	e := New(cfg)
	var saw bool
	e.Advance()
	for i := 0; i < 3000 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if len(d.WindowReasons) > 0 {
			saw = true
		}
		pick := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pick = o.Index
			}
		}
		if pick < 0 {
			pick = d.Options[0].Index
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	return saw
}

// TestWindowDiagnosticsLandDropExhausted: after seat 0 uses its land drop, a
// second land in hand is withheld with land:drop_exhausted. Preconditions:
// the land is in hand, a first land was actually played this turn.
func TestWindowDiagnosticsLandDropExhausted(t *testing.T) {
	t.Parallel()
	forest := card(t, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n")
	second := card(t, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	decks := [][]*cards.Card{
		append([]*cards.Card{forest, second}, mountainDeck(t, 38)...),
		mountainDeck(t, 40),
	}
	e := New(seatZeroStart(Config{Seed: 3, Names: []string{"a", "b"}, Decks: decks, WindowDiagnostics: true}))
	e.Advance()
	// Find both lands in hand.
	var first, held state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		switch e.G.Obj(id).Face().Name {
		case "Forest":
			first = id
		case "Island":
			held = id
		}
	}
	if first == 0 || held == 0 {
		got := e.G.Zone(state.ZHand, 0)
		var names []string
		for _, id := range got {
			names = append(names, e.G.Obj(id).Face().Name)
		}
		// Fall back to the library: bridge both in as a logged move.
		for _, id := range e.G.Zone(state.ZLibrary, 0) {
			switch e.G.Obj(id).Face().Name {
			case "Forest":
				first = id
			case "Island":
				held = id
			}
		}
		if first == 0 || held == 0 {
			t.Fatalf("lands not found (hand=%v)", e.G.Zone(state.ZHand, 0))
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: first, From: state.ZLibrary, To: state.ZHand})
		e.emit(events.Event{Kind: events.MoveZone, Obj: held, From: state.ZLibrary, To: state.ZHand})
		e.pending = nil
		e.Advance()
	}
	// Sorcery speed main: play the first land from the pending decision.
	if !e.G.Step.IsMain() || e.G.Active != 0 {
		toMain1(t, e)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision: %+v", d)
	}
	played := false
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == first {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("play land: %v", err)
			}
			played = true
			break
		}
	}
	if !played {
		t.Fatalf("first land was not offered; options=%v", d.Options)
	}
	if e.G.Players[0].LandsPlayed < 1 {
		t.Fatalf("precondition failed: LandsPlayed=%d after playing a land", e.G.Players[0].LandsPlayed)
	}
	if !hasInHand(e, held) {
		t.Fatalf("precondition failed: held land %d not in hand", held)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision after land: %+v", d)
	}
	if hasOptionFor(d, held) {
		t.Fatalf("held land unexpectedly offered: %+v", d.Options)
	}
	r, ok := windowReasonFor(d, held, windowKindLand)
	if !ok {
		t.Fatalf("no land reason for %d: %+v", held, d.WindowReasons)
	}
	if r.Reason != wrLandDropExhausted {
		t.Fatalf("reason=%q want %q", r.Reason, wrLandDropExhausted)
	}
}

// TestWindowDiagnosticsOfferedMorphLandHasNoReason: playing a land spends the
// land drop, but a Morph land remains castable face down. No withholding
// reason may coexist with its cast option, even though the two routes have
// different option kinds.
func TestWindowDiagnosticsOfferedMorphLandHasNoReason(t *testing.T) {
	t.Parallel()
	cavern := card(t, "Name:Zoetic Cavern\nManaCost:no cost\nTypes:Land\nK:Morph:2\nA:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\nOracle:{T}: Add {C}.\\nMorph {2}\n")
	decks := [][]*cards.Card{
		append([]*cards.Card{cavern}, mountainDeck(t, 39)...),
		mountainDeck(t, 40),
	}
	e := New(seatZeroStart(Config{Seed: 3, Names: []string{"a", "b"}, Decks: decks, WindowDiagnostics: true}))
	e.Advance()
	id := windowBridgeHand(t, e, "Zoetic Cavern")
	if id == 0 || !hasInHand(e, id) {
		t.Fatal("precondition failed: Morph land not in hand")
	}
	toMain1(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("precondition failed: no seat-0 priority in main: %+v", d)
	}
	var land *decision.Option
	for i := range d.Options {
		if d.Options[i].Kind == "play_land" && d.Options[i].Obj != id {
			land = &d.Options[i]
			break
		}
	}
	if land == nil {
		t.Fatalf("precondition failed: no other land to spend drop: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{land.Index}}); err != nil {
		t.Fatalf("play land: %v", err)
	}
	if e.G.Players[0].LandsPlayed != 1 || !hasInHand(e, id) {
		t.Fatalf("precondition failed: land drop=%d; cavern in hand=%v", e.G.Players[0].LandsPlayed, hasInHand(e, id))
	}
	addManaQuiet(t, e, 0, "CCCC")
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("precondition failed: no seat-0 priority after mana: %+v", d)
	}
	castOffered := false
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id && o.Mode == "morphed" {
			castOffered = true
		}
	}
	if !castOffered {
		t.Fatalf("precondition failed: face-down cast not offered: %+v", d.Options)
	}
	for _, r := range d.WindowReasons {
		if r.Obj == id {
			t.Fatalf("offered Morph land %d retained withholding reason: %+v", id, r)
		}
	}
}

func hasInHand(e *Engine, id state.ObjID) bool {
	for _, cand := range e.G.Zone(state.ZHand, 0) {
		if cand == id {
			return true
		}
	}
	return false
}

// TestWindowDiagnosticsTimingNotMain: a sorcery in hand during a non-main step
// is withheld for timing, not for cost, even though it is also unaffordable --
// the gate-order contract.
func TestWindowDiagnosticsTimingNotMain(t *testing.T) {
	t.Parallel()
	sorcery := card(t, "Name:Expensive Sorcery\nManaCost:9 R\nTypes:Sorcery\nOracle:x\n")
	decks := [][]*cards.Card{append([]*cards.Card{sorcery}, mountainDeck(t, 39)...), mountainDeck(t, 40)}
	e := New(seatZeroStart(Config{Seed: 11, Names: []string{"a", "b"}, Decks: decks, WindowDiagnostics: true}))
	e.Advance()
	id := windowBridgeHand(t, e, "Expensive Sorcery")
	if id == 0 {
		t.Fatal("precondition failed: sorcery not in hand")
	}
	// Walk to seat 0's end step, a non-main step the active seat still gets
	// priority in.
	driveToStep(t, e, 1, 0, state.StepEnd)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("no seat-0 priority decision at end step: %+v", d)
	}
	if e.G.Step.IsMain() {
		t.Fatalf("precondition failed: step %s is a main phase", e.G.Step)
	}
	if !hasInHand(e, id) {
		t.Fatalf("precondition failed: sorcery not in hand")
	}
	if hasOptionFor(d, id) {
		t.Fatalf("sorcery offered outside a main phase: %+v", d.Options)
	}
	r, ok := windowReasonFor(d, id, windowKindCard)
	if !ok {
		t.Fatalf("no card reason for %d: %+v", id, d.WindowReasons)
	}
	if r.Reason != wrTimingNotMain {
		t.Fatalf("reason=%q want %q (cost:insufficient_mana would be the wrong, later gate)", r.Reason, wrTimingNotMain)
	}
}

// TestWindowDiagnosticsInsufficientMana: an affordable-timing sorcery with no
// mana is withheld for cost:insufficient_mana.
func TestWindowDiagnosticsInsufficientMana(t *testing.T) {
	t.Parallel()
	sorcery := card(t, "Name:Costly Spell\nManaCost:5 R\nTypes:Sorcery\nOracle:x\n")
	decks := [][]*cards.Card{append([]*cards.Card{sorcery}, mountainDeck(t, 39)...), mountainDeck(t, 40)}
	e := New(seatZeroStart(Config{Seed: 13, Names: []string{"a", "b"}, Decks: decks, WindowDiagnostics: true}))
	e.Advance()
	id := windowBridgeHand(t, e, "Costly Spell")
	if id == 0 {
		t.Fatal("precondition failed: spell not in hand")
	}
	toMain1(t, e)
	// Drain any pool so the cost is unpayable.
	e.G.Players[0].Pool = state.Mana{}
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("no seat-0 priority decision: %+v", d)
	}
	if hasOptionFor(d, id) {
		t.Fatalf("unaffordable spell offered: %+v", d.Options)
	}
	r, ok := windowReasonFor(d, id, windowKindCard)
	if !ok {
		t.Fatalf("no card reason for %d: %+v", id, d.WindowReasons)
	}
	if r.Reason != wrCostInsufficientMana {
		t.Fatalf("reason=%q want %q", r.Reason, wrCostInsufficientMana)
	}
}

// TestWindowDiagnosticsNoLegalTarget: a targeted sorcery with no legal target
// is withheld for target:no_legal_target.
func TestWindowDiagnosticsNoLegalTarget(t *testing.T) {
	t.Parallel()
	bolt := card(t, "Name:Test Bolt\nManaCost:R\nTypes:Sorcery\nA:SP$ DealDamage | Cost$ R | ValidTgts$ Creature | NumDmg$ 3 | SpellDescription$ x\nOracle:x\n")
	decks := [][]*cards.Card{append([]*cards.Card{bolt}, mountainDeck(t, 39)...), mountainDeck(t, 40)}
	e := New(seatZeroStart(Config{Seed: 17, Names: []string{"a", "b"}, Decks: decks, WindowDiagnostics: true}))
	e.Advance()
	id := windowBridgeHand(t, e, "Test Bolt")
	if id == 0 {
		t.Fatal("precondition failed: bolt not in hand")
	}
	toMain1(t, e)
	addManaQuiet(t, e, 0, "R")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("no seat-0 priority decision: %+v", d)
	}
	// Precondition: no creature exists for it to target.
	for _, p := range e.G.AliveFrom(0) {
		for _, cand := range e.G.Zone(state.ZBattlefield, p) {
			if e.G.Obj(cand).Face() != nil && e.G.Obj(cand).Face().IsCreature() {
				t.Fatalf("precondition failed: creature %d on battlefield", cand)
			}
		}
	}
	if hasOptionFor(d, id) {
		t.Fatalf("targetless bolt offered: %+v", d.Options)
	}
	r, ok := windowReasonFor(d, id, windowKindCard)
	if !ok {
		t.Fatalf("no card reason for %d: %+v", id, d.WindowReasons)
	}
	if r.Reason != wrTargetNoLegalTarget {
		t.Fatalf("reason=%q want %q", r.Reason, wrTargetNoLegalTarget)
	}
}

// TestWindowDiagnosticsActivationLimitReached: an ability with ActivationLimit$ 1
// that has already been activated is withheld for activation:limit_reached,
// and the entry names the source object.
func TestWindowDiagnosticsActivationLimitReached(t *testing.T) {
	t.Parallel()
	src := "Name:Limiter\nManaCost:1\nTypes:Artifact\nA:AB$ Draw | Cost$ 1 | NumCards$ 1 | Defined$ You | ActivationLimit$ 1 | SpellDescription$ Draw a card.\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 21, src)
	e.windowDiagnostics = true
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	addManaQuiet(t, e, 0, "CCCC")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision: %+v", d)
	}
	var opt decision.Option
	ok := false
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id {
			opt, ok = o, true
			break
		}
	}
	if !ok {
		t.Fatalf("precondition failed: ability not offered before use: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("activate: %v", err)
	}
	// Resolve the ability and any follow-ups back to priority.
	for i := 0; i < 50; i++ {
		d = e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			break
		}
		pick := d.Options[0].Index
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
			t.Fatalf("resolve: %v", err)
		}
	}
	addManaQuiet(t, e, 0, "CCCC")
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority after activation: %+v", d)
	}
	if used := e.activationUsedCount(id, 0, "", false); used < 1 {
		t.Fatalf("precondition failed: activation count=%d", used)
	}
	if findAbilityOptionOK(e, id, 0) {
		t.Fatal("used-up ability still offered")
	}
	r, ok := windowReasonFor(d, id, windowKindActivation)
	if !ok {
		t.Fatalf("no activation reason for %d: %+v", id, d.WindowReasons)
	}
	if r.Reason != wrActivationLimit {
		t.Fatalf("reason=%q want %q", r.Reason, wrActivationLimit)
	}
}

func findAbilityOptionOK(e *Engine, id state.ObjID, idx int) bool {
	d := e.Pending()
	if d == nil {
		return false
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id && o.Ability == idx {
			return true
		}
	}
	return false
}

// TestWindowDiagnosticsCollectorBoundedSortedAndClosed: the collector bounds
// entries at windowReasonLimit, sorts by (Obj, Kind, Reason), and coerces an
// unknown token to the fail-closed ability:unsupported.
func TestWindowDiagnosticsCollectorBoundedSortedAndClosed(t *testing.T) {
	t.Parallel()
	w := newWindowCollector(0)
	for i := 30; i >= 1; i-- {
		w.record(state.ObjID(i), windowKindCard, wrAbilityUnsupported)
	}
	got := w.finish(nil)
	if len(got) != windowReasonLimit {
		t.Fatalf("entries=%d want bound %d", len(got), windowReasonLimit)
	}
	for i := 1; i < len(got); i++ {
		a, b := got[i-1], got[i]
		if a.Obj > b.Obj || (a.Obj == b.Obj && a.Kind > b.Kind) ||
			(a.Obj == b.Obj && a.Kind == b.Kind && a.Reason > b.Reason) {
			t.Fatalf("entries not sorted: %+v", got)
		}
	}
	if !windowReasonTokens[wrAbilityUnsupported] {
		t.Fatal("fail-closed token not in vocabulary")
	}
}

// TestWindowDiagnosticsFirstGateWins: a candidate refused by two gates keeps
// the FIRST gate's token -- the collector's record-once contract, which is
// what makes gate order the player-facing truth.
func TestWindowDiagnosticsFirstGateWins(t *testing.T) {
	t.Parallel()
	w := newWindowCollector(0)
	w.record(5, windowKindCard, wrTimingNotMain)
	w.record(5, windowKindCard, wrCostInsufficientMana) // later gate, must be ignored
	got := w.finish(nil)
	if len(got) != 1 {
		t.Fatalf("entries=%d want 1: %+v", len(got), got)
	}
	if got[0].Reason != wrTimingNotMain {
		t.Fatalf("reason=%q want %q", got[0].Reason, wrTimingNotMain)
	}
	// A different route for the same object clears all its entries, including
	// kinds not mapped to any classifier (new special actions need no switch).
	for _, offeredKind := range []string{"cast", "station", "unlock", "turn_face_up", "specialize"} {
		w2 := newWindowCollector(0)
		w2.record(5, windowKindLand, wrLandDropExhausted)
		w2.record(5, windowKindCard, wrCostInsufficientMana)
		w2.record(6, windowKindCard, wrCostUnpayable)
		out := w2.finish([]decision.Option{{Kind: offeredKind, Obj: 5}, {Kind: "pass", Obj: 0}})
		if len(out) != 1 || out[0].Obj != 6 {
			t.Fatalf("offered %q object kept a reason or cleared another: %+v", offeredKind, out)
		}
	}
}

// TestWindowDiagnosticsNilCollectorIsEmpty: with diagnostics off the collector
// is nil and the walk records nothing.
func TestWindowDiagnosticsNilCollectorIsEmpty(t *testing.T) {
	t.Parallel()
	var w *windowCollector
	w.record(1, windowKindCard, wrCostUnpayable)
	w.remove(1)
	if got := w.finish([]decision.Option{{Kind: "pass"}}); got != nil {
		t.Fatalf("nil collector returned %v", got)
	}
}

// TestWindowDiagnosticsOffHasNoSidecar: a default-off engine attaches no
// WindowReasons to any priority decision.
func TestWindowDiagnosticsOffHasNoSidecar(t *testing.T) {
	t.Parallel()
	names := []string{"a", "b"}
	decks := [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}
	e := New(seatZeroStart(Config{Seed: 5, Names: names, Decks: decks}))
	e.Advance()
	sawPriority := false
	for i := 0; i < 2000 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			sawPriority = true
			if d.WindowReasons != nil {
				t.Fatalf("default-off decision carried WindowReasons: %+v", d.WindowReasons)
			}
		}
		pick := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pick = o.Index
			}
		}
		if pick < 0 {
			pick = d.Options[0].Index
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	if !sawPriority {
		t.Fatal("precondition failed: no priority decision seen")
	}
}

// bridgeToHand moves the named deck card from seat 0's library to its hand
// (with a logged MoveZone) and re-asks, returning the id or 0.
func windowBridgeHand(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(id).Face().Name == name {
			return id
		}
	}
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if e.G.Obj(id).Face().Name == name {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
			e.pending = nil
			e.Advance()
			// Advance may land on the opponent's decision; drive to seat 0's
			// priority at the same point with a pass.
			for i := 0; i < 200; i++ {
				d := e.Pending()
				if d != nil && d.Kind == decision.KPriority && d.Player == 0 {
					break
				}
				if d == nil {
					break
				}
				pick := -1
				for _, o := range d.Options {
					if o.Kind == "pass" {
						pick = o.Index
					}
				}
				if pick < 0 {
					pick = d.Options[0].Index
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
					t.Fatalf("submit: %v", err)
				}
			}
			return id
		}
	}
	return 0
}

// addManaQuiet adds mana and re-asks without driving a step (the caller is
// already at the right step).
func addManaQuiet(t *testing.T, e *Engine, p state.PlayerID, symbols string) {
	t.Helper()
	for _, r := range symbols {
		e.emit(events.Event{Kind: events.ManaAdd, Player: p, Counter: string(r), Amount: 1})
	}
	e.priorityRound()
}

var _ = events.MoveZone
