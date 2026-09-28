// Task m32: CR 903.9, a commander's return to the command zone. A commander
// about to be put into its owner's graveyard, hand or library from anywhere,
// or exiled from anywhere, may instead be put into the command zone by its
// OWNER -- a replacement effect that applies before the object would change
// zones (rules/replacement.go's commanderZoneReplacementApplies +
// parkCommanderZoneMove + handleCmdZone, asked as decision.KCommanderZone).
//
// The five traps CR 903.9 trips on, each pinned by a named test below:
//   - it is a REPLACEMENT EFFECT, not a trigger that moves the card after:
//     the commander never touches the destination zone, and the choice is
//     asked of the owner before anything moves;
//   - "may": the owner may decline, and a decline changes nothing (the
//     original zone change happens unchanged -- and must not re-offer);
//   - "from anywhere": battlefield, stack, graveyard, hand and library are
//     all sources (there is no From restriction);
//   - the four destinations are graveyard, hand, library and exile: a
//     commander moving to the battlefield or the stack is not replaced;
//   - ownership, not control: a stolen commander goes to its owner's
//     command zone and its owner is asked.
//
// Everything is gated on Config.Format == FormatCommander (a Constructed
// game, even one carrying a Commanders config, never runs any of it), and
// the choice is a recorded intent with the resulting MoveZone in the log,
// so a log-only replay reproduces both branches.
package rules

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// tinyCmdSrc is a minimal 1/1 legendary creature commander: {0} cost, no
// abilities, no targets -- the shape every CR 903.9 test here needs (cheap
// to park anywhere, lethal after one point of damage, decision-free).
const tinyCmdSrc = `Name:Tiny
ManaCost:0
Types:Legendary Creature Bear
PT:1/1
K:Partner
Oracle:x
`

// tinyCmdSrc2 is tinyCmdSrc under a DIFFERENT name: a test that fields TWO
// commanders for one player must give them different names (the m33
// convention, commander_damage_test.go) -- two identically-named legendaries
// under one controller is a board CR 704.5j forbids, and the legend rule's
// controller choice would be asked before any sweep of this test's own.
const tinyCmdSrc2 = `Name:Tiny Two
ManaCost:0
Types:Legendary Creature Bear
PT:1/1
K:Partner
Oracle:x
`

// cmdZoneGame builds a two-seat Commander game with the given per-seat
// commander card sources (delegating to the shared commanderGame, which parks
// each configured commander in the command zone at genesis), positioned so
// the tests can drive a lethal-damage state-based action directly: the game
// is at turn 1 upkeep with no decision pending.
func cmdZoneGame(t *testing.T, seatCmds [][]string) (*Engine, Config) {
	t.Helper()
	return commanderGame(t, commanderDamageSeed, FormatCommander, 40, seatCmds)
}

// assertAskCount pins how many KCommanderZone DecisionAsk events the log
// carries -- zero in a non-Commander game, exactly one per parked
// commander, and the no-spin proof that a decline is never re-asked.
func assertAskCount(t *testing.T, l *events.Log, want int) {
	t.Helper()
	got := 0
	for _, ev := range l.Events {
		if ev.Kind == events.DecisionAsk && ev.Text == string(decision.KCommanderZone) {
			got++
		}
	}
	if got != want {
		t.Fatalf("commander-zone asks in log = %d, want %d", got, want)
	}
}

// TestDestroyedCommanderOffersTheOwnerTheChoiceAndAcceptSendsItToTheCommandZone
// is the SBA path of the acceptance's headline case: a commander destroyed
// by lethal damage parks its graveyard-bound move and asks its OWNER; an
// accept emits a real MoveZone to the command zone, so the commander lands
// there and never touches the graveyard (the replacement-effect point: the
// log holds no commander MoveZone to the graveyard at all). The decision
// shape is pinned too: Min == Max == 1, options "command_zone" first then
// "leave", Player the owner.
func TestDestroyedCommanderOffersTheOwnerTheChoiceAndAcceptSendsItToTheCommandZone(t *testing.T) {
	t.Parallel()
	e, _ := cmdZoneGame(t, [][]string{{tinyCmdSrc}, {}})
	cmd := fieldCommander(t, e, 0, 0)
	e.G.Obj(cmd).Damage = 1 // 1 >= toughness 1: lethal (CR 704.5g)

	e.checkStateBased()

	d := e.Pending()
	if d == nil || d.Kind != decision.KCommanderZone {
		t.Fatalf("pending = %+v, want a commander_zone decision", d)
	}
	if d.Player != 0 {
		t.Fatalf("decision player = %d, want 0 (the commander's owner)", d.Player)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "command_zone" || d.Options[1].Kind != "leave" {
		t.Fatalf("options = %+v, want [command_zone, leave]", d.Options)
	}
	if got := d.Source; got != cmd {
		t.Fatalf("decision source = %d, want %d (the dying commander)", got, cmd)
	}

	submit(t, e, 0) // accept: into the command zone

	if got := e.G.Obj(cmd).Zone; got != state.ZCommand {
		t.Fatalf("accepted commander zone = %v, want command", got)
	}
	if contains(e.G.Zone(state.ZGraveyard, 0), cmd) {
		t.Fatal("accepted commander is in the graveyard: the replacement must apply BEFORE the zone change")
	}
	// The replacement-effect point, as a log fact: no MoveZone of this
	// commander to the graveyard ever happened.
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == cmd && ev.To == state.ZGraveyard {
			t.Fatalf("log records the commander touching the graveyard: %+v (replacement must precede the move)", ev)
		}
	}
}

// TestDestroyedCommanderDeclineLetsTheGraveyardHappen is the "may" half: a
// decline answers option 1 and the park is emitted verbatim, so the
// commander goes to the graveyard exactly as it would have with no
// commander rule at all. It is also the no-spin pin: after the decline the
// engine advances through a real priority decision -- the SBA pass that
// destroyed the commander does not re-offer the same choice (the attempted
// set, the queue dedup and the "replacement applies only once" de-park
// emit each close a re-offer arm), and the log carries exactly one
// commander-zone ask.
func TestDestroyedCommanderDeclineLetsTheGraveyardHappen(t *testing.T) {
	t.Parallel()
	e, _ := cmdZoneGame(t, [][]string{{tinyCmdSrc}, {}})
	cmd := fieldCommander(t, e, 0, 0)
	e.G.Obj(cmd).Damage = 1

	e.checkStateBased()
	if d := e.Pending(); d == nil || d.Kind != decision.KCommanderZone {
		t.Fatalf("pending = %+v, want a commander_zone decision", d)
	}
	submit(t, e, 1) // decline: the move happens unchanged

	if got := e.G.Obj(cmd).Zone; got != state.ZGraveyard {
		t.Fatalf("declined commander zone = %v, want graveyard", got)
	}

	// No-spin: after the decline the game continues (Submit's tail already
	// advanced it) to a real decision, and the log never offers the choice
	// again -- the SBA pass that destroyed the commander does not re-offer
	// it (the attempted set, the queue dedup and the "replacement applies
	// only once" de-park emit each close a re-offer arm).
	if d := e.Pending(); d == nil || d.Kind == decision.KCommanderZone {
		t.Fatalf("after the decline the game must continue to a real decision, got %+v", d)
	}
	assertAskCount(t, e.L, 1)
}

// TestCommanderExiledAcceptsTheCommandZone is the exile destination: a
// commander whose exile is attempted (the ChangeZone Destination$ Exile
// shape) is offered the choice, exactly like a destroy.
func TestCommanderExiledAcceptsTheCommandZone(t *testing.T) {
	t.Parallel()
	e, _ := cmdZoneGame(t, [][]string{{tinyCmdSrc}, {}})
	cmd := fieldCommander(t, e, 0, 0)

	e.emit(events.Event{Kind: events.MoveZone, Obj: cmd, From: state.ZBattlefield, To: state.ZExile})
	if d := e.Pending(); d == nil || d.Kind != decision.KCommanderZone {
		t.Fatalf("pending = %+v, want a commander_zone decision for the exile", d)
	}
	submit(t, e, 0)

	if got := e.G.Obj(cmd).Zone; got != state.ZCommand {
		t.Fatalf("exiled-then-accepted commander zone = %v, want command", got)
	}
	if contains(e.G.Zone(state.ZExile, 0), cmd) {
		t.Fatal("accepted commander is in exile: the move must be replaced, not followed by a second one")
	}
}

// TestCommanderReturnedToHandDeclinedKeepsTheHand is the hand destination
// and the decline arm of a non-destruction move: the commander goes where
// the effect pointed it, unchanged.
func TestCommanderReturnedToHandDeclinedKeepsTheHand(t *testing.T) {
	t.Parallel()
	e, _ := cmdZoneGame(t, [][]string{{tinyCmdSrc}, {}})
	cmd := fieldCommander(t, e, 0, 0)

	e.emit(events.Event{Kind: events.MoveZone, Obj: cmd, From: state.ZBattlefield, To: state.ZHand})
	if d := e.Pending(); d == nil || d.Kind != decision.KCommanderZone {
		t.Fatalf("pending = %+v, want a commander_zone decision for the bounce", d)
	}
	submit(t, e, 1)

	if got := e.G.Obj(cmd).Zone; got != state.ZHand {
		t.Fatalf("declined bounce zone = %v, want hand", got)
	}
}

// TestCommanderPutOnTopOfLibraryAcceptsTheCommandZone is the library
// destination: the same choice, and an accept sends an object that was
// heading for the library into the command zone instead.
func TestCommanderPutOnTopOfLibraryAcceptsTheCommandZone(t *testing.T) {
	t.Parallel()
	e, _ := cmdZoneGame(t, [][]string{{tinyCmdSrc}, {}})
	cmd := fieldCommander(t, e, 0, 0)

	e.emit(events.Event{Kind: events.MoveZone, Obj: cmd, From: state.ZBattlefield, To: state.ZLibrary})
	if d := e.Pending(); d == nil || d.Kind != decision.KCommanderZone {
		t.Fatalf("pending = %+v, want a commander_zone decision for the library", d)
	}
	submit(t, e, 0)

	if got := e.G.Obj(cmd).Zone; got != state.ZCommand {
		t.Fatalf("library-bound accepted commander zone = %v, want command", got)
	}
}

// TestCounteredCommanderOnTheStackIsOfferedTheChoice is the from-anywhere
// pin for the STACK as a source: a commander spell that would be put into
// its owner's graveyard from the stack (the exact MoveZone effCounter emits
// -- CR 608.2b's countered-spell shape) is offered the choice, and an accept
// sends it from the stack to the command zone. Deleting the "from anywhere"
// scope (restricting the replacement to battlefield sources) fails this test
// by name.
func TestCounteredCommanderOnTheStackIsOfferedTheChoice(t *testing.T) {
	t.Parallel()
	e, _ := cmdZoneGame(t, [][]string{{tinyCmdSrc}, {}})
	cmd := fieldCommander(t, e, 0, 0)
	// Stands in for the cast: a real cast (the m31 tax task's commitCast,
	// not merged yet) emits PutOnStack, which events.Apply moves to the shared
	// stack the same way this MoveZone does; the counter below is
	// effCounter's exact emit shape.
	e.emit(events.Event{Kind: events.MoveZone, Obj: cmd, From: state.ZBattlefield, To: state.ZStack})
	if e.Pending() != nil {
		t.Fatal("moving to the stack is not a CR 903.9 destination: nothing may be asked")
	}
	if got := e.G.Obj(cmd).Zone; got != state.ZStack {
		t.Fatalf("commander zone = %v, want stack", got)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: cmd, From: state.ZStack, To: state.ZGraveyard, Text: "countered"})
	if d := e.Pending(); d == nil || d.Kind != decision.KCommanderZone {
		t.Fatalf("pending = %+v, want a commander_zone decision for the countered spell", d)
	}
	submit(t, e, 0)

	if got := e.G.Obj(cmd).Zone; got != state.ZCommand {
		t.Fatalf("countered-and-accepted commander zone = %v, want command", got)
	}
	if contains(e.G.Stack, cmd) {
		t.Fatal("accepted commander is still on the stack: the counter's move must be fully replaced")
	}
}

// TestOwnersCommanderInGraveyardReturnedToHandAsks is the second from-
// anywhere pin, for the GRAVEYARD as a source: a commander already in its
// owner's graveyard that would move to the hand (a "return target card from
// your graveyard to your hand" effect) is offered the choice, and an accept
// leaves the graveyard for the command zone.
func TestOwnersCommanderInGraveyardReturnedToHandAsks(t *testing.T) {
	t.Parallel()
	e, _ := cmdZoneGame(t, [][]string{{tinyCmdSrc}, {}})
	cmd := fieldCommander(t, e, 0, 0)
	// Fixture: the commander rests in its owner's graveyard. (A real game
	// produces the same state through a declined destroy; the fixture nests
	// the object directly instead, the same class of direct setup write as
	// onBoard and the theft test below -- emitting the same shape through
	// the engine would itself park and ask, which is the thing under test.)
	e.G.Obj(cmd).Zone = state.ZGraveyard
	e.G.SetZone(state.ZBattlefield, 0, withoutID(e.G.Zone(state.ZBattlefield, 0), cmd))
	e.G.SetZone(state.ZGraveyard, 0, append(e.G.Zone(state.ZGraveyard, 0), cmd))

	e.emit(events.Event{Kind: events.MoveZone, Obj: cmd, From: state.ZGraveyard, To: state.ZHand})
	if d := e.Pending(); d == nil || d.Kind != decision.KCommanderZone || d.Player != 0 {
		t.Fatalf("pending = %+v, want a commander_zone decision for the owner over a graveyard->hand move", d)
	}
	submit(t, e, 0)

	if got := e.G.Obj(cmd).Zone; got != state.ZCommand {
		t.Fatalf("accepted graveyard-return zone = %v, want command", got)
	}
}

// TestStolenCommanderGoesToItsOwnersCommandZoneByItsOwnersChoice pins
// ownership over control: seat 0's commander has been stolen (it sits on
// seat 1's battlefield, controlled by seat 1 -- the fixture rewrites
// Controller and re-homes the zone lists directly; no effect in this build
// transfers control, so there is no real path to produce the state), and
// seat 1 destroys it. CR 903.9 looks at the OWNER: the decision goes to
// seat 0, and an accept sends the commander to seat 0's command zone, never
// seat 1's. Deleting the owner-not-controller lookup (reading o.Controller
// instead of o.Owner) fails this test by name.
func TestStolenCommanderGoesToItsOwnersCommandZoneByItsOwnersChoice(t *testing.T) {
	t.Parallel()
	e, _ := cmdZoneGame(t, [][]string{{tinyCmdSrc}, {}})
	cmd := fieldCommander(t, e, 0, 0)
	// Theft: seat 1 controls the commander on its battlefield. events.Move
	// keys the battlefield by controller, so the fixture updates both the
	// object's controller and the two zone lists (the same class of direct
	// setup write as the Damage/SummonSick lines elsewhere in this file).
	e.G.Obj(cmd).Controller = 1
	e.G.SetZone(state.ZBattlefield, 0, withoutID(e.G.Zone(state.ZBattlefield, 0), cmd))
	e.G.SetZone(state.ZBattlefield, 1, append(e.G.Zone(state.ZBattlefield, 1), cmd))
	if got := e.G.Obj(cmd).Controller; got != 1 {
		t.Fatalf("fixture: commander controller = %d, want 1 (stolen)", got)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: cmd, From: state.ZBattlefield, To: state.ZGraveyard})
	d := e.Pending()
	if d == nil || d.Kind != decision.KCommanderZone {
		t.Fatalf("pending = %+v, want a commander_zone decision", d)
	}
	if d.Player != 0 {
		t.Fatalf("decision player = %d, want 0 (the OWNER, not the controlling seat 1)", d.Player)
	}
	submit(t, e, 0)

	if got := e.G.Obj(cmd).Zone; got != state.ZCommand {
		t.Fatalf("stolen commander zone = %v, want command", got)
	}
	if contains(e.G.Zone(state.ZCommand, 1), cmd) {
		t.Fatal("stolen commander is in seat 1's command zone: the command zone is the OWNER's")
	}
	if !contains(e.G.Zone(state.ZCommand, 0), cmd) {
		t.Fatal("stolen commander is not in seat 0's command zone")
	}
}

// TestCommanderMovingToBattlefieldOrStackIsNotReplaced pins the four-
// destination guard from the outside: a commander entering the battlefield
// (a recast -- the m31/tax casts land here) or the stack sits untouched,
// with no decision asked and nothing parked. This is the mutation guard for
// the destination switch: adding ZBattlefield or ZStack to the replaced set
// makes fieldCommander itself (and this test) ask a decision where CR 903.9
// must not.
func TestCommanderMovingToBattlefieldOrStackIsNotReplaced(t *testing.T) {
	t.Parallel()
	e, _ := cmdZoneGame(t, [][]string{{tinyCmdSrc}, {}})
	cmd := e.G.Players[0].Commanders[0]
	// fieldCommander emits the logged CZ -> battlefield MoveZone. If the
	// destination gate were wrong this would already be asking.
	if id := fieldCommander(t, e, 0, 0); id != cmd {
		t.Fatalf("fielded commander = %d, want %d", id, cmd)
	}
	if e.Pending() != nil {
		t.Fatalf("entering the battlefield must not ask: pending = %+v", e.Pending())
	}
	if len(e.cmdZone) != 0 {
		t.Fatalf("entry to the battlefield must not park: queue = %+v", e.cmdZone)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: cmd, From: state.ZBattlefield, To: state.ZStack})
	if e.Pending() != nil || len(e.cmdZone) != 0 {
		t.Fatalf("moving to the stack must not ask or park: pending = %+v, queue = %v", e.Pending(), e.cmdZone)
	}
}

// TestNonCommanderGameNeverRunsTheReplacement is the FormatCommander gate: a
// Constructed-format game, even one whose Config carries a Commanders list
// (so m30's genesis parked a commander and sized the bookkeeping), must not
// offer the choice at all -- the commander dies to the graveyard with no
// decision, no queue and no commander-zone events anywhere in the log.
// Deleting the format gate fails this test by name.
func TestNonCommanderGameNeverRunsTheReplacement(t *testing.T) {
	t.Parallel()
	e, _ := commanderGame(t, commanderDamageSeed, FormatConstructed, 40, [][]string{{tinyCmdSrc}, {}})
	cmd := fieldCommander(t, e, 0, 0)
	e.G.Obj(cmd).Damage = 1

	e.checkStateBased()

	if e.Pending() != nil {
		t.Fatalf("Constructed game asked a commander-zone decision: %+v", e.Pending())
	}
	if got := e.G.Obj(cmd).Zone; got != state.ZGraveyard {
		t.Fatalf("Constructed destroyed commander zone = %v, want graveyard (the mechanic must not run)", got)
	}
	assertAskCount(t, e.L, 0)
}

// TestNonCommanderCreatureIsNeverAsked is the membership guard's positive
// control, inside a Commander game: destroying an ordinary creature (one not
// in any Commanders list) must not even be considered for the replacement.
// Deleting the Commanders membership check fails this test by name.
func TestNonCommanderCreatureIsNeverAsked(t *testing.T) {
	t.Parallel()
	e, _ := cmdZoneGame(t, [][]string{{tinyCmdSrc}, {}})
	bear := onBoard(t, e, 0, "Name:Plain\nManaCost:1 G\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	e.G.Obj(bear).Damage = 1

	e.checkStateBased()

	if e.Pending() != nil {
		t.Fatalf("an ordinary creature's death asked a decision: %+v", e.Pending())
	}
	if got := e.G.Obj(bear).Zone; got != state.ZGraveyard {
		t.Fatalf("ordinary destroyed creature zone = %v, want graveyard", got)
	}
	assertAskCount(t, e.L, 0)
}

// TestTwoCommandersParkedAtOnceAreAskedOneAtATime pins the queue: a burst
// that would destroy two of the same owner's commanders (a board-wipe
// shape, or one SBA pass finding two lethal) parks both but asks only one
// decision at a time -- the first answer emits its move and hands the queue
// to the second. Both accepts land both commanders in the command zone, and
// the log carries exactly two commander-zone asks (never one, never three).
// The dedup in parkCommanderZoneMove is what keeps the SBA pass that
// re-runs between the two answers from re-parking the second commander
// (the no-progress failure shape).
func TestTwoCommandersParkedAtOnceAreAskedOneAtATime(t *testing.T) {
	t.Parallel()
	e, _ := cmdZoneGame(t, [][]string{{tinyCmdSrc, tinyCmdSrc2}, {}})
	cmds := append([]state.ObjID(nil), e.G.Zone(state.ZCommand, 0)...)
	a, b := cmds[0], cmds[1]
	fieldCommanderByID(t, e, a)
	fieldCommanderByID(t, e, b)
	e.G.Obj(a).Damage = 1
	e.G.Obj(b).Damage = 1

	e.checkStateBased()

	d := e.Pending()
	if d == nil || d.Kind != decision.KCommanderZone {
		t.Fatalf("pending = %+v, want a commander_zone decision (first of two)", d)
	}
	submit(t, e, 0) // accept a
	if got := e.G.Obj(a).Zone; got != state.ZCommand {
		t.Fatalf("first accepted commander zone = %v, want command", got)
	}

	d = e.Pending()
	if d == nil || d.Kind != decision.KCommanderZone || d.Source != b {
		t.Fatalf("pending = %+v, want the SECOND commander's decision (source %d)", d, b)
	}
	submit(t, e, 0) // accept b
	if got := e.G.Obj(b).Zone; got != state.ZCommand {
		t.Fatalf("second accepted commander zone = %v, want command", got)
	}

	if !contains(e.G.Zone(state.ZCommand, 0), a) || !contains(e.G.Zone(state.ZCommand, 0), b) {
		t.Fatalf("command zone = %v, want both commanders", e.G.Zone(state.ZCommand, 0))
	}
	assertAskCount(t, e.L, 2)
}

// TestCommanderZoneChoiceIsInTheLog pins the "this is a player decision, an
// intent, not something derived" contract: the choice travels as an Intent
// plus a DecisionAsk/DecisionMade pair with the kind name, and the accept
// outcome is a real MoveZone to the command zone. A log-only replay from
// Config + events re-derives the identical state for BOTH branches --
// accept lands in the command zone, decline in the graveyard (Task m33's
// commanderReplayFromLog fold, the same helper it uses for the CmdDamage
// event: the log alone never needs the live engine's memory).
func TestCommanderZoneChoiceIsInTheLog(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		choice int
		want   state.Zone
	}{
		{name: "accept", choice: 0, want: state.ZCommand},
		{name: "decline", choice: 1, want: state.ZGraveyard},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg := cmdZoneGame(t, [][]string{{tinyCmdSrc}, {}})
			cmd := fieldCommander(t, e, 0, 0)
			e.G.Obj(cmd).Damage = 1
			e.checkStateBased()

			d := e.Pending()
			if d == nil || d.Kind != decision.KCommanderZone {
				t.Fatalf("pending = %+v, want a commander_zone decision", d)
			}
			// The intent is recorded verbatim...
			wantIntent := decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{tc.choice}}
			if err := e.Submit(wantIntent); err != nil {
				t.Fatalf("submit: %v", err)
			}
			if got := e.G.Obj(cmd).Zone; got != tc.want {
				t.Fatalf("live zone = %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(e.L.Intents[len(e.L.Intents)-1], wantIntent) {
				t.Fatalf("recorded intent = %+v, want %+v", e.L.Intents[len(e.L.Intents)-1], wantIntent)
			}
			// ...and the log carries the ask and the made marker (the made
			// event's Text carries the kind plus the chosen indices, the
			// %s:%v shape every DecisionMade uses).
			askSeen, madeSeen := false, false
			for _, ev := range e.L.Events {
				if ev.Kind == events.DecisionAsk && ev.Text == string(decision.KCommanderZone) {
					askSeen = true
				}
				if ev.Kind == events.DecisionMade &&
					strings.HasPrefix(ev.Text, string(decision.KCommanderZone)) {
					madeSeen = true
				}
			}
			if !askSeen || !madeSeen {
				t.Fatalf("log missing ask/made markers: ask=%v made=%v", askSeen, madeSeen)
			}

			// A replay from Config + log alone reproduces the same zone.
			re := commanderReplayFromLog(t, cfg, e.L.Events)
			if got := re.Obj(cmd).Zone; got != tc.want {
				t.Fatalf("log-only replay zone = %v, want %v", got, tc.want)
			}
			inCZ := contains(re.Zone(state.ZCommand, 0), cmd)
			if inCZ != (tc.want == state.ZCommand) {
				t.Fatalf("replay command zone = %v, live = %v", re.Zone(state.ZCommand, 0), e.G.Zone(state.ZCommand, 0))
			}
		})
	}
}

// TestCommanderZoneDecisionSurvivesClone pins the Clone contract for the
// parked queue: a clone taken while the KCommanderZone decision is
// outstanding carries the same pending decision AND the same parked move,
// so the clone answers the decision the way the original would (the
// commander moves where the answer says), and the two engines then evolve
// independently (m32's own branch of the m33 clone test: the queue is new
// engine state, and dropping it from Clone would make the clone's answer
// find no parked move and leave the commander stranded).
func TestCommanderZoneDecisionSurvivesClone(t *testing.T) {
	t.Parallel()
	e, _ := cmdZoneGame(t, [][]string{{tinyCmdSrc}, {}})
	cmd := fieldCommander(t, e, 0, 0)
	e.G.Obj(cmd).Damage = 1
	e.checkStateBased()
	if d := e.Pending(); d == nil || d.Kind != decision.KCommanderZone {
		t.Fatalf("pending = %+v, want a commander_zone decision to clone across", d)
	}

	c := e.Clone()
	if len(c.cmdZone) != 1 {
		t.Fatalf("clone parked queue = %v, want the original's single park", c.cmdZone)
	}
	if d := c.Pending(); d == nil || d.Kind != decision.KCommanderZone {
		t.Fatalf("clone pending = %+v, want the copied commander_zone decision", d)
	}

	submit(t, c, 0) // clone accepts
	if got := c.G.Obj(cmd).Zone; got != state.ZCommand {
		t.Fatalf("clone zone = %v, want command (the clone must move the commander it was asked about)", got)
	}

	// The original still owes its own answer and answers it independently.
	if d := e.Pending(); d == nil || d.Kind != decision.KCommanderZone {
		t.Fatalf("original pending = %+v, want its own commander_zone decision untouched by the clone", d)
	}
	submit(t, e, 1)
	if got := e.G.Obj(cmd).Zone; got != state.ZGraveyard {
		t.Fatalf("original zone = %v, want graveyard (independent of the clone's accept)", got)
	}
}

// withoutID returns ids minus want, preserving order. Used by the theft
// fixture to move a commander between battlefields; a test-side helper, the
// same class as commander_damage_test.go's contains.
func withoutID(ids []state.ObjID, want state.ObjID) []state.ObjID {
	out := make([]state.ObjID, 0, len(ids)-1)
	for _, id := range ids {
		if id != want {
			out = append(out, id)
		}
	}
	return out
}
