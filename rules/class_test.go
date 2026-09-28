package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Class enchantment type (CR 702.118), pinned end to end on the REAL
// corpus cards. Before the kw:Class expansion every one of these cards'
// level-up activators and level-granted abilities was inert: the K:Class line
// expanded to nothing, so a Class never gained a level and its level bands
// never turned on. The helpers come from search_library_test.go
// (searchTestRegistry, searchCorpusCard, searchMoveByName), cast_test.go
// (submitChoices, toMain1, addMana) and replacement_updated_test.go
// (passUntilStackEmpty). No Forge script text is committed (the decks are
// compiled corpus cards) and no Class card is in any repo deck or legacy
// golden deck, so no chain head depends on these cards.

// classEngine deals seat 0 a 40-card deck opening with the named fixtures
// (the Class under test first, then any support cards), followed by eight
// Forests and Grizzly Bears; the opponent's deck is all Mountains. seat 0 is
// pinned to the starting seat so its Main1 is the first turn driven to.
func classEngine(t *testing.T, reg *cards.Registry, fixtures ...string) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range fixtures {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 702118,
		Names: []string{"classPlayer", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// classMove is searchMoveByName for a named card, but it also accepts a
// card already on the battlefield (returns it) so a fixture can be moved and
// then acted on without depending on draw order.
func classMove(t *testing.T, e *Engine, name string, to state.Zone) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZBattlefield, state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == name {
				if z != to {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				}
				e.pending = nil
				e.priorityRound()
				return id
			}
		}
	}
	t.Fatalf("corpus fixture %q absent from seat 0 zones", name)
	return 0
}

// TestPaladinClassEntersAtLevel1LevelsUpAndTurnsOnItsStatic is the brief's
// done shape on Paladin Class: the Class enters at level 1, its level-2
// activator is offered as a sorcery for {2}{W}, and once the level is gained
// the level-2 SPump static ("Creatures you control get +1/+1") is actually
// applied to a creature on the battlefield.
func TestPaladinClassEntersAtLevel1LevelsUpAndTurnsOnItsStatic(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := classEngine(t, reg, "Paladin Class", "Grizzly Bears")

	class := classMove(t, e, "Paladin Class", state.ZBattlefield)
	bear := classMove(t, e, "Grizzly Bears", state.ZBattlefield)

	// CR 702.118a: a Class enters at level 1.
	if got := e.G.Obj(class).Counter("LEVEL"); got != 1 {
		t.Fatalf("precondition: Class entered with LEVEL=%d, want 1", got)
	}
	// Precondition: the lord is NOT live at level 1, and the bear is a real
	// 2/2, so the post-level assertion below is not vacuous.
	if got := e.Power(bear); got != 2 {
		t.Fatalf("precondition: bear power %d at level 1, want 2", got)
	}
	if got := e.Toughness(bear); got != 2 {
		t.Fatalf("precondition: bear toughness %d at level 1, want 2", got)
	}

	// The level-2 activator is offered, as a sorcery, for the K:Class cost.
	addMana(t, e, 0, "WWW")                   // {2}{W}
	opt, ok := findAbilityOption(e, class, 0) // the level-2 activator (first K:Class line)
	if !ok {
		t.Fatalf("level-2 activator not offered: %+v", e.Pending().Options)
	}
	if opt.Label == "" {
		t.Fatal("activator option has no label")
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)

	// CR 702.118b: the level counter is on, the next level's ability is live.
	if got := e.G.Obj(class).Counter("LEVEL"); got != 2 {
		t.Fatalf("after activating level 2: LEVEL=%d, want 2", got)
	}
	if got := e.Power(bear); got != 3 {
		t.Fatalf("level-2 SPump not applied: bear power %d, want 3", got)
	}
	if got := e.Toughness(bear); got != 3 {
		t.Fatalf("level-2 SPump not applied: bear toughness %d, want 3", got)
	}

	// The level-2 activator is withdrawn once the level is reached (a Class
	// at level 2 is no longer below level 2), while the level-3 activator --
	// below level 3 -- still exists.
	e.Advance()
	if _, ok := findAbilityOption(e, class, 0); ok {
		t.Fatalf("level-2 activator still offered at level 2: %+v", e.Pending().Options)
	}
	if !classHasAbility(e, class, 1) {
		t.Fatal("level-3 activator is not a real ability on the face")
	}
}

// TestPaladinClassBandUpIsSorcerySpeed pins that the activator is withheld
// outside a sorcery window -- the CR 702.118b "only as a sorcery" clause the
// expansion carries as SorcerySpeed$ True.
func TestPaladinClassBandUpIsSorcerySpeed(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := classEngine(t, reg, "Paladin Class")
	class := classMove(t, e, "Paladin Class", state.ZBattlefield)

	// Positive control: in seat 0's own Main1 the activator IS offered, so
	// the negative below is about the timing window, not a dead activator.
	addMana(t, e, 0, "WWW")
	if _, ok := findAbilityOption(e, class, 0); !ok {
		t.Fatalf("level-up not offered in a sorcery window: %+v", e.Pending().Options)
	}

	// Drive to seat 0's own BeginCombat: seat 0 holds priority there, but it
	// is not a sorcery window, so the activator must be withheld. Mana is
	// deliberately NOT added here -- addMana would drive back to Main1; the
	// SorcerySpeed$ gate runs before the cost gate, so none is needed.
	driveToStep(t, e, e.G.Turn, 0, state.StepBeginCombat)
	e.Advance()
	d := e.Pending()
	if d == nil {
		t.Fatal("no priority decision at BeginCombat")
	}
	if _, ok := findAbilityOption(e, class, 0); ok {
		t.Fatalf("level-up offered outside a sorcery window: %+v", d.Options)
	}
}

// TestCaretakersTalentLevelGainedTriggerFiresOnTheLevelUp pins the
// Mode$ ClassLevelGained trigger (CR 702.118c) on Caretaker's Talent's real
// script: its level-2 grant is a trigger ("When this Class becomes level 2,
// create a token that's a copy of target token you control"), which fires
// from the same CounterChange event the level-up activator emits.
func TestCaretakersTalentLevelGainedTriggerFiresOnTheLevelUp(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := classEngine(t, reg, "Caretaker's Talent")
	class := classMove(t, e, "Caretaker's Talent", state.ZBattlefield)
	if got := e.G.Obj(class).Counter("LEVEL"); got != 1 {
		t.Fatalf("precondition: Class at level %d, want 1", got)
	}
	// Precondition: the level-2 ability here is a TRIGGER, not a static, so
	// the post-assertion below is specifically about the ClassLevelGained
	// matcher.
	if !classHasTriggerMode(e.G.Obj(class).Face(), "ClassLevelGained") {
		t.Fatal("precondition: Caretaker's Talent carries no ClassLevelGained trigger")
	}

	addMana(t, e, 0, "W")
	opt, ok := findAbilityOption(e, class, 0)
	if !ok {
		t.Fatalf("level-2 activator not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(class).Counter("LEVEL"); got != 2 {
		t.Fatalf("LEVEL=%d after level-up, want 2", got)
	}
	// The ClassLevelGained trigger fired: its push names the Class as source.
	fired := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == class {
			fired = true
			break
		}
	}
	if !fired {
		t.Fatal("no TriggerPush for the Class after gaining level 2: ClassLevelGained did not match")
	}
}

// classHasAbility reports whether the live object offers ability index idx.
func classHasAbility(e *Engine, id state.ObjID, idx int) bool {
	o := e.G.Obj(id)
	if o == nil {
		return false
	}
	_, ok := o.PileAbilityAt(idx)
	return ok
}

// classHasTriggerMode reports whether the face carries a trigger with the
// named mode -- used as a precondition so a trigger test that silently lost
// its trigger fails loudly instead of passing.
func classHasTriggerMode(f *cards.Face, mode string) bool {
	if f == nil {
		return false
	}
	for _, tr := range f.Triggers {
		if tr.Mode == mode {
			return true
		}
	}
	return false
}

// TestFortuneTellersTalentLevelTwoGrantsPlayFromTop pins the brief's own
// carrier end to end: Fortune Teller's Talent's level-2 grant is the
// Mode$ Continuous MayPlay$ True static over Affected$ Card.TopLibrary+YouCtrl
// gated on CheckSVar$ X ("as long as you've cast a spell this turn, you may
// play cards from the top of your library"). The level gate the kw:Class
// expansion adds (counters_GE2_LEVEL) AND the static's own CheckSVar$ both
// have to hold before the top card is playable, and the top card must then
// actually be offered.
func TestFortuneTellersTalentLevelTwoGrantsPlayFromTop(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := classEngine(t, reg, "Fortune Teller's Talent", "Grizzly Bears", "Forest", "Forest")
	class := classMove(t, e, "Fortune Teller's Talent", state.ZBattlefield)
	if got := e.G.Obj(class).Counter("LEVEL"); got != 1 {
		t.Fatalf("precondition: Class at level %d, want 1", got)
	}

	addMana(t, e, 0, "UUUU") // {3}{U}
	opt, ok := findAbilityOption(e, class, 0)
	if !ok {
		t.Fatalf("level-2 activator not offered: %+v", e.Pending().Options)
	}
	if !strings.Contains(opt.Label, "Level 2") {
		t.Fatalf("activator label %q does not name level 2", opt.Label)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(class).Counter("LEVEL"); got != 2 {
		t.Fatalf("LEVEL=%d after level-up, want 2", got)
	}

	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) == 0 {
		t.Fatal("precondition: empty library")
	}
	top := lib[0]
	if e.G.Obj(top).Face() == nil || e.G.Obj(top).Face().IsLand() {
		t.Fatalf("precondition: top card %v is not a spell", e.G.Obj(top).Face())
	}
	// The static's own CheckSVar$ X ("you've cast a spell this turn") is
	// false, so the Class's level gate alone must not grant the play.
	if _, grant := e.mayPlayGrant(0, top); grant {
		t.Fatal("grant live before any spell this turn: the CheckSVar$ X gate was not read")
	}

	// Cast a spell this turn, then the top card becomes playable.
	addMana(t, e, 0, "GG")
	castOpt := castByName(t, e, 0, "Grizzly Bears")
	if castOpt == nil {
		t.Fatalf("no Grizzly Bears cast option: %+v", e.Pending().Options)
	}
	submitChoices(t, e, castOpt.Index)
	passUntilStackEmpty(t, e, 20)

	// The hand cast spent the pool; fund the top-of-library cast before
	// reading the offer, or the affordability gate (not the grant) is what
	// withholds it.
	addMana(t, e, 0, "GG")

	if free, grant := e.mayPlayGrant(0, top); !grant || free {
		t.Fatalf("top card grant after a spell this turn: free=%v grant=%v, want a non-free grant", free, grant)
	}
	// End to end: the top card is actually offered as a cast.
	d := e.Pending()
	if d == nil {
		t.Fatal("no priority decision after resolving the cast")
	}
	offered := false
	for _, o := range d.Options {
		if o.Obj == top && (o.Kind == "cast" || o.Kind == "mayplay") {
			offered = true
			break
		}
	}
	if !offered {
		t.Fatalf("top card %d not offered as a play: %+v", top, d.Options)
	}
}

// classLevelUp funds seat 0 generously and activates the level-up activator
// at ability index idx (0 = the first K:Class line, 1 = the second), then
// drains the stack. It asserts the CLASS had the expected level before and
// +1 after, so a vacuous activation (the activator not actually offered, or
// resolving to nothing) fails loudly instead of leaving the caller to
// misinterpret a later assertion.
func classLevelUp(t *testing.T, e *Engine, class state.ObjID, idx int) {
	t.Helper()
	before := e.G.Obj(class).Counter("LEVEL")
	addMana(t, e, 0, "WWWWWWGGGGGG")
	opt, ok := findAbilityOption(e, class, idx)
	if !ok {
		t.Fatalf("level-up activator %d not offered: %+v", idx, e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(class).Counter("LEVEL"); got != before+1 {
		t.Fatalf("level-up did not advance the Class: LEVEL %d -> %d, want %d", before, got, before+1)
	}
}

// TestHuntersTalentEndStepTriggerIsGatedByItsOwnIsPresentAndTheLevelBand is
// the regression pin for the level-band/own-IsPresent$ collision. Hunter's
// Talent's level-3 grant is a Phase trigger whose body carries its OWN
// IsPresent$ ("if you control a creature with power 4 or greater"). The old
// kw:Class expansion pushed the level band into IsPresent2$, which the trigger
// gate reads as a UNION with IsPresent$ -- so at LEVEL 1, with a 6/4 Craw Wurm
// on the battlefield, the level-3 "draw a card" fired every end step. The
// band is now a separate ClassBand$ AND gate.
func TestHuntersTalentEndStepTriggerIsGatedByItsOwnIsPresentAndTheLevelBand(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := classEngine(t, reg, "Hunter's Talent", "Craw Wurm")
	class := classMove(t, e, "Hunter's Talent", state.ZBattlefield)
	wurm := classMove(t, e, "Craw Wurm", state.ZBattlefield)

	// Preconditions: the Class is at level 1, the trigger exists, its body
	// has its own IsPresent$ (so the union reading would over-fire), and the
	// power-4 creature is on the battlefield satisfying THAT clause.
	if got := e.G.Obj(class).Counter("LEVEL"); got != 1 {
		t.Fatalf("precondition: Class at level %d, want 1", got)
	}
	if !classHasTriggerMode(e.G.Obj(class).Face(), "Phase") {
		t.Fatal("precondition: Hunter's Talent carries no Phase trigger")
	}
	if e.G.Obj(wurm) == nil || e.Power(wurm) < 4 {
		t.Fatalf("precondition: power-%d Craw Wurm not power>=4", e.Power(wurm))
	}

	// Level 1: the end step must NOT draw. The hand-draw the trigger would
	// emit is the only hand change across the end step.
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
	before := len(e.G.Zone(state.ZHand, 0))
	e.Advance()
	passUntilStackEmpty(t, e, 40)
	if got := len(e.G.Zone(state.ZHand, 0)); got != before {
		t.Fatalf("level-1 end step drew: hand %d -> %d (the band was ORed with the body's IsPresent$)", before, got)
	}

	// Level up to 3 across the next own Main1, then the SAME end step DOES
	// draw -- so the negative above is about the level band, not a dead
	// trigger.
	nextMain1 := e.G.Turn + 2 // seat 0's next own turn (2 seats)
	driveToStepAll(t, e, nextMain1, 0, state.StepMain1)
	classLevelUp(t, e, class, 0) // -> level 2
	classLevelUp(t, e, class, 1) // -> level 3
	if got := e.G.Obj(class).Counter("LEVEL"); got != 3 {
		t.Fatalf("precondition: Class at level %d, want 3", got)
	}
	driveToStepAll(t, e, nextMain1, 0, state.StepEnd)
	before = len(e.G.Zone(state.ZHand, 0))
	e.Advance()
	passUntilStackEmpty(t, e, 40)
	if got := len(e.G.Zone(state.ZHand, 0)); got != before+1 {
		t.Fatalf("level-3 end step did not draw: hand %d -> %d, want %d", before, got, before+1)
	}
}

// TestPaladinClassBandThreeGrantFires pins the level-3 grant path the old
// suite never asserted live: Paladin Class's level-3 AddTrigger$ is the
// AttackersDeclared "target attacking creature gets +1/+1 per other attacker
// and gains double strike". After the level-3 activation the trigger is on
// the live face and fires on an attack, so the granted trigger is not merely
// appended but actually reaches the trigger scan.
func TestPaladinClassBandThreeGrantFires(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := classEngine(t, reg, "Paladin Class", "Grizzly Bears")
	class := classMove(t, e, "Paladin Class", state.ZBattlefield)
	classMove(t, e, "Grizzly Bears", state.ZBattlefield)

	if got := e.G.Obj(class).Counter("LEVEL"); got != 1 {
		t.Fatalf("precondition: Class at level %d, want 1", got)
	}
	classLevelUp(t, e, class, 0) // -> level 2
	classLevelUp(t, e, class, 1) // -> level 3
	if got := e.G.Obj(class).Counter("LEVEL"); got != 3 {
		t.Fatalf("precondition: Class at level %d, want 3", got)
	}
	if !classHasTriggerMode(e.G.Obj(class).Face(), "AttackersDeclared") {
		t.Fatal("precondition: level-3 AttackersDeclared trigger is not on the live face")
	}

	// Attack with the Bear: the level-3 granted trigger must fire (a
	// TriggerPush naming the Class as source).
	passToKind(t, e, decision.KAttackers)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected a declare-attackers decision, got %+v", d)
	}
	attacker := -1
	for _, o := range d.Options {
		if o.Kind == "attacker" {
			attacker = o.Index
		}
	}
	if attacker < 0 {
		t.Fatalf("no attacker option for the Bear: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{attacker}}); err != nil {
		t.Fatalf("declare attacker: %v", err)
	}
	fired := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == class {
			fired = true
			break
		}
	}
	if !fired {
		t.Fatal("level-3 AttackersDeclared trigger did not fire on the attack")
	}
}

// TestClassBandBandGatesGrantedReplacement pins the replacement half of the
// level-band class: a granted replacement body that carries its own IsPresent$
// must keep that clause AND the level band. replacementConditionHolds reads
// IsPresent$ but never IsPresent2$, so the OLD expansion's band-in-IsPresent2$
// silently vanished and the level-N replacement was live from level 1 -- the
// wrong-wide direction. The band is now its own ClassBand$ AND gate, and the
// control below (same body, no band) proves the level-1 refusal is the band
// and not the body's own present clause.
func TestClassBandBandGatesGrantedReplacement(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := classEngine(t, reg, "Paladin Class", "Grizzly Bears")
	class := classMove(t, e, "Paladin Class", state.ZBattlefield)
	classMove(t, e, "Grizzly Bears", state.ZBattlefield)
	if got := e.G.Obj(class).Counter("LEVEL"); got != 1 {
		t.Fatalf("precondition: Class at level %d, want 1", got)
	}

	// Control: the body's own IsPresent$ (a creature you control) holds at
	// level 1, so a body WITHOUT the band is already live -- the band is the
	// only thing that can withhold the gated one below.
	control := cards.Repl{Event: "Moved", Params: map[string]string{"IsPresent": "Creature.YouCtrl"}}
	if !e.replacementConditionHolds(control, class, 0) {
		t.Fatal("control: body's own IsPresent$ does not hold, test premise broken")
	}
	gated := cards.Repl{Event: "Moved", Params: map[string]string{
		"IsPresent": "Creature.YouCtrl",
		"ClassBand": "2",
	}}
	if e.replacementConditionHolds(gated, class, 0) {
		t.Fatal("level-2 granted replacement live at level 1: the ClassBand$ band was not read")
	}

	classLevelUp(t, e, class, 0) // -> level 2
	if got := e.G.Obj(class).Counter("LEVEL"); got != 2 {
		t.Fatalf("precondition: Class at level %d, want 2", got)
	}
	if !e.replacementConditionHolds(gated, class, 0) {
		t.Fatal("granted replacement not live at level 2")
	}
}
