package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The AddsCounters$ mana-spend rider (task opalp). Opal Palace's second
// ability is the corpus pin: "{1}, {T}: Add one mana of any color in your
// commander's color identity. If you spend this mana to cast your commander,
// it enters with a number of additional +1/+1 counters on it equal to the
// number of times it's been cast from the command zone this game." The mana
// carries AddsCounters$ Card.YouOwn+IsCommander_P1P1_ManaAddsCounterNum and
// its count is the non-Total Count$CommanderCastFromCommandZone head.
//
// Every fixture here is a real Forge script SHAPE (Opal Palace is a corpus
// carrier, byte-identical script) written inline per the repo's licensing
// rule: never a .cards/ .txt.

const opalPalaceSrc = `Name:Opal Palace
ManaCost:no cost
Types:Land
A:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.
A:AB$ Mana | Cost$ 1 T | Produced$ Combo ColorIdentity | AddsCounters$ Card.YouOwn+IsCommander_P1P1_ManaAddsCounterNum | SpellDescription$ Add one mana of any color in your commander's color identity. If you spend this mana to cast your commander, it enters with a number of additional +1/+1 counters on it equal to the number of times it's been cast from the command zone this game.
SVar:ManaAddsCounterNum:Count$CommanderCastFromCommandZone
Oracle:x
`

// opalCommanderSrc is a decision-free mono-red legendary creature commander
// (identity {R}), so Opal Palace's Combo ColorIdentity ability offers exactly
// one colour and the cast is a single red pip.
const opalCommanderSrc = `Name:Ember Commander
ManaCost:R
Types:Legendary Creature Elemental
PT:2/2
K:Partner
Oracle:x
`

// opalBystanderSrc is a plain non-commander creature used to prove the rider
// is scoped to the commander: spending the same mana on this spell grants no
// counters.
const opalBystanderSrc = `Name:Mountain Goat
ManaCost:R
Types:Creature Goat
PT:1/1
Oracle:x
`

// opalPalaceGame builds a two-seat Commander game whose seat 0 leads with the
// mono-red inline commander and carries Opal Palace plus the bystander
// creature among its deck cards. Opal Palace is moved onto the battlefield
// through a logged MoveZone (moveToBattlefieldByName) and returned. The game
// is driven to seat 0's Main1, the only point a creature command-zone cast is
// offered.
func opalPalaceGame(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg := colourIdentityGame(t, seed, FormatCommander,
		card(t, opalCommanderSrc), nil,
		card(t, opalPalaceSrc), card(t, opalBystanderSrc))
	opal := moveToBattlefieldByName(t, e, 0, "Opal Palace")
	return e, cfg, opal
}

// activateOpalRider activates Opal Palace's {1},{T} AddsCounters$ mana
// ability, answering the mana-ability wheel with the rider ability's own
// option for colour and leaving exactly one such unit in the pool. The {1}
// activation cost must already be funded.
func activateOpalRider(t *testing.T, e *Engine, opal state.ObjID, colour string) {
	t.Helper()
	mas := e.availableManaAbilities(0, opal)
	riderIdx := -1
	for i, ma := range mas {
		if strings.TrimSpace(ma.Params["AddsCounters"]) != "" {
			riderIdx = i
		}
	}
	if riderIdx < 0 {
		t.Fatalf("precondition: Opal Palace has no AddsCounters$ mana ability: %d abilities", len(mas))
	}
	e.priorityRound()
	activateMana(t, e, opal)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("Opal Palace mana-ability ask = %+v, want KChoose", d)
	}
	// Stage 1: choose the rider ability itself (Combo ColorIdentity is not
	// enumerable at option-build time, so it is one option plus a stage-2
	// colour ask).
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "mana" && o.Ability == riderIdx {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no rider mana-ability option (index %d): %+v", riderIdx, d.Options)
	}
	submitChoices(t, e, idx)
	// Stage 2: the concrete colour. A no-colour-ask shape (a flattened
	// Combo list) leaves no pending decision here.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		return
	}
	idx = -1
	for _, o := range d.Options {
		if o.Kind == "mana" && strings.HasSuffix(o.Label, "Add "+colour) {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no rider colour option producing %s: %+v", colour, d.Options)
	}
	submitChoices(t, e, idx)
}

// opalCastCommander submits the command-zone cast option for id and drains
// the stack, leaving the commander on the battlefield. It does not return the
// commander to the command zone, so the caller can assert its counters first.
func opalCastCommander(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	opt := commanderCastOption(e, id)
	if opt == nil {
		t.Fatalf("no command-zone cast option for commander %d: %+v", id, e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 30)
}

// TestOpalPalaceRiderCountsCommandZoneCasts is the reported-symptom pin. The
// first command-zone cast is paid with ordinary (rider-less) mana, so the
// commander enters with NO counters even though the command-zone count is
// already 1 -- the negative half. The second cast is paid with Opal Palace's
// rider mana, so the commander enters with counters equal to the count head
// (2: both command-zone casts so far). The two different values prove the
// grant is driven by the rider plus the count, not by a constant.
func TestOpalPalaceRiderCountsCommandZoneCasts(t *testing.T) {
	e, cfg, opal := opalPalaceGame(t, 401)
	cmd := e.G.Players[0].Commanders[0]

	// Preconditions: the land is really on the battlefield untapped and the
	// commander is in the command zone, not already cast.
	if o := e.G.Obj(opal); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("precondition: Opal Palace = %+v, want untapped on the battlefield", o)
	}
	if o := e.G.Obj(cmd); o == nil || o.Zone != state.ZCommand {
		t.Fatalf("precondition: commander zone = %v, want the command zone", o.Zone)
	}
	if got := e.CommanderCastsFromCommandZone(0); got != 0 {
		t.Fatalf("precondition: command-zone casts = %d, want 0", got)
	}

	// Cast 1: ordinary red mana (no rider). The commander enters with no
	// counters; the count head is already 1 for this very cast.
	addMana(t, e, 0, "R")
	opalCastCommander(t, e, cmd)
	o := e.G.Obj(cmd)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: commander zone after cast 1 = %v, want the battlefield", o.Zone)
	}
	if got := e.CommanderCastsFromCommandZone(0); got != 1 {
		t.Fatalf("count head after cast 1 = %d, want 1", got)
	}
	if got := o.Counter("P1P1"); got != 0 {
		t.Fatalf("counters after an ordinary-mana cast = %d, want 0 (no rider mana was spent)", got)
	}

	// Return the commander to the command zone for a second cast (test
	// fixture, the castCommanderAndReturn pattern).
	e.emit(events.Event{Kind: events.MoveZone, Obj: cmd, From: state.ZBattlefield, To: state.ZCommand})
	e.pending = nil
	e.Advance()
	if o := e.G.Obj(cmd); o.Zone != state.ZCommand {
		t.Fatalf("precondition: commander zone after return = %v, want the command zone", o.Zone)
	}

	// Cast 2: fund the {2} CR 903.8 tax with colourless mana, then spend
	// Opal Palace's rider mana (the {1} activation cost is paid from the
	// colourless pool, leaving {C}{C} for the tax, then {R} for the pip).
	addMana(t, e, 0, "CCC")
	activateOpalRider(t, e, opal, "R")
	if got := e.G.Players[0].Pool[state.MR]; got != 1 {
		t.Fatalf("precondition: pool R = %d, want the one rider unit", got)
	}
	if n := len(e.G.Players[0].RestrictedMana); n != 1 {
		t.Fatalf("precondition: provenance batches = %d, want Opal Palace's one rider batch", n)
	} else if src := e.G.Players[0].RestrictedMana[0].Source; src != opal {
		t.Fatalf("precondition: rider batch Source = %d, want Opal Palace %d", src, opal)
	}
	opalCastCommander(t, e, cmd)
	o = e.G.Obj(cmd)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: commander zone after cast 2 = %v, want the battlefield", o.Zone)
	}
	if got := e.CommanderCastsFromCommandZone(0); got != 2 {
		t.Fatalf("count head after cast 2 = %d, want 2", got)
	}
	if got := o.Counter("P1P1"); got != 2 {
		t.Fatalf("counters after spending the rider mana on the commander = %d, want 2 (equal to the command-zone cast count)", got)
	}
	// The rider links must not survive the spent batch: no second batch is
	// left to re-grant on a later unrelated entry.
	if n := len(e.G.Players[0].RestrictedMana); n != 0 {
		t.Fatalf("provenance batches after payment = %d, want 0 (the rider batch was consumed)", n)
	}
	commanderReplayCheck(t, e, cfg)
}

// TestOpalPalaceRiderScopesToTheCommittedCommanderSpend proves the rider is
// not misattributed: mana produced with the rider that is NOT spent on the
// commander grants nothing, and mana spent on a different creature spell
// grants nothing to that creature.
func TestOpalPalaceRiderScopesToTheCommittedCommanderSpend(t *testing.T) {
	e, cfg, opal := opalPalaceGame(t, 402)
	cmd := e.G.Players[0].Commanders[0]
	bystander := moveSeededToHand(t, e, 0, "Mountain Goat")

	// Produce the rider mana and spend it on the NON-commander creature:
	// the filter Card.YouOwn+IsCommander fails, so the Goat enters with no
	// counters even though the batch was really consumed by its cast.
	addMana(t, e, 0, "CC")
	activateOpalRider(t, e, opal, "R")
	if n := len(e.G.Players[0].RestrictedMana); n != 1 {
		t.Fatalf("precondition: provenance batches = %d, want 1 before the bystander cast", n)
	}
	castSeeded(t, e, bystander)
	passUntilStackEmpty(t, e, 30)
	if o := e.G.Obj(bystander); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Goat zone = %v, want the battlefield", o.Zone)
	}
	if got := e.G.Obj(bystander).Counter("P1P1"); got != 0 {
		t.Fatalf("Goat counters after the rider mana paid for it = %d, want 0 (not the commander)", got)
	}

	// Produce another rider unit and leave it UNSPENT; cast the commander
	// with ordinary mana. The unused rider mana must not reach the
	// commander's entry.
	toMain1(t, e)
	e.emit(events.Event{Kind: events.Untap, Obj: opal})
	e.pending = nil
	e.Advance()
	addMana(t, e, 0, "CC")
	activateOpalRider(t, e, opal, "R")
	// Bystander is already on the battlefield and the commander costs {R};
	// the rider unit can pay it, so spend an ORDINARY R instead by draining
	// the rider batch's slot into a fresh ordinary unit is not possible --
	// instead cast the commander with ordinary mana only: the pool holds a
	// rider R, and a cost of {R} would take it. So first empty the rider
	// unit by ending the step, then fund an ordinary R.
	e.emit(events.Event{Kind: events.ManaClear, Player: 0})
	if n := len(e.G.Players[0].RestrictedMana); n != 0 {
		t.Fatalf("precondition: rider batch survived the step end: %d", n)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("precondition: pool after the step end = %d, want 0", got)
	}
	addMana(t, e, 0, "R")
	opalCastCommander(t, e, cmd)
	o := e.G.Obj(cmd)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: commander zone = %v, want the battlefield", o.Zone)
	}
	if got := e.CommanderCastsFromCommandZone(0); got != 1 {
		t.Fatalf("count head = %d, want 1", got)
	}
	if got := o.Counter("P1P1"); got != 0 {
		t.Fatalf("commander counters after unused rider mana + ordinary cast = %d, want 0", got)
	}
	commanderReplayCheck(t, e, cfg)
}

// TestParseAddsCounters pins the rider grammar so a malformed value fails
// closed rather than inventing a counter.
func TestParseAddsCounters(t *testing.T) {
	filter, kind, amount, ok := parseAddsCounters("Card.YouOwn+IsCommander_P1P1_ManaAddsCounterNum")
	if !ok || filter != "Card.YouOwn+IsCommander" || kind != "P1P1" || amount != "ManaAddsCounterNum" {
		t.Fatalf("opal parse = (%q,%q,%q,%v)", filter, kind, amount, ok)
	}
	if _, _, _, ok := parseAddsCounters("NoSeparators"); ok {
		t.Fatal("a value with no underscore must fail closed")
	}
	if _, _, _, ok := parseAddsCounters("Card.Creature_P1P1"); ok {
		t.Fatal("a two-part value must fail closed")
	}
	if _, _, _, ok := parseAddsCounters("_P1P1_1"); ok {
		t.Fatal("an empty filter must fail closed")
	}
}
