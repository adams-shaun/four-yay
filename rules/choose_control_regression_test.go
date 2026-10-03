package rules

import (
	"strings"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// choiceCorpus is the corpus registry opened once for this file's tests:
// each open decodes the whole compiled corpus (~0.25s), and a registry is
// read-only after load.
var choiceCorpus struct {
	sync.Mutex
	reg *cards.Registry
}

func choiceCorpusRegistry(t *testing.T) *cards.Registry {
	t.Helper()
	choiceCorpus.Lock()
	// Deferred so a Skip/Fatal inside CorpusRegistry (runtime.Goexit)
	// cannot leave the lock held for the next test.
	defer choiceCorpus.Unlock()
	if choiceCorpus.reg == nil {
		choiceCorpus.reg = testutil.CorpusRegistry(t)
	}
	return choiceCorpus.reg
}

func choiceCorpusCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	reg := choiceCorpusRegistry(t)
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("missing corpus card %q", name)
	}
	return c
}

func submitChoicePass(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("wanted priority, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "pass" {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no pass option: %+v", d.Options)
}

func TestVialSmasherChosenPlayerTakesDamage(t *testing.T) {
	t.Parallel()
	e := New(Config{Seed: 715, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}})
	vial := e.G.AddObject(choiceCorpusCard(t, "Vial Smasher the Fierce"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: vial.ID, From: state.ZLibrary, To: state.ZBattlefield})
	sa := cards.ResolveSVar(vial.Face().SVars, "TrigChoose")
	if sa == nil || sa.Sub == nil {
		t.Fatalf("Vial Smasher chain not linked: %+v", sa)
	}
	svars := make(map[string]string, len(vial.Face().SVars))
	for k, v := range vial.Face().SVars {
		svars[k] = v
	}
	// TriggeredSpellAbility$CardManaCostLKI is a separate count-expression
	// gap; hold the real choice/damage chain constant at a known amount here.
	svars["X"] = "4"
	ctx := &effects.Ctx{Source: vial.ID, Controller: 0, SVars: svars}
	effects.Resolve(e, ctx, sa)
	if e.G.Players[1].Life != 16 || e.G.Players[2].Life != 20 {
		t.Fatalf("Vial Smasher life = [%d %d], want [16 20] (ctx chosen=%+v state chosen=%+v)", e.G.Players[1].Life, e.G.Players[2].Life, ctx.Chosen, vial.Chosen)
	}
}

// TestSleeperAgentETBControlGoesToChosenOpponent drives Sleeper Agent's real
// ETB (T:Mode$ ChangesZone ... Execute$ TrigGainControl: DB$ GainControl |
// Defined$ Self | ValidTgts$ Opponent, no NewController$) end to end: the
// ask offers the opponent, the answer binds the player on the resolution
// ctx, and the PERMANENT changes controller -- the pre-fix engine recorded
// the choice and handed control to nobody.
func TestSleeperAgentETBControlGoesToChosenOpponent(t *testing.T) {
	t.Parallel()
	e := New(seatZeroStart(Config{Seed: 721, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}))
	sleeper := e.G.AddObject(choiceCorpusCard(t, "Sleeper Agent"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: sleeper.ID, From: state.ZLibrary, To: state.ZBattlefield})
	e.Advance()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Sleeper ETB target ask = %+v", d)
	}
	idx := indexOfPlayerOption(d, 1)
	if idx < 0 {
		t.Fatalf("no opponent option: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	for e.Pending() != nil && e.Pending().Kind == decision.KPriority {
		submitChoicePass(t, e)
	}
	if got := e.G.Obj(sleeper.ID).Controller; got != 1 {
		t.Fatalf("Sleeper controller = %d, want 1 (the chosen opponent)", got)
	}
}

func TestFlayerTemporaryControlExpiresAndZoneChangeResetsControl(t *testing.T) {
	t.Parallel()
	decks := [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}
	e := New(Config{Seed: 713, Names: []string{"a", "b"}, Decks: decks})
	flayer := e.G.AddObject(choiceCorpusCard(t, "Flayer of Loyalties"), 0)
	target := e.G.AddObject(card(t, "Name:Target\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	for _, id := range []state.ObjID{flayer.ID, target.ID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	}
	sa := cards.ResolveSVar(flayer.Face().SVars, "TrigGainControl")
	effects.Resolve(e, &effects.Ctx{Source: flayer.ID, Controller: 0, Targets: []state.Target{{Obj: target.ID}}, TargetsOffered: true, SVars: flayer.Face().SVars}, sa)
	if e.G.Obj(target.ID).Controller != 0 {
		t.Fatalf("Flayer did not gain control: %d", e.G.Obj(target.ID).Controller)
	}
	e.EndOfTurnCleanup()
	if e.G.Obj(target.ID).Controller != 1 {
		t.Fatalf("temporary control did not expire: %d", e.G.Obj(target.ID).Controller)
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: target.ID, Player: 0})
	e.emit(events.Event{Kind: events.MoveZone, Obj: target.ID, From: state.ZBattlefield, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.MoveZone, Obj: target.ID, From: state.ZGraveyard, To: state.ZBattlefield})
	if e.G.Obj(target.ID).Controller != 1 {
		t.Fatalf("zone change retained stolen control: %d", e.G.Obj(target.ID).Controller)
	}
}

func TestSowerOfDiscordETBChoicesResume(t *testing.T) {
	t.Parallel()
	e, _, _ := etbConfig(t, 711, nil, nil)
	sower := e.G.AddObject(choiceCorpusCard(t, "Sower of Discord"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: sower.ID, From: state.ZLibrary, To: state.ZHand})
	addMana(t, e, 0, "BBBBBB")

	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == sower.ID {
			submitChoices(t, e, o.Index)
			break
		}
	}
	for e.Pending() != nil && e.Pending().Kind == decision.KPriority {
		submitChoicePass(t, e)
	}
	if d = e.Pending(); d == nil || d.Kind != decision.KChoose || d.Source != sower.ID {
		t.Fatalf("Sower ETB did not ask: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	if d = e.Pending(); d == nil || d.Kind != decision.KChoose || d.Source != sower.ID {
		t.Fatalf("Sower second ETB choice did not resume: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)

	o := e.G.Obj(sower.ID)
	if o.Zone != state.ZBattlefield || len(o.Remembered) != 1 || len(o.Chosen) != 1 ||
		o.Remembered[0].Player == o.Chosen[0].Player {
		t.Fatalf("Sower choices not persisted separately: %+v", o)
	}
}

// abilityIndex returns the index of the first face ability with api.
func abilityIndex(t *testing.T, c *cards.Card, api string) int {
	t.Helper()
	for i, sa := range c.Faces[0].Abilities {
		if sa.API == api {
			return i
		}
	}
	t.Fatalf("%s has no %s ability", c.Faces[0].Name, api)
	return -1
}

// TestOnlyBloodRepeatEachResumesEachOpponentsDiscard drives a real RepeatEach
// whose iteration asks through the engine's resolution: each opponent in
// turn chooses two cards to discard. The first answer must resume that
// opponent's discard with the loop's player still bound (Defined$
// Player.IsRemembered), and then continue the loop to the second opponent
// instead of dropping it.
func TestOnlyBloodRepeatEachResumesEachOpponentsDiscard(t *testing.T) {
	t.Parallel()
	e := New(Config{Seed: 719, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}})
	scheme := e.G.AddObject(choiceCorpusCard(t, "Only Blood Ends Your Nightmares"), 0)
	hands := map[state.PlayerID][]state.ObjID{}
	graveyards := map[state.PlayerID]int{}
	for _, p := range []state.PlayerID{1, 2} {
		hands[p] = append([]state.ObjID(nil), e.G.Zone(state.ZHand, p)...)
		graveyards[p] = len(e.G.Zone(state.ZGraveyard, p))
		if len(hands[p]) < 3 {
			t.Fatalf("player %d opening hand = %d cards, want at least 3", p, len(hands[p]))
		}
	}
	e.emit(events.Event{Kind: events.TriggerPush, Obj: scheme.ID, Player: 0, Amount: 0})
	e.resolveTop()
	for _, p := range []state.PlayerID{1, 2} {
		d := e.Pending()
		if d == nil || d.Kind != decision.KModes || d.Player != p || d.Min != 2 || len(d.Options) != len(hands[p]) {
			t.Fatalf("iteration for player %d asked %+v", p, d)
		}
		for _, o := range d.Options {
			if o.Player != p || !containsObj(hands[p], o.Obj) {
				t.Fatalf("player %d offered a card outside their hand: %+v", p, o)
			}
		}
		submitChoices(t, e, 0, 1)
	}
	for _, p := range []state.PlayerID{1, 2} {
		if got, gy := len(e.G.Zone(state.ZHand, p)), len(e.G.Zone(state.ZGraveyard, p)); got != len(hands[p])-2 || gy != graveyards[p]+2 {
			t.Fatalf("player %d hand/graveyard = %d/%d, want %d/%d", p, got, gy, len(hands[p])-2, graveyards[p]+2)
		}
	}
	if d := e.Pending(); d != nil && (d.Kind == decision.KModes || d.Kind == decision.KChoose) {
		t.Fatalf("loop asked again after every opponent answered: %+v", d)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("resolved RepeatEach left the stack non-empty: %v", e.G.Stack)
	}
}

func containsObj(ids []state.ObjID, id state.ObjID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// stealWith resolves a real GainControl SA from src's face against target.
func stealWith(t *testing.T, e *Engine, src *state.Object, sa *cards.SA, controller state.PlayerID, target state.ObjID) {
	t.Helper()
	effects.Resolve(e, &effects.Ctx{Source: src.ID, Controller: controller, Targets: []state.Target{{Obj: target}}, TargetsOffered: true, SVars: src.Face().SVars}, sa)
}

func controlBoard(t *testing.T, seed uint64, thief string) (*Engine, *state.Object, *state.Object) {
	t.Helper()
	e := New(Config{Seed: seed, Names: []string{"a", "b"}, Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	src := e.G.AddObject(choiceCorpusCard(t, thief), 0)
	victim := e.G.AddObject(card(t, "Name:Victim\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	for _, id := range []state.ObjID{src.ID, victim.ID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	}
	return e, src, victim
}

// TestVedalkenShacklesControlEndsWhenSourceUntaps covers LoseControl$
// Untap,LeavesPlay: control lasts exactly as long as the source stays tapped,
// and CR 611.2b makes the steal do nothing if the source is already untapped.
func TestVedalkenShacklesControlEndsWhenSourceUntaps(t *testing.T) {
	t.Parallel()
	e, shackles, victim := controlBoard(t, 720, "Vedalken Shackles")
	sa := shackles.Face().Abilities[abilityIndex(t, shackles.Card, "GainControl")]
	stealWith(t, e, shackles, sa, 0, victim.ID)
	if e.G.Obj(victim.ID).Controller != 1 {
		t.Fatalf("untapped Shackles stole the creature (CR 611.2b): controller %d", e.G.Obj(victim.ID).Controller)
	}
	e.emit(events.Event{Kind: events.Tap, Obj: shackles.ID})
	stealWith(t, e, shackles, sa, 0, victim.ID)
	if e.G.Obj(victim.ID).Controller != 0 {
		t.Fatalf("tapped Shackles did not steal: controller %d", e.G.Obj(victim.ID).Controller)
	}
	e.EndOfTurnCleanup()
	if e.G.Obj(victim.ID).Controller != 0 {
		t.Fatalf("Shackles control ended at cleanup while still tapped: controller %d", e.G.Obj(victim.ID).Controller)
	}
	e.emit(events.Event{Kind: events.Untap, Obj: shackles.ID})
	if e.G.Obj(victim.ID).Controller != 1 {
		t.Fatalf("control survived Shackles untapping: controller %d", e.G.Obj(victim.ID).Controller)
	}
}

// TestKelloggControlEndsWhenSourceLeavesOrChangesHands covers LoseControl$
// LeavesPlay,LoseControl ("for as long as you control Kellogg") and the
// layering of overlapping control effects: an earlier until-end-of-turn
// steal that ends while Kellogg's is live changes nothing, and Kellogg's end
// returns the creature to the controller it had before either effect.
func TestKelloggControlEndsWhenSourceLeavesOrChangesHands(t *testing.T) {
	t.Parallel()
	e, kellogg, victim := controlBoard(t, 721, "Kellogg, Dangerous Mind")
	sa := kellogg.Face().Abilities[abilityIndex(t, kellogg.Card, "GainControl")]
	stealWith(t, e, kellogg, sa, 0, victim.ID)
	if e.G.Obj(victim.ID).Controller != 0 {
		t.Fatalf("Kellogg did not steal: controller %d", e.G.Obj(victim.ID).Controller)
	}
	e.EndOfTurnCleanup()
	if e.G.Obj(victim.ID).Controller != 0 {
		t.Fatalf("Kellogg's control ended at cleanup: controller %d", e.G.Obj(victim.ID).Controller)
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: kellogg.ID, Player: 1})
	if e.G.Obj(victim.ID).Controller != 1 {
		t.Fatalf("control survived Kellogg changing hands: controller %d", e.G.Obj(victim.ID).Controller)
	}

	e, kellogg, victim = controlBoard(t, 722, "Kellogg, Dangerous Mind")
	flayer := e.G.AddObject(choiceCorpusCard(t, "Flayer of Loyalties"), 0)
	stealWith(t, e, flayer, cards.ResolveSVar(flayer.Face().SVars, "TrigGainControl"), 0, victim.ID)
	stealWith(t, e, kellogg, sa, 0, victim.ID)
	e.EndOfTurnCleanup()
	if e.G.Obj(victim.ID).Controller != 0 {
		t.Fatalf("earlier EOT steal expiring overrode Kellogg's later effect: controller %d", e.G.Obj(victim.ID).Controller)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: kellogg.ID, From: state.ZBattlefield, To: state.ZGraveyard})
	if e.G.Obj(victim.ID).Controller != 1 {
		t.Fatalf("control survived Kellogg leaving play: controller %d", e.G.Obj(victim.ID).Controller)
	}
}

// TestPowerOfPersuasionControlLastsThroughYourNextTurn covers LoseControl$
// UntilTheEndOfYourNextTurn: control survives this turn's cleanup and the
// opponent's turn, and ends at the cleanup of the controller's next turn.
func TestPowerOfPersuasionControlLastsThroughYourNextTurn(t *testing.T) {
	t.Parallel()
	e := New(Config{Seed: 723, Names: []string{"a", "b"}, Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	spell := e.G.AddObject(choiceCorpusCard(t, "Power of Persuasion"), 0)
	victim := e.G.AddObject(card(t, "Name:Victim\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	e.emit(events.Event{Kind: events.MoveZone, Obj: victim.ID, From: state.ZLibrary, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 1})
	stealWith(t, e, spell, cards.ResolveSVar(spell.Face().SVars, "DBControl"), 0, victim.ID)
	for turn, active := range []state.PlayerID{0, 1} {
		if turn > 0 {
			e.emit(events.Event{Kind: events.TurnChange, Player: active, Amount: int32(turn + 1)})
		}
		e.EndOfTurnCleanup()
		if e.G.Obj(victim.ID).Controller != 0 {
			t.Fatalf("control ended at the cleanup of turn %d: controller %d", turn+1, e.G.Obj(victim.ID).Controller)
		}
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 3})
	e.EndOfTurnCleanup()
	if e.G.Obj(victim.ID).Controller != 1 {
		t.Fatalf("control survived the end of the controller's next turn: controller %d", e.G.Obj(victim.ID).Controller)
	}
}

// TestOldManOfTheSeaStaticCommandCheck covers LoseControl$ StaticCommandCheck:
// the stolen creature returns once its power exceeds Old Man's power.
func TestOldManOfTheSeaStaticCommandCheck(t *testing.T) {
	t.Parallel()
	e, oldMan, victim := controlBoard(t, 724, "Old Man of the Sea")
	sa := oldMan.Face().Abilities[abilityIndex(t, oldMan.Card, "GainControl")]
	e.emit(events.Event{Kind: events.Tap, Obj: oldMan.ID})
	stealWith(t, e, oldMan, sa, 0, victim.ID)
	if e.G.Obj(victim.ID).Controller != 0 {
		t.Fatalf("Old Man did not steal a 1/1: controller %d", e.G.Obj(victim.ID).Controller)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: victim.ID, Counter: "P1P1", Amount: 1})
	if e.G.Obj(victim.ID).Controller != 0 {
		t.Fatalf("control ended while power 2 <= Old Man's 2: controller %d", e.G.Obj(victim.ID).Controller)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: victim.ID, Counter: "P1P1", Amount: 1})
	if e.G.Obj(victim.ID).Controller != 1 {
		t.Fatalf("control survived power 3 > Old Man's 2: controller %d", e.G.Obj(victim.ID).Controller)
	}
}

// TestControlEndsAtEndOfCombatAndWhenAuraUnattaches covers the two remaining
// LoseControl$ terms with their real scripts. Their triggers' bindings are
// supplied directly, because the trigger modes themselves (AttackersDeclared
// with AttackingPlayer$, Attached) are not what is under test: Tahngarth's
// effect is resolved with the attacking opponent as TriggeredAttackingPlayer,
// and Eriette's with the Aura as the triggering source and the enchanted
// permanent as its target.
func TestControlEndsAtEndOfCombatAndWhenAuraUnattaches(t *testing.T) {
	t.Parallel()
	e, tahngarth, _ := controlBoard(t, 725, "Tahngarth, First Mate")
	sa := cards.ResolveSVar(tahngarth.Face().SVars, "TrigGainControl")
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	ctx := &effects.Ctx{Source: tahngarth.ID, Controller: 0, SVars: tahngarth.Face().SVars}
	ctx.AttackingPlayer = state.Target{Player: 1, IsPlayer: true}
	effects.Resolve(e, ctx, sa)
	if e.G.Obj(tahngarth.ID).Controller != 1 {
		t.Fatalf("Tahngarth not given to the attacking player: controller %d", e.G.Obj(tahngarth.ID).Controller)
	}
	e.setStep(state.StepEndCombat)
	if e.G.Obj(tahngarth.ID).Controller != 1 {
		t.Fatalf("control ended before the end of combat step ended: controller %d", e.G.Obj(tahngarth.ID).Controller)
	}
	e.setStep(state.StepMain2)
	if e.G.Obj(tahngarth.ID).Controller != 0 {
		t.Fatalf("control survived the end of combat: controller %d", e.G.Obj(tahngarth.ID).Controller)
	}

	e, eriette, victim := controlBoard(t, 726, "Eriette, the Beguiler")
	aura := e.G.AddObject(card(t, "Name:Aura\nTypes:Enchantment Aura\nOracle:x\n"), 0)
	stageAuraEntry(t, e, aura.ID, victim.ID)
	sa = cards.ResolveSVar(eriette.Face().SVars, "TrigGainControl")
	ctx = &effects.Ctx{Source: eriette.ID, Controller: 0, Targets: []state.Target{{Obj: victim.ID}}, SVars: eriette.Face().SVars}
	ctx.TriggerSource = aura.ID
	// Eriette's script is `Defined$ TriggeredTarget`: the triggering
	// permanent the Aura became attached to. The fixture supplies the
	// binding a real Attached-trigger capture would carry (previously the
	// unbound form fell back to the resolution's Targets; it now fails
	// closed like every other known-but-absent referent, so the fixture
	// must carry the referent itself).
	ctx.TriggerTarget = state.Target{Obj: victim.ID}
	effects.Resolve(e, ctx, sa)
	if e.G.Obj(victim.ID).Controller != 0 {
		t.Fatalf("Eriette did not gain control: controller %d", e.G.Obj(victim.ID).Controller)
	}
	e.EndOfTurnCleanup()
	if e.G.Obj(victim.ID).Controller != 0 {
		t.Fatalf("Aura-bound control ended at cleanup: controller %d", e.G.Obj(victim.ID).Controller)
	}
	e.emit(events.Event{Kind: events.Attach, Obj: aura.ID})
	if e.G.Obj(victim.ID).Controller != 1 {
		t.Fatalf("control survived the Aura becoming unattached: controller %d", e.G.Obj(victim.ID).Controller)
	}
}

// TestChaosDefilerRemembersEveryAskedIteration drives a real RepeatEach whose
// iterations ask (each opponent's ChooseCard with RememberChosen$) and whose
// Sub reads what the loop remembered. Every iteration here completes on a
// resumed frame, so each choice reaches the post-loop random choice only if
// the resumed iteration hands its Remembered back to the loop frame. The
// seed is fixed so the random pick lands on the FIRST opponent's permanent,
// which is in the pool only through that hand-over. Each iteration offers
// ONLY its own opponent's relic: Forge's getDefinedPlayers("Remembered")
// names remembered players alone, so ControlledBy Remembered in iteration 2
// is seat 2 even though the relic seat 1 remembered is still in the set.
func TestChaosDefilerRemembersEveryAskedIteration(t *testing.T) {
	t.Parallel()
	e := New(Config{Seed: 702, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}})
	defiler := e.G.AddObject(choiceCorpusCard(t, "Chaos Defiler"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: defiler.ID, From: state.ZLibrary, To: state.ZBattlefield})
	relics := map[state.PlayerID]state.ObjID{}
	for _, p := range []state.PlayerID{1, 2} {
		o := e.G.AddObject(card(t, "Name:Relic\nTypes:Artifact\nOracle:x\n"), p)
		e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
		relics[p] = o.ID
	}
	e.emit(events.Event{Kind: events.TriggerPush, Obj: defiler.ID, Player: 0, Amount: 0})
	e.resolveTop()
	for _, p := range []state.PlayerID{1, 2} {
		d := e.Pending()
		// Each iteration offers exactly its own opponent's relic: the loop
		// subject is the only remembered PLAYER (Forge's addPlayer ignores
		// the remembered card from the previous iteration).
		pick := -1
		if d != nil {
			for _, o := range d.Options {
				if o.Obj == relics[p] {
					pick = o.Index
				}
			}
		}
		if d == nil || d.Kind != decision.KChoose || d.Player != 0 || len(d.Options) != 1 || pick < 0 {
			t.Fatalf("iteration for opponent %d asked %+v, want 1 option (relic %d)", p, d, relics[p])
		}
		submitChoices(t, e, pick)
	}
	if z1, z2 := e.G.Obj(relics[1]).Zone, e.G.Obj(relics[2]).Zone; z1 != state.ZGraveyard || z2 != state.ZBattlefield {
		t.Fatalf("relic zones = [%v %v], want opponent 1's destroyed and opponent 2's kept", z1, z2)
	}
}

// stealEngine is a two-seat game at Main 1 of turn 1 with seat 0 active.
func stealEngine(t *testing.T, seed uint64) *Engine {
	t.Helper()
	e := New(seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}))
	e.Advance()
	toMain1(t, e)
	return e
}

func inZone(e *Engine, z state.Zone, p state.PlayerID, id state.ObjID) bool {
	return containsObj(e.G.Zone(z, p), id)
}

func pendingFrom(e *Engine, source state.ObjID) []pendingTrigger {
	var out []pendingTrigger
	for _, pt := range e.pendingTriggers {
		if pt.Source == source {
			out = append(out, pt)
		}
	}
	return out
}

const diesToYou = "Name:Victim\nTypes:Creature Bear\nPT:2/2\n" +
	"T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Card.Self | Execute$ TrigLife | TriggerDescription$ When this dies, you gain 1 life.\n" +
	"SVar:TrigLife:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"

// TestGainControlAmpersandAddKWsGrantsEveryKeyword pins GainControl's AddKWs$
// reader through the rules engine. It is a separate keyword-list reader from
// static AddKeyword$ and Pump/PumpAll KW$, and must share their parser.
func TestGainControlAmpersandAddKWsGrantsEveryKeyword(t *testing.T) {
	t.Parallel()
	e := stealEngine(t, 730)
	victim := onBoardReady(t, e, 1, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	gain := card(t, "Name:Steal\nTypes:Sorcery\nA:SP$ GainControl | ValidTgts$ Creature | LoseControl$ EOT | AddKWs$ Haste & Lifelink\nOracle:x\n")

	effects.Resolve(e, &effects.Ctx{Controller: 0, Targets: []state.Target{{Obj: victim}}, TargetsOffered: true}, gain.Faces[0].SpellAbility())
	if got := e.G.Obj(victim).Controller; got != 0 {
		t.Fatalf("controlled creature controller = %d, want 0", got)
	}
	for _, kw := range []string{"Haste", "Lifelink"} {
		if !e.HasKeyword(victim, kw) {
			t.Errorf("controlled creature missing %q; derived keywords %v", kw, e.Keywords(victim))
		}
	}
}

// TestStolenCreatureLivesInItsControllersBattlefield drives a real Act of
// Treason steal through attack, death and reanimation, checking the game's
// zone invariants at every step. A control change moves the permanent to its
// new controller's battlefield list (every per-controller reader -- attackers,
// the untap step, statics -- sees it there), takes it out of combat (CR
// 506.4), makes it summoning sick (CR 302.6) unless AddKWs$ grants haste,
// and a leaves-the-battlefield trigger is controlled by the player who
// controlled it as it died (CR 603.3a/603.10a).
func TestStolenCreatureLivesInItsControllersBattlefield(t *testing.T) {
	t.Parallel()
	e := stealEngine(t, 731)
	victim := onBoardReady(t, e, 1, diesToYou)
	e.emit(events.Event{Kind: events.Tap, Obj: victim})
	testutil.CheckInvariants(t, e.G, nil, "setup")

	treason := e.G.AddObject(choiceCorpusCard(t, "Act of Treason"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: treason.ID, From: state.ZLibrary, To: state.ZStack})
	effects.Resolve(e, &effects.Ctx{Source: treason.ID, Controller: 0, Targets: []state.Target{{Obj: victim}},
		TargetsOffered: true, SVars: treason.Face().SVars}, treason.Face().SpellAbility())
	testutil.CheckInvariants(t, e.G, nil, "after steal")
	v := e.G.Obj(victim)
	if v.Controller != 0 || !inZone(e, state.ZBattlefield, 0, victim) || inZone(e, state.ZBattlefield, 1, victim) {
		t.Fatalf("stolen creature controller %d, in lists [0:%v 1:%v]", v.Controller,
			inZone(e, state.ZBattlefield, 0, victim), inZone(e, state.ZBattlefield, 1, victim))
	}
	if v.Tapped || !v.SummonSick || !e.HasKeyword(victim, "Haste") || !e.canAttack(victim) {
		t.Fatalf("Act of Treason: tapped=%v sick=%v haste=%v canAttack=%v, want untapped, sick, hasty and able to attack",
			v.Tapped, v.SummonSick, e.HasKeyword(victim, "Haste"), e.canAttack(victim))
	}

	passUntilKind(t, e, decision.KAttackers, 40)
	d := e.Pending()
	pick := -1
	for _, o := range d.Options {
		if o.Obj == victim && pick < 0 {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("thief was not offered the stolen creature as an attacker: %+v", d.Options)
	}
	submitChoices(t, e, pick)
	testutil.CheckInvariants(t, e.G, nil, "after attack")
	if !v.IsAttacking {
		t.Fatal("stolen creature is not attacking")
	}

	// CR 506.4: a control change removes it from combat.
	e.emit(events.Event{Kind: events.ControlChange, Obj: victim, Player: 1})
	testutil.CheckInvariants(t, e.G, nil, "control returned mid-combat")
	if v.IsAttacking || !inZone(e, state.ZBattlefield, 1, victim) {
		t.Fatalf("returned creature attacking=%v in owner's list=%v", v.IsAttacking, inZone(e, state.ZBattlefield, 1, victim))
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: victim, Player: 0})

	e.emit(events.Event{Kind: events.MoveZone, Obj: victim, From: state.ZBattlefield, To: state.ZGraveyard})
	testutil.CheckInvariants(t, e.G, nil, "after death")
	if !inZone(e, state.ZGraveyard, 1, victim) || inZone(e, state.ZBattlefield, 0, victim) || inZone(e, state.ZBattlefield, 1, victim) {
		t.Fatal("dead stolen creature is not only in its owner's graveyard")
	}
	if pts := pendingFrom(e, victim); len(pts) != 1 || pts[0].Controller != 0 {
		t.Fatalf("dies trigger instances = %+v, want exactly one controlled by the taker", pts)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: victim, From: state.ZGraveyard, To: state.ZBattlefield})
	testutil.CheckInvariants(t, e.G, nil, "after reanimation")
	if v.Controller != 1 || !inZone(e, state.ZBattlefield, 1, victim) || e.HasKeyword(victim, "Haste") {
		t.Fatalf("reanimated creature controller=%d haste=%v, want a new object under its owner", v.Controller, e.HasKeyword(victim, "Haste"))
	}
	e.EndOfTurnCleanup()
	testutil.CheckInvariants(t, e.G, nil, "after cleanup")
	if v.Controller != 1 {
		t.Fatalf("expired Act of Treason moved the new object: controller %d", v.Controller)
	}
}

// TestStolenPermanentUntapsAndSickensUnderItsController: a permanent control
// change on the taker's own turn leaves the creature unable to attack (CR
// 302.6); the owner's untap step no longer untaps it and the taker's does,
// which is also when its summoning sickness ends.
func TestStolenPermanentUntapsAndSickensUnderItsController(t *testing.T) {
	t.Parallel()
	e := stealEngine(t, 732)
	victim := onBoardReady(t, e, 1, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.ControlChange, Obj: victim, Player: 0})
	testutil.CheckInvariants(t, e.G, nil, "after steal")
	if e.canAttack(victim) {
		t.Fatal("a creature taken this turn can attack without haste")
	}
	e.emit(events.Event{Kind: events.Tap, Obj: victim})
	e.beginTurn(1)
	testutil.CheckInvariants(t, e.G, nil, "owner's untap")
	if v := e.G.Obj(victim); !v.Tapped || !v.SummonSick {
		t.Fatalf("owner's untap step touched the taker's creature: tapped=%v sick=%v", v.Tapped, v.SummonSick)
	}
	e.beginTurn(0)
	testutil.CheckInvariants(t, e.G, nil, "taker's untap")
	if v := e.G.Obj(victim); v.Tapped || v.SummonSick || !e.canAttack(victim) {
		t.Fatalf("taker's turn: tapped=%v sick=%v canAttack=%v", v.Tapped, v.SummonSick, e.canAttack(victim))
	}
}

// TestStolenCreatureDeathAndSacrificeUseLastKnownController: "a creature you
// control dies" and "whenever you sacrifice a permanent" (Evendo Brushrazer)
// see a stolen permanent as its taker's as it leaves, although the move has
// already returned it to its owner.
func TestStolenCreatureDeathAndSacrificeUseLastKnownController(t *testing.T) {
	t.Parallel()
	e := stealEngine(t, 733)
	watch := "Name:Watch\nTypes:Enchantment\n" +
		"T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Creature.%s | TriggerZones$ Battlefield | Execute$ TrigLife | TriggerDescription$ x\n" +
		"SVar:TrigLife:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	takerYou := onBoard(t, e, 0, strings.ReplaceAll(watch, "%s", "YouCtrl"))
	takerOpp := onBoard(t, e, 0, strings.ReplaceAll(watch, "%s", "OppCtrl"))
	ownerYou := onBoard(t, e, 1, strings.ReplaceAll(watch, "%s", "YouCtrl"))
	victim := onBoardReady(t, e, 1, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.ControlChange, Obj: victim, Player: 0})
	e.emit(events.Event{Kind: events.MoveZone, Obj: victim, From: state.ZBattlefield, To: state.ZGraveyard})
	if len(pendingFrom(e, takerYou)) != 1 || len(pendingFrom(e, takerOpp)) != 0 || len(pendingFrom(e, ownerYou)) != 0 {
		t.Fatalf("dies watchers fired taker-YouCtrl=%d taker-OppCtrl=%d owner-YouCtrl=%d, want 1/0/0",
			len(pendingFrom(e, takerYou)), len(pendingFrom(e, takerOpp)), len(pendingFrom(e, ownerYou)))
	}

	e = stealEngine(t, 734)
	takerBrush := onBoardCard(t, e, 0, choiceCorpusCard(t, "Evendo Brushrazer"))
	ownerBrush := onBoardCard(t, e, 1, choiceCorpusCard(t, "Evendo Brushrazer"))
	relic := onBoard(t, e, 1, "Name:Relic\nTypes:Artifact\nOracle:x\n")
	e.emit(events.Event{Kind: events.ControlChange, Obj: relic, Player: 0})
	e.emit(events.Sacrifice(relic))
	if len(pendingFrom(e, takerBrush)) != 1 || len(pendingFrom(e, ownerBrush)) != 0 {
		t.Fatalf("Brushrazer fired taker=%d owner=%d, want the taker's sacrifice only",
			len(pendingFrom(e, takerBrush)), len(pendingFrom(e, ownerBrush)))
	}
}

// TestTriggeredManaGoesToTheDefinedPlayer: Defined$ TriggeredPlayer (Eladamri's
// Vineyard, each player's first main phase) and Defined$ TriggeredActivator
// (Tangleroot, whoever casts a creature spell) add the mana for THAT player,
// and only for them. Over turns 1-3 with seat 0 starting, seat 0 has two
// first main phases and seat 1 one.
func TestTriggeredManaGoesToTheDefinedPlayer(t *testing.T) {
	t.Parallel()
	reg := choiceCorpusRegistry(t)
	want := map[string][2]int32{"Eladamri's Vineyard": {4, 2}, "Tangleroot": {1, 1}}
	for _, name := range []string{"Eladamri's Vineyard", "Tangleroot"} {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		e := New(seatZeroStart(Config{Seed: 900, Names: []string{"a", "b"}, Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}))
		e.Advance()
		onBoardCard(t, e, 0, c)
		start := len(e.L.Events)
		if name == "Tangleroot" {
			for p := state.PlayerID(0); p < 2; p++ {
				cr := e.G.AddObject(card(t, "Name:Bear\nTypes:Creature Bear\nManaCost:G\nPT:2/2\nOracle:x\n"), p)
				e.emit(events.Event{Kind: events.MoveZone, Obj: cr.ID, From: state.ZLibrary, To: state.ZHand})
				e.emit(events.Event{Kind: events.PutOnStack, Obj: cr.ID, Player: p, From: state.ZHand, To: state.ZStack, Text: "Bear"})
				e.putTriggersOnStack()
				for i := 0; i < 50 && len(e.G.Stack) > 0; i++ {
					d := e.Pending()
					if d == nil {
						e.Advance()
						continue
					}
					if d.Kind == decision.KPriority {
						submitPass(t, e)
						continue
					}
					ch := []int{}
					for j := 0; j < d.Min; j++ {
						ch = append(ch, j)
					}
					submitChoices(t, e, ch...)
				}
			}
		}
		driveToStepAll(t, e, 3, 0, state.StepMain2)
		var sum [2]int32
		for _, ev := range e.L.Events[start:] {
			if ev.Kind == events.ManaAdd && int(ev.Player) < 2 {
				sum[ev.Player] += ev.Amount
			}
		}
		if sum != want[name] {
			t.Errorf("%s mana by player = %v, want %v", name, sum, want[name])
		}
	}
}

// stealAndKill gives player 1's fresh Bear to player 0 and kills it, then
// puts the resulting triggers on the stack (none of these fixtures asks to
// order them).
func stealAndKill(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	bear := onBoardReady(t, e, 1, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.ControlChange, Obj: bear, Player: 0})
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	e.putTriggersOnStack()
	if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOrder {
		t.Fatalf("unexpected trigger ordering: %+v", d)
	}
	return bear
}

// TestTriggeredCardControllerFromTheStackIsTheLastController: a synthetic
// "whenever a creature dies, its controller draws a card" resolving from the
// stack draws for the stolen creature's taker.
func TestTriggeredCardControllerFromTheStackIsTheLastController(t *testing.T) {
	t.Parallel()
	e := stealEngine(t, 739)
	onBoard(t, e, 1, "Name:Wake\nTypes:Enchantment\n"+
		"T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Creature | TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ x\n"+
		"SVar:TrigDraw:DB$ Draw | Defined$ TriggeredCardController | NumCards$ 1\nOracle:x\n")
	stealAndKill(t, e)
	hands := [2]int{len(e.G.Zone(state.ZHand, 0)), len(e.G.Zone(state.ZHand, 1))}
	e.resolveTop()
	if got := [2]int{len(e.G.Zone(state.ZHand, 0)), len(e.G.Zone(state.ZHand, 1))}; got != [2]int{hands[0] + 1, hands[1]} {
		t.Fatalf("hands = %v, want the taker to draw: %v", got, [2]int{hands[0] + 1, hands[1]})
	}
}

// TestMeathookMassacreIIPayerIsTheStolenCreaturesLastController: the owner's
// Meathook Massacre II sees its stolen creature die as "a creature an
// opponent controls" and names that opponent (the taker) as the player who
// may pay 3 life. ChangeZone does not pose UnlessCost$ yet, so this pins the
// payer referent the way the stack object will resolve it: through the
// trigger context the ability carries onto the stack.
func TestMeathookMassacreIIPayerIsTheStolenCreaturesLastController(t *testing.T) {
	t.Parallel()
	e := stealEngine(t, 740)
	meathook := onBoardCard(t, e, 1, choiceCorpusCard(t, "Meathook Massacre II"))
	stealAndKill(t, e)
	if len(e.G.Stack) != 1 {
		t.Fatalf("Meathook trigger not on the stack: %v", e.G.Stack)
	}
	id := e.G.Stack[0]
	o := e.G.Obj(id)
	if o.Source != meathook || o.Ability.Params["UnlessPayer"] != "TriggeredCardController" {
		t.Fatalf("stack object is not Meathook's opponent-death trigger: %+v", o.Ability)
	}
	ctx := &effects.Ctx{Source: o.Source, Controller: o.Controller, Remembered: o.Remembered, TriggerContext: e.triggerContexts[id]}
	payer := effects.Defined(e, ctx, &cards.SA{Params: map[string]string{"Defined": o.Ability.Params["UnlessPayer"]}})
	if len(payer) != 1 || !payer[0].IsPlayer || payer[0].Player != 0 {
		t.Fatalf("UnlessPayer$ TriggeredCardController resolves to %+v, want the taker (player 0)", payer)
	}
}
