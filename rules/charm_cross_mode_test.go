package rules

// The cross-mode "Each mode must target a different player" Charm family
// (task agent-20260918T232601Z-47783205): Optional$ True on a Charm lowers
// the mode minimum to 0, and a Charm whose chosen modes include two or more
// single-target player-kind target-bearing modes asks ONE combined KTarget
// with per-player Option.Group exclusivity, so two chosen modes can never act
// on the same player — and each mode's body resolves against its OWN target,
// including across a mid-mode suspension (the remaining chosen modes resume
// through the charm_rest continuation).
//
// The Shadrix pins deliberately inline the REAL shadrix_silverquill.txt
// script (never a .cards file — the licensing rule) so the corpus carrier's
// exact parameter spellings are what the engine defends. The duo/duel
// suspension fixture is a synthetic carrier of the same shape (ChangeZone's
// hidden graveyard pick suspends mid-mode), documented as such.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// shadrixScript is shadrix_silverquill.txt's script, verbatim except the
// Oracle line (display text) and the deck-related rider.
const shadrixScript = "Name:Shadrix Silverquill\nManaCost:3 W B\nTypes:Legendary Creature Elder Dragon\nPT:2/5\n" +
	"K:Flying\nK:Double Strike\n" +
	"T:Mode$ Phase | Phase$ BeginCombat | ValidPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigCharm | TriggerDescription$ At the beginning of combat on your turn, you may ABILITY\n" +
	"SVar:TrigCharm:DB$ Charm | CharmNum$ 2 | Optional$ True | Choices$ Token,Card,Counters | AdditionalDescription$ Each mode must target a different player.\n" +
	"SVar:Token:DB$ Token | ValidTgts$ Player | TargetUnique$ True | TgtPrompt$ Select target player to create a 2/1 white and black inkling creature token with flying | TokenScript$ wb_2_1_inkling_flying | TokenOwner$ ThisTargetedPlayer | SpellDescription$ Target player creates a 2/1 white and black Inkling creature token with flying.\n" +
	"SVar:Card:DB$ Draw | ValidTgts$ Player | TargetUnique$ True | TgtPrompt$ Select target player to draw a card and lose 1 life | SubAbility$ DBLoseLife | SpellDescription$ Target player draws a card and loses 1 life.\n" +
	"SVar:DBLoseLife:DB$ LoseLife | Defined$ ParentTarget | LifeAmount$ 1\n" +
	"SVar:Counters:DB$ PutCounterAll | ValidTgts$ Player | Placer$ TargetedPlayer | TargetUnique$ True | TgtPrompt$ Select target player to put a +1/+1 counter on each creature they control | ValidCards$ Creature.TargetedPlayerCtrl | CounterType$ P1P1 | CounterNum$ 1 | SpellDescription$ Target player puts a +1/+1 counter on each creature they control.\n" +
	"Oracle:x\n"

const inklingScript = "Name:Inkling\nManaCost:\nTypes:Token Creature Inkling\nPT:2/1\nK:Flying\nOracle:x\n"

const vanillaCreatureScript = "Name:Vanilla\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// duoCharmScript is the synthetic suspension carrier: a cross-mode TargetUnique
// Charm whose first chosen mode (a hidden graveyard pick, donnie's and mikey's
// ChangeZone shape) suspends mid-mode and whose second mode must run after it
// on its own target.
func duoCharmScript() string {
	return "Name:Duo\nManaCost:1 U\nTypes:Creature Mutant Ninja Human Turtle\nPT:2/2\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCharm | TriggerDescription$ When NICKNAME enter, ABILITY\n" +
		"SVar:TrigCharm:DB$ Charm | CharmNum$ 2 | Choices$ MReturn,MLife | AdditionalDescription$ Each mode must target a different player.\n" +
		"SVar:MReturn:DB$ ChangeZone | ValidTgts$ Player | TargetUnique$ True | Hidden$ True | Mandatory$ True | ChangeType$ Creature.OwnedBy ThisTargetedPlayer | ChangeTypeDesc$ creature | ChangeNum$ 1 | Origin$ Graveyard | Destination$ Hand | SpellDescription$ Target player returns a creature card from their graveyard to their hand.\n" +
		"SVar:MLife:DB$ LoseLife | ValidTgts$ Player | TargetUnique$ True | LifeAmount$ 3 | SpellDescription$ Target player loses 3 life.\n" +
		"Oracle:x\n"
}

// shadrixAtPlacement drives a battlefield Shadrix to its combat trigger's
// placement modes ask and returns the engine with that decision pending.
// The placement ask (askTriggerModes, CR 603.3c) is posed by
// putTriggersOnStack itself — resolveTop must NOT be driven while it is
// pending, or effCharm would pose a second, mid-resolution ask on top of it.
func shadrixAtPlacement(t *testing.T, seed uint64) (*Engine, Config) {
	t.Helper()
	e, cfg, _ := newFixtureDeck(t, seed, shadrixScript)
	setFixtureToken(e, "wb_2_1_inkling_flying", card(t, inklingScript))
	putCreature(t, e, 0, shadrixScript)
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want exactly Shadrix's combat trigger", e.G.Stack)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "modes" {
		t.Fatalf("pending = %+v, want the placement KModes ask", d)
	}
	return e, cfg
}

// TestShadrixOptionalCharmAsksTheElection pins Part 1: Optional$ True lowers
// the placement ask's minimum to 0, so the "you may choose two" election is
// real — a zero-mode answer validates on the wire, and the declined charm
// resolves having done nothing at all.
func TestShadrixOptionalCharmAsksTheElection(t *testing.T) {
	e, cfg := shadrixAtPlacement(t, 6301)
	d := e.Pending()
	if d.Min != 0 || d.Max != 2 {
		t.Fatalf("placement ask bounds Min=%d Max=%d, want 0..2 (Optional$ True)", d.Min, d.Max)
	}
	if len(d.Options) != 3 {
		t.Fatalf("mode options = %d, want the three Choices$ candidates", len(d.Options))
	}
	for _, want := range []string{"Inkling", "loses 1 life", "+1/+1 counter"} {
		found := false
		for _, o := range d.Options {
			if strings.Contains(o.Label, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("SpellDescription$ label %q not offered: %+v", want, d.Options)
		}
	}
	// The Min-0 validation: a zero-mode answer is a legal intent (the same
	// shape TestCharmCannotBeCastForNoMode rejects against a Min-1 charm).
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
		t.Fatalf("zero-mode answer rejected against the Optional$ charm: %v", err)
	}
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	submitChoices(t, e) // decline: no modes
	passUntilStackEmpty(t, e, 20)
	if e.G.Players[0].Life != life0 || e.G.Players[1].Life != life1 {
		t.Fatalf("a declined charm moved life: %d/%d, want %d/%d",
			e.G.Players[0].Life, e.G.Players[1].Life, life0, life1)
	}
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Face() != nil && o.Face().Name == "Inkling" {
			t.Fatal("a declined charm created a token anyway")
		}
	}
	replayCheck(t, e, cfg)
}

// TestShadrixTwoModesTargetDifferentPlayers pins Part 2 + Part 3 end to end
// on the real script: choosing two modes asks ONE combined KTarget (Min ==
// Max == 2), the offered options carry per-player Group exclusivity, and the
// answers attribute positionally — the Token mode's target creates the token
// (TokenOwner$ ThisTargetedPlayer), the Card mode's OWN target draws and
// loses the life (Defined$ ParentTarget reads the mode's target, not the
// shared list).
func TestShadrixTwoModesTargetDifferentPlayers(t *testing.T) {
	e, cfg := shadrixAtPlacement(t, 6302)
	submitChoices(t, e, 0, 1) // Token, Card
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 2 || d.Max != 2 {
		t.Fatalf("pending = %+v, want the combined Min==Max==2 KTarget ask", d)
	}
	if len(d.Options) == 0 {
		t.Fatal("no target candidates offered")
	}
	byPlayer := map[state.PlayerID]int{}
	for _, o := range d.Options {
		if o.Group == "" {
			t.Fatalf("target option %+v carries no Group exclusivity", o)
		}
		byPlayer[o.Player]++
	}
	if byPlayer[0] != 1 || byPlayer[1] != 1 {
		t.Fatalf("candidate players: %v, want exactly one option per seat", byPlayer)
	}
	// The Group rule is the "different player" rule on the wire: an intent
	// that reuses a player cannot validate.
	idx0, idx1 := -1, -1
	for _, o := range d.Options {
		if o.Player == 0 {
			idx0 = o.Index
		}
		if o.Player == 1 {
			idx1 = o.Index
		}
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx0, idx0}}); err == nil {
		t.Fatal("an intent targeting the same player for both modes validated")
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx1, idx0}}); err != nil {
		t.Fatalf("a different-player intent was rejected: %v", err)
	}
	hand0, life0 := len(e.G.Zone(state.ZHand, 0)), e.G.Players[0].Life
	life1 := e.G.Players[1].Life
	// Answer order == chosen-mode order: player 1 is the Token mode's target,
	// player 0 the Card mode's.
	submitChoices(t, e, idx1, idx0)
	passUntilStackEmpty(t, e, 20)
	tokID := findByName(e, "Inkling", 1)
	if tokID == 0 {
		t.Fatal("the Token mode's target (player 1) did not create the Inkling token")
	}
	if o := e.G.Obj(tokID); o.Controller != 1 || o.Zone != state.ZBattlefield {
		t.Fatalf("token controller/zone = %d/%s, want 1/Battlefield (TokenOwner$ ThisTargetedPlayer)",
			o.Controller, o.Zone)
	}
	if findByName(e, "Inkling", 0) != 0 {
		t.Fatal("an Inkling ended up under player 0 — the modes shared a target")
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand0+1 {
		t.Fatalf("player 0 hand = %d, want %d (the Card mode's target drew a card)", got, hand0+1)
	}
	if e.G.Players[0].Life != life0-1 {
		t.Fatalf("player 0 life = %d, want %d (Defined$ ParentTarget read the Card mode's own target)",
			e.G.Players[0].Life, life0-1)
	}
	if e.G.Players[1].Life != life1 {
		t.Fatalf("player 1 life = %d, want %d (the LoseLife sub rode the Card mode's target only)",
			e.G.Players[1].Life, life1)
	}
	replayCheck(t, e, cfg)
}

// TestCrossModeCharmModesSurviveTheMidModeSuspension pins the suspension
// contract on a synthetic carrier of the family shape: mode 1 (a hidden
// graveyard pick) suspends mid-mode, the re-entered ChangeZone attributes its
// pick to ITS OWN mode target (player 1's graveyard, not the shared list),
// and the remaining mode resumes through the charm_rest continuation and acts
// on its own target rather than never running or running while the
// suspension was live.
func TestCrossModeCharmModesSurviveTheMidModeSuspension(t *testing.T) {
	t.Parallel()
	duo := duoCharmScript()
	e, cfg := charmTwoSeatDeck(t, 6303, duo)
	putCreature(t, e, 0, duo)
	// One creature in EACH graveyard: the pick's eligibility must come from
	// the mode's own target's graveyard only.
	addToGraveyard(t, e, 0, vanillaCreatureScript)
	addToGraveyard(t, e, 1, vanillaCreatureScript)
	e.putTriggersOnStack()
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("pending = %+v, want the placement modes ask", d)
	}
	submitChoices(t, e, 0, 1) // MReturn, MLife
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 2 || d.Max != 2 {
		t.Fatalf("pending = %+v, want the combined different-player KTarget", d)
	}
	idx0, idx1 := -1, -1
	for _, o := range d.Options {
		if o.Player == 0 {
			idx0 = o.Index
		}
		if o.Player == 1 {
			idx1 = o.Index
		}
	}
	submitChoices(t, e, idx1, idx0) // MReturn -> player 1, MLife -> player 0
	// Pass the priority rounds: the charm resolves and the ChangeZone mode's
	// hidden pick suspends the resolution mid-mode.
	for i := 0; i < 10; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision pending while the charm should resolve")
		}
		if d.Kind != decision.KPriority {
			break
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
			}
		}
		submitChoices(t, e, pass)
	}
	// The suspended hidden pick: the chooser is the mode's own target's
	// graveyard (player 1's), and only player 1's creature is eligible.
	d = e.Pending()
	if d == nil || d.ResumeKind != "hidden_pick" {
		t.Fatalf("pending = %+v, want the ChangeZone hidden graveyard pick", d)
	}
	for _, o := range d.Options {
		if o.Player != 1 {
			t.Fatalf("pick option %+v names player %d: the pick left the mode's own graveyard", o, o.Player)
		}
	}
	if len(d.Options) != 1 {
		t.Fatalf("pick options = %d, want only player 1's graveyard creature", len(d.Options))
	}
	life0 := e.G.Players[0].Life
	life1 := e.G.Players[1].Life
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	// The remaining mode ran (the rest continuation) and on its OWN target.
	if e.G.Players[0].Life != life0-3 {
		t.Fatalf("player 0 life = %d, want %d (the resumed MLife mode ran on its own target)",
			e.G.Players[0].Life, life0-3)
	}
	if e.G.Players[1].Life != life1 {
		t.Fatalf("player 1 life = %d, want %d", e.G.Players[1].Life, life1)
	}
	// The pick's move: player 1's creature went to player 1's hand.
	if findByName(e, "Vanilla", 1) == 0 || countZoneName(t, e, 1, state.ZHand, "Vanilla") != 1 {
		t.Fatalf("the picked creature did not reach player 1's hand")
	}
	replayCheck(t, e, cfg)
}

// TestCrossModeCharmSacrificeReentryKeepsItsOwnTarget pins the re-derived
// attribution on the re-entry that RE-EVALUATES its targets: a Sacrifice mode
// (balor's and vindictive_lich's shape) re-runs Defined() over the rebuilt
// context after its answered sacrifice pick, so without the re-derived
// per-mode target it would walk the WHOLE shared list and pose a second
// sacrifice ask for the other mode's target.
func TestCrossModeCharmSacrificeReentryKeepsItsOwnTarget(t *testing.T) {
	t.Parallel()
	lich := "Name:LichC\nManaCost:1 B\nTypes:Creature Zombie\nPT:2/2\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCharm | TriggerDescription$ When NICKNAME enter, ABILITY\n" +
		"SVar:TrigCharm:DB$ Charm | CharmNum$ 2 | Choices$ MSac,MLife | AdditionalDescription$ Each mode must target a different player.\n" +
		"SVar:MSac:DB$ Sacrifice | ValidTgts$ Player | TargetUnique$ True | SacValid$ Creature | SpellDescription$ Target player sacrifices a creature.\n" +
		"SVar:MLife:DB$ LoseLife | ValidTgts$ Player | TargetUnique$ True | LifeAmount$ 3 | SpellDescription$ Target player loses 3 life.\n" +
		"Oracle:x\n"
	e, cfg := charmTwoSeatDeck(t, 6305, lich)
	putCreature(t, e, 0, lich)
	putCreature(t, e, 0, vanillaCreatureScript) // seat 0's creature must be untouched
	putCreature(t, e, 1, vanillaCreatureScript)
	putCreature(t, e, 1, vanillaCreatureScript) // two eligibles: the sacrifice ask fires
	e.putTriggersOnStack()
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("pending = %+v, want the placement modes ask", d)
	}
	submitChoices(t, e, 0, 1) // MSac, MLife
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 2 || d.Max != 2 {
		t.Fatalf("pending = %+v, want the combined different-player KTarget", d)
	}
	idx0, idx1 := -1, -1
	for _, o := range d.Options {
		if o.Player == 0 {
			idx0 = o.Index
		}
		if o.Player == 1 {
			idx1 = o.Index
		}
	}
	submitChoices(t, e, idx1, idx0) // MSac -> player 1, MLife -> player 0
	// Drain: the charm resolves, MSac poses player 1's sacrifice pick, the
	// answer completes MSac, the rest continuation runs MLife on player 0.
	for i := 0; i < 30; i++ {
		d = e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			submitChoices(t, e, pass)
			continue
		}
		if d.ResumeKind != "sacrifice" || d.Player != 1 {
			break // the charm's effects have all landed; the rest is turn flow
		}
		// Sacrifice the LAST offered creature (never the first): the answer
		// order is the player's, and the attribution must survive the resume.
		submitChoices(t, e, d.Options[len(d.Options)-1].Index)
	}
	d = e.Pending()
	if d != nil && d.ResumeKind == "sacrifice" {
		t.Fatalf("a second sacrifice ask pended for the other mode's target: %+v", d)
	}
	if n := countZoneName(t, e, 1, state.ZBattlefield, "Vanilla"); n != 1 {
		t.Fatalf("player 1 battlefield Vanillas = %d, want 1 (one sacrificed)", n)
	}
	if n := countZoneName(t, e, 0, state.ZBattlefield, "Vanilla"); n != 1 {
		t.Fatalf("player 0 battlefield Vanillas = %d, want 1 (never a sacrifice target)", n)
	}
	replayCheck(t, e, cfg)
}

// TestCrossModeUnsupportedShapeStaysLoudAndNarrow pins the boundary the other
// way: a charm whose target-bearing modes carry TargetUnique$ but DISAGREE on
// the spec (or whose bounds are not single-target) keeps the historical
// first-target-bearing-mode narrowing and emits one loud Note naming the
// shape — never silent, never the combined ask.
func TestCrossModeUnsupportedShapeStaysLoudAndNarrow(t *testing.T) {
	t.Parallel()
	mixed := "Name:Mixed\nManaCost:1 U\nTypes:Creature Human\nPT:2/2\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCharm | TriggerDescription$ When NICKNAME enter, ABILITY\n" +
		"SVar:TrigCharm:DB$ Charm | CharmNum$ 2 | Choices$ MLife,MDrain | AdditionalDescription$ x\n" +
		"SVar:MLife:DB$ LoseLife | ValidTgts$ Player | TargetUnique$ True | LifeAmount$ 2 | SpellDescription$ Target player loses 2 life.\n" +
		"SVar:MDrain:DB$ Draw | ValidTgts$ Opponent | TargetUnique$ True | NumCards$ 1 | SpellDescription$ Target opponent draws a card.\n" +
		"Oracle:x\n"
	e, cfg := charmTwoSeatDeck(t, 6306, mixed)
	putCreature(t, e, 0, mixed)
	e.putTriggersOnStack()
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("pending = %+v, want the placement modes ask", d)
	}
	submitChoices(t, e, 0, 1)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 1 || d.Max != 1 {
		t.Fatalf("pending = %+v, want the historical first-mode single-target ask", d)
	}
	for _, o := range d.Options {
		if o.Group != "" {
			t.Fatalf("option %+v carries a Group: the combined ask leaked into an unsupported shape", o)
		}
	}
	sub := d.Options[0].Index
	submitChoices(t, e, sub)
	passUntilStackEmpty(t, e, 20)
	noted := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "cross-mode TargetUnique$ Charm shape unimplemented") {
			noted = true
		}
	}
	if !noted {
		t.Fatal("no loud Note named the unsupported cross-mode shape")
	}
	replayCheck(t, e, cfg)
}

func countZoneName(t *testing.T, e *Engine, p state.PlayerID, z state.Zone, name string) int {
	t.Helper()
	n := 0
	for _, id := range e.G.Zone(z, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			n++
		}
	}
	return n
}

// charmTwoSeatDeck builds a two-seat engine whose BOTH decks carry the vanilla
// creature (newFixtureDeck only customises seat 0's deck, and the suspension
// pins need creatures in BOTH seats' zones), seat 0's deck led by the
// protagonist charm card, with the toss starting seat 0.
func charmTwoSeatDeck(t *testing.T, seed uint64, protagonist string) (*Engine, Config) {
	t.Helper()
	deck := func(seeds ...string) []*cards.Card {
		out := make([]*cards.Card, 0, 40)
		for _, s := range seeds {
			out = append(out, card(t, s))
		}
		return append(out, mountainDeck(t, 40-len(out))...)
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{deck(protagonist, vanillaCreatureScript, vanillaCreatureScript), deck(vanillaCreatureScript, vanillaCreatureScript)},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	return e, cfg
}

// TestNonTargetUniqueCharmUsesDistinctModeTargets covers the ordinary
// two-target-bearing-mode Charm shape (the 406-SVA population, Kolaghan's
// Command's class): each selected mode gets its own target slot.
func TestNonTargetUniqueCharmUsesDistinctModeTargets(t *testing.T) {
	t.Parallel()
	charm := "Name:Cmd\nManaCost:B\nTypes:Instant\n" +
		"A:SP$ Charm | CharmNum$ 2 | Choices$ MLife,MDrain\n" +
		"SVar:MLife:DB$ LoseLife | ValidTgts$ Player | LifeAmount$ 2 | SpellDescription$ Target player loses 2 life.\n" +
		"SVar:MDrain:DB$ Draw | ValidTgts$ Player | NumCards$ 1 | SpellDescription$ Target player draws a card.\n" +
		"Oracle:x\n"
	e, cfg, id := newFixtureDeck(t, 6304, charm)
	addMana(t, e, 0, "B")
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("pending = %+v, want the cast modes ask", d)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err == nil {
		t.Fatal("a non-Optional charm accepted a zero-mode answer")
	}
	submitChoices(t, e, 0, 1) // both modes
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 2 || d.Max != 2 {
		t.Fatalf("pending = %+v, want one target slot for each selected mode", d)
	}
	if d.Options[0].Group == "" || d.Options[0].Group == d.Options[2].Group {
		t.Fatalf("mode target groups = %q and %q, want distinct per-mode groups", d.Options[0].Group, d.Options[2].Group)
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[2].Index)
	passUntilStackEmpty(t, e, 20)
	replayCheck(t, e, cfg)
}
