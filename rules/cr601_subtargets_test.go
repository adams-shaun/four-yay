package rules

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// CR 601.2c: "The player announces their choice of an appropriate object or
// player for each target the spell requires." EVERY target -- the root
// declaration's and each SubAbility$ link's -- is chosen as the spell is cast
// (CR 602.2b: or the ability activated), before any cost is paid. These tests
// pin that timing and its consequences on the cast flow's chain ask
// (subTargetAsk): the offer census, the targeting events, the CR 608.2b
// recheck, and the shapes that are deliberately still asked at resolution.

// cr601Board deals each seat the named corpus cards (moved to the given
// zones) over a Mountain deck and stops at seat 0's first main-phase
// priority. Card names are walked in sorted order so the replay rebuild sees
// the same decks.
func cr601Board(t *testing.T, seed uint64, seat0, seat1 map[string]state.Zone) (*Engine, Config, map[string]state.ObjID, map[string]state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	names := func(m map[string]state.Zone) []string {
		out := make([]string, 0, len(m))
		for n := range m {
			out = append(out, n)
		}
		sort.Strings(out)
		return out
	}
	deck := func(ns []string) []*cards.Card {
		var d []*cards.Card
		for _, n := range ns {
			d = append(d, mustCorpusCard(t, reg, n))
		}
		return append(d, mountainDeck(t, 40-len(d))...)
	}
	n0, n1 := names(seat0), names(seat1)
	cfg := Config{Seed: seed, Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck(n0), deck(n1)},
		Tokens: reg.Tokens}
	e := New(cfg)
	ids0, ids1 := map[string]state.ObjID{}, map[string]state.ObjID{}
	for _, n := range n0 {
		ids0[n] = moveByName(t, e, 0, n, seat0[n])
	}
	for _, n := range n1 {
		ids1[n] = moveByName(t, e, 1, n, seat1[n])
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.Advance()
	edrSeatZeroPriority(t, e)
	return e, cfg, ids0, ids1
}

// cr601Cast submits seat 0's cast option for the card (any mode when mode is
// "", else the option carrying exactly that Mode).
func cr601Cast(t *testing.T, e *Engine, id state.ObjID, mode string) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want seat 0's priority", d)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id && o.Mode == mode {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no cast option (mode %q) for object %d: %+v", mode, id, d.Options)
}

// cr601Offered reports whether seat 0's pending priority offers a cast of id.
func cr601Offered(t *testing.T, e *Engine, id state.ObjID) bool {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want seat 0's priority", d)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			return true
		}
	}
	return false
}

// cr601ResolveQuietly passes priority until the stack is empty and fails on
// any non-priority decision: a spell whose targets were all announced on cast
// asks nothing as it resolves.
func cr601ResolveQuietly(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 40 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("the resolution posed %+v: every target was already announced on cast (CR 601.2c)", d)
		}
		submitChoices(t, e, passIndex(t, d))
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("the stack did not empty: %v", e.G.Stack)
	}
}

// subTargetEvents returns the chain-link TargetsChosen events recorded on obj.
func subTargetEvents(e *Engine, obj state.ObjID) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.TargetsChosen && ev.Obj == obj && ev.Text == events.SubTargetNotice {
			out = append(out, ev)
		}
	}
	return out
}

// TestBiteDownSecondTargetIsAnnouncedBeforePayment is the defect's own card:
// "Target creature you control deals damage equal to its power to target
// creature or planeswalker you don't control." Both targets are chosen on
// cast. The second is a real KTarget posed while the spell is still unpaid;
// it is recorded on the stack object as a chain target (not as a root
// target), it carries the effect the bot ranks it by, and the creature named
// on cast -- not a creature picked at resolution -- takes the damage.
func TestBiteDownSecondTargetIsAnnouncedBeforePayment(t *testing.T) {
	t.Parallel()
	e, cfg, mine, theirs := cr601Board(t, 60101,
		map[string]state.Zone{"Bite Down": state.ZHand, "Centaur Courser": state.ZBattlefield},
		map[string]state.Zone{"Grizzly Bears": state.ZBattlefield, "Craw Wurm": state.ZBattlefield})
	spell, courser := mine["Bite Down"], mine["Centaur Courser"]
	bears, wurm := theirs["Grizzly Bears"], theirs["Craw Wurm"]
	addMana(t, e, 0, "G1")
	edrSeatZeroPriority(t, e)
	poolBefore := e.G.Players[0].Pool.Total()
	if poolBefore != 2 {
		t.Fatalf("precondition: pool total %d, want the 2 mana Bite Down costs", poolBefore)
	}
	cr601Cast(t, e, spell, "")

	root := e.Pending()
	if root == nil || root.Kind != decision.KTarget || root.ResumeKind == "cast_sub" {
		t.Fatalf("pending = %+v, want the root's own target ask", root)
	}
	answerTargetAsk(t, e, []state.ObjID{courser})

	sub := castSubAsk(t, e)
	if sub.Player != 0 || sub.Min != 1 || sub.Max != 1 {
		t.Fatalf("second target ask = player %d bounds %d..%d, want the caster's exactly-one ask", sub.Player, sub.Min, sub.Max)
	}
	if sub.TargetEffect == nil || sub.TargetEffect.API != "DealDamage" {
		t.Fatalf("second target ask carries effect %+v, want the link's DealDamage (what a bot ranks the target by)", sub.TargetEffect)
	}
	offered := map[state.ObjID]bool{}
	for _, o := range sub.Options {
		offered[o.Obj] = true
	}
	if !offered[bears] || !offered[wurm] || offered[courser] {
		t.Fatalf("second target ask offers %+v, want exactly the creatures the caster doesn't control", sub.Options)
	}
	// 601.2c precedes 601.2h: nothing has been paid while targets are chosen.
	if got := e.G.Players[0].Pool.Total(); got != poolBefore {
		t.Fatalf("mana was spent before the second target was chosen: pool %d -> %d", poolBefore, got)
	}
	answerCastSubObj(t, e, wurm)

	if d := e.Pending(); d == nil || d.Kind != decision.KPriority || len(e.G.Stack) != 1 {
		t.Fatalf("after the announcement: pending %+v, stack %v; want priority with Bite Down on the stack", d, e.G.Stack)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("the spell was not paid for after its targets were chosen: pool %d", got)
	}
	so := e.G.Obj(spell)
	if len(so.Targets) != 1 || so.Targets[0].Obj != courser {
		t.Fatalf("root targets = %+v, want only the caster's creature", so.Targets)
	}
	if len(so.SubTargets) != 1 || so.SubTargets[0].Obj != wurm {
		t.Fatalf("chain targets = %+v, want the Craw Wurm named on cast", so.SubTargets)
	}
	evs := subTargetEvents(e, spell)
	if len(evs) != 1 || len(evs[0].IDs) != 1 || evs[0].IDs[0] != wurm {
		t.Fatalf("chain TargetsChosen events = %+v, want one naming the Craw Wurm (what ward and 'becomes the target' match)", evs)
	}

	cr601ResolveQuietly(t, e)
	if o := e.G.Obj(wurm); o.Zone != state.ZBattlefield || o.Damage != 3 {
		t.Fatalf("Craw Wurm zone %s damage %d, want battlefield with the Courser's 3", o.Zone, o.Damage)
	}
	if o := e.G.Obj(bears); o.Zone != state.ZBattlefield || o.Damage != 0 {
		t.Fatalf("Grizzly Bears zone %s damage %d: the creature NOT named on cast was touched", o.Zone, o.Damage)
	}
	replayCheck(t, e, cfg)
}

// TestSpellWithNoLegalChainTargetIsNotOffered: a mandatory chain target with
// no legal candidate makes the spell unannounceable (CR 601.2c), so the offer
// census withholds the cast instead of offering it and reversing the
// proposal. Giving the opponent a creature brings the offer back.
func TestSpellWithNoLegalChainTargetIsNotOffered(t *testing.T) {
	t.Parallel()
	e, cfg, mine, _ := cr601Board(t, 60102,
		map[string]state.Zone{"Bite Down": state.ZHand, "Centaur Courser": state.ZBattlefield},
		map[string]state.Zone{"Grizzly Bears": state.ZHand})
	addMana(t, e, 0, "G1")
	edrSeatZeroPriority(t, e)
	if cr601Offered(t, e, mine["Bite Down"]) {
		t.Fatal("Bite Down is offered with no creature or planeswalker the caster doesn't control")
	}
	moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	e.pending = nil
	e.priorityRound()
	edrSeatZeroPriority(t, e)
	if !cr601Offered(t, e, mine["Bite Down"]) {
		t.Fatal("Bite Down is not offered once the opponent controls a creature")
	}
	replayCheck(t, e, cfg)
}

// TestChainTargetGoneRestStillResolves is CR 608.2b's partial case on Felling
// Blow ("Put a +1/+1 counter on target creature you control. Then that
// creature deals damage equal to its power to target creature an opponent
// controls."): when the SECOND target is gone at resolution the spell still
// resolves for its legal target -- the counter lands -- and the damage is
// not redirected to another creature that opponent controls.
func TestChainTargetGoneRestStillResolves(t *testing.T) {
	t.Parallel()
	e, cfg, mine, theirs := cr601Board(t, 60103,
		map[string]state.Zone{"Felling Blow": state.ZHand, "Centaur Courser": state.ZBattlefield},
		map[string]state.Zone{"Grizzly Bears": state.ZBattlefield, "Craw Wurm": state.ZBattlefield})
	spell, courser := mine["Felling Blow"], mine["Centaur Courser"]
	bears, wurm := theirs["Grizzly Bears"], theirs["Craw Wurm"]
	addMana(t, e, 0, "G11")
	edrSeatZeroPriority(t, e)
	cr601Cast(t, e, spell, "")
	answerTargetAsk(t, e, []state.ObjID{courser})
	answerCastSubObj(t, e, bears)
	// The named creature leaves before the spell resolves.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bears, From: state.ZBattlefield, To: state.ZGraveyard})
	cr601ResolveQuietly(t, e)
	if got := p1p1On(t, e, courser); got != 1 {
		t.Fatalf("the caster's creature has %d +1/+1 counters, want 1: the spell must resolve for its remaining legal target (CR 608.2b)", got)
	}
	if o := e.G.Obj(wurm); o.Zone != state.ZBattlefield || o.Damage != 0 {
		t.Fatalf("Craw Wurm zone %s damage %d: the damage was redirected to a creature that was never targeted", o.Zone, o.Damage)
	}
	if o := e.G.Obj(spell); o.Zone != state.ZGraveyard {
		t.Fatalf("Felling Blow rests in %s, want its owner's graveyard", o.Zone)
	}
	replayCheck(t, e, cfg)
}

// TestKickerOnlyChainTargetIsAnnouncedOnlyWhenKicked is CR 601.2c's cost
// clause on Probe ("If this spell was kicked, target player discards two
// cards"): the link behind the unpaid kicker announces no target, the kicked
// cast announces it on cast.
func TestKickerOnlyChainTargetIsAnnouncedOnlyWhenKicked(t *testing.T) {
	t.Parallel()
	t.Run("unkicked", func(t *testing.T) {
		e, _, mine, _ := cr601Board(t, 60104, map[string]state.Zone{"Probe": state.ZHand}, nil)
		addMana(t, e, 0, "U11")
		edrSeatZeroPriority(t, e)
		cr601Cast(t, e, mine["Probe"], "")
		if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
			t.Fatalf("an unkicked Probe posed %+v: its only target sits behind the unpaid kicker", d)
		}
		if so := e.G.Obj(mine["Probe"]); len(so.SubTargets) != 0 {
			t.Fatalf("an unkicked Probe recorded chain targets %+v", so.SubTargets)
		}
	})
	t.Run("kicked", func(t *testing.T) {
		e, _, mine, _ := cr601Board(t, 60105, map[string]state.Zone{"Probe": state.ZHand}, nil)
		addMana(t, e, 0, "UB111")
		edrSeatZeroPriority(t, e)
		cr601Cast(t, e, mine["Probe"], "kicked")
		answerCastSubPlayer(t, e, 1)
		if so := e.G.Obj(mine["Probe"]); len(so.SubTargets) != 1 || !so.SubTargets[0].IsPlayer || so.SubTargets[0].Player != 1 {
			t.Fatalf("a kicked Probe's chain targets = %+v, want seat 1", so.SubTargets)
		}
	})
}

// pingerScript is an activated ability whose ROOT declares no target and
// whose chain link does ("{1}: Draw no cards, then 1 damage to target
// creature an opponent controls").
const pingerScript = "Name:Chain Pinger\nManaCost:1\nTypes:Artifact\n" +
	"A:AB$ Draw | Cost$ 1 | NumCards$ 0 | SubAbility$ DBPing | SpellDescription$ x\n" +
	"SVar:DBPing:DB$ DealDamage | ValidTgts$ Creature.OppCtrl | NumDmg$ 1\n" +
	"Oracle:x\n"

const cr601OgreScript = "Name:Chain Ogre\nManaCost:2 R\nTypes:Creature Ogre\nPT:3/3\nOracle:x\n"

// TestAbilityChainTargetIsAnnouncedOnActivation is CR 602.2b importing
// 601.2c for an activated ability: the link's target is chosen as the ability
// is activated, before its cost is paid; with no legal target the ability is
// not offered at all; and the target is recorded on the ability's stack
// object once that object exists.
func TestAbilityChainTargetIsAnnouncedOnActivation(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeckWithOpponentCard(t, 60107, pingerScript, cr601OgreScript, cr601OgreScript)
	toMain1(t, e)
	pinger := moveSeeded(t, e, 0, pingerScript, state.ZBattlefield)
	addMana(t, e, 0, "C")
	e.priorityRound()
	if _, ok := findAbilityOption(e, pinger, 0); ok {
		t.Fatal("the ability is offered with no creature an opponent controls: its chain target has no legal candidate (CR 602.2b / 601.2c)")
	}
	ogre := putCreature(t, e, 1, cr601OgreScript)
	e.priorityRound()
	opt, ok := findAbilityOption(e, pinger, 0)
	if !ok {
		t.Fatalf("the ability is not offered with a legal chain target: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	castSubAsk(t, e)
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("the activation cost was paid before the target was chosen: pool %d, want 1", got)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("an ability object exists before its targets were chosen: stack %v", e.G.Stack)
	}
	answerCastSubObj(t, e, ogre)
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the activated ability", e.G.Stack)
	}
	ab := e.G.Obj(e.G.Stack[0])
	if len(ab.Targets) != 0 || len(ab.SubTargets) != 1 || ab.SubTargets[0].Obj != ogre {
		t.Fatalf("ability targets = %+v / chain %+v, want only the chain target (the ogre)", ab.Targets, ab.SubTargets)
	}
	if evs := subTargetEvents(e, ab.ID); len(evs) != 1 {
		t.Fatalf("chain TargetsChosen events on the ability = %+v, want exactly one", evs)
	}
	cr601ResolveQuietly(t, e)
	if o := e.G.Obj(ogre); o.Damage != 1 {
		t.Fatalf("the ogre has %d damage, want the 1 the ability deals", o.Damage)
	}
	replayCheck(t, e, cfg)
}

// crimeWatcherScript gains its controller 1 life whenever they commit a crime.
const crimeWatcherScript = "Name:Crime Watcher\nManaCost:1\nTypes:Artifact\n" +
	"T:Mode$ CommitCrime | ValidPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigGain | TriggerDescription$ x\n" +
	"SVar:TrigGain:DB$ GainLife | LifeAmount$ 1\n" +
	"Oracle:x\n"

// twoCrimesScript targets an opponent's creature with the root AND with a
// chain link: two criminal targets, one crime (CR 700.13).
const twoCrimesScript = "Name:Two Crimes\nManaCost:1\nTypes:Instant\n" +
	"A:SP$ Pump | ValidTgts$ Creature.OppCtrl | SubAbility$ DBTap | SpellDescription$ x\n" +
	"SVar:DBTap:DB$ Tap | ValidTgts$ Creature.OppCtrl\n" +
	"Oracle:x\n"

// oneCrimeScript targets the caster's own permanent with the root and an
// opponent's creature only with the chain link: the crime is committed by the
// chain target alone.
const oneCrimeScript = "Name:One Crime\nManaCost:1\nTypes:Instant\n" +
	"A:SP$ Pump | ValidTgts$ Artifact.YouCtrl | SubAbility$ DBTap | SpellDescription$ x\n" +
	"SVar:DBTap:DB$ Tap | ValidTgts$ Creature.OppCtrl\n" +
	"Oracle:x\n"

// TestChainTargetCommitsACrimeExactlyOnce: a chain link's target is a real
// targeting, so naming an opponent's creature with it commits a crime (CR
// 700.13) even when the root targets nothing criminal -- and a spell whose
// root AND link both name an opponent's creature still commits exactly one.
func TestChainTargetCommitsACrimeExactlyOnce(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, script string
		rootOwn      bool
	}{
		{"link only", oneCrimeScript, true},
		{"root and link", twoCrimesScript, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, spell := newFixtureDeckWithOpponentCard(t, 60109, tc.script, crimeWatcherScript, cr601OgreScript)
			toMain1(t, e)
			watcher := moveSeeded(t, e, 0, crimeWatcherScript, state.ZBattlefield)
			ogre := putCreature(t, e, 1, cr601OgreScript)
			addMana(t, e, 0, "C")
			e.priorityRound()
			cr601Cast(t, e, spell, "")
			if tc.rootOwn {
				answerTargetAsk(t, e, []state.ObjID{watcher})
			} else {
				answerTargetAsk(t, e, []state.ObjID{ogre})
			}
			answerCastSubObj(t, e, ogre)
			lifeBefore := e.G.Players[0].Life
			passUntilStackEmpty(t, e, 30)
			if got := e.G.Players[0].Life - lifeBefore; got != 1 {
				t.Fatalf("the crime trigger gained %d life, want exactly 1 (one crime per spell, CR 700.13)", got)
			}
			if o := e.G.Obj(ogre); !o.Tapped {
				t.Fatal("the chain link did not tap the creature named on cast")
			}
			replayCheck(t, e, cfg)
		})
	}
}

// ridersWithScryScript announces two TargetUnique$ player riders around a
// Scry: the targets are settled on cast, and the Scry's mid-resolution ask
// must neither re-pose them nor lose them.
const ridersWithScryScript = "Name:Riders Around Scry\nManaCost:1\nTypes:Sorcery\n" +
	"A:SP$ Scry | ScryNum$ 1 | SubAbility$ R1\n" +
	"SVar:R1:DB$ GainLife | LifeAmount$ 2 | ValidTgts$ Player | TargetUnique$ True | SubAbility$ R2\n" +
	"SVar:R2:DB$ Scry | ScryNum$ 2 | SubAbility$ R3\n" +
	"SVar:R3:DB$ LoseLife | LifeAmount$ 3 | ValidTgts$ Player | TargetUnique$ True\n" +
	"Oracle:x\n"

// TestSpellRidersAnnouncedOnCastSurviveASuspension: both riders are asked on
// cast (the second excluding the first's target), the resolution then
// suspends on the Scry asks, and each rider still acts on the player named on
// cast.
func TestSpellRidersAnnouncedOnCastSurviveASuspension(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 60110, ridersWithScryScript)
	addMana(t, e, 0, "C")
	castFirst(t, e, "cast")
	first := castSubAsk(t, e)
	if !pendingPlayerIDs(t, e)[0] || !pendingPlayerIDs(t, e)[1] {
		t.Fatalf("the first rider should offer both players: %+v", first.Options)
	}
	answerCastSubPlayer(t, e, 0)
	second := castSubAsk(t, e)
	if pendingPlayerIDs(t, e)[0] || !pendingPlayerIDs(t, e)[1] {
		t.Fatalf("the second TargetUnique$ rider should offer only seat 1: %+v", second.Options)
	}
	answerCastSubPlayer(t, e, 1)
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	scries := 0
	for i := 0; i < 40 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while the spell resolves")
		}
		switch d.Kind {
		case decision.KPriority:
			submitChoices(t, e, passIndex(t, d))
		case decision.KArrange:
			scries++
			submitArrange(t, e, d, nil)
		default:
			t.Fatalf("the resolution posed %+v: only the two Scry asks are owed, the riders were announced on cast", d)
		}
	}
	if scries != 2 {
		t.Fatalf("the resolution posed %d Scry asks, want 2 (the suspension this test needs)", scries)
	}
	if got := e.G.Players[0].Life - life0; got != 2 {
		t.Fatalf("seat 0 (the first rider's target) gained %d life, want 2", got)
	}
	if got := life1 - e.G.Players[1].Life; got != 3 {
		t.Fatalf("seat 1 (the second rider's target) lost %d life, want 3", got)
	}
	replayCheck(t, e, cfg)
}
