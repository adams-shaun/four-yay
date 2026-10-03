// This file is package view (internal), not view_test: describeFixture
// reuses identity_test.go's boltSrc, an unexported package-view constant.
// TestDescribeIsIdenticalAcrossTwoRunsOfTheSameMatch lives in
// describe_replay_test.go instead, under package view_test, for the same
// reason visibility_test.go does (see that file's own doc comment): it
// needs seat.Bot to drive a real game, and seat imports view (bot.go takes
// a view.View) — an internal test file that also imported seat would be a
// genuine import cycle for Go's test tooling (view[.test] -> seat ->
// view), not a style choice. That test touches no package-private symbol,
// so package view_test costs it nothing.
package view

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func describeFixture(t *testing.T) (*state.Game, state.ObjID, state.ObjID) {
	t.Helper()
	bear, _ := cards.ParseBytes("b.txt", []byte("Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	bolt, _ := cards.ParseBytes("l.txt", []byte(boltSrc))
	goblin, _ := cards.ParseBytes("g.txt", []byte("Name:Goblin\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n"))
	g := state.NewGame([]string{"Ann", "Bob"})
	b := g.AddObject(bear, 0)
	l := g.AddObject(bolt, 1)
	g.SetZone(state.ZBattlefield, 0, []state.ObjID{b.ID})
	b.Zone = state.ZBattlefield
	g.SetZone(state.ZHand, 1, []state.ObjID{l.ID})
	l.Zone = state.ZHand
	g.Players[0].Life = 17
	// TokenCreate's template needs a token table, and CmdDamage's needs a
	// roster: bear is Ann's only commander, so its match-wide dense index
	// is 0, and every seat's tally is sized to the roster the way rules.New
	// sizes it. The nonzero CmdDamage entries stand for the cumulative
	// totals as of the event being described (Ann 7, Bob 6) -- Describe's
	// "game as of the last event in ev's batch" contract.
	g.Tokens = map[string]*cards.Card{"r_1_1_goblin": goblin}
	g.Players[0].Commanders = []state.ObjID{b.ID}
	g.Players[0].CmdDamage = []int32{7}
	g.Players[1].CmdDamage = []int32{6}
	return g, b.ID, l.ID
}

// TestDescribeDamageCarriesNoSource is the defect-2 finding, pinned. The
// user's ask -- any time damage is assigned to a player, state the source
// card and the player target -- cannot be honoured: the Damage event carries
// no source field (events.Event has none, and neither effects/DealDamage nor
// rules/combat.go's emitters set one), so Describe cannot name what dealt the
// damage even when the game state has an obvious source. Fixing it requires
// adding a source to events.Event, which is forbidden (the binary encoding is
// hash-chained and replayed). This test documents the BLOCKED state and must
// be updated to assert the source the day the event gains one.
func TestDescribeDamageCarriesNoSource(t *testing.T) {
	g, _, _ := describeFixture(t)
	line := Describe(g, events.Event{Kind: events.Damage, Player: 1, Amount: 3})
	if line != "Bob takes 3 damage" {
		t.Fatalf("Describe = %q, want the source-less line (defect 2 is BLOCKED)", line)
	}
	if strings.Contains(line, "#") || strings.Contains(line, "Bolt") {
		t.Fatalf("Describe = %q named a source the Damage event does not carry", line)
	}
}

func TestDescribeTemplates(t *testing.T) {
	g, bear, bolt := describeFixture(t)
	cases := []struct {
		name string
		ev   events.Event
		want string
	}{
		{"game start", events.Event{Kind: events.GameStart, Amount: 4}, "Game starts with 4 players"},
		{"shuffle", events.Event{Kind: events.Shuffle, Player: 1}, "Bob shuffles their library"},
		{"move", events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard}, "Bear #1 moves from battlefield to graveyard"},
		{"hidden draw", events.Event{Kind: events.Draw, Player: 0}, "Ann draws a card"},
		{"visible draw", events.Event{Kind: events.Draw, Player: 1, Obj: bolt}, "Bob draws Bolt #2"},
		{"life gain", events.Event{Kind: events.LifeChange, Player: 0, Amount: 3}, "Ann gains 3 life (17)"},
		{"life loss", events.Event{Kind: events.LifeChange, Player: 0, Amount: -2}, "Ann loses 2 life (17)"},
		{"damage to creature", events.Event{Kind: events.Damage, Obj: bear, Amount: 2}, "Bear #1 takes 2 damage"},
		{"damage to player", events.Event{Kind: events.Damage, Player: 1, Amount: 3}, "Bob takes 3 damage"},
		{"tap", events.Event{Kind: events.Tap, Obj: bear}, "Bear #1 taps"},
		{"untap", events.Event{Kind: events.Untap, Obj: bear}, "Bear #1 untaps"},
		{"step", events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers}, "Step: declare-attackers"},
		{"turn", events.Event{Kind: events.TurnChange, Player: 1, Amount: 7}, "Turn 7: Bob"},
		{"priority", events.Event{Kind: events.Priority, Player: 0}, "Ann has priority"},
		{"cast", events.Event{Kind: events.PutOnStack, Player: 1, Obj: bolt}, "Bob casts Bolt #2"},
		{"resolve", events.Event{Kind: events.Resolve, Obj: bolt}, "Bolt #2 resolves"},
		{"mana add", events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 2}, "Ann adds {G}{G}"},
		{"mana spend", events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: -1}, "Ann spends {R}"},
		{"mana clear", events.Event{Kind: events.ManaClear, Player: 0}, "Ann's mana pool empties"},
		{"counter add", events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 2}, "Bear #1 gets 2 P1P1 counters"},
		{"counter remove", events.Event{Kind: events.CounterChange, Obj: bear, Counter: "M1M1", Amount: -1}, "Bear #1 loses 1 M1M1 counter"},
		{"stun counter remove", events.Event{Kind: events.CounterChange, Obj: bear, Counter: "STUN", Amount: -1}, "Bear #1 loses 1 STUN counter"},
		{"stun replacement", events.Event{Kind: events.CounterChange, Obj: bear, Counter: "STUN", Amount: -1, Text: events.UntapReplacedByStunNotice}, "Bear #1 would untap, but a stun counter is removed instead"},
		{"attackers", events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{bear}}, "Bear #1 attacks Bob"},
		{"no attackers", events.Event{Kind: events.DeclareAttackers, Player: 1}, "No attackers"},
		{"blockers", events.Event{Kind: events.DeclareBlockers, Pairs: [][2]state.ObjID{{bear, bolt}}}, "Bolt #2 blocks Bear #1"},
		{"no blockers", events.Event{Kind: events.DeclareBlockers}, "No blocks"},
		{"player lost", events.Event{Kind: events.PlayerLost, Player: 0}, "Ann loses the game"},
		{"win", events.Event{Kind: events.GameOver, Player: 1}, "Bob wins the game"},
		{"draw game", events.Event{Kind: events.GameOver, Amount: 1}, "The game is a draw"},
		{"ask", events.Event{Kind: events.DecisionAsk, Player: 0, Text: "priority"}, "Ann is asked: priority"},
		{"answer", events.Event{Kind: events.DecisionMade, Player: 0, Text: "priority:[2]"}, "Ann answers priority:[2]"},
		{"note", events.Event{Kind: events.Note, Text: "Bob reveals Bolt"}, "Bob reveals Bolt"},
		// effReveal's reveal (task fb-20260914T033246Z-3f1cc033, defect 1):
		// the Note carries the ids and no text, and Describe names what was
		// revealed — the client's only data path for hidden-zone ids.
		{"reveal note", events.Event{Kind: events.Note, Player: 1, IDs: []state.ObjID{bolt}}, "Bob reveals Bolt #2"},
		{"reveal note two", events.Event{Kind: events.Note, Player: 0, IDs: []state.ObjID{bolt, bear}}, "Ann reveals Bolt #2, Bear #1"},
		{"redacted note", events.Event{Kind: events.Note, Secret: true, Player: 1}, "Bob looks at hidden cards"},
		{"land", events.Event{Kind: events.LandPlayed, Player: 0}, "Ann plays a land"},
		{"target player", events.Event{Kind: events.TargetsChosen, Obj: bolt, Player: 0, Amount: 1}, "Bolt #2 targets Ann"},
		{"target objects", events.Event{Kind: events.TargetsChosen, Obj: bolt, IDs: []state.ObjID{bear}}, "Bolt #2 targets Bear #1"},
		// The append shapes: a second root target, and a SubAbility$ link's
		// cast-time target (CR 601.2c) -- a player one must not print as an
		// empty object list.
		{"target player appended", events.Event{Kind: events.TargetsChosen, Obj: bolt, Player: 1, Amount: 3}, "Bolt #2 targets Bob"},
		{"chain target player", events.Event{Kind: events.TargetsChosen, Obj: bolt, Player: 1, Amount: 3, Text: events.SubTargetNotice}, "Bolt #2 targets Bob"},
		{"chain target object", events.Event{Kind: events.TargetsChosen, Obj: bolt, IDs: []state.ObjID{bear}, Amount: 2, Text: events.SubTargetNotice}, "Bolt #2 targets Bear #1"},
		{"flip", events.Event{Kind: events.FlipFace, Obj: bear, Amount: 1}, "Bear #1 turns to face 1"},
		{"clock", events.Event{Kind: events.ClockTick}, ""},
		{"trigger", events.Event{Kind: events.TriggerPush, Player: 0, Obj: bear}, "Bear #1 triggers"},
		// The mutate fold and its under-card trigger push (CR 702.140): Text
		// is the placement choice; the merged push reads like the delayed and
		// granted pushes -- the resolving ability's own line says what it does.
		{"mutate top", events.Event{Kind: events.Mutate, Obj: bear, IDs: []state.ObjID{bolt}, Text: "top"}, "Bear #1 mutates with a card on top of it"},
		{"mutate under", events.Event{Kind: events.Mutate, Obj: bear, IDs: []state.ObjID{bolt}, Text: "under"}, "Bear #1 mutates with a card under it"},
		{"merged trigger", events.Event{Kind: events.MergedTriggerPush, Player: 0, Obj: bear}, "Bear #1 triggers (merged)"},
		{"end combat", events.Event{Kind: events.EndCombatReset}, "Combat ends"},
		{"cast info x", events.Event{Kind: events.CastInfo, Obj: bolt, Amount: 3}, "Bob casts Bolt #2 (X = 3)"},
		{"cast info mode", events.Event{Kind: events.CastInfo, Obj: bolt, Counter: "kicked"}, "Bob casts Bolt #2 (kicked)"},
		{"choose name", events.Event{Kind: events.Choose, Obj: bolt, Counter: "name", Text: "Giant Growth"}, "Bob chooses the name Giant Growth for Bolt #2"},
		{"choose type", events.Event{Kind: events.Choose, Obj: bolt, Counter: "type", Text: "Elf"}, "Bob chooses the type Elf for Bolt #2"},
		{"choose number", events.Event{Kind: events.Choose, Obj: bolt, Counter: "number", Amount: 3}, "Bob chooses the number 3 for Bolt #2"},
		// CR 310.10: the protector Choose carries the chosen opponent's seat
		// on Player, so the transcript names that opponent rather than the
		// card's controller (which the generic wordings read).
		{"choose protector", events.Event{Kind: events.Choose, Obj: bolt, Counter: "protector", Player: 0}, "Ann protects Bolt #2 (chosen by Bob)"},
		{"token", events.Event{Kind: events.TokenCreate, Player: 1, Text: "r_1_1_goblin"}, "Bob creates a Goblin token"},
		{"token unknown script", events.Event{Kind: events.TokenCreate, Player: 1, Text: "nope"}, "Bob creates a token"},
		{"copy", events.Event{Kind: events.StackCopy, Obj: bolt, Player: 0}, "Ann copies Bolt #2"},
		{"attach", events.Event{Kind: events.Attach, Obj: bolt, IDs: []state.ObjID{bear}}, "Bolt #2 attaches to Bear #1"},
		{"detach", events.Event{Kind: events.Attach, Obj: bolt}, "Bolt #2 detaches"},
		{"detach with reason", events.Event{Kind: events.Attach, Obj: bolt, Text: "Equipmentbearer is no longer a creature"}, "Bolt #2 detaches (Equipmentbearer is no longer a creature)"},
		{"activate", events.Event{Kind: events.AbilityPush, Obj: bear, Player: 0, Amount: 0}, "Ann activates Bear #1"},
		{"modes", events.Event{Kind: events.ModeChosen, Player: 0, Text: "Fear,Terror"}, "Ann chooses Fear, Terror"},
		{"unless pay", events.Event{Kind: events.ModeChosen, Player: 1, Text: "Don't pay"}, "Bob chooses Don't pay"},
		{"commander damage", events.Event{Kind: events.CmdDamage, Player: 1, Obj: bear, Amount: 2}, "Bear #1 deals 2 commander damage to Bob (6 total; 21 is lethal)"},
		{"commander damage unknown clock", events.Event{Kind: events.CmdDamage, Player: 1, Amount: 2}, "a card deals 2 commander damage to Bob"},
		{"monarch", events.Event{Kind: events.MonarchChange, Player: 1}, "Bob becomes the monarch"},
		{"ring tempts", events.Event{Kind: events.RingTemptsYou, Player: 1, Obj: bear}, "The Ring tempts Bob (Bear #1 is the Ring-bearer)"},
		{"ring tempts, no bearer", events.Event{Kind: events.RingTemptsYou, Player: 1}, "The Ring tempts Bob"},
		{"unknown kind", events.Event{Kind: 250}, "unknown event"},
		{"unknown seat", events.Event{Kind: events.Priority, Player: 9}, "seat 9 has priority"},
		{"unknown object", events.Event{Kind: events.Tap, Obj: 77}, "#77 taps"},
	}
	for _, tc := range cases {
		if got := Describe(g, tc.ev); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestDescribeAbilityObjectNamesSource pins the lc1 object-rendering change:
// a faceless ability stack object (minted by events.Apply's
// TriggerPush/AbilityPush/DelayedPush paths, which set o.Source to the source
// permanent) must be named by its source card rather than the unhelpful "an
// ability", and must degrade to the old phrasing when the source is 0,
// unresolvable, or itself faceless.
func TestDescribeAbilityObjectNamesSource(t *testing.T) {
	bear, _ := cards.ParseBytes("b.txt", []byte("Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	// A source card with a real face, plus a faceless ability object.
	g := state.NewGame([]string{"Ann", "Bob"})
	b := g.AddObject(bear, 0)

	mint := func(source state.ObjID) *state.Object {
		o := g.AddObject(nil, 0)
		o.Ability = &cards.SA{API: "X"}
		o.Source = source
		return o
	}

	// A triggered/activated ability names its source card (task lc1 req 3).
	sourced := mint(b.ID)
	if got := obj(g, sourced.ID); got != "Bear's ability #"+itoa(int64(sourced.ID)) {
		t.Fatalf("obj with resolvable source = %q", got)
	}

	// A source id of 0 (never set, or redacted) degrades to the bare phrasing.
	zero := mint(0)
	if got := obj(g, zero.ID); got != "an ability #"+itoa(int64(zero.ID)) {
		t.Fatalf("obj with zero source = %q", got)
	}

	// A source id the game no longer holds also degrades, never an empty possessive.
	stale := mint(1 << 30)
	if got := obj(g, stale.ID); got != "an ability #"+itoa(int64(stale.ID)) {
		t.Fatalf("obj with unresolvable source = %q", got)
	}

	// A source that is itself faceless (an ability object) degrades too.
	faceless := mint(0)
	fromAbility := mint(faceless.ID)
	if got := obj(g, fromAbility.ID); got != "an ability #"+itoa(int64(fromAbility.ID)) {
		t.Fatalf("obj with faceless source = %q", got)
	}
}

// TestDescribeCastInfoOnAnActivatedAbilitySaysActivates is the view-side
// regression pin for the paid-{X} binding's ability arm: commitCast
// (rules/cast.go) records an activated ability's chosen {X} with the same
// CastInfo event the spell arm uses, on the AbilityPush-minted stack object,
// and that object is faceless -- so the old unconditional "casts" verb
// rendered the activation as "Ann casts Bear's ability #N (X = 2)". The verb
// follows the object: "activates" for an ability stack object that is not a
// trigger mint (state.TriggerOf, the classifier StackView.Kind shares),
// "casts" for a spell object and for a trigger mint (which no CastInfo
// emitter ever targets; the branch is Describe's totality stance).
func TestDescribeCastInfoOnAnActivatedAbilitySaysActivates(t *testing.T) {
	bear, _ := cards.ParseBytes("b.txt", []byte("Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	crier, _ := cards.ParseBytes("c.txt", []byte("Name:Crier\nManaCost:1 W\nTypes:Creature Human\n"+
		"T:Mode$ Phase | Phase$ End of Turn | Execute$ TrigLife | TriggerDescription$ x\n"+
		"SVar:TrigLife:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"))
	crier.Link()
	g := state.NewGame([]string{"Ann", "Bob"})
	b := g.AddObject(bear, 0)
	c := g.AddObject(crier, 0)

	mint := func(source state.ObjID, sa *cards.SA) *state.Object {
		o := g.AddObject(nil, 0)
		o.Ability = sa
		o.Source = source
		return o
	}

	// An activated-ability stack object (no T: line on Bear's face matches it,
	// so state.TriggerOf misses) reads as an activation.
	activated := mint(b.ID, &cards.SA{API: "PutCounter"})
	if got, want := Describe(g, events.Event{Kind: events.CastInfo, Obj: activated.ID, Amount: 2}),
		"Ann activates Bear's ability #"+itoa(int64(activated.ID))+" (X = 2)"; got != want {
		t.Fatalf("CastInfo on an activated ability = %q, want %q", got, want)
	}

	// A spell object still reads as a cast (the templates table's own
	// "cast info x" row covers this; asserted here against a real face too).
	if got := Describe(g, events.Event{Kind: events.CastInfo, Obj: b.ID, Amount: 3}); !strings.Contains(got, " casts ") {
		t.Fatalf("CastInfo on a spell object = %q, want the casts verb", got)
	}

	// A trigger mint (state.TriggerOf matches the source face's T: Effect by
	// pointer identity) keeps the casts verb: no emitter targets one, so this
	// only pins the branch's totality stance.
	triggered := mint(c.ID, crier.Faces[0].Triggers[0].Effect)
	if got := Describe(g, events.Event{Kind: events.CastInfo, Obj: triggered.ID, Amount: 1}); !strings.Contains(got, " casts ") {
		t.Fatalf("CastInfo on a trigger mint = %q, want the casts verb", got)
	}
}

func TestDescribeCoversEveryKind(t *testing.T) {
	g, bear, _ := describeFixture(t)
	// The bound derives from the enum (events.NumKinds), never a kind name:
	// the old loop ran to EndCombatReset, which stopped being the last Kind
	// when TargetsChosen was appended, so the eight kinds after it (CastInfo
	// through CmdDamage) fell outside the loop and rendered "unknown event"
	// for months without a single failure. With the bound derived from
	// NumKinds, appending a Kind and not describing it fails this test by
	// construction -- no edit to this loop is ever needed again.
	for k := events.GameStart; int(k) < events.NumKinds; k++ {
		ev := events.Event{Kind: k, Player: 0, Obj: bear, Amount: 1, IDs: []state.ObjID{bear}, Pairs: [][2]state.ObjID{{bear, bear}}}
		got := Describe(g, ev)
		if k == events.ClockTick {
			if got != "" {
				t.Errorf("ClockTick should describe as empty, got %q", got)
			}
			continue
		}
		if got == "" || got == "unknown event" {
			t.Errorf("kind %s (%d) has no description: give its events/kindinfo.go entry a Describe template, "+
				"or a case in view/describe.go if the line needs more than the event's own fields", k, k)
		}
	}
}

// TestDescribeTemplateRendersEveryField holds describeTemplate to the
// placeholder vocabulary events declares: a field added to
// events.DescribeFields without a renderer here would print raw.
func TestDescribeTemplateRendersEveryField(t *testing.T) {
	g, bear, _ := describeFixture(t)
	ev := events.Event{Player: 0, Obj: bear, Amount: 7, Text: "why"}
	for _, f := range events.DescribeFields {
		tmpl := "<{" + f + "}>"
		if got := describeTemplate(g, ev, tmpl); got == tmpl || strings.Contains(got, "{") {
			t.Errorf("placeholder {%s} is not rendered: %q", f, got)
		}
	}
	if got, want := describeTemplate(g, ev, "{player} notes {amount}: {text}"), player(g, 0)+" notes 7: why"; got != want {
		t.Fatalf("describeTemplate = %q, want %q", got, want)
	}
}

func TestDescribeNeverPanics(t *testing.T) {
	g, _, _ := describeFixture(t)
	for _, gg := range []*state.Game{nil, g, state.NewGame(nil)} {
		// Every defined Kind plus an over-large tail of undefined values:
		// Describe must return "unknown event" rather than panic for a kind
		// it has no case for, even hostile fields. The bound is the enum's
		// own count plus a fixed guard tail -- the old hardcoded 40 silently
		// shrank as kinds were appended, until it would have stopped fuzzing
		// them entirely. The +8 tail keeps a few undefined kind values in
		// the fuzz; new kinds always land inside the loop.
		for k := 0; k < int(events.NumKinds)+8; k++ {
			ev := events.Event{Kind: events.Kind(k), Player: 250, Obj: 1 << 30, Amount: -7, Step: 99, From: 99, To: 99,
				IDs: []state.ObjID{0, 1 << 30}, Pairs: [][2]state.ObjID{{0, 1 << 30}}}
			_ = Describe(gg, ev)
		}
	}
}

func TestDescribeManaAddNamesTypedUnitsByColour(t *testing.T) {
	for counter, want := range map[string]string{"ArtifactC": "{C}{C}", "TreasureW": "{W}{W}", "G": "{G}{G}"} {
		got := Describe(nil, events.Event{Kind: events.ManaAdd, Counter: counter, Amount: 2})
		if !strings.HasSuffix(got, " adds "+want) {
			t.Fatalf("ManaAdd %s describes as %q, want suffix %q", counter, got, want)
		}
	}
}
