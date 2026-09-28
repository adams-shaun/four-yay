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

// counterchoice1: the two counter primitives whose real shape is a
// mid-resolution choice — api:AddOrRemoveCounter (Clockspinning's
// "choose a counter on target permanent or suspended card. Remove that
// counter ... or put another of those counters on it", targeting across the
// battlefield AND exile) and api:RemoveCounter's bare-Choices$ card-election
// arm (Amy Pond's "choose a suspended card you own and remove that many time
// counters from it"). Before this file's change the AddOrRemoveCounter fell
// to the generic "unimplemented API" fallback and the Choices$ RemoveCounter
// emitted one loud "unimplemented RemoveCounter shape: Choices$" Note; both
// moved nothing.
//
// Every test runs on the real compiled corpus cards (no Forge script text is
// committed), drives the ordinary ask/resume machinery, seeds every card as a
// real deck card moved with logged events, and ends replay-verified. Every
// board assertion also excludes the generic "unimplemented" notes, so an
// unregistered primitive cannot pass silently.

// counterChoiceGame builds a 2-seat game whose seat-0 deck holds the given
// cards (real deck cards, so genesis creates them and a replay reconstructs
// them at the same ids) over mountains, parked at seat 0's turn-1 Main1.
func counterChoiceGame(t *testing.T, seed uint64, seat0 ...*cards.Card) (*Engine, Config) {
	t.Helper()
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append(seat0, mountainDeck(t, 40-len(seat0))...),
			mountainDeck(t, 40),
		},
		Tokens: testutil.CorpusRegistry(t).Tokens,
	}
	e := New(seatZeroStart(cfg))
	e.Advance()
	return e, cfg
}

// counterChoiceGamePre is counterChoiceGame without the opening Advance, for
// a fixture that must emit moves while no priority decision is pending (the
// CR 310.10 Siege-protector replacement skips an entry while any decision is
// outstanding).
func counterChoiceGamePre(t *testing.T, seed uint64, seat0 ...*cards.Card) (*Engine, Config) {
	t.Helper()
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append(seat0, mountainDeck(t, 40-len(seat0))...),
			mountainDeck(t, 40),
		},
		Tokens: testutil.CorpusRegistry(t).Tokens,
	}
	e := New(seatZeroStart(cfg))
	return e, cfg
}

// aorMoveTo moves a seeded object to to with a logged MoveZone whose From is
// the object's ACTUAL zone (genesis shuffles the deck, so a fixture card can
// sit in the library just as well as in the hand — a From mismatch would
// silently move nothing).
func aorMoveTo(t *testing.T, e *Engine, id state.ObjID, to state.Zone) {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil {
		t.Fatalf("no object %d to move", id)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: to})
}

// findSeededCard finds seat p's copy of the named card in hand or library.
func findSeededCard(t *testing.T, e *Engine, p state.PlayerID, name string) (state.ObjID, state.Zone) {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				return id, z
			}
		}
	}
	t.Fatalf("seat %d has no %q in hand or library", p, name)
	return 0, 0
}

// suspendCardInExile turns a real deck card into a suspended card with the
// exact logged event sequence the real Suspend action emits (payCast's
// suspend branch: CastInfo carrying the FlagSuspend provenance, the exile
// move marked "suspended", then the TIME counters), returning the exile
// object.
func suspendCardInExile(t *testing.T, e *Engine, id state.ObjID, from state.Zone, time int32) *state.Object {
	t.Helper()
	if o := e.G.Obj(id); o == nil || o.Zone != from {
		t.Fatalf("suspend source not in %s: %+v", from, e.G.Obj(id))
	}
	e.emit(events.Event{Kind: events.CastInfo, Obj: id, Counter: events.FlagsString(state.FlagSuspend)})
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: state.ZExile, Text: "suspended"})
	if time > 0 {
		e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "TIME", Amount: time})
	}
	o := e.G.Obj(id)
	if o.Zone != state.ZExile || o.CastFlags&state.FlagSuspend == 0 || o.Counter("TIME") != time {
		t.Fatalf("suspended card fabrication incomplete: %+v", o)
	}
	return o
}

// assertNoCounterUnimplementedNote fails when the log carries a generic
// "unimplemented" Note naming either primitive — the "feature ran" guard: a
// test asserting only board state would pass with the whole registration
// reverted.
func assertNoCounterUnimplementedNote(t *testing.T, e *Engine) {
	t.Helper()
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && (strings.Contains(ev.Text, "unimplemented API AddOrRemoveCounter") ||
			strings.Contains(ev.Text, "unimplemented RemoveCounter shape") ||
			strings.Contains(ev.Text, "unimplemented AddOrRemoveCounter shape")) {
			t.Fatalf("unimplemented-shape note in the log: %q", ev.Text)
		}
	}
}

// aorTargetOption answers the pending KTarget decision with the option naming
// target and returns whether such an option existed.
func aorTargetOption(t *testing.T, e *Engine, target state.ObjID) bool {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == target {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("submit target: %v", err)
			}
			return true
		}
	}
	return false
}

// aorPassPriorityOnce answers one pending priority decision with its pass
// option (passUntilNonPriority's step, inlined for a loop that must also
// answer other decision kinds).
func aorPassPriorityOnce(t *testing.T, e *Engine, d *decision.Decision) {
	t.Helper()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "pass" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("priority decision with no pass option: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit pass: %v", err)
	}
}

// clockspinningFixtureBear is the authored battlefield counter carrier.
const clockspinningFixtureBear = "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// TestClockspinningAddOrRemoveCounterChoice is the brief's headline: the
// instant targets across TgtZone$ Battlefield,Exile (the ask offers BOTH the
// battlefield permanent and the suspended card), enumerates the chosen
// target's counters (the suspended card carries only TIME, so no kind pick —
// the strict-supersets rule), and the answered PUT election adds the counter.
func TestClockspinningAddOrRemoveCounterChoice(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	clock, _ := reg.Lookup("Clockspinning")
	profane, _ := reg.Lookup("Profane Tutor")
	e, cfg := counterChoiceGame(t, 4711, card(t, clockspinningFixtureBear), clock, profane)
	bearID, _ := findSeededCard(t, e, 0, "Bear")
	aorMoveTo(t, e, bearID, state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "P1P1", Amount: 1})
	if e.G.Obj(bearID).Counter("P1P1") != 1 {
		t.Fatalf("bear precondition: P1P1 = %d, want 1", e.G.Obj(bearID).Counter("P1P1"))
	}
	profaneID, zone := findSeededCard(t, e, 0, "Profane Tutor")
	suspendCardInExile(t, e, profaneID, zone, 2)

	clockID, _ := findSeededCard(t, e, 0, "Clockspinning")
	addMana(t, e, 0, "U")
	castFromPriority(t, e, clockID)
	d0 := e.Pending()
	if d0 == nil || d0.Kind != decision.KTarget {
		t.Fatalf("clockspinning cast did not pose its target ask: %+v", d0)
	}
	// The census spans both zones: one battlefield bear, one suspended card.
	if len(d0.Options) != 2 {
		t.Fatalf("target ask offers %d options, want the bear and the suspended card: %+v", len(d0.Options), d0.Options)
	}
	foundBear, foundSuspend := false, false
	for _, o := range d0.Options {
		if o.Obj == bearID {
			foundBear = true
		}
		if o.Obj == profaneID {
			foundSuspend = true
		}
	}
	if !foundBear || !foundSuspend {
		t.Fatalf("target options missing a zone half: %+v", d0.Options)
	}
	for _, o := range d0.Options {
		if o.Obj == profaneID {
			if err := e.Submit(decision.Intent{Seq: d0.Seq, Player: d0.Player,
				Choices: []int{o.Index}}); err != nil {
				t.Fatalf("submit target: %v", err)
			}
			break
		}
	}

	// The election: TIME is the only counter the suspended card carries, so
	// the kind pick is skipped (strict-supersets) and the add/remove election
	// asks directly.
	d := passUntilNonPriority(t, e, 60)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "aor_elect" {
		t.Fatalf("no aor_election after targeting the suspended card: %+v", d)
	}
	if d.Player != 0 || d.Min != 1 || d.Max != 1 || len(d.Options) != 2 {
		t.Fatalf("election ask shape = seat %d %d..%d, %d options: %+v", d.Player, d.Min, d.Max, len(d.Options), d.Options)
	}
	if d.Options[0].Kind != "aor_remove:TIME" || d.Options[1].Kind != "aor_put:TIME" {
		t.Fatalf("election options not the TIME pair: %+v", d.Options)
	}
	// Answer PUT: another time counter lands on the suspended card.
	submitChoices(t, e, d.Options[1].Index)
	passUntilStackEmpty(t, e, 60)
	if got := e.G.Obj(profaneID).Counter("TIME"); got != 3 {
		t.Fatalf("answered PUT left TIME = %d, want 3", got)
	}
	assertNoCounterUnimplementedNote(t, e)
	replayCheck(t, e, cfg)
}

// TestClockspinningKindPickWhenSeveralCounters pins the absent-kind shape on
// a TWO-kind target: one combined ask over (remove|put) × the kinds present
// (four options, ResumeKind "aor_elect"), the answered REMOVE-TIME option
// removes a time counter, and the unpicked P1P1 kind is left alone.
func TestClockspinningKindPickWhenSeveralCounters(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	clock, _ := reg.Lookup("Clockspinning")
	e, cfg := counterChoiceGame(t, 4712, card(t, clockspinningFixtureBear), clock)
	bearID, _ := findSeededCard(t, e, 0, "Bear")
	aorMoveTo(t, e, bearID, state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "P1P1", Amount: 1})
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "TIME", Amount: 2})

	clockID, _ := findSeededCard(t, e, 0, "Clockspinning")
	addMana(t, e, 0, "U")
	castFromPriority(t, e, clockID)
	if !aorTargetOption(t, e, bearID) {
		t.Fatalf("target ask offers no bear option: %+v", e.Pending())
	}
	d := passUntilNonPriority(t, e, 60)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "aor_elect" {
		t.Fatalf("no combined counter pick on a two-kind target: %+v", d)
	}
	if len(d.Options) != 4 ||
		d.Options[0].Kind != "aor_remove:P1P1" || d.Options[1].Kind != "aor_put:P1P1" ||
		d.Options[2].Kind != "aor_remove:TIME" || d.Options[3].Kind != "aor_put:TIME" {
		t.Fatalf("combined pick options = %+v, want (remove|put) x kinds in slice order", d.Options)
	}
	// Pick REMOVE-TIME (the third option).
	submitChoices(t, e, d.Options[2].Index)
	passUntilStackEmpty(t, e, 60)
	o := e.G.Obj(bearID)
	if o.Counter("TIME") != 1 || o.Counter("P1P1") != 1 {
		t.Fatalf("after removing TIME: TIME=%d P1P1=%d, want 1 and 1 (the unpicked kind untouched)", o.Counter("TIME"), o.Counter("P1P1"))
	}
	assertNoCounterUnimplementedNote(t, e)
	replayCheck(t, e, cfg)
}

// amyPondGame builds the Amy Pond combat fixture: Amy Pond on seat 0's
// battlefield, nSuspended corpus Profane Tutor copies suspended in exile
// (TIME counters each), plus one NON-suspended exiled Profane Tutor copy —
// eligible for nothing. It returns the engine, config, Amy's id and the
// suspended cards' ids in exile zone order.
func amyPondGame(t *testing.T, seed uint64, nSuspended int, time int32) (*Engine, Config, state.ObjID, []state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	amy, _ := reg.Lookup("Amy Pond")
	profane, _ := reg.Lookup("Profane Tutor")
	seat0 := []*cards.Card{card(t, clockspinningFixtureBear), amy}
	for i := 0; i <= nSuspended; i++ {
		seat0 = append(seat0, profane) // nSuspended to suspend + 1 plain
	}
	e, cfg := counterChoiceGame(t, seed, seat0...)
	amyID, _ := findSeededCard(t, e, 0, "Amy Pond")
	aorMoveTo(t, e, amyID, state.ZBattlefield)
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Zone == state.ZBattlefield && o.Owner == 0 &&
			o.Face() != nil && o.Face().IsCreature() {
			o.SummonSick = false
		}
	}
	var profaneIDs []state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Face() != nil && o.Face().Name == "Profane Tutor" &&
			(o.Zone == state.ZHand || o.Zone == state.ZLibrary) {
			profaneIDs = append(profaneIDs, o.ID)
		}
	}
	if len(profaneIDs) != nSuspended+1 {
		t.Fatalf("found %d Profane Tutor copies, want %d", len(profaneIDs), nSuspended+1)
	}
	var suspended []state.ObjID
	for i := 0; i < nSuspended; i++ {
		id := profaneIDs[i]
		suspendCardInExile(t, e, id, e.G.Obj(id).Zone, time)
		suspended = append(suspended, id)
	}
	plain := profaneIDs[nSuspended]
	e.emit(events.Event{Kind: events.MoveZone, Obj: plain, From: e.G.Obj(plain).Zone, To: state.ZExile})
	if o := e.G.Obj(plain); o.Zone != state.ZExile || o.CastFlags&state.FlagSuspend != 0 {
		t.Fatalf("plain control copy not exiled un-flagged: %+v", o)
	}
	// main's kw:Partner-with expansion (CR 702.128) mints Amy Pond's ETB
	// may-search trigger ("target player may put Rory into their hand"), so
	// Amy's battlefield entry now queues a trigger with a ValidTgts$ Player
	// target ask. Answer it — target seat 0, Amy's controller — and decline
	// the targeted player's Optional$ may-find (Min 0: submit no choices),
	// leaving the fixture quiescent exactly as it was before the expansion
	// landed (Rory Williams is in neither deck, so the decline is forced).
	d := passUntilNonPriority(t, e, 30)
	if d.Kind != decision.KTarget {
		t.Fatalf("Amy Pond's Partner-with ETB did not ask its target player: %+v", d)
	}
	p0 := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 0 {
			p0 = o.Index
		}
	}
	if p0 < 0 {
		t.Fatalf("Partner-with target ask offers no player-0 option: %+v", d.Options)
	}
	submitChoices(t, e, p0)
	// Forge's confirmAction gate for the Optional$ ChangeZone asks before the
	// fetch list even when the named card is absent (the CR 701.23b
	// fail-to-find split still confirms first), so decline -- Rory Williams is
	// in neither deck, and accepting would reach the same empty answer anyway.
	// Stop on the resumed priority; the fixture must not pass further and
	// advance the turn.
	c := passUntilNonPriority(t, e, 30)
	if c.Kind != decision.KChoose || c.ResumeKind != "search_confirm" {
		t.Fatalf("Amy Pond's Partner-with search confirmation = %+v, want the Optional$ gate", c)
	}
	submitChoices(t, e, c.Options[1].Index) // no
	return e, cfg, amyID, suspended
}

// amyPondAttack drives Amy Pond's unblocked attack through the combat damage
// step (2 damage to the defender), then passes priority until the trigger's
// card-election ask appears (returned) or the stack empties (nil returned —
// the single-eligible strict-supersets shape, where the deterministic act
// removed without an ask).
func amyPondAttack(t *testing.T, e *Engine, amyID state.ObjID) *decision.Decision {
	t.Helper()
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	e.askAttackers()
	submitAttackers(t, e, amyID)
	drainCombatDamagePriority(t, e)
	for i := 0; i < 80 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			return nil
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "counter_pick" {
			return d
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "search_confirm" {
			// Amy Pond's own Partner-with Rory Williams is a pending
			// Optional$ library search: Forge's confirmAction gate now poses
			// before the fetch. Accept it so the drive reaches Amy's
			// counter_pick, exactly the pre-confirmation flow.
			submitChoices(t, e, d.Options[0].Index)
			continue
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected non-priority decision while driving Amy's trigger: %+v", d)
		}
		aorPassPriorityOnce(t, e, d)
		if len(e.G.Stack) == 0 {
			return nil
		}
	}
	t.Fatal("Amy Pond's trigger never resolved within the drive budget")
	return nil
}

// TestAmyPondRemovesChosenSuspendedCounters is the brief's second pin, end to
// end on the real corpus card: Amy Pond deals 2 combat damage to a player,
// the trigger asks the CHOICE over the TWO suspended cards you own in exile
// (the shared "counter_pick" arm), and the answer removes that many time
// counters from the chosen card only. Two of its three TIME counters come
// off; the other card keeps all three, and the NON-suspended exiled copy is
// never offered.
func TestAmyPondRemovesChosenSuspendedCounters(t *testing.T) {
	t.Parallel()
	e, cfg, amyID, suspended := amyPondGame(t, 4713, 2, 3)
	d := amyPondAttack(t, e, amyID)
	if d.Min != 1 || d.Max != 1 || d.Player != 0 {
		t.Fatalf("election range/player = %d..%d seat %d, want 1..1 seat 0", d.Min, d.Max, d.Player)
	}
	if len(d.Options) != 2 {
		t.Fatalf("election options = %+v, want exactly the two suspended cards (the plain exiled copy must not be offered)", d.Options)
	}
	for i, o := range d.Options {
		if o.Obj != suspended[i] {
			t.Fatalf("option %d = obj %d, want exile-order suspended card %d", i, o.Obj, suspended[i])
		}
	}
	// Answer with the SECOND card (the deterministic first pick would remove
	// from suspended[0]).
	submitChoices(t, e, d.Options[1].Index)
	passUntilStackEmpty(t, e, 60)
	if got := e.G.Obj(suspended[1]).Counter("TIME"); got != 1 {
		t.Fatalf("chosen card TIME = %d, want 1 (X=2 removed of 3)", got)
	}
	if got := e.G.Obj(suspended[0]).Counter("TIME"); got != 3 {
		t.Fatalf("unchosen card TIME = %d, want 3 (untouched)", got)
	}
	assertNoCounterUnimplementedNote(t, e)
	replayCheck(t, e, cfg)
}

// TestAmyPondFinalCounterOffersSuspendCast pins the emit-hook half of the
// same card: removing the LAST time counter from a suspended card queues CR
// 702.62a's may-cast offer at the next step — the route the upkeep tick used
// to be the only feeder of, which stranded a zero-TIME card in exile forever
// when the removal came mid-resolution.
func TestAmyPondFinalCounterOffersSuspendCast(t *testing.T) {
	t.Parallel()
	e, cfg, amyID, suspended := amyPondGame(t, 4714, 1, 2)
	profaneID := suspended[0]
	// Exactly one SUSPENDED card is eligible (the plain exiled copy is not),
	// so the election is the strict-supersets deterministic act — the ask
	// nobody could answer differently is never posed — and the removal still
	// happens.
	amyPondAttack(t, e, amyID)
	if d := e.Pending(); d != nil && d.ResumeKind == "counter_pick" {
		t.Fatalf("single-eligible exact-1 election posed an ask (strict-supersets): %+v", d)
	}
	// X=2 removes BOTH time counters; the may-cast offer must surface (the
	// drain runs at the next step boundary, through the combat/priority
	// drive). Drive until it appears or the flow wedges.
	sawCast := false
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if len(d.Options) == 2 && d.Options[0].Kind == "suspend_cast_yes" {
			sawCast = true
			break
		}
		if d.Kind == decision.KPriority {
			aorPassPriorityOnce(t, e, d)
			continue
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	if !sawCast {
		t.Fatalf("removing the last TIME counter never offered the CR 702.62a may-cast ask (final pending %+v)", e.Pending())
	}
	if got := e.G.Obj(profaneID).Counter("TIME"); got != 0 {
		t.Fatalf("suspended card TIME = %d, want 0", got)
	}
	assertNoCounterUnimplementedNote(t, e)
	replayCheck(t, e, cfg)
}

// TestEtchedHostConditionRemovesDefenseFromAnOppProtectedBattle pins the
// RemoveConditionSVar$ shape end to end (Etched Host Doombringer's charm mode
// 2): the condition `Targeted$Valid Battle.OppProtect` — the wordOppProtect
// predicate over the battle's CR 310.10 protector — resolves TRUE for seat
// 0's own battle (its protector is seat 1, an opponent of the resolving
// controller), so THREE defense counters are removed with no ask (the
// condition IS the choice).
func TestEtchedHostConditionRemovesDefenseFromAnOppProtectedBattle(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	host, _ := reg.Lookup("Etched Host Doombringer")
	battle, _ := reg.Lookup("Invasion of Arcavios")
	e, cfg := counterChoiceGamePre(t, 4717, card(t, clockspinningFixtureBear), host, battle)
	battleID, _ := findSeededCard(t, e, 0, "Invasion of Arcavios")
	aorMoveTo(t, e, battleID, state.ZBattlefield)
	bo := e.G.Obj(battleID)
	if !bo.ProtectorValid || bo.Protector != 1 {
		t.Fatalf("precondition: battle protector = %+v (valid=%v), want seat 1 auto-chosen in a 2-seat game", bo.Protector, bo.ProtectorValid)
	}
	e.Advance()
	if got := bo.Counter("DEFENSE"); got != 7 {
		t.Fatalf("precondition: battle defense = %d, want the printed 7", got)
	}
	// Invasion's ETB library search finds no instant or sorcery in this
	// mountain-only fixture. It now asks whether to shuffle even when nothing
	// was found; decline before driving to main1 to cast the Host.
	searchShuffle := passUntilNonPriority(t, e, 60)
	if searchShuffle.Kind != decision.KChoose || searchShuffle.ResumeKind != "search_mayshuffle" ||
		searchShuffle.Player != 0 || len(searchShuffle.Options) != 2 || searchShuffle.Options[1].Kind != "no" {
		t.Fatalf("Invasion's empty-search shuffle confirm = %+v", searchShuffle)
	}
	submitChoices(t, e, searchShuffle.Options[1].Index)
	passUntilStackEmpty(t, e, 60)
	hostID, _ := findSeededCard(t, e, 0, "Etched Host Doombringer")
	addMana(t, e, 0, "BBBBB")
	castFromPriority(t, e, hostID)
	// The ETB charm's mode pick: mode 1 (index 1) is the battle mode.
	d := passUntilNonPriority(t, e, 60)
	if d == nil || d.Kind != decision.KModes || len(d.Options) != 2 {
		t.Fatalf("no charm mode pick: %+v", d)
	}
	submitChoices(t, e, d.Options[1].Index)
	// The battle target ask.
	d = passUntilNonPriority(t, e, 60)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no battle target ask: %+v", d)
	}
	if !aorTargetOption(t, e, battleID) {
		t.Fatalf("target ask offers no battle option: %+v", e.Pending())
	}
	passUntilStackEmpty(t, e, 60)
	if got := e.G.Obj(battleID).Counter("DEFENSE"); got != 4 {
		t.Fatalf("after the condition-true REMOVE: defense = %d, want 4 (7 minus 3)", got)
	}
	assertNoCounterUnimplementedNote(t, e)
	replayCheck(t, e, cfg)
}

// TestShapeOfTheWiitigoConditionFalsePuts pins the condition shape's false
// arm on the real card: the aura's upkeep trigger evaluates
// `Count$Valid Card.EnchantedBy+!attackedOrBlockedSinceYourLastUpkeep` — an
// unknown predicate fails closed to an EMPTY match, so the count resolves
// (ok) to 0, the condition is false, and the PUT arm runs with no ask. This
// is the documented degradation for that carrier (oracle: an attacker gains,
// a non-attacker loses — the unmodelled combat-history predicate means the
// non-attacker's remove half is unreachable), pinned here so a silent
// regression to a loud unimplemented note is caught.
func TestShapeOfTheWiitigoConditionFalsePuts(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	wiitigo, _ := reg.Lookup("Shape of the Wiitigo")
	e, cfg := counterChoiceGame(t, 4718, card(t, clockspinningFixtureBear), wiitigo)
	bearID, _ := findSeededCard(t, e, 0, "Bear")
	aorMoveTo(t, e, bearID, state.ZBattlefield)
	wID, _ := findSeededCard(t, e, 0, "Shape of the Wiitigo")
	addMana(t, e, 0, "CCGGGG")
	castFromPriority(t, e, wID)
	if !aorTargetOption(t, e, bearID) {
		t.Fatalf("aura cast target ask offers no bear option: %+v", e.Pending())
	}
	passUntilStackEmpty(t, e, 60)
	if got := e.G.Obj(bearID).Counter("P1P1"); got != 6 {
		t.Fatalf("precondition: enchanted bear carries %d P1P1, want the ETB's 6", got)
	}
	// Seat 0's next upkeep: the Phase trigger fires, the condition resolves
	// false, and the PUT arm adds one — no ask anywhere. seat 1's turn
	// intervenes; its combat decisions are answered with empty declarations
	// (no attackers, no damage).
	driveToStepAll(t, e, 3, 0, state.StepUpkeep)
	d := passUntilNonPriority(t, e, 60)
	if d != nil && d.ResumeKind == "aor_elect" {
		t.Fatalf("condition shape posed an election ask (the condition IS the choice): %+v", d)
	}
	passUntilStackEmpty(t, e, 60)
	if got := e.G.Obj(bearID).Counter("P1P1"); got != 7 {
		t.Fatalf("after the condition-false PUT: P1P1 = %d, want 7", got)
	}
	assertNoCounterUnimplementedNote(t, e)
	replayCheck(t, e, cfg)
}

// TestDramatistsPuppetElectsForEachKind pins EachExistingCounter$: the ETB
// trigger elects for EVERY kind the target carries (two elections for a
// two-kind target), each answered independently, and the resolution completes
// — the per-kind cursor (rules' aorAsk pending map) must not re-ask an
// already-answered PUT kind, whose count is still positive and therefore
// still enumerates.
func TestDramatistsPuppetElectsForEachKind(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	puppet, _ := reg.Lookup("Dramatist's Puppet")
	e, cfg := counterChoiceGame(t, 4715, card(t, clockspinningFixtureBear), puppet)
	bearID, _ := findSeededCard(t, e, 0, "Bear")
	aorMoveTo(t, e, bearID, state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "P1P1", Amount: 1})
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "TIME", Amount: 1})
	puppetID, zone := findSeededCard(t, e, 0, "Dramatist's Puppet")
	e.emit(events.Event{Kind: events.MoveZone, Obj: puppetID, From: zone, To: state.ZBattlefield})

	d := passUntilNonPriority(t, e, 60)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no trigger target ask: %+v", d)
	}
	if !aorTargetOption(t, e, bearID) {
		t.Fatalf("trigger target ask offers no bear option: %+v", e.Pending())
	}
	// Election 1 (P1P1): answer PUT.
	d = passUntilNonPriority(t, e, 60)
	if d == nil || d.ResumeKind != "aor_elect" || len(d.Options) != 2 ||
		d.Options[0].Kind != "aor_remove:P1P1" {
		t.Fatalf("first election not about P1P1: %+v", d)
	}
	submitChoices(t, e, d.Options[1].Index)
	// Election 2 (TIME): answer REMOVE.
	d = passUntilNonPriority(t, e, 60)
	if d == nil || d.ResumeKind != "aor_elect" || len(d.Options) != 2 ||
		d.Options[0].Kind != "aor_remove:TIME" {
		t.Fatalf("second election not about TIME: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 60)
	o := e.G.Obj(bearID)
	if o.Counter("P1P1") != 2 || o.Counter("TIME") != 0 {
		t.Fatalf("after the two elections: P1P1=%d TIME=%d, want 2 and 0", o.Counter("P1P1"), o.Counter("TIME"))
	}
	assertNoCounterUnimplementedNote(t, e)
	replayCheck(t, e, cfg)
}

// TestDramatistsPuppetCloneKeepsTheAnsweredKindCursor pins the clone half of
// the aorAsk discipline: the per-kind cursor lives in Engine scratch, so a
// Clone taken while the resolution is suspended on its SECOND election (the
// pending ask is an intent boundary) must carry the first answered kind
// forward. Without the copy the clone re-asks the already-answered PUT kind
// the moment the second election is answered — a decision and event stream
// the original never produces (counterchoice1 round 2).
func TestDramatistsPuppetCloneKeepsTheAnsweredKindCursor(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	puppet, _ := reg.Lookup("Dramatist's Puppet")
	e, cfg := counterChoiceGame(t, 4715, card(t, clockspinningFixtureBear), puppet)
	bearID, _ := findSeededCard(t, e, 0, "Bear")
	aorMoveTo(t, e, bearID, state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "P1P1", Amount: 1})
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearID, Counter: "TIME", Amount: 1})
	puppetID, zone := findSeededCard(t, e, 0, "Dramatist's Puppet")
	e.emit(events.Event{Kind: events.MoveZone, Obj: puppetID, From: zone, To: state.ZBattlefield})

	d := passUntilNonPriority(t, e, 60)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no trigger target ask: %+v", d)
	}
	if !aorTargetOption(t, e, bearID) {
		t.Fatalf("trigger target ask offers no bear option: %+v", e.Pending())
	}
	// Election 1 (P1P1): answer PUT.
	d = passUntilNonPriority(t, e, 60)
	if d == nil || d.ResumeKind != "aor_elect" || len(d.Options) != 2 ||
		d.Options[0].Kind != "aor_remove:P1P1" {
		t.Fatalf("first election not about P1P1: %+v", d)
	}
	submitChoices(t, e, d.Options[1].Index)
	// Election 2 (TIME): the resolution is now suspended on its second ask —
	// an intent boundary. Clone HERE, then answer the election on each engine
	// independently.
	d = passUntilNonPriority(t, e, 60)
	if d == nil || d.ResumeKind != "aor_elect" || len(d.Options) != 2 ||
		d.Options[0].Kind != "aor_remove:TIME" {
		t.Fatalf("second election not about TIME: %+v", d)
	}
	timeRemoveIdx := d.Options[0].Index
	c := e.Clone()
	if cp := c.Pending(); cp == nil || cp.ResumeKind != "aor_elect" || cp.Options[0].Kind != "aor_remove:TIME" {
		t.Fatalf("precondition: clone did not carry the pending TIME election: %+v", cp)
	}

	// The CLONE: answer TIME-remove, then the resolution must drain with no
	// further ask — P1P1 (now 2, still positive) is already answered, so the
	// next decision must be plain priority.
	submitChoices(t, c, timeRemoveIdx)
	if pd := c.Pending(); pd == nil || pd.Kind != decision.KPriority {
		t.Fatalf("clone posed an extra ask after the answered TIME election: %+v", pd)
	}
	passUntilStackEmpty(t, c, 60)
	co := c.G.Obj(bearID)
	if co.Counter("P1P1") != 2 || co.Counter("TIME") != 0 {
		t.Fatalf("clone after both elections: P1P1=%d TIME=%d, want 2 and 0", co.Counter("P1P1"), co.Counter("TIME"))
	}
	assertNoCounterUnimplementedNote(t, c)

	// The ORIGINAL, answered identically, must reach the same end state and
	// likewise never re-ask — the clone must not have drained it.
	submitChoices(t, e, timeRemoveIdx)
	if pd := e.Pending(); pd == nil || pd.Kind != decision.KPriority {
		t.Fatalf("original posed an extra ask after the answered TIME election: %+v", pd)
	}
	passUntilStackEmpty(t, e, 60)
	o := e.G.Obj(bearID)
	if o.Counter("P1P1") != 2 || o.Counter("TIME") != 0 {
		t.Fatalf("original after both elections: P1P1=%d TIME=%d, want 2 and 0", o.Counter("P1P1"), o.Counter("TIME"))
	}
	assertNoCounterUnimplementedNote(t, e)
	replayCheck(t, e, cfg)
	replayCheck(t, c, cfg)
}

// TestPlagueBoilerElectionPuts pins the plain named-kind election (no
// condition, no Optional$): the {1}{B}{G} activation asks remove-or-put and
// the answered PUT lands one plague counter.
func TestPlagueBoilerElectionPuts(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	boilerCard, _ := reg.Lookup("Plague Boiler")
	e, cfg := counterChoiceGame(t, 4716, boilerCard)
	boilerID, _ := findSeededCard(t, e, 0, "Plague Boiler")
	aorMoveTo(t, e, boilerID, state.ZBattlefield)
	if o := e.G.Obj(boilerID); o.Counter("PLAGUE") != 0 {
		t.Fatalf("precondition: boiler carries %d plague counters, want 0", o.Counter("PLAGUE"))
	}
	addMana(t, e, 0, "CBGG")
	opt := abilityOption(t, e, boilerID, 0)
	submitChoices(t, e, opt.Index)
	d := passUntilNonPriority(t, e, 60)
	if d == nil || d.ResumeKind != "aor_elect" || len(d.Options) != 2 ||
		d.Options[0].Kind != "aor_remove:PLAGUE" {
		t.Fatalf("no PLAGUE election: %+v", d)
	}
	submitChoices(t, e, d.Options[1].Index) // put
	passUntilStackEmpty(t, e, 60)
	if o := e.G.Obj(boilerID); o.Counter("PLAGUE") != 1 {
		t.Fatalf("after PUT: %d plague counters, want 1", o.Counter("PLAGUE"))
	}
	assertNoCounterUnimplementedNote(t, e)
	replayCheck(t, e, cfg)
}
