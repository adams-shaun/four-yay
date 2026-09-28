package rules

// GameActivationLimit$ is the per-GAME sibling of ActivationLimit$: "Activate
// only once." (Stalking Leonin, Haunted Screen, the Gate cycle). It was
// unread -- no reader in rules/ -- so such an ability was offered again every
// turn for the rest of the game. These leaves pin the shared gate
// (activationLimitBlocked, rules/legal.go) that reads BOTH limits at every
// activation offer site (printed, granted, mana):
//
//   - the file's own fixture carrier: offered once, withheld on the second
//     activation in the same turn, AND still withheld on a later turn (the
//     point of a per-GAME limit: the per-turn sibling re-arms at TurnChange,
//     this one must not);
//   - the real corpus carrier Haunted Screen, end to end;
//   - the GRANTED-ability carrier Touch of Vitae, whose Animate-delivered
//     "{0}: Untap this creature. Activate only once." is the shape the old
//     granted-ability comment claimed did not exist ("no corpus granted
//     ability carries a limit"), with the SVar-name activation identity
//     DelayedPush/GrantAbilityPush record.
//
// The per-turn ActivationLimit$ behaviour is unchanged and stays pinned by
// activation_limit_test.go.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// gameLimitedBeastSrc is the fixture: a cheap self-pump carrying
// GameActivationLimit$ 1 and no other gate, so a withheld second activation
// can only be the limit -- never a dry pool or a target requirement.
const gameLimitedBeastSrc = "Name:OnceBeast\nManaCost:0\nTypes:Creature Beast\nPT:2/2\n" +
	"A:AB$ Pump | Cost$ 1 G | Defined$ Self | Power$ 1 | Toughness$ 1 | GameActivationLimit$ 1 | SpellDescription$ CARDNAME gets +1/+1. Activate only once.\nOracle:x\n"

// TestGameActivationLimitWithholdsSecondActivationSameGame is the core leaf:
// a GameActivationLimit$ 1 ability is offered once, withheld on the second
// activation of the same turn, and -- unlike its per-turn sibling -- stays
// withheld after the turn changes.
func TestGameActivationLimitWithholdsSecondActivationSameGame(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 11, gameLimitedBeastSrc)
	moveByName(t, e, 0, "OnceBeast", state.ZBattlefield)
	// Four green: two {1}{G} activations are payable, so the withheld second
	// offer is genuinely the limit, never a dry pool.
	addMana(t, e, 0, "GGGG")

	if _, ok := findAbilityOption(e, id, 0); !ok {
		t.Fatalf("GameActivationLimit$ 1 ability not offered on the first activation: %+v", e.Pending().Options)
	}
	opt := abilityOption(t, e, id, 0)
	submitChoices(t, e, opt.Index)
	if len(e.G.Stack) != 1 {
		t.Fatalf("first activation did not push an ability object: stack=%v", e.G.Stack)
	}
	// Second activation is WITHHELD (used=1 >= 1). The per-turn sibling would
	// also withhold here, so this alone does not prove the per-GAME read.
	if _, ok := findAbilityOption(e, id, 0); ok {
		t.Fatalf("GameActivationLimit$ 1 ability offered twice in the same turn: %+v", e.Pending().Options)
	}

	// Advance across a turn boundary: the per-turn limit re-arms, this one
	// must not. Fund the new turn's activation so a withheld offer is the
	// limit rather than an empty pool.
	turnBefore := e.G.Turn
	for i := 0; i < 400 && e.G.Turn == turnBefore && !e.G.Over; i++ {
		submitPass(t, e)
	}
	if e.G.Turn == turnBefore {
		t.Fatalf("game did not advance past turn %d (over=%v)", turnBefore, e.G.Over)
	}
	addMana(t, e, 0, "GGGG")
	if _, ok := findAbilityOption(e, id, 0); ok {
		t.Fatalf("GameActivationLimit$ 1 ability re-offered on turn %d (a per-game limit must not re-arm): %+v",
			e.G.Turn, e.Pending().Options)
	}
}

// TestGameActivationLimitRealCorpusHauntedScreen pins the real corpus carrier
// end to end: Haunted Screen's "Put seven +1/+1 counters on CARDNAME. It
// becomes a 0/0 Spirit creature ... Activate only once." is offered once and
// withheld after that activation.
func TestGameActivationLimitRealCorpusHauntedScreen(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	hs, ok := reg.Lookup("Haunted Screen")
	if !ok {
		t.Fatal("corpus fixture: Haunted Screen missing")
	}
	e := New(Config{Seed: 42, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append([]*cards.Card{hs}, mountainDeck(t, 40)...), mountainDeck(t, 40)}})
	e.Advance()
	toMain1(t, e)
	id := moveByName(t, e, 0, "Haunted Screen", state.ZBattlefield)
	// Fourteen: the {7} activation twice over, so the withheld second offer is
	// the game limit, never the pool.
	addMana(t, e, 0, "CCCCCCCCCCCCCC")

	// Haunted Screen's activated ability is not flat index 0: the face also
	// carries a PayLife mana ability. Find the offer by its SpellDescription
	// text so the leaf does not hardcode the census.
	opt := abilityOptionByLabel(t, e, id, "Put seven +1/+1 counters")
	submitChoices(t, e, opt.Index)
	if len(e.G.Stack) != 1 {
		t.Fatalf("Haunted Screen activation did not push an ability object: stack=%v", e.G.Stack)
	}
	if _, ok := findAbilityOptionByLabel(e, id, "Put seven +1/+1 counters"); ok {
		t.Fatalf("Haunted Screen offered a second time (GameActivationLimit$ 1 unread): %+v", e.Pending().Options)
	}
}

// TestGameActivationLimitGrantedAbilityWithheldAfterOneUse pins the granted
// half of the class fix. Granted activations do NOT mint an AbilityPush with a
// flat face index: beginGrantedActivation emits a DelayedPush whose Counter is
// the SVar name (rules/speed.go), so the per-game count must match that
// identity. The fixture animates itself with a granted AB carrying
// GameActivationLimit$ 1 -- the Animate grant resolves the SVar name off the
// animated object's own face table (effects/combatfx.go), so a self-animate
// is the shape that actually works (see the Touch of Vitae concern in the
// report: a NON-permanent source's Animate grant resolves against the
// recipient's table and so never finds its SVar today).
func TestGameActivationLimitGrantedAbilityWithheldAfterOneUse(t *testing.T) {
	t.Parallel()
	const grantedSrc = "Name:GrantBeast\nManaCost:0\nTypes:Creature Beast\nPT:2/2\n" +
		"A:AB$ Animate | Cost$ 0 | Defined$ Self | Abilities$ ABLimited | Duration$ Permanent | SpellDescription$ CARDNAME gains an ability.\n" +
		"SVar:ABLimited:AB$ Untap | Cost$ 0 | Defined$ Self | GameActivationLimit$ 1 | SpellDescription$ Untap this creature. Activate only once.\n" +
		"Oracle:x\n"
	e, _, id := newFixtureDeck(t, 13, grantedSrc)
	moveByName(t, e, 0, "GrantBeast", state.ZBattlefield)
	addMana(t, e, 0, "") // re-ask priority so the moved permanent is offered

	// Turn on the grant: the self-Animate resolves and installs an
	// AddAbilities continuous effect naming ABLimited on this object's own
	// (empty) SVar table is NOT where it lives -- the grant resolves against
	// the animated object, whose face DOES define ABLimited here.
	opt := abilityOption(t, e, id, 0) // the {0} Animate is the face's only AB
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 400)

	if _, ok := findGrantedAbilityOption(e, id, "ABLimited"); !ok {
		t.Fatalf("self-Animate grant not offered after resolution: %+v", e.Pending().Options)
	}
	gopt := grantedAbilityOption(t, e, id, "ABLimited")
	submitChoices(t, e, gopt.Index)
	passUntilStackEmpty(t, e, 400)

	if _, ok := findGrantedAbilityOption(e, id, "ABLimited"); ok {
		t.Fatalf("granted GameActivationLimit$ 1 ability re-offered after its one use: %+v", e.Pending().Options)
	}
}

// abilityOptionByLabel / findAbilityOptionByLabel locate an "ability" option
// on obj whose label contains want, independent of its flat pile index.
func abilityOptionByLabel(t *testing.T, e *Engine, id state.ObjID, want string) decision.Option {
	t.Helper()
	o, ok := findAbilityOptionByLabel(e, id, want)
	if !ok {
		t.Fatalf("no ability option on %d containing %q: %+v", id, want, e.Pending().Options)
	}
	return o
}

func findAbilityOptionByLabel(e *Engine, id state.ObjID, want string) (decision.Option, bool) {
	d := e.Pending()
	if d == nil {
		return decision.Option{}, false
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id && strings.Contains(o.Label, want) {
			return o, true
		}
	}
	return decision.Option{}, false
}

// TestTouchOfVitaeGrantsTargetedUntapAbility pins Animate's cross-object
// AddAbilities provenance: the Bears remain the continuous effect's Source,
// while Touch owns the SVar body and therefore the activation grant source.
func TestTouchOfVitaeGrantsTargetedUntapAbility(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	touch := lookup(t, reg, "Touch of Vitae")
	bearsCard := lookup(t, reg, "Grizzly Bears")
	e, cfg := animateTriggersEngine(t, reg, touch, bearsCard)
	touchID := moveByName(t, e, 0, "Touch of Vitae", state.ZHand)
	bearsID := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	if e.G.Obj(bearsID).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Bears not on battlefield: %+v", e.G.Obj(bearsID))
	}
	driveToStep(t, e, 3, 0, state.StepMain1)
	addMana(t, e, 0, "CCG")
	e.emit(events.Event{Kind: events.Tap, Obj: bearsID})
	if !e.G.Obj(bearsID).Tapped {
		t.Fatalf("precondition: Bears could not be tapped on battlefield: %+v", e.G.Obj(bearsID))
	}
	e.priorityRound()
	var cast decision.Option
	found := false
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == touchID {
			cast, found = o, true
			break
		}
	}
	if !found {
		t.Fatalf("Touch of Vitae cast option absent: obj=%+v pending=%+v", e.G.Obj(touchID), e.Pending().Options)
	}
	submitChoices(t, e, cast.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Touch target decision absent: %+v", d)
	}
	target := -1
	for _, o := range d.Options {
		if o.Obj == bearsID {
			target = o.Index
		}
	}
	if target < 0 {
		t.Fatalf("Bears absent from Touch targets: %+v", d.Options)
	}
	submitChoices(t, e, target)
	passUntilStackEmpty(t, e, 100)
	if e.G.Obj(bearsID).Zone != state.ZBattlefield || !e.G.Obj(bearsID).Tapped {
		t.Fatalf("precondition after Touch: Bears left battlefield or became untapped: %+v", e.G.Obj(bearsID))
	}
	var grant *ContinuousEffect
	for i := range e.continuous {
		ce := &e.continuous[i]
		if ce.Source == bearsID && len(ce.AddAbilities) != 0 {
			grant = ce
			break
		}
	}
	if grant == nil {
		t.Fatal("Touch did not register an AddAbilities grant on Bears")
	}
	if grant.AbilityGrantor != touchID || grant.Source != bearsID {
		t.Fatalf("grant Source/AbilityGrantor = %d/%d, want %d/%d", grant.Source, grant.AbilityGrantor, bearsID, touchID)
	}
	if cards.ResolveSVar(grant.SVars, "ABUntap") == nil {
		t.Fatal("Touch AddAbilities grant does not resolve ABUntap from its SVar table")
	}
	gopt := grantedAbilityOption(t, e, bearsID, "ABUntap")
	if gopt.GrantSource != touchID {
		t.Fatalf("granted option source = %d, want Touch %d", gopt.GrantSource, touchID)
	}
	submitChoices(t, e, gopt.Index)
	passUntilStackEmpty(t, e, 100)
	if e.G.Obj(bearsID).Tapped {
		t.Fatal("Touch's granted ABUntap did not untap Bears")
	}
	// Re-tap the recipient before checking the second offer: withholding is
	// therefore demonstrably the GameActivationLimit, not an untapped-target
	// legality shortcut.
	e.emit(events.Event{Kind: events.Tap, Obj: bearsID})
	if !e.G.Obj(bearsID).Tapped {
		t.Fatal("precondition: Bears could not be re-tapped for the limit check")
	}
	var pushes []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.GrantAbilityPush && ev.Obj == bearsID {
			pushes = append(pushes, ev)
		}
	}
	if len(pushes) != 1 || len(pushes[0].IDs) != 1 || pushes[0].IDs[0] != touchID {
		t.Fatalf("GrantAbilityPush = %+v, want recipient Bears and grantor Touch", pushes)
	}
	if _, ok := findGrantedAbilityOption(e, bearsID, "ABUntap"); ok {
		t.Fatal("Touch's GameActivationLimit$ 1 ability was offered after its one use")
	}
	replayCheck(t, e, cfg)
}

// findGrantedAbilityOption locates a granted option on obj by its SVar name.
func findGrantedAbilityOption(e *Engine, obj state.ObjID, svar string) (decision.Option, bool) {
	d := e.Pending()
	if d == nil {
		return decision.Option{}, false
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == obj && o.SVar == svar {
			return o, true
		}
	}
	return decision.Option{}, false
}

func grantedAbilityOption(t *testing.T, e *Engine, obj state.ObjID, svar string) decision.Option {
	t.Helper()
	o, ok := findGrantedAbilityOption(e, obj, svar)
	if !ok {
		t.Fatalf("no granted ability %q option on %d: %+v", svar, obj, e.Pending().Options)
	}
	return o
}
