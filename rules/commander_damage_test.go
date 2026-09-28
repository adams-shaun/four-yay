// Task m33: commander damage, and the 21-damage state-based action (CR
// 903.10). Combat damage dealt to a player by a commander is tallied
// per-commander (state.Player.CmdDamage, keyed by the match-wide dense
// commander index m30's genesis assigns); a player with 21 or more from the
// same commander loses, as a state-based action like the life-loss check.
//
// The four traps CR 903.10 trips on, each pinned by a named test below:
//   - COMBAT damage only (a ping / burn / triggered damage never tallies);
//   - cumulative over the WHOLE game, across death and recast (the tally is
//     keyed by the commander's stable ObjID, so a recast keeps the slot);
//   - damage dealt to a PLAYER, never to a permanent (only the toPlayer
//     combat branch ever emits the CmdDamage event);
//   - a state-based action that loses the game like any other loss (sba.go
//     emits the same PlayerLost checkLoseConditions already uses).
//
// Everything is gated on Config.Format == FormatCommander: a Constructed-
// format game, even one carrying a Commanders config, is untouched.
package rules

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// cmdCreature renders a Green Legendary creature commander whose power is
// the argument; toughness is set high (99) so the attacker never dies to its
// own lethal damage, which would otherwise muddy threshold/cululative tests.
func cmdCreature(pw int32) string {
	return cmdCreatureNamed("Commander", pw)
}

// cmdCreatureNamed is cmdCreature with the name spelled out. A test that
// fields TWO commanders for one player must give them DIFFERENT names: they
// are Legendary, so two identically-named ones under a single controller is a
// board CR 704.5j forbids, and the legend rule now really does put one of them
// into the graveyard. That would be a broken fixture rather than a test of
// anything -- the per-commander damage keying it means to pin (CR 903.10) is
// about two commanders BOTH being on the battlefield.
func cmdCreatureNamed(name string, pw int32) string {
	return "Name:" + name + "\nManaCost:2 G\nTypes:Legendary Creature Bear\nPT:" +
		strconv.Itoa(int(pw)) + "/99\nK:Trample\nK:Partner\nOracle:x\n"
}

// commanderGame is the ONE shared Commander fixture: a two-seat game with
// the given construction format (FormatCommander for the mechanics,
// FormatConstructed for the gate probe) and the given per-seat commander card
// sources (seat p's slice lists its commanders in deck order; an empty slice
// means no commanders for that seat). m30's genesis pulls each configured
// commander to the command zone and sizes CmdDamage regardless of Format, so
// the format gate is the ONLY thing that should keep a Constructed game from
// running the mechanic. The engine is returned together with its Config for
// log-only replay, deliberately NOT advanced, so a test can drive combat
// directly (the combatEngine pattern); the m31 commander-tax suite reaches it
// through commanderTaxGame (rules/commander_tax_test.go), which drives to
// turn 1 Main1 on top of it.
//
// seed seeds the shuffle: the m33 damage suite and the m32 zone-replacement
// suite pass the fixed commanderDamageSeed they were written against (its
// original Config.Seed was 9), the m31 tax suite passes its own per-test
// seed. One fixture, three suites, each under exactly the stream it always
// measured.
func commanderGame(t *testing.T, seed uint64, format Format, life int32, seatCmds [][]string) (*Engine, Config) {
	t.Helper()
	names := make([]string, len(seatCmds))
	decks := make([][]*cards.Card, len(seatCmds))
	cmds := make([][]int, len(seatCmds))
	for p := range seatCmds {
		names[p] = string(rune('a' + p))
		var deck []*cards.Card
		for i, src := range seatCmds[p] {
			deck = append(deck, card(t, src))
			cmds[p] = append(cmds[p], i)
		}
		deck = append(deck, mountainDeck(t, 40-len(deck))...)
		decks[p] = deck
	}
	cfg := Config{Seed: seed, Names: names, Decks: decks,
		Commanders: cmds, StartingLife: life, Format: format}
	// The commander suites' protagonist is seat 0 (every test drives to
	// turn 1 seat 0); seatZeroStart advances the seed until the CR 103.1
	// toss starts seat 0. The effective seed travels out in cfg.
	cfg = seatZeroStart(cfg)
	return New(cfg), cfg
}

// commanderDamageSeed is the seed the m33 commander-damage tests were written
// against (the original fixture hard-coded Config.Seed: 9). The m32
// zone-replacement suite shares it through cmdZoneGame; the m31 tax suite
// passes its own per-test seeds instead.
const commanderDamageSeed uint64 = 9

// fieldCommander moves the k-th commander of seat p (in the command-zone
// order m30's genesis produced) onto the battlefield through a LOGGED
// MoveZone (so a log-only replay can reconstruct it), then readies it to
// attack (clears summoning sickness). It returns the commander's ObjID, which
// is stable across the move. Only valid to index the command zone while its
// order is still the pristine genesis output, i.e. before any other commander
// of that seat has been fielded.
func fieldCommander(t *testing.T, e *Engine, p state.PlayerID, k int) state.ObjID {
	t.Helper()
	id := e.G.Zone(state.ZCommand, p)[k]
	return fieldCommanderByID(t, e, id)
}

// fieldCommanderByID moves the named commander from the command zone to the
// battlefield through a logged MoveZone and readies it to attack. Unlike
// fieldCommander it does not index the live command zone, so it is safe to
// call for several of a seat's commanders in any order.
func fieldCommanderByID(t *testing.T, e *Engine, id state.ObjID) state.ObjID {
	t.Helper()
	for p := range e.G.Players {
		for _, c := range e.G.Zone(state.ZCommand, state.PlayerID(p)) {
			if c == id {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id,
					From: state.ZCommand, To: state.ZBattlefield})
				o := e.G.Obj(id)
				o.SummonSick = false
				o.Tapped = false
				return id
			}
		}
	}
	t.Fatalf("commander %d not found in any command zone", id)
	return 0
}

// swing drives one combat step with the given attackers (seat 0 takes the
// active role and attacks its defending player, seat 1). Submit runs the
// damage step and the post-combat state-based-action pass, so a defender
// that just crossed 21 commander damage is Lost when swing returns.
func swing(t *testing.T, e *Engine, attackers ...state.ObjID) {
	t.Helper()
	if e.G.Over {
		t.Fatalf("cannot swing an already-over game")
	}
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	for _, id := range attackers {
		o := e.G.Obj(id)
		o.Tapped = false
		o.SummonSick = false
		o.IsAttacking = false
		o.Damage = 0
	}
	e.askAttackers()
	submitAttackers(t, e, attackers...)
}

// TestTwentyCommanderDamageDoesNotLose is the low side of the CR 903.10
// threshold: 20 combat damage from a single commander is one short, so the
// defender (health 40, 20 left) survives the state-based-action pass.
func TestTwentyCommanderDamageDoesNotLose(t *testing.T) {
	t.Parallel()
	e, _ := commanderGame(t, commanderDamageSeed, FormatCommander, 40, [][]string{{cmdCreature(20)}, {}})
	cmd := fieldCommander(t, e, 0, 0)
	swing(t, e, cmd)

	if e.G.Players[1].Lost {
		t.Fatalf("defender lost to 20 commander damage: CmdDamage=%v", e.G.Players[1].CmdDamage)
	}
	if got := e.G.Players[1].CmdDamage[0]; got != 20 {
		t.Fatalf("defender commander damage = %d, want 20", got)
	}
}

// TestTwentyOneCommanderDamageLoses is the upper side of the threshold (and
// the mutation pin for ">= 21"): one point more, and the player loses even
// though their life total (19) is still positive -- commander damage is an
// independent loss condition, not a proxy for life.
func TestTwentyOneCommanderDamageLoses(t *testing.T) {
	t.Parallel()
	e, _ := commanderGame(t, commanderDamageSeed, FormatCommander, 40, [][]string{{cmdCreature(21)}, {}})
	cmd := fieldCommander(t, e, 0, 0)
	swing(t, e, cmd)

	if !e.G.Players[1].Lost {
		t.Fatalf("defender did not lose to 21 commander damage: CmdDamage=%v", e.G.Players[1].CmdDamage)
	}
	if got := e.G.Players[1].CmdDamage[0]; got != 21 {
		t.Fatalf("defender commander damage = %d, want 21", got)
	}
	if !e.G.Over {
		t.Fatal("the game must be over once the defending player loses")
	}
}

// TestTwoCommandersElevenEachDoesNotLose is the per-commander-keying pin
// (CR 903.10 counts per-commander, never summed): two commanders each
// dealing 11 to the same player is 22 aggregate but a safe 11 apiece, so the
// defender must survive -- a tally that pooled commanders into one slot
// would wrongly lose them. It is the mutation guard for commanderDenseIndex:
// collapsing every commander onto slot 0 turns 11+11 into a single 22 and
// fails this test by making the defender lose.
func TestTwoCommandersElevenEachDoesNotLose(t *testing.T) {
	t.Parallel()
	// Two DIFFERENT legendary names: same-named legendaries under one
	// controller are illegal (CR 704.5j) and the legend rule would bin one of
	// them before either could deal its 11.
	e, _ := commanderGame(t, commanderDamageSeed, FormatCommander, 40,
		[][]string{{cmdCreatureNamed("Commander One", 11), cmdCreatureNamed("Commander Two", 11)}, {}})
	cmds := e.G.Zone(state.ZCommand, 0)
	a := fieldCommanderByID(t, e, cmds[0])
	b := fieldCommanderByID(t, e, cmds[1])
	swing(t, e, a, b)

	if e.G.Players[1].Lost {
		t.Fatalf("defender lost though no single commander reached 21: CmdDamage=%v", e.G.Players[1].CmdDamage)
	}
	if want := []int32{11, 11}; !reflect.DeepEqual(e.G.Players[1].CmdDamage, want) {
		t.Fatalf("per-commander damage = %v, want %v (each commander keeps its own slot)", e.G.Players[1].CmdDamage, want)
	}
}

// TestSameCommanderTwiceOpposesTheTwoAbove is the other half of cumulative
// keying: 22 from the SAME commander (across two swings) is a loss, even
// though each swing was only 11. Between the swings the commander "dies"
// (leaves the battlefield through a logged MoveZone) and is "recast" (the
// same ObjID re-enters the battlefield) -- the tally must remember the first
// 11 across that zone round-trip, because state.ObjID is stable across a
// commander's moves (m30 keeps one game object throughout).
func TestSameCommanderTwiceCumulatesToTwentyTwoAndLoses(t *testing.T) {
	t.Parallel()
	e, _ := commanderGame(t, commanderDamageSeed, FormatCommander, 40, [][]string{{cmdCreature(11)}, {}})
	cmd := fieldCommander(t, e, 0, 0)

	swing(t, e, cmd)
	if got := e.G.Players[1].CmdDamage[0]; got != 11 {
		t.Fatalf("after the first swing commander damage = %d, want 11", got)
	}
	if e.G.Players[1].Lost {
		t.Fatal("defender must survive the first 11")
	}

	// Simulate the commander dying and being recast: it leaves the
	// battlefield (to the graveyard, standing in for "returns to the command
	// zone" -- the command-zone-return and cast-from-command-zone plumbing is
	// another milestone task, so this test moves the object directly, which
	// is exactly the same ObjID either way).
	for civ := state.ZBattlefield; ; {
		ids := e.G.Zone(civ, 0)
		if contains(ids, cmd) {
			e.emit(events.Event{Kind: events.MoveZone, Obj: cmd,
				From: civ, To: state.ZBattlefield})
			break
		}
		civ = state.ZGraveyard
	}
	o := e.G.Obj(cmd)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("recast commander not on the battlefield: zone %v", o.Zone)
	}

	swing(t, e, cmd)
	if !e.G.Players[1].Lost {
		t.Fatalf("defender survived a RECAST commander's second 11 (11+11 same ObjID must lose): CmdDamage=%v",
			e.G.Players[1].CmdDamage)
	}
	if got := e.G.Players[1].CmdDamage[0]; got != 22 {
		t.Fatalf("defender commander damage = %d, want 22 (cumulative across recast)", got)
	}
}

// TestNonCombatCommanderDamageDoesNotTally is the COMBAT-ONLY trap: a
// commander's damage dealt outside combat -- a ping, a burn spell, a
// triggered ability -- lands as a plain Damage event to the player and must
// never add to the commander tally, no matter how big. The tally is written
// only from the combat damage step, so 21 non-combat damage leaves the tally
// at zero (the defender's 19-life survival is the proof that the zero tally
// was not masked by a life-loss death).
func TestNonCombatCommanderDamageDoesNotTally(t *testing.T) {
	t.Parallel()
	e, _ := commanderGame(t, commanderDamageSeed, FormatCommander, 40, [][]string{{cmdCreature(1)}, {}})
	fieldCommander(t, e, 0, 0)

	// A resolved ping/burn/triggered effect: damage to the player that is not
	// the combat damage step (there is no attacking creature here).
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 21})
	if got := e.G.Players[1].CmdDamage[0]; got != 0 {
		t.Fatalf("non-combat commander damage tallied: CmdDamage=%d, want 0", got)
	}
	e.checkStateBased()
	if e.G.Players[1].Lost {
		t.Fatal("defender lost to non-combat damage: commander damage must be combat-only")
	}
}

// TestPreventedCombatDamageDoesNotInflateTheTally is the prevented-damage
// trap, exercised through the one prevention the engine can actually produce:
// a blocker protected from the attacking commander's colour. The commander's
// damage to the protected blocker is prevented (the damage emit is replaced
// by a Note, rules/combat.go's existing pattern); what spills past with
// Trample is the only damage that reaches the player and thus the only
// commander damage tallied. A 10/10 trampler past a 1/2 protected blocker
// deals 8 real player damage; the 2 that was prevented must not be counted,
// so the tally is 8, not 10. (Player-command-damage prevention itself --
// a fog that targets the player -- has no engine path yet: protection is
// permanents-only and the replacement machinery matches MoveZone events
// only. The player branch of damageStep nonetheless reads the emitted kind
// exactly like the blocker branch does, so the moment such a prevention
// exists this same guard declines the tally; see the report.)
func TestPreventedCombatDamageDoesNotInflateTheTally(t *testing.T) {
	t.Parallel()
	e, _ := commanderGame(t, commanderDamageSeed, FormatCommander, 40, [][]string{{cmdCreature(10)}, {}})
	atk := fieldCommander(t, e, 0, 0)

	blk := onBoard(t, e, 1, "Name:Guard\nManaCost:1 R\nTypes:Creature Goblin\nPT:1/2\nK:Protection from green\nOracle:x\n")

	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	e.askAttackers()
	submitAttackers(t, e, atk)
	submitBlockers(t, e, blk)

	if got := e.G.Players[1].CmdDamage[0]; got != 8 {
		t.Fatalf("commander damage = %d, want 8 (the 2 dealt to the protected blocker is prevented and must not be tallied)", got)
	}
	if e.G.Players[1].Life != 32 {
		t.Fatalf("defender life = %d, want 32 (40 - 8 real damage; the prevented 2 must not reach life either)", e.G.Players[1].Life)
	}
}

// TestNonCommanderGameIgnoresCommanderDamage is the FormatCommander gate: a
// Constructed-format game, even one whose Config carries a Commanders list
// (so m30's genesis sized CmdDamage and parked a commander in the command
// zone), must have NONE of the mechanic run. The combat gate declines to
// emit any CmdDamage event and the state-based-action gate declines to check
// the (zero) tally, so 21 commander-power combat damage to the defender
// leaves them alive and the tally untouched. Removing either gate lets the
// defender lose here and fails this test by name.
func TestNonCommanderGameIgnoresCommanderDamage(t *testing.T) {
	t.Parallel()
	e, _ := commanderGame(t, commanderDamageSeed, FormatConstructed, 40, [][]string{{cmdCreature(21)}, {}})
	cmd := fieldCommander(t, e, 0, 0)
	swing(t, e, cmd)

	if got := e.G.Players[1].CmdDamage[0]; got != 0 {
		t.Fatalf("Constructed game tallied commander damage: CmdDamage=%d, want 0", got)
	}
	if e.G.Players[1].Lost {
		t.Fatal("Constructed-format defender lost to commander damage: the mechanic must not run")
	}
}

// TestCommanderDamageReplaysFromTheLogAlone is the replay-fidelity pin: the
// tally is carried in a CmdDamage event (events.Apply folds it into the
// per-commander slot), so a reconstruction that starts from the Config plus
// the logged events -- nothing else -- reproduces the exact per-commander
// tally. This is why the tally is carried in an event of its own rather than
// written directly or re-derived from the existing Damage events (which do
// not record which commander the source was).
func TestCommanderDamageReplaysFromTheLogAlone(t *testing.T) {
	t.Parallel()
	e, cfg := commanderGame(t, commanderDamageSeed, FormatCommander, 40, [][]string{{cmdCreature(21)}, {}})
	cmd := fieldCommander(t, e, 0, 0)
	swing(t, e, cmd)
	live := e.G.Players[1].CmdDamage

	re := commanderReplayFromLog(t, cfg, e.L.Events)
	if got, want := re.Players[1].CmdDamage, live; !reflect.DeepEqual(got, want) {
		t.Fatalf("log-only replay commander damage = %v, want %v from the live game", got, want)
	}
}

// TestCommanderDamageSurvivesClone is the Clone half of the brief's first
// line: both the per-commander tally and the format gate travel through
// Engine.Clone (Game.Clone deep-copies CmdDamage; clone.go carries format),
// so a cloned engine sees the identical tally and stays a Commander game.
func TestCommanderDamageSurvivesClone(t *testing.T) {
	t.Parallel()
	e, _ := commanderGame(t, commanderDamageSeed, FormatCommander, 40, [][]string{{cmdCreature(20)}, {}})
	cmd := fieldCommander(t, e, 0, 0)
	swing(t, e, cmd)
	if e.Pending() == nil && !e.G.Over {
		t.Fatal("clone must be taken at an intent boundary")
	}
	c := e.Clone()
	if got, want := c.G.Players[1].CmdDamage, e.G.Players[1].CmdDamage; !reflect.DeepEqual(got, want) {
		t.Fatalf("clone commander damage = %v, want %v", got, want)
	}
	if c.format != FormatCommander {
		t.Fatalf("clone format = %v, want FormatCommander", c.format)
	}
}

// commanderReplayFromLog is the log-only reconstruction the replay test
// uses: it builds a fresh game from cfg (so object IDs match genesis), folds
// in the command-zone commander bookkeeping the way rules.New's genesis does
// (Commanders and the match-wide-sized CmdDamage -- the parts that let
// events.Apply resolve a CmdDamage event's dense index), then replays every
// logged event through events.Apply. Nothing else carries the tally across.
func commanderReplayFromLog(t *testing.T, cfg Config, log []events.Event) *state.Game {
	t.Helper()
	g := state.NewGame(cfg.Names)
	g.Tokens = cfg.Tokens
	for i, deck := range cfg.Decks {
		p := state.PlayerID(i)
		ids := make([]state.ObjID, 0, len(deck))
		for _, c := range deck {
			ids = append(ids, g.AddObject(c, p).ID)
		}
		g.SetZone(state.ZLibrary, p, ids)
		var myCmds []state.ObjID
		for _, k := range cfg.commandersFor(i, len(deck)) {
			myCmds = append(myCmds, ids[k])
		}
		g.Players[p].Commanders = myCmds
	}
	total := 0
	for i := range cfg.Names {
		total += len(cfg.commandersFor(i, len(cfg.Decks[i])))
	}
	for p := range g.Players {
		g.Players[p].CmdDamage = make([]int32, total)
	}
	for _, ev := range log {
		events.Apply(g, ev)
	}
	return g
}

func contains(ids []state.ObjID, want state.ObjID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
