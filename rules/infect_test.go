package rules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// corpusInfectCard parses the live GPL corpus at test time so the
// kw:Infect primitives this ticket registers are exercised against the
// exact Forge scripts whose coverage entries they retire, the way
// combat_keywords_test.go's corpusKeywordCard does for its keywords.
func corpusInfectCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	paths := map[string]string{
		"Blight Mamba":         "b/blight_mamba.txt",
		"Blightsteel Colossus": "b/blightsteel_colossus.txt",
		"Grafted Exoskeleton":  "g/grafted_exoskeleton.txt",
		"Ichor Rats":           "i/ichor_rats.txt",
		"Winding Constrictor":  "w/winding_constrictor.txt",
	}
	path, ok := paths[name]
	if !ok {
		t.Fatalf("no corpus path for %q", name)
	}
	c, ds := cards.Parse(filepath.Join("..", ".cards", "cardsfolder", path))
	if len(ds) != 0 {
		t.Fatalf("parse %s: %v", name, ds)
	}
	if ds = c.Link(); len(ds) != 0 {
		t.Fatalf("link %s: %v", name, ds)
	}
	return c
}

// TestInfectDamageToCreatureIsMinusOneCountersNotMarkedDamage pins CR
// 702.90b's creature half: Blight Mamba's combat damage to its blocker is
// dealt as a -1/-1 counter, with NO marked damage, while the blocker's own
// (non-infect) hit back stays ordinary marked damage.
func TestInfectDamageToCreatureIsMinusOneCountersNotMarkedDamage(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	mamba := onBoardCard(t, e, 0, corpusInfectCard(t, "Blight Mamba"))
	e.G.Obj(mamba).SummonSick = false
	guard := onBoard(t, e, 1, "Name:Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/2\nOracle:x\n")

	// Preconditions the assertions below depend on: the printed infect
	// keyword is readable before the step, and the blocker arrives
	// undamaged, uncountered and tough enough that the one counter does not
	// kill it (so the assertions read a live battlefield object).
	if !e.HasKeyword(mamba, "Infect") {
		t.Fatal("precondition: Blight Mamba does not read infect before the damage step")
	}
	if e.G.Obj(mamba).Face().PT != "1/1" {
		t.Fatalf("precondition: Blight Mamba PT = %s, want 1/1", e.G.Obj(mamba).Face().PT)
	}
	if got := e.G.Obj(guard).Counter("M1M1"); got != 0 {
		t.Fatalf("precondition: blocker already carries %d -1/-1 counters", got)
	}
	if got := e.G.Obj(guard).Damage; got != 0 {
		t.Fatalf("precondition: blocker already carries %d marked damage", got)
	}

	e.askAttackers()
	submitAttackers(t, e, mamba)
	submitBlockers(t, e, guard)

	if got := e.G.Obj(guard).Counter("M1M1"); got != 1 {
		t.Fatalf("blocker -1/-1 counters = %d, want 1 (the mamba's power, dealt in counter form)", got)
	}
	if got := e.G.Obj(guard).Damage; got != 0 {
		t.Fatalf("blocker marked damage = %d, want 0 (infect damage is never marked)", got)
	}
	if o := e.G.Obj(guard); o.Zone != state.ZBattlefield {
		t.Fatalf("2/2 blocker with one -1/-1 counter is in %v, want battlefield (toughness 1 survives)", o.Zone)
	}
	if got := e.G.Obj(mamba).Counter("M1M1"); got != 0 {
		t.Fatalf("attacker -1/-1 counters = %d, want 0 (the blocker has no infect, its hit back is marked damage)", got)
	}
	if o := e.G.Obj(mamba); o.Zone == state.ZBattlefield && o.Damage != 1 {
		t.Fatalf("attacker marked damage = %d, want 1 (the blocker's ordinary hit back)", o.Damage)
	}
}

// TestInfectDamageToPlayerIsPoisonAndTenPoisonLoses pins CR 702.90b's
// player half and CR 704.5b's end of the same line: an unblocked Blightsteel
// Colossus deals its 11 power to the defending player as 11 poison counters
// (life untouched), and the tenth poison counter loses the game.
func TestInfectDamageToPlayerIsPoisonAndTenPoisonLoses(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	colo := onBoardCard(t, e, 0, corpusInfectCard(t, "Blightsteel Colossus"))
	e.G.Obj(colo).SummonSick = false

	// Preconditions: the keywords are readable and the defender is alive,
	// unpoisoned and at the engine's starting life.
	if !e.HasKeyword(colo, "Infect") {
		t.Fatal("precondition: Blightsteel Colossus does not read infect")
	}
	if got := e.G.Players[1].Counter("POISON"); got != 0 {
		t.Fatalf("precondition: defender already carries %d poison", got)
	}
	if lost := e.G.Players[1].Lost; lost {
		t.Fatal("precondition: defender already lost")
	}

	e.askAttackers()
	submitAttackers(t, e, colo)

	if got := e.G.Players[1].Counter("POISON"); got != 11 {
		t.Fatalf("defender poison = %d, want 11 (the colossus's power, dealt in poison form)", got)
	}
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("defender life = %d, want 20 (infect damage to a player is never life loss)", got)
	}
	if !e.G.Players[1].Lost {
		t.Fatal("defender with 11 poison counters did not lose (CR 704.5b)")
	}
}

// TestGraftedExoskeletonGrantedInfectDealsInCounterForm pins the
// static-grant path: the equipment's `AddKeyword$ Infect` makes the equipped
// creature read infect (so its combat damage converts), here as poison on
// the defending player for its boosted 4 power.
func TestGraftedExoskeletonGrantedInfectDealsInCounterForm(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	exo := onBoardCard(t, e, 0, corpusInfectCard(t, "Grafted Exoskeleton"))
	bearer := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Obj(bearer).SummonSick = false

	// Attach through the ordinary event, then prove the grant path before
	// the step: the equipped creature must read infect from the static's
	// AddKeyword$, and the +2/+2 rider must have raised its power to 4 --
	// the number the poison count below is asserted against.
	e.emit(events.Event{Kind: events.Attach, Obj: exo, IDs: []state.ObjID{bearer}})
	if !e.HasKeyword(bearer, "Infect") {
		t.Fatal("precondition: the equipped creature does not read granted infect")
	}
	if got := e.Power(bearer); got != 4 {
		t.Fatalf("precondition: equipped bearer power = %d, want 4 (the static's +2/+2)", got)
	}
	if got := e.G.Players[1].Counter("POISON"); got != 0 {
		t.Fatalf("precondition: defender already carries %d poison", got)
	}

	e.askAttackers()
	submitAttackers(t, e, bearer)

	if got := e.G.Players[1].Counter("POISON"); got != 4 {
		t.Fatalf("defender poison = %d, want 4 (the granted-infect bearer's boosted power)", got)
	}
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("defender life = %d, want 20 (granted infect converts the same way)", got)
	}
}

// TestInfectDamageToNonCreatureArtifactIsMarked pins the recipient-form
// split the r2 review demanded (CR 702.90b rewrites creature and player
// damage ONLY): an infect source's damage to a plain artifact -- a legal
// DealDamage target that is neither creature nor player -- is ordinary
// marked damage, with NO -1/-1 counters and NO -1/-1 CounterChange event in
// the log.
func TestInfectDamageToNonCreatureArtifactIsMarked(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	mamba := onBoardCard(t, e, 0, corpusInfectCard(t, "Blight Mamba"))
	artifact := onBoard(t, e, 1, "Name:Brass Sprocket\nManaCost:2\nTypes:Artifact\nOracle:x\n")

	// Preconditions the assertions below depend on: the source reads
	// infect, the artifact is on the battlefield, reads as a NON-creature,
	// and arrives undamaged and uncountered.
	if !e.HasKeyword(mamba, "Infect") {
		t.Fatal("precondition: Blight Mamba does not read infect")
	}
	if o := e.G.Obj(artifact); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: the artifact is not on the battlefield")
	}
	if e.IsCreature(artifact) {
		t.Fatal("precondition: the artifact reads as a creature")
	}

	effects.Resolve(e, &effects.Ctx{Source: mamba, Controller: 0,
		Targets: []state.Target{{Obj: artifact}}}, &cards.SA{Kind: "DB", API: "DealDamage",
		Params: map[string]string{"Defined": "Targeted", "NumDmg": "3"}})

	if got := e.G.Obj(artifact).Damage; got != 3 {
		t.Fatalf("artifact marked damage = %d, want 3 (a non-creature recipient takes infect damage normally)", got)
	}
	if got := e.G.Obj(artifact).Counter("M1M1"); got != 0 {
		t.Fatalf("artifact -1/-1 counters = %d, want 0 (CR 702.90b rewrites creature damage only)", got)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == artifact && ev.Counter == "M1M1" {
			t.Fatalf("the log carries a -1/-1 CounterChange for the artifact (%+v): the fold must not convert a non-creature recipient", ev)
		}
	}
}

// TestInfectDamageToPlainPlaneswalkerOnlyRemovesLoyalty pins the same
// recipient-form split for a printed planeswalker: CR 306.8's loyalty
// exchange applies and nothing else -- no marked damage, no -1/-1 counters
// (the r1 defect put counters on the walker IN ADDITION to the loyalty).
func TestInfectDamageToPlainPlaneswalkerOnlyRemovesLoyalty(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	mamba := onBoardCard(t, e, 0, corpusInfectCard(t, "Blight Mamba"))
	walker := onBoard(t, e, 1, "Name:Plain Walker\nTypes:Planeswalker Jace\nLoyalty:5\nOracle:x\n")
	// onBoard's placement is eventless, so the printed starting loyalty is
	// not granted by it; place it through the ordinary counter event.
	e.emit(events.Event{Kind: events.CounterChange, Obj: walker, Counter: "LOYALTY", Amount: 5})

	// Preconditions: the source reads infect; the walker is on the
	// battlefield with 5 loyalty, reads as a NON-creature, and arrives
	// uncountered and undamaged.
	if !e.HasKeyword(mamba, "Infect") {
		t.Fatal("precondition: Blight Mamba does not read infect")
	}
	if o := e.G.Obj(walker); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: the walker is not on the battlefield")
	}
	if e.IsCreature(walker) {
		t.Fatal("precondition: the walker reads as a creature")
	}
	if got := e.G.Obj(walker).Counter("LOYALTY"); got != 5 {
		t.Fatalf("precondition: walker loyalty = %d, want 5", got)
	}

	effects.Resolve(e, &effects.Ctx{Source: mamba, Controller: 0,
		Targets: []state.Target{{Obj: walker}}}, &cards.SA{Kind: "DB", API: "DealDamage",
		Params: map[string]string{"Defined": "Targeted", "NumDmg": "3"}})

	if got := e.G.Obj(walker).Counter("LOYALTY"); got != 2 {
		t.Fatalf("walker loyalty = %d, want 2 (CR 306.8's loyalty exchange is the only form)", got)
	}
	if got := e.G.Obj(walker).Damage; got != 0 {
		t.Fatalf("walker marked damage = %d, want 0", got)
	}
	if got := e.G.Obj(walker).Counter("M1M1"); got != 0 {
		t.Fatalf("walker -1/-1 counters = %d, want 0 (CR 702.90b does not rewrite non-creature permanent damage)", got)
	}
}

// TestInfectCountersRideTheCounterReplacementPath pins the r2 review's
// second MAJOR: the conversion emits REAL CounterChange/PlayerCounterChange
// events, so the repl:AddCounter class replaces them -- Winding
// Constrictor's "one more" lines double both the -1/-1 counters an opposing
// infect creature puts on your blocker and the poison counters one puts on
// you, exactly as it doubles any other placement.
func TestInfectCountersRideTheCounterReplacementPath(t *testing.T) {
	t.Parallel()
	constrictor := corpusInfectCard(t, "Winding Constrictor")

	// Creature half: the constrictor's controller blocks an opposing infect
	// creature, and its `ValidCard$ Creature.YouCtrl` line doubles the
	// -1/-1 counters the hit converts to.
	e := combatEngine(t)
	onBoardCard(t, e, 1, constrictor)
	mamba := onBoardCard(t, e, 0, corpusInfectCard(t, "Blight Mamba"))
	e.G.Obj(mamba).SummonSick = false
	// A 4/4 blocker so the DOUBLED pair of -1/-1 counters leaves it alive --
	// a 2/2 would die at toughness 0 and its counters would leave with it.
	blocker := onBoard(t, e, 1, "Name:Bear\nManaCost:3 G\nTypes:Creature Bear\nPT:4/4\nOracle:x\n")
	if got := e.G.Obj(blocker).Counter("M1M1"); got != 0 {
		t.Fatalf("precondition: blocker already carries %d -1/-1 counters", got)
	}
	e.askAttackers()
	submitAttackers(t, e, mamba)
	submitBlockers(t, e, blocker)
	if got := e.G.Obj(blocker).Counter("M1M1"); got != 2 {
		t.Fatalf("blocker -1/-1 counters = %d, want 2 (the mamba's 1, doubled by the constrictor's AddCounter replacement)", got)
	}

	// Player half: the same constrictor's controller is hit unblocked by an
	// opposing infect creature, and its `ValidPlayer$ You` line doubles the
	// poison the hit converts to.
	e2 := combatEngine(t)
	onBoardCard(t, e2, 1, constrictor)
	mamba2 := onBoardCard(t, e2, 0, corpusInfectCard(t, "Blight Mamba"))
	e2.G.Obj(mamba2).SummonSick = false
	if got := e2.G.Players[1].Counter("POISON"); got != 0 {
		t.Fatalf("precondition: defender already carries %d poison", got)
	}
	e2.askAttackers()
	submitAttackers(t, e2, mamba2)
	// The defender controls a creature (the constrictor), so the combat
	// flow waits for its (empty) block declaration before the damage step.
	submitBlockers(t, e2)
	if got := e2.G.Players[1].Counter("POISON"); got != 2 {
		t.Fatalf("defender poison = %d, want 2 (the mamba's 1, doubled by the constrictor's AddCounter replacement)", got)
	}
	if got := e2.G.Players[1].Life; got != 20 {
		t.Fatalf("defender life = %d, want 20", got)
	}
	if e2.G.Players[1].Lost {
		t.Fatal("defender lost with only 2 poison counters")
	}
}

// TestInfectDamageRedirectedToCreatureIsCountersOnly pins the review-sol1
// MAJOR: a damage REDIRECT changes the event's recipient (Palisade Giant's
// `ReplaceEvent Affected$ Self` moves a player-targeted hit onto the Giant),
// and the infect marker must be recomputed for the NEW recipient. Before the
// fix the bare "infect" (player-form) marker survived onto the creature
// recipient, so events.Apply marked ordinary damage on the Giant AND
// convertInfectDamage placed -1/-1 counters for the same hit -- both forms of
// one hit. After the fix the redirect lands exactly one form: -1/-1 counters,
// no marked damage.
func TestInfectDamageRedirectedToCreatureIsCountersOnly(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	// The infect attacker belongs to the active player (seat 1) and swings
	// unblocked at seat 0, whose Palisade Giant redirects the hit to itself.
	e.G.Active = 1
	giant := onBoardCard(t, e, 0, mustCorpusCard(t, sharedCorpus(t), "Palisade Giant"))
	mamba := onBoardCard(t, e, 1, corpusInfectCard(t, "Blight Mamba"))
	e.G.Obj(mamba).SummonSick = false

	// Preconditions the assertions below depend on: the source reads infect,
	// the redirect target is on the battlefield as a creature, and it
	// arrives undamaged and uncountered -- so the two counts below read a
	// single hit's result, not a leftover from setup.
	if !e.HasKeyword(mamba, "Infect") {
		t.Fatal("precondition: Blight Mamba does not read infect")
	}
	if o := e.G.Obj(giant); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Palisade Giant is not on the battlefield")
	}
	if !e.IsCreature(giant) {
		t.Fatal("precondition: Palisade Giant does not read as a creature")
	}
	if got := e.G.Obj(giant).Damage; got != 0 {
		t.Fatalf("precondition: Palisade Giant already carries %d marked damage", got)
	}
	if got := e.G.Obj(giant).Counter("M1M1"); got != 0 {
		t.Fatalf("precondition: Palisade Giant already carries %d -1/-1 counters", got)
	}

	e.askAttackers()
	submitAttackers(t, e, mamba)
	// Seat 0 controls a creature, so combat waits for its (empty) block
	// declaration before the unblocked hit lands.
	submitBlockers(t, e)

	if got := e.G.Obj(giant).Counter("M1M1"); got != 1 {
		t.Fatalf("Palisade Giant -1/-1 counters = %d, want 1 (the redirected infect hit's counter form)", got)
	}
	if got := e.G.Obj(giant).Damage; got != 0 {
		t.Fatalf("Palisade Giant marked damage = %d, want 0 (a redirected infect hit lands in counter form ONLY, never both)", got)
	}
	if got := e.G.Players[0].Life; got != 20 {
		t.Fatalf("defending player life = %d, want 20 (the hit was redirected away)", got)
	}
	if got := e.G.Players[0].Counter("POISON"); got != 0 {
		t.Fatalf("defending player poison = %d, want 0 (the hit was redirected away)", got)
	}
}

// TestInfectDamageRedirectedToPlayerIsPoisonOnly is the reverse redirect: a
// creature-targeted infect hit (the compound "infect+creature" marker) moved
// onto a player must become the player form -- poison counters, no -1/-1
// counters and no game loss from an impossible creature counter. It drives
// ReplaceEvent directly, the one recipient-rewriting site a redirect owns.
func TestInfectDamageRedirectedToPlayerIsPoisonOnly(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	mamba := onBoardCard(t, e, 0, corpusInfectCard(t, "Blight Mamba"))
	self := onBoard(t, e, 0, "Name:Redirector\nTypes:Enchantment\nOracle:x\n")

	// Preconditions: the source reads infect and the held event starts in
	// the creature form with a creature recipient that exists.
	if !e.HasKeyword(mamba, "Infect") {
		t.Fatal("precondition: Blight Mamba does not read infect")
	}
	if got := e.G.Players[1].Counter("POISON"); got != 0 {
		t.Fatalf("precondition: seat 1 already carries %d poison", got)
	}

	e.replacingSource = self
	held := events.Event{Kind: events.Damage, Obj: mamba, Amount: 3, Counter: "infect+creature"}
	e.replacingEvent = &held
	e.ReplaceEvent("Affected", "You", 0)
	e.replacingEvent = nil
	e.replacingSource = 0

	if held.Player != 0 || held.Obj != 0 {
		t.Fatalf("redirected recipient = obj %d / player %d, want player 0", held.Obj, held.Player)
	}
	if held.Counter != "infect" {
		t.Fatalf("redirected marker = %q, want %q (creature->player must become the poison form)", held.Counter, "infect")
	}
}

// TestInfectDeathtouchKillsThroughCounters pins the counter-form
// half of CR 704.5g: a deathtouch infect creature's damage is dealt as
// -1/-1 counters with nothing marked, and the creature it damaged still
// dies -- the Deathtouched mark is lethal on its own, without marked
// damage, exactly as it is beside it.
func TestInfectDeathtouchKillsThroughCounters(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	stinger := onBoard(t, e, 0, "Name:Toxic Stinger\nManaCost:B\nTypes:Creature Insect\nPT:1/1\nK:Deathtouch\nK:Infect\nOracle:x\n")
	e.G.Obj(stinger).SummonSick = false
	blocker := onBoard(t, e, 1, "Name:Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/2\nOracle:x\n")

	// Preconditions: the stinger reads both keywords, the blocker is on the
	// battlefield, undamaged, uncountered, and tall enough that the one
	// -1/-1 counter ALONE would not kill it (toughness 1 after it) -- so
	// only the deathtouch half can be why it dies.
	if !e.HasKeyword(stinger, "Infect") || !e.HasKeyword(stinger, "Deathtouch") {
		t.Fatal("precondition: the stinger does not read deathtouch + infect")
	}
	if o := e.G.Obj(blocker); o.Zone != state.ZBattlefield || o.Damage != 0 || o.Counter("M1M1") != 0 {
		t.Fatal("precondition: the blocker does not arrive alive, undamaged and uncountered")
	}
	if got := e.Toughness(blocker); got != 2 {
		t.Fatalf("precondition: blocker toughness = %d, want 2 (an undamaged 2/2)", got)
	}

	e.askAttackers()
	submitAttackers(t, e, stinger)
	submitBlockers(t, e, blocker)

	// The blocker dies, so its counters leave with it (a zone change clears
	// them) -- the placement is read off the log instead: exactly one
	// -1/-1 CounterChange, and the damage event it rode is
	// infect-marked (so nothing was ever marked on it).
	counters := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == blocker && ev.Counter == "M1M1" {
			if ev.Amount != 1 {
				t.Fatalf("the blocker's -1/-1 placement = %+v, want exactly 1 (the stinger's power, in counter form)", ev)
			}
			counters++
		}
	}
	if counters != 1 {
		t.Fatalf("the log carries %d -1/-1 placements for the blocker, want 1 (the infect form)", counters)
	}
	if o := e.G.Obj(blocker); o.Zone == state.ZBattlefield {
		t.Fatal("a 2/2 that took 1 deathtouch infect damage (one -1/-1 counter, nothing marked) survived -- CR 704.5g's mark-alone lethality did not fire")
	}
}

// corpusInfectSrc is corpusInfectCard's raw-source twin, for the fixture-deck
// helpers that build a Config from card SOURCE rather than a parsed card. The
// GPL script is read at test time and never committed, exactly as
// corpusInfectCard reads it.
func corpusInfectSrc(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", ".cards", "cardsfolder", path))
	if err != nil {
		t.Fatalf("read corpus script %s: %v", path, err)
	}
	return string(b)
}

// TestSacrificedGraftedExoskeletonBearerDealsInfectFromLKI pins CR 113.7a for
// the granted-infect half of the damage rider, the review-sol1 MAJOR. Grafted
// Exoskeleton grants infect to a creature whose damage ability sacrifices that
// creature as its cost: the sacrifice detaches the Equipment (attachment SBAs)
// before the independent ability resolves, so the live board no longer grants
// infect. Before the fix newDamageRider re-read HasKeyword live and the hit
// landed as one life loss; the source's last known infect must make it one
// poison counter instead.
func TestSacrificedGraftedExoskeletonBearerDealsInfectFromLKI(t *testing.T) {
	t.Parallel()
	pinger := "Name:Sac Pinger\nManaCost:2 R\nTypes:Creature Wizard\nPT:1/2\n" +
		"A:AB$ DealDamage | Cost$ Sac<1/CARDNAME> | ValidTgts$ Player | NumDmg$ 1 | SpellDescription$ deals 1.\nOracle:x\n"
	e, cfg, exoID := newFixtureDeck(t, 107, corpusInfectSrc(t, "g/grafted_exoskeleton.txt"), pinger)
	var pingerID state.ObjID
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Face() != nil && o.Face().Name == "Sac Pinger" {
			pingerID = o.ID
		}
	}
	if pingerID == 0 {
		t.Fatal("board missing the Sac Pinger fixture")
	}
	for _, id := range []state.ObjID{exoID, pingerID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZBattlefield})
	}
	addMana(t, e, 0, "CC")
	e.Advance()
	equip := abilityOption(t, e, exoID, 0)
	submitChoices(t, e, equip.Index)
	targetObject(t, e, pingerID)
	passUntilStackEmpty(t, e, 20)

	// Preconditions: the grant is live BEFORE the activation, and the
	// defender arrives at full life with no poison -- so the two counts
	// below read this one hit.
	if !e.HasKeyword(pingerID, "Infect") {
		t.Fatal("precondition: the equipped pinger does not read granted infect before activation")
	}
	if got := e.G.Players[1].Counter("POISON"); got != 0 {
		t.Fatalf("precondition: defender already carries %d poison", got)
	}

	opt := abilityOption(t, e, pingerID, 0)
	submitChoices(t, e, opt.Index)
	activateAnswers(t, e, pingerID, 0, 1)
	passUntilStackEmpty(t, e, 20)

	// The cost really did remove the source and its grant before resolution:
	// without both of these the test could pass on a live read.
	if o := e.G.Obj(pingerID); o.Zone != state.ZGraveyard {
		t.Fatalf("sacrificed source zone = %s, want graveyard", o.Zone)
	}
	if e.HasKeyword(pingerID, "Infect") {
		t.Fatal("the sacrificed source still reads infect live: the test would pass without LKI")
	}

	if got := e.G.Players[1].Counter("POISON"); got != 1 {
		t.Fatalf("defender poison = %d, want 1 (the departed source's last known infect, CR 113.7a/702.90b)", got)
	}
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("defender life = %d, want 20 (infect damage to a player is never life loss)", got)
	}
	replayCheck(t, e, cfg)
}

// TestSacrificedGrantedDeathtouchSourceUsesLKI is the same class one keyword
// over: newDamageRider now resolves deathtouch once, from the same LKI, so an
// Equipment-granted deathtouch source sacrificed to pay for its own damage
// ability still marks its hit deadly (CR 702.2b/113.7a). Before the fix the
// live read lost the grant and the 4/4 survived one damage.
func TestSacrificedGrantedDeathtouchSourceUsesLKI(t *testing.T) {
	collar := "Name:Fang Collar\nManaCost:1\nTypes:Artifact Equipment\nK:Equip:2\n" +
		"S:Mode$ Continuous | Affected$ Creature.EquippedBy | AddKeyword$ Deathtouch | Description$ Equipped creature has deathtouch.\nOracle:x\n"
	pinger := "Name:Sac Biter\nManaCost:2 B\nTypes:Creature Wizard\nPT:1/2\n" +
		"A:AB$ DealDamage | Cost$ Sac<1/CARDNAME> | ValidTgts$ Creature | NumDmg$ 1 | SpellDescription$ deals 1.\nOracle:x\n"
	e, cfg, collarID := newFixtureDeck(t, 107, collar, pinger)
	var pingerID state.ObjID
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Face() != nil && o.Face().Name == "Sac Biter" {
			pingerID = o.ID
		}
	}
	if pingerID == 0 {
		t.Fatal("board missing the Sac Biter fixture")
	}
	for _, id := range []state.ObjID{collarID, pingerID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZBattlefield})
	}
	// A 4/4 victim, so only the deathtouch half can be why it dies: one
	// damage is nowhere near its toughness. Minted through the logged token
	// path, not onBoard's eventless placement, so replayCheck below can
	// rebuild it from the log alone.
	victim := putToken(t, e, 1, "Name:Ox\nManaCost:3 G\nTypes:Creature Ox\nPT:4/4\nOracle:x\n", state.ZBattlefield)
	addMana(t, e, 0, "CC")
	e.Advance()
	equip := abilityOption(t, e, collarID, 0)
	submitChoices(t, e, equip.Index)
	targetObject(t, e, pingerID)
	passUntilStackEmpty(t, e, 20)
	if !e.HasKeyword(pingerID, "Deathtouch") {
		t.Fatal("precondition: the equipped biter does not read granted deathtouch before activation")
	}

	opt := abilityOption(t, e, pingerID, 0)
	submitChoices(t, e, opt.Index)
	activateAnswers(t, e, pingerID, victim, 0)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(pingerID); o.Zone != state.ZGraveyard {
		t.Fatalf("sacrificed source zone = %s, want graveyard", o.Zone)
	}
	if e.HasKeyword(pingerID, "Deathtouch") {
		t.Fatal("the sacrificed source still reads deathtouch live: the test would pass without LKI")
	}
	if o := e.G.Obj(victim); o.Zone == state.ZBattlefield {
		t.Fatalf("the 4/4 victim survived 1 damage from a departed deathtouch source (marked %d)", o.Damage)
	}
	replayCheck(t, e, cfg)
}

// TestInfectEffectDamageToCreatureIsMinusOneCountersNotMarkedDamage pins the
// effect-path (non-combat) creature half of CR 702.90b, the one branch of the
// emitter the combat tests never reach: effects/damage.go's emitObjectDamage
// tags a DealDamage-style hit "infect+creature" when the
// recipient reads as a creature and the source reads infect, and
// Engine.convertInfectDamage then places -1/-1 counters while the fold marks
// nothing. Every existing creature-half test drives combat; if the emitter's
// effect-path tag regressed, a pinger-style infect hit would mark ordinary
// damage and place no counters with every combat test still green.
func TestInfectEffectDamageToCreatureIsMinusOneCountersNotMarkedDamage(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	// Blight Mamba is the corpus infect source (1/1, so one point of damage
	// converts to exactly one -1/-1 counter), driven here through the effect
	// path rather than combat.
	mamba := onBoardCard(t, e, 0, corpusInfectCard(t, "Blight Mamba"))
	// A 2/2 recipient: one -1/-1 counter leaves it alive at toughness 1, so
	// the assertions below read a live battlefield object and the counter
	// count cannot be explained by a lethal cleanup.
	victim := onBoard(t, e, 1, "Name:Ox\nManaCost:3 G\nTypes:Creature Ox\nPT:2/2\nOracle:x\n")

	// Preconditions the assertions below depend on: the source reads infect
	// via the derived keyword list, the victim is a live battlefield
	// creature that arrives undamaged and uncountered, and its toughness is
	// high enough that ONE counter does not kill it -- so the two counts
	// below read this one hit's form, not a leftover or a death cleanup.
	if !e.HasKeyword(mamba, "Infect") {
		t.Fatal("precondition: Blight Mamba does not read infect")
	}
	if o := e.G.Obj(victim); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: the victim is not on the battlefield")
	}
	if !e.IsCreature(victim) {
		t.Fatal("precondition: the victim does not read as a creature")
	}
	if got := e.G.Obj(victim).Damage; got != 0 {
		t.Fatalf("precondition: the victim already carries %d marked damage", got)
	}
	if got := e.G.Obj(victim).Counter("M1M1"); got != 0 {
		t.Fatalf("precondition: the victim already carries %d -1/-1 counters", got)
	}
	if got := e.Toughness(victim); got != 2 {
		t.Fatalf("precondition: victim toughness = %d, want 2 (an undamaged 2/2)", got)
	}

	effects.Resolve(e, &effects.Ctx{Source: mamba, Controller: 0,
		Targets: []state.Target{{Obj: victim}}}, &cards.SA{Kind: "DB", API: "DealDamage",
		Params: map[string]string{"Defined": "Targeted", "NumDmg": "1"}})

	if got := e.G.Obj(victim).Counter("M1M1"); got != 1 {
		t.Fatalf("victim -1/-1 counters = %d, want 1 (the mamba's power, dealt in counter form on the effect path)", got)
	}
	if got := e.G.Obj(victim).Damage; got != 0 {
		t.Fatalf("victim marked damage = %d, want 0 (infect damage is never marked)", got)
	}
	if o := e.G.Obj(victim); o.Zone != state.ZBattlefield {
		t.Fatalf("2/2 victim with one -1/-1 counter is in %v, want battlefield (toughness 1 survives)", o.Zone)
	}
	// Exactly one -1/-1 placement rides the real counter event, and no
	// non-convertible leftover: the same shape the artifact test asserts in
	// the negative.
	counters := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == victim && ev.Counter == "M1M1" {
			if ev.Amount != 1 {
				t.Fatalf("the victim's -1/-1 placement = %+v, want exactly 1", ev)
			}
			counters++
		}
	}
	if counters != 1 {
		t.Fatalf("the log carries %d -1/-1 placements for the victim, want 1 (the effect-path infect form)", counters)
	}
}
