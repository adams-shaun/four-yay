package rules

import (
	"fmt"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// castOptions lists every "cast" option in the current priority decision
// (all of seat 0's castable hand and graveyard cards, in legalActions'
// order).
func castOptions(t *testing.T, e *Engine) []decision.Option {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority: %+v", d)
	}
	var out []decision.Option
	for _, o := range d.Options {
		if o.Kind == "cast" {
			out = append(out, o)
		}
	}
	return out
}

// submitChoices submits choices against whatever decision is currently
// pending, for that decision's own player.
func submitChoices(t *testing.T, e *Engine, choices ...int) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("submit %v: %v", choices, err)
	}
}

// toMain1 drives to Main1 of whatever turn/seat is currently active --
// idempotent (driveToStep returns immediately once already there), which is
// what lets addMana/putToken be called any number of times across a test
// without re-driving anything.
func toMain1(t *testing.T, e *Engine) {
	t.Helper()
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepMain1)
}

// addMana drives to Main1 (idempotent), adds one ManaAdd event per mana
// symbol in symbols (each byte one of WUBRGC) into p's pool, then re-asks
// priority so the pending decision (what castOptions/castFirst read)
// reflects the funded pool -- the same driveToStep-then-priorityRound shape
// replacement_updated_test.go's castAndResolveTappedCreature already uses
// for the identical reason.
func addMana(t *testing.T, e *Engine, p state.PlayerID, symbols string) {
	t.Helper()
	toMain1(t, e)
	for _, r := range symbols {
		e.emit(events.Event{Kind: events.ManaAdd, Player: p, Counter: string(r), Amount: 1})
	}
	e.priorityRound()
}

// putToken adds a fresh, freely-authored fixture card (never a corpus .txt,
// per the licensing rule) into the game. A card that must start in a
// graveyard or hand is seeded as a REAL deck card through newFixtureDeck's
// extras and moved with a logged MoveZone -- never IsToken, because a token
// off the battlefield ceases to exist (CR 111.7; ceaseDeadTokens would move
// it to ZCeased before the test's own assertions run). A card placed on
// the battlefield via TokenCreate is a real permanent and is fine, but only
// when it STAYS there: putCreature below deliberately uses the seeded-card
// path too, because the flashback test's Bear is sacrificed as a cost and a
// sacrificed token would be exiled by the SBA before the test's own
// assertion runs.
//
// Deliberately leaves e.pending nil (not a fresh ask): the caller's own next
// Advance() (or a later addMana's priorityRound) is what produces a pending
// decision reflecting every fixture placed since the last real ask, in one
// snapshot -- exactly what TestFlashbackCastsFromTheGraveyardPaysASacrificeAndExiles
// needs when it raw-emits a second setup move right after this and then
// calls Advance once for both.
func putToken(t *testing.T, e *Engine, p state.PlayerID, src string, to state.Zone) state.ObjID {
	t.Helper()
	if to == state.ZBattlefield {
		// TokenCreate path: mint a token onto the battlefield. Only safe for
		// a fixture that stays there (see the doc above); the seeded-card
		// path below is the default for anything that can leave it.
		toMain1(t, e)
		c := card(t, src)
		key := fmt.Sprintf("fixture:%d", e.G.NextID)
		if e.G.Tokens == nil {
			e.G.Tokens = map[string]*cards.Card{}
		}
		e.G.Tokens[key] = c
		e.emit(events.Event{Kind: events.TokenCreate, Player: p, Text: key})
		e.pending = nil
		return e.G.NextID - 1
	}
	return moveSeeded(t, e, p, src, to)
}

// moveSeeded finds the fixture card named by src -- seeded into seat p's
// deck by newFixtureDeck's extras, so genesis created it as a real card
// (IsToken=false) at an ID replayFromLog's own genesis reproduces -- and
// moves it to to with a logged MoveZone. The card is a real deck card, so
// the SBA never exiles it and replayCheck sees the same object in both
// games.
func moveSeeded(t *testing.T, e *Engine, p state.PlayerID, src string, to state.Zone) state.ObjID {
	t.Helper()
	toMain1(t, e)
	name := card(t, src).Faces[0].Name
	for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				e.pending = nil
				return id
			}
		}
	}
	t.Fatalf("fixture card %q not found in seat %d's library or hand -- was it seeded via newFixtureDeck's extras?", name, p)
	return 0
}

func putCreature(t *testing.T, e *Engine, p state.PlayerID, src string) state.ObjID {
	t.Helper()
	// Seeded-card path, not TokenCreate: the flashback test's Bear is
	// sacrificed as a cost, and a sacrificed token would be exiled by the
	// SBA (CR 111.7) before the test's own assertion runs.
	return moveSeeded(t, e, p, src, state.ZBattlefield)
}

func addToGraveyard(t *testing.T, e *Engine, p state.PlayerID, src string) state.ObjID {
	t.Helper()
	return moveSeeded(t, e, p, src, state.ZGraveyard)
}

func addToHand(t *testing.T, e *Engine, p state.PlayerID, src string) state.ObjID {
	t.Helper()
	return moveSeeded(t, e, p, src, state.ZHand)
}

// castObj picks the cast option for id out of the current priority decision,
// answers a target decision with option 0 if one appears, then drains the
// stack -- a real player would let their spell resolve (or at least empty
// the stack) before taking a further sorcery-speed action, and the surge
// test's own Reckless Bushwhacker (a creature, sorcery-speed-only) is not
// offered at all while anything else sits on the stack.
func castObj(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget && len(d.Options) > 0 {
		submitChoices(t, e, d.Options[0].Index)
	}
	passUntilStackEmpty(t, e, 20)
}

// hasEvent reports whether the log carries an event of kind against obj.
func hasEvent(e *Engine, kind events.Kind, obj state.ObjID) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == kind && ev.Obj == obj {
			return true
		}
	}
	return false
}

// hasNote reports whether the log carries a Note event whose Text contains substr.
func hasNote(e *Engine, substr string) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, substr) {
			return true
		}
	}
	return false
}

// replayCheck rebuilds the game from the log alone and compares it with the
// live game — the same fidelity check trigger_test.go's replayFromLog gives.
func replayCheck(t *testing.T, e *Engine, cfg Config) {
	t.Helper()
	if diff := diffGames(e.G, replayFromLog(t, cfg, e.L.Events)); diff != "" {
		t.Fatalf("log-only replay differs:\n%s", diff)
	}
}

func TestXIsChosenAndRecorded(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 20, "Name:Endless\nManaCost:X\nTypes:Creature Eldrazi\nPT:0/0\nOracle:x\n")
	addMana(t, e, 0, "GGG") // helper: three ManaAdd events into seat 0's pool
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" || len(d.Options) != 4 || d.Options[3].Label != "X = 3" {
		t.Fatalf("X decision %+v", d)
	}
	submitChoices(t, e, 2)
	o := e.G.Obj(id)
	if o.Zone != state.ZStack || o.X != 2 || e.G.Players[0].Pool.Total() != 1 {
		t.Fatalf("after X=2: zone %s X %d pool %d", o.Zone, o.X, e.G.Players[0].Pool.Total())
	}
	if !hasEvent(e, events.CastInfo, id) {
		t.Fatal("no CastInfo event")
	}
	replayCheck(t, e, cfg)
}

func TestKickerOffersASecondCastOptionAndFlagsTheSpell(t *testing.T) {
	t.Parallel()
	src := "Name:Whacker\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nK:Kicker:R\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self+kicked | Execute$ TrigPump | TriggerDescription$ if kicked\n" +
		"SVar:TrigPump:DB$ PumpAll | ValidCards$ Creature.YouCtrl | NumAtt$ +1\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 22, src)
	addMana(t, e, 0, "R")
	if opts := castOptions(t, e); len(opts) != 1 || opts[0].Mode != "" {
		t.Fatalf("one mana: %+v", opts)
	}
	addMana(t, e, 0, "R")
	opts := castOptions(t, e)
	if len(opts) != 2 || opts[1].Mode != "kicked" || opts[1].Label != "Cast Whacker (kicked)" {
		t.Fatalf("two mana: %+v", opts)
	}
	submitChoices(t, e, opts[1].Index)
	if o := e.G.Obj(id); o.CastFlags&state.FlagKicked == 0 || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("kicked cast: %+v pool %d", o, e.G.Players[0].Pool.Total())
	}
	passUntilStackEmpty(t, e, 20)
	if len(e.pendingTriggers) == 0 && !hasEvent(e, events.TriggerPush, id) {
		t.Fatal("the 'if kicked' trigger did not fire")
	}
	// Not kicked: the trigger must not fire.
	e2, _, id2 := newFixtureDeck(t, 25, src)
	addMana(t, e2, 0, "RR")
	submitChoices(t, e2, castOptions(t, e2)[0].Index)
	passUntilStackEmpty(t, e2, 20)
	if hasEvent(e2, events.TriggerPush, id2) {
		t.Fatal("the 'if kicked' trigger fired on an unkicked cast")
	}
}

func TestSurgeNeedsAnotherSpellThisTurn(t *testing.T) {
	t.Parallel()
	src := "Name:Reckless\nManaCost:2 R\nTypes:Creature Goblin\nPT:2/1\nK:Surge:1 R\nK:Haste\nOracle:x\n"
	boltSrc := "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n"
	e, _, reckless := newFixtureDeck(t, 24, src, boltSrc)
	bolt := addToHand(t, e, 0, boltSrc)
	addMana(t, e, 0, "RRR")
	for _, o := range castOptions(t, e) {
		if o.Mode == "surged" {
			t.Fatal("surge offered before any spell this turn")
		}
	}
	castObj(t, e, bolt) // helper: choose the cast option for this object, answer its target with the first option
	if pay.SpellsCastThisTurn(asPayer(e), 0) != 1 {
		t.Fatalf("spells cast this turn = %d", pay.SpellsCastThisTurn(asPayer(e), 0))
	}
	var surged *decision.Option
	for _, o := range castOptions(t, e) {
		if o.Mode == "surged" {
			surged = &o
		}
	}
	if surged == nil {
		t.Fatal("surge not offered after a spell this turn")
	}
	pool := e.G.Players[0].Pool.Total()
	submitChoices(t, e, surged.Index)
	if e.G.Players[0].Pool.Total() != pool-2 {
		t.Fatal("surge cost {1}{R} not what was paid")
	}
	if o := e.G.Obj(reckless); o.CastFlags&state.FlagSurged == 0 {
		t.Fatalf("surged cast not flagged: %+v", o.CastFlags)
	}
}

func TestFlashbackCastsFromTheGraveyardPaysASacrificeAndExiles(t *testing.T) {
	t.Parallel()
	src := "Name:Therapy\nManaCost:B\nTypes:Sorcery\nK:Flashback:Sac<1/Creature>\n" +
		"A:SP$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	bearSrc := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, therapy := newFixtureDeck(t, 25, src, bearSrc)
	bear := putCreature(t, e, 0, bearSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: therapy, From: state.ZHand, To: state.ZGraveyard})
	e.Advance()
	var fb *decision.Option
	for _, o := range castOptions(t, e) {
		if o.Mode == "flashback" && o.Obj == therapy {
			fb = &o
		}
	}
	if fb == nil {
		t.Fatal("flashback not offered from the graveyard")
	}
	submitChoices(t, e, fb.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "sacrifice" || d.Options[0].Obj != bear {
		t.Fatalf("sacrifice choice %+v", d)
	}
	submitChoices(t, e, 0)
	if e.G.Obj(bear).Zone != state.ZGraveyard || e.G.Obj(therapy).Zone != state.ZStack || e.G.Obj(therapy).CastFlags&state.FlagFlashback == 0 {
		t.Fatalf("after paying: bear %s therapy %s", e.G.Obj(bear).Zone, e.G.Obj(therapy).Zone)
	}
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(therapy).Zone != state.ZExile {
		t.Fatalf("flashback spell went to %s, want exile", e.G.Obj(therapy).Zone)
	}
	replayCheck(t, e, cfg)
}

func TestDelveExilesFromTheGraveyardToPayGeneric(t *testing.T) {
	t.Parallel()
	src := "Name:Angler\nManaCost:6 B\nTypes:Creature Zombie Fish\nPT:5/5\nK:Delve\nOracle:x\n"
	junkSrc := "Name:Junk\nManaCost:1\nTypes:Sorcery\nOracle:x\n"
	e, cfg, angler := newFixtureDeck(t, 26, src, junkSrc, junkSrc, junkSrc, junkSrc)
	var gy []state.ObjID
	for i := 0; i < 4; i++ {
		gy = append(gy, addToGraveyard(t, e, 0, junkSrc))
	}
	addMana(t, e, 0, "BGG")
	opts := castOptions(t, e)
	if len(opts) != 1 {
		t.Fatalf("with delve, {6}{B} is castable off B+2 and four graveyard cards: %+v", opts)
	}
	submitChoices(t, e, opts[0].Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "exile" || d.Min != 0 || d.Max != 4 {
		t.Fatalf("delve choice %+v", d)
	}
	submitChoices(t, e, 0, 1, 2, 3)
	if e.G.Obj(angler).Zone != state.ZStack || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("angler %s pool %d", e.G.Obj(angler).Zone, e.G.Players[0].Pool.Total())
	}
	for _, id := range gy {
		if e.G.Obj(id).Zone != state.ZExile {
			t.Fatal("delved card not exiled")
		}
	}
	replayCheck(t, e, cfg)
	// Without delve fodder the same hand is not castable.
	e2, _, _ := newFixtureDeck(t, 27, src)
	addMana(t, e2, 0, "BGG")
	if len(castOptions(t, e2)) != 0 {
		t.Fatal("castable without enough mana or graveyard")
	}
}

// TestDelveSkipsCleanlyWithAnEmptyGraveyard: Delve with no graveyard cards
// must not ask an exile decision at all -- the cast goes straight from the
// priority option to the stack, paying the full generic requirement.
func TestDelveSkipsCleanlyWithAnEmptyGraveyard(t *testing.T) {
	t.Parallel()
	src := "Name:Angler\nManaCost:6 B\nTypes:Creature Zombie Fish\nPT:5/5\nK:Delve\nOracle:x\n"
	e, cfg, angler := newFixtureDeck(t, 28, src)
	addMana(t, e, 0, "BBBBBBB")
	opts := castOptions(t, e)
	if len(opts) != 1 {
		t.Fatalf("castable off six mana alone: %+v", opts)
	}
	submitChoices(t, e, opts[0].Index)
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("delve asked an exile decision with an empty graveyard: %+v", d)
	}
	if e.G.Obj(angler).Zone != state.ZStack || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("angler %s pool %d", e.G.Obj(angler).Zone, e.G.Players[0].Pool.Total())
	}
	replayCheck(t, e, cfg)
}

// TestXZeroIsTheOnlyOption: with no mana at all, {X} still offers X = 0 as
// the sole option and the spell is cast for free.
func TestXZeroIsTheOnlyOption(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 29, "Name:Endless\nManaCost:X\nTypes:Creature Eldrazi\nPT:0/0\nOracle:x\n")
	toMain1(t, e)
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" || len(d.Options) != 1 || d.Options[0].Label != "X = 0" {
		t.Fatalf("X decision %+v", d)
	}
	submitChoices(t, e, 0)
	if o := e.G.Obj(id); o.Zone != state.ZStack || o.X != 0 {
		t.Fatalf("after X=0: zone %s X %d", o.Zone, o.X)
	}
	replayCheck(t, e, cfg)
}

// TestUnderDelveAbortsTheCast: a hand-built intent that promises more Delve
// credit than the graveyard can cover reaches commitCast with an unpayable
// cost; the cast must abort with a Note, leave the card where it was, clear
// the flow, and let priority resume so the game is not stranded. (The
// engine's own delve decision caps Max at the graveyard size, so this is
// only reachable from a hand-built intent -- exactly the stranding risk the
// review called out.)
func TestUnderDelveAbortsTheCast(t *testing.T) {
	t.Parallel()
	src := "Name:Angler\nManaCost:6 B\nTypes:Creature Zombie Fish\nPT:5/5\nK:Delve\nOracle:x\n"
	junkSrc := "Name:Junk\nManaCost:1\nTypes:Sorcery\nOracle:x\n"
	e, cfg, angler := newFixtureDeck(t, 30, src, junkSrc)
	junk := addToGraveyard(t, e, 0, junkSrc)
	addMana(t, e, 0, "B")
	// Claim two delved cards when only one exists: generic 6 - 2 = 4 plus
	// {B} is not payable off a single {B} pool.
	e.cast = &pendingCast{player: 0, card: angler, from: state.ZHand, mode: "", ability: -1,
		cost: ParseCost("6 B"), x: 0, xDone: true, delveDone: true,
		delve: []state.ObjID{junk, junk}}
	// The transaction commits via the split stage flow (push, target, pay) -
	// the cost is unpayable, so payCast's abort arm reverses the push and
	// clears the flow, leaving the card in hand.
	e.continueCast()
	if !hasNote(e, "cast aborted: cost no longer payable") {
		t.Fatal("no abort Note")
	}
	if e.G.Obj(angler).Zone != state.ZHand {
		t.Fatalf("angler %s, want hand (card stays put)", e.G.Obj(angler).Zone)
	}
	if e.cast != nil || e.choosing != chooseNone {
		t.Fatal("cast flow not cleared after abort")
	}
	e.Advance()
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority did not resume after abort: %+v", d)
	}
	replayCheck(t, e, cfg)
}

// TestFlashbackedSpellCounteredGoesToExile: CR 702.33b exiles a flashbacked
// spell "any time it would leave the stack", which includes being countered
// -- effCounter must not send it back to the graveyard where it could be
// flashbacked again.
func TestFlashbackedSpellCounteredGoesToExile(t *testing.T) {
	t.Parallel()
	src := "Name:Therapy\nManaCost:B\nTypes:Sorcery\nK:Flashback:Sac<1/Creature>\n" +
		"A:SP$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	bearSrc := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, therapy := newFixtureDeck(t, 33, src, bearSrc)
	putCreature(t, e, 0, bearSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: therapy, From: state.ZHand, To: state.ZGraveyard})
	e.Advance()
	var fb *decision.Option
	for _, o := range castOptions(t, e) {
		if o.Mode == "flashback" && o.Obj == therapy {
			fb = &o
		}
	}
	if fb == nil {
		t.Fatal("flashback not offered from the graveyard")
	}
	submitChoices(t, e, fb.Index)
	submitChoices(t, e, 0) // sacrifice the Bear
	if e.G.Obj(therapy).Zone != state.ZStack {
		t.Fatalf("therapy %s, want stack", e.G.Obj(therapy).Zone)
	}
	// Counter it with a hand-built Counter effect against the stack object.
	effects.Resolve(e, &effects.Ctx{Controller: 1, Targets: []state.Target{{Obj: therapy}}, TargetsOffered: true},
		card(t, "Name:Counterspell\nManaCost:U U\nTypes:Instant\nA:SP$ Counter | ValidTgts$ Spell\nOracle:x\n").Faces[0].SpellAbility())
	if e.G.Obj(therapy).Zone != state.ZExile {
		t.Fatalf("countered flashbacked spell went to %s, want exile", e.G.Obj(therapy).Zone)
	}
	replayCheck(t, e, cfg)
}

// TestAlternativeCostWithSacPartIsGatedOnCastable: an AlternativeCost whose
// Cost$ carries a Sac part must only be offered when the sacrifice is
// actually satisfiable -- otherwise the cast flow would ask a sacrifice
// decision with no options and the game could never leave the cast.
func TestAlternativeCostWithSacPartIsGatedOnCastable(t *testing.T) {
	t.Parallel()
	src := "Name:Zap\nManaCost:2 R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\n" +
		"S:Mode$ AlternativeCost | ValidCard$ Card.Self | Cost$ Sac<1/Creature>\nOracle:x\n"
	// No creatures: the base cost is castable off three red, but the
	// sacrifice alternative must NOT be offered -- the cast flow would ask a
	// sacrifice decision with no options and the game could never leave the
	// cast.
	e, _, _ := newFixtureDeck(t, 31, src)
	addMana(t, e, 0, "RRR")
	opts := castOptions(t, e)
	if len(opts) != 1 || opts[0].AltCostIndex != 0 {
		t.Fatalf("alt cost offered with no creature to sacrifice: %+v", opts)
	}
	// With a creature on the battlefield the alt option appears and the
	// sacrifice is asked and paid.
	bearSrc := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e2, _, _ := newFixtureDeck(t, 32, src, bearSrc)
	bear := putCreature(t, e2, 0, bearSrc)
	addMana(t, e2, 0, "R")
	var alt *decision.Option
	for _, o := range castOptions(t, e2) {
		if o.AltCostIndex != 0 {
			alt = &o
		}
	}
	if alt == nil {
		t.Fatal("alt cost not offered with a creature to sacrifice")
	}
	submitChoices(t, e2, alt.Index)
	d := e2.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "sacrifice" || d.Options[0].Obj != bear {
		t.Fatalf("sacrifice choice %+v", d)
	}
	submitChoices(t, e2, 0)
	// CR 601.2c-before-601.2h: the sacrifice is part of paying the cost
	// (601.2h), so it is executed only once the target is chosen (601.2c).
	// Answer the target, then assert the bear was sacrificed.
	td := e2.Pending()
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("expected a target decision after the sacrifice choice, got %+v", td)
	}
	submitChoices(t, e2, indexOfPlayerOption(td, 1))
	if e2.G.Obj(bear).Zone != state.ZGraveyard {
		t.Fatalf("bear %s, want graveyard", e2.G.Obj(bear).Zone)
	}
}
