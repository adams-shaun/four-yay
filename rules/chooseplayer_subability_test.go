package rules

// chooseplayer-subability-riders: ChoosePlayer's chained riders
// (ChooseSubAbility$/CantChooseSubAbility$) and the per-player MustAttack$
// ChosenPlayer requirement they bind, pinned on the real corpus carrier
// Territorial Hellkite (which is NOT in any repo deck -- it is loaded from
// the corpus registry by name, so "corpus-pinned" means the real compiled
// Forge script, not a hand-written fixture).
//
// The card's begin-combat trigger chooses an opponent at random that the
// Hellkite did not attack during the last combat
// (Choices$ Player.Opponent+!IsRemembered | Random$ True). A successful
// choice runs ChooseSubAbility$ DBPump, which registers an Effect-delivered
// `Mode$ MustAttack | ValidCreature$ Card.EffectSource | MustAttack$
// ChosenPlayer` for the combat; a choice with no candidate runs
// CantChooseSubAbility$ DBTap, tapping the dragon. The requirement is read at
// the declare-attackers step through the shared attackRequirements collector
// (rules/combat.go), which decides which offered (attacker, defender) pairs
// satisfy the most requirements and marks the creature required.
//
// Every fixture mutation goes through e.emit, so every game replays
// byte-identically.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// hellkiteEngine builds a three-seat game (seat 0 is the active player
// controlling the real corpus Territorial Hellkite, seats 1 and 2 are its
// opponents) parked at seat 0's Main1, ready for the test to cross into
// BeginCombat. Mountain decks fill every seat so the fixture's only
// non-land permanent is the Hellkite.
func hellkiteEngine(t *testing.T, cfgOut *Config) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	m, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("corpus fixture: Mountain missing")
	}
	fill := func(n int) []*cards.Card {
		out := make([]*cards.Card, n)
		for i := range out {
			out[i] = m
		}
		return out
	}
	hk := lookup(t, reg, "Territorial Hellkite")
	if hk == nil {
		t.Fatal("corpus fixture: Territorial Hellkite missing")
	}
	cfg := seatZeroStart(Config{Seed: 716, Names: []string{"a", "b", "c"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append([]*cards.Card{hk}, fill(40)...)[:40],
			fill(40),
			fill(40),
		}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	id := moveByName(t, e, 0, "Territorial Hellkite", state.ZBattlefield)
	*cfgOut = cfg
	return e, id
}

// crossIntoBeginCombat emits the StepChange for seat 0's BeginCombat and
// drains the trigger's resolution, so the ChoosePlayer rider has run by the
// time it returns. It uses the real Phase trigger machinery (the card's
// `T:Mode$ Phase | Phase$ BeginCombat` line), not a hand-resolved SVar.
func crossIntoBeginCombat(t *testing.T, e *Engine) {
	t.Helper()
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	// Place the queued Phase trigger on the stack (the direct StepChange the
	// phase-fixture tests use queues rather than places), answer any trigger
	// order, then resolve it: the ChoosePlayer rider runs as the trigger
	// resolves.
	e.putTriggersOnStack()
	answerTriggerOrders(t, e)
	passUntilStackEmpty(t, e, 20)
}

// TestTerritorialHellkiteChoosesAndBindsAttackDefender is the brief's leaf:
// with two un-remembered opponents the random pick is a real choice (one of
// seats 1/2), the chosen player is recorded on the source, and the
// declare-attackers offer list contains ONLY that defender for the dragon,
// with the dragon marked Required.
func TestTerritorialHellkiteChoosesAndBindsAttackDefender(t *testing.T) {
	t.Parallel()
	var cfg Config
	e, hk := hellkiteEngine(t, &cfg)

	// Precondition: the dragon is on the battlefield under seat 0's control
	// and the board starts with NO chosen player and NO remembered opponents,
	// so the Choices$ filter has both opponents eligible.
	o := e.G.Obj(hk)
	if o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: Hellkite not on seat 0's battlefield: %+v", o)
	}
	if len(o.Chosen) != 0 || len(o.Remembered) != 0 {
		t.Fatalf("precondition: dragon already has chosen/remembered state: chosen=%v remembered=%v", o.Chosen, o.Remembered)
	}

	crossIntoBeginCombat(t, e)

	// The rider registered the requirement and the random pick recorded
	// exactly one opponent as chosen.
	o = e.G.Obj(hk)
	if len(o.Chosen) != 1 || !o.Chosen[0].IsPlayer || (o.Chosen[0].Player != 1 && o.Chosen[0].Player != 2) {
		t.Fatalf("after the begin-combat trigger, chosen = %+v, want exactly one opponent seat", o.Chosen)
	}
	chosen := o.Chosen[0].Player
	if !e.attackRequiresDefender(hk, chosen) {
		t.Fatalf("no MustAttack requirement binds the dragon to chosen player %d", chosen)
	}

	// Determinism: an identical engine driven identically picks the SAME seat
	// (the choice is a seeded rng draw, never ambient state).
	var cfg2 Config
	e2, hk2 := hellkiteEngine(t, &cfg2)
	crossIntoBeginCombat(t, e2)
	if got2 := e2.G.Obj(hk2).Chosen; len(got2) != 1 || got2[0].Player != chosen {
		t.Fatalf("second identical run chose %+v, want the same seat %d (non-deterministic pick)", got2, chosen)
	}

	// Drive the real step machinery into declare-attackers (a direct step
	// write would not be replayed) and inspect the offer list.
	passToKind(t, e, decision.KAttackers)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected a KAttackers decision, got %+v", d)
	}
	sawDragon := false
	for _, opt := range d.Options {
		if opt.Obj != hk {
			continue
		}
		sawDragon = true
		if opt.Player != chosen {
			t.Fatalf("dragon offered against seat %d; the requirement binds it to seat %d", opt.Player, chosen)
		}
		if !opt.Required {
			t.Fatalf("dragon option against the chosen player is not marked Required")
		}
	}
	if !sawDragon {
		t.Fatalf("dragon has no offered attack pair at all; options=%+v", d.Options)
	}
	// The declaration must be accepted against the chosen player.
	submitAttackersOnly(t, e, hk)
	replayCheck(t, e, cfg)
}

// TestTerritorialHellkiteNoCandidateTaps is the "If you can't choose an
// opponent this way, tap CARDNAME" arm: with every opponent already in the
// dragon's remembered set (the set the Choices$ !IsRemembered filter
// excludes), CantChooseSubAbility$ DBTap runs, the dragon taps, and no
// requirement is registered.
func TestTerritorialHellkiteNoCandidateTaps(t *testing.T) {
	t.Parallel()
	var cfg Config
	e, hk := hellkiteEngine(t, &cfg)

	// Precondition: the dragon is on the battlefield and both opponents are
	// already remembered, so the Choices$ filter admits nobody.
	o := e.G.Obj(hk)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Hellkite not on the battlefield")
	}
	e.emit(events.Event{Kind: events.Choose, Obj: hk, Counter: "remembered",
		IDs: []state.ObjID{state.PlayerRef(1), state.PlayerRef(2)}})
	if got := len(e.G.Obj(hk).Remembered); got != 2 {
		t.Fatalf("precondition: remembered set = %d players, want 2", got)
	}
	if e.G.Obj(hk).Tapped {
		t.Fatal("precondition: dragon already tapped")
	}

	crossIntoBeginCombat(t, e)

	if !e.G.Obj(hk).Tapped {
		t.Fatal("with no eligible opponent the dragon should have tapped (CantChooseSubAbility$)")
	}
	if len(e.G.Obj(hk).Chosen) != 0 {
		t.Fatalf("no-candidate arm recorded a chosen player: %+v", e.G.Obj(hk).Chosen)
	}
	// No per-player requirement may survive the failed choice.
	if e.attackRequirements(hk).Any() {
		t.Fatal("a failed choice still registered a MustAttack requirement")
	}
	replayCheck(t, e, cfg)
}

// requiredForDefender reports whether id's requirement set names defender as
// the DEFENDER IT IS MOST BOUND TO -- it satisfies strictly more named
// requirements than any other named defender (a test-side read of the
// engine's own attackRequirements collector).
func (e *Engine) requiredForDefender(id state.ObjID, defender state.PlayerID) bool {
	rs := e.attackRequirements(id)
	return rs.Named[defender] > 0 && rs.SatisfiedBy(defender) == rs.MaxNamed()
}

// attackRequiresDefender reports whether id is required to attack AND the
// single maximal named defender is defender (the strict one-defender case the
// Hellkite and the single-named-requirement tests want).
func (e *Engine) attackRequiresDefender(id state.ObjID, defender state.PlayerID) bool {
	rs := e.attackRequirements(id)
	if !rs.Any() {
		return false
	}
	if rs.SatisfiedBy(defender) != rs.MaxNamed() || rs.SatisfiedBy(defender) == 0 {
		return false
	}
	// No OTHER defender may be equally maximal (the strict single-destination
	// case); a tie is legal but not what this helper asserts.
	n := 0
	for _, c := range rs.Named {
		if c == rs.MaxNamed() {
			n++
		}
	}
	return n == 1
}

// TestTerritorialHellkiteNamedRequirementVersusGoad is the t2 review's
// interaction case: a named MustAttack$ ChosenPlayer duty that CONFLICTS with
// a goad by the chosen player must not cancel the attack out. The goad
// restriction removes the (dragon, chosen-goader) pair, and the dragon's
// remaining legal pair (the other opponent) still satisfies the goad
// requirement, so the dragon is required and attacks that other opponent.
// The pre-fix single-defender filter removed the non-chosen pair as well, the
// offer list emptied, the dragon was reported as not required, and no legal
// declaration existed that included it.
func TestTerritorialHellkiteNamedRequirementVersusGoad(t *testing.T) {
	t.Parallel()
	var cfg Config
	e, hk := hellkiteEngine(t, &cfg)
	crossIntoBeginCombat(t, e)

	o := e.G.Obj(hk)
	if o == nil || len(o.Chosen) != 1 || !o.Chosen[0].IsPlayer {
		t.Fatalf("precondition: expected exactly one chosen opponent, got %+v", o)
	}
	chosen := o.Chosen[0].Player
	if chosen != 1 && chosen != 2 {
		t.Fatalf("precondition: chosen player %d is not an opponent", chosen)
	}
	other := state.PlayerID(1)
	if chosen == 1 {
		other = 2
	}
	// The chosen player goads the dragon. The event's Player is the goader;
	// IDs[0] is the goad's source and Amount carries the goaded object's
	// controller+1 (the effGoad wire shape).
	e.emit(events.Event{Kind: events.Goad, Obj: hk, Player: chosen, Text: "Permanent",
		IDs: []state.ObjID{hk}, Amount: int32(o.Controller) + 1})
	if !e.goadedBy(e.G.Obj(hk), chosen) {
		t.Fatalf("precondition: goad from player %d did not register", chosen)
	}

	passToKind(t, e, decision.KAttackers)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected a KAttackers decision, got %+v", d)
	}
	// The dragon has exactly one offered pair, against the non-goader, and it
	// is Required. It must NOT have disappeared (the pre-fix defect) and must
	// NOT be offered against the goader.
	sawOther, sawGoader := false, false
	for _, opt := range d.Options {
		if opt.Obj != hk {
			continue
		}
		switch opt.Player {
		case other:
			sawOther = true
			if !opt.Required {
				t.Fatalf("dragon's only legal pair (player %d) is not marked Required", other)
			}
		case chosen:
			sawGoader = true
		}
	}
	if !sawOther {
		t.Fatalf("dragon has no legal attack pair despite an available non-goader; options=%+v", d.Options)
	}
	if sawGoader {
		t.Fatalf("goaded dragon was offered its goader's pair (player %d); goad forbids it", chosen)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player}); err == nil {
		t.Fatal("required dragon was allowed to skip its attack entirely")
	}
	// Submit the dragon's one legal pair and confirm it attacked the non-goader.
	idx := -1
	for _, opt := range d.Options {
		if opt.Obj == hk && opt.Player == other {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no dragon option against player %d: %+v", other, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("legal declaration rejected: %v", err)
	}
	if !e.G.Obj(hk).IsAttacking || e.G.Obj(hk).Attacking != other {
		t.Fatalf("dragon did not attack player %d (attacking=%v player=%d)", other, e.G.Obj(hk).IsAttacking, e.G.Obj(hk).Attacking)
	}
	replayCheck(t, e, cfg)
}

// TestMustAttackTwoNamedRequirementsKeepBothDefenders is the review's second
// case: two named MustAttack requirements naming DIFFERENT opponents must
// both stay offered and Required (each satisfies one requirement, neither
// dominates), instead of the first-registration-wins collapse. The two
// registrations carry RememberedPlayer bindings, resolved against each
// effect's own RememberedPlayers capture.
func TestMustAttackTwoNamedRequirementsKeepBothDefenders(t *testing.T) {
	t.Parallel()
	// A bare 3-seat board parked directly at seat 0's declare-attackers step:
	// no real trigger runs, so the only requirements are the two explicit ones
	// below (the Hellkite fixture would resolve its own begin-combat choice and
	// add a third named requirement, confounding the assertion).
	e := New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{
		mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40),
	}}))
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	hk := onBoardReady(t, e, 0, "Name:Dragon\nTypes:Creature\nPT:5/5\nOracle:x\n")
	if o := e.G.Obj(hk); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: dragon not on the battlefield")
	}
	// Two Effect-delivered named requirements, one per opponent, each on the
	// SAME creature (ValidCreature$ Card.Self) but naming a different player.
	for _, def := range []state.PlayerID{1, 2} {
		e.AddContinuous(ContinuousEffect{
			Source: hk, Controller: 0, UntilEOT: true,
			Restriction:       "MustAttack",
			RestrictParams:    map[string]string{"Mode": "MustAttack", "ValidCreature": "Card.Self", "MustAttack": "RememberedPlayer"},
			RememberedPlayers: []state.PlayerID{def},
		})
	}
	rs := e.attackRequirements(hk)
	if !rs.Any() {
		t.Fatalf("precondition: the two named requirements did not bind the dragon: %+v", rs)
	}
	if rs.SatisfiedBy(1) != 1 || rs.SatisfiedBy(2) != 1 || rs.MaxNamed() != 1 {
		t.Fatalf("precondition: requirements should name 1 and 2 once each, got %+v", rs.Named)
	}
	if !e.requiredForDefender(hk, 1) || !e.requiredForDefender(hk, 2) {
		t.Fatal("precondition: both named defenders must be maximal")
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected a KAttackers decision, got %+v", d)
	}
	seen := map[state.PlayerID]bool{}
	for _, opt := range d.Options {
		if opt.Obj != hk {
			continue
		}
		if !opt.Required {
			t.Fatalf("named-required pair against %d is not Required", opt.Player)
		}
		seen[opt.Player] = true
	}
	if !seen[1] || !seen[2] {
		t.Fatalf("both named defenders must stay offered; got %+v", d.Options)
	}
	// Either maximal declaration is legal: pick the first dragon option.
	idx := -1
	for _, opt := range d.Options {
		if opt.Obj == hk {
			idx = opt.Index
			break
		}
	}
	if idx < 0 {
		t.Fatalf("no dragon option: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("a maximal named declaration was rejected: %v", err)
	}
}

// TestMustAttackFaceAndEffectWhitelistsAgree guards the one-whitelist-home
// contract between the face S: line route and the Effect-delivered route.
// The face whitelist is the Effect registration whitelist EXTENDED by exactly
// the condition-gate keys rules' continuousGateHolds evaluates
// (agent task: face restriction gates): the face route can evaluate those
// gates, the Effect registration path cannot, so a gate-bearing line is
// face-readable but stays OFF the Effect whitelist -- registering it blanket
// would over-require. The test pins three directions: the rules wrapper
// delegates to the effects list (no re-duplicated copy), every
// effect-readable line is face-readable (the superset direction), and the
// gate keys diverge in exactly that direction.
func TestMustAttackFaceAndEffectWhitelistsAgree(t *testing.T) {
	t.Parallel()
	cases := []map[string]string{
		{"Mode": "MustAttack", "ValidCreature": "Card.Self", "MustAttack": "ChosenPlayer", "Description": "x"},
		{"Mode": "MustAttack", "ValidCreature": "Card.Self", "MustAttack": "ChosenPlayer", "Secondary": "True"},
		{"Mode": "MustAttack", "IsPresent": "Card.Self", "ValidCreature": "Card.Self"},
		{"Mode": "MustAttack", "ValidCreature": "Card.Self", "Condition": "Threshold"},
		{"Mode": "MustAttack", "ValidCreature": "Card.Self", "CheckSVar": "X", "SVarCompare": "GE1"},
	}
	for i, p := range cases {
		if got, want := MustAttackParamsReadableForRules(p), effects.MustAttackParamsReadableForRules(p); got != want {
			t.Fatalf("case %d: rules wrapper = %v, effects face whitelist = %v for %v", i, got, want, p)
		}
		if eff, face := effects.MustAttackParamsReadable(p), MustAttackParamsReadableForRules(p); eff && !face {
			t.Fatalf("case %d: effect-readable param set is not face-readable: %v", i, p)
		}
	}
	gated := map[string]string{"Mode": "MustAttack", "ValidCreature": "Card.Self", "IsPresent": "Card.Self"}
	if !MustAttackParamsReadableForRules(gated) {
		t.Fatal("gate-key face line unreadable: the face route lost its gate extension")
	}
	if effects.MustAttackParamsReadable(gated) {
		t.Fatal("gate-key line admitted by the Effect registration whitelist: it would over-require blanket")
	}
}

// TestMustAttackRememberedPlayerBindsThroughRealEffectRegistration is the
// review's end-to-end case: the Effect-delivered MustAttack$ RememberedPlayer
// requirement must bind the player captured by a REAL `DB$ Effect |
// RememberObjects$ Remembered | StaticAbilities$ MustAttack` registration,
// not only a hand-built AddContinuous with RememberedPlayers prefilled. The
// fixture resolves the actual compiled `DBEff` SVar of the real corpus
// carrier Furygale Flocking, whose DBToken chain is the per-opponent
// RepeatEach loop body: by the time DBEff runs, the loop's current player is
// bound into Ctx.Remembered beside the tokens the chain created. The
// registered requirement then has to reach attackRequirements with that
// player named -- effectRemembered records objects only, so the player half
// must come from effectRememberedPlayers reading the same
// `RememberObjects$ Remembered` spelling the card writes.
func TestMustAttackRememberedPlayerBindsThroughRealEffectRegistration(t *testing.T) {
	t.Parallel()
	deck := func() []*cards.Card {
		out := make([]*cards.Card, 40)
		for i := range out {
			out[i] = card(t, "Name:Mountain\nTypes:Basic Land\nOracle:x\n")
		}
		return out
	}
	e := New(Config{Seed: 5, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{deck(), deck(), deck()}})

	// A real creature to carry the requirement, and a real token object the
	// Effect's object half remembers (the card creates the tokens itself; the
	// helper exercises the registration in isolation, so both are on board).
	dragon := onBoardReady(t, e, 0, "Name:Elemental\nTypes:Creature\nPT:3/3\nOracle:x\n")
	token := onBoardReady(t, e, 0, "Name:Elemental Token\nTypes:Creature\nPT:3/3\nOracle:x\n")
	if o := e.G.Obj(dragon); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: requirement carrier not on the battlefield")
	}
	if o := e.G.Obj(token); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: remembered token not on the battlefield")
	}

	flock := choiceCorpusCard(t, "Furygale Flocking")
	if flock == nil {
		t.Fatal("corpus fixture: Furygale Flocking missing")
	}
	sa := cards.ResolveSVar(flock.Faces[0].SVars, "DBEff")
	if sa == nil || sa.API != "Effect" {
		t.Fatalf("Furygale Flocking DBEff not compiled as an Effect SA: %+v", sa)
	}
	if got := sa.Params["RememberObjects"]; got != "Remembered" {
		t.Fatalf("precondition: DBEff RememberObjects$ = %q, want Remembered", got)
	}
	must := strings.TrimSpace(flock.Faces[0].SVars[strings.TrimSpace(sa.Params["StaticAbilities"])])
	if !strings.Contains(must, "MustAttack$ RememberedPlayer") {
		t.Fatalf("precondition: Furygale Flocking's MustAttack SVar not the RememberedPlayer line: %q", must)
	}

	ctx := &effects.Ctx{Source: dragon, Controller: 0, SVars: flock.Faces[0].SVars,
		Remembered: []state.Target{{Obj: token}, {Player: 1, IsPlayer: true}}}
	effects.Resolve(e, ctx, sa)

	// The registration reached the continuous-effect registry (not a Note).
	regs := 0
	for _, ce := range e.active() {
		if ce.Restriction == "MustAttack" {
			regs++
			if len(ce.RememberedPlayers) != 1 || ce.RememberedPlayers[0] != 1 {
				t.Fatalf("registered MustAttack RememberedPlayers = %v, want [1]", ce.RememberedPlayers)
			}
			if len(ce.Remembered) != 1 || ce.Remembered[0] != token {
				t.Fatalf("registered MustAttack Remembered = %v, want [%d]", ce.Remembered, token)
			}
		}
	}
	if regs != 1 {
		t.Fatalf("expected exactly one registered MustAttack effect, got %d", regs)
	}

	// The requirement binds the remembered player on the remembered creature
	// (ValidCreature$ Card.IsRemembered selects the Effect's own remembered
	// token, not an arbitrary board object).
	rs := e.attackRequirements(token)
	if !rs.Any() {
		t.Fatalf("registered requirement did not bind the remembered token: %+v", rs)
	}
	if rs.Named[1] != 1 {
		t.Fatalf("requirement names = %+v, want player 1 required once", rs.Named)
	}
	if !e.requiredForDefender(token, 1) {
		t.Fatal("remembered token is not required to attack the remembered player 1")
	}
	if e.requiredForDefender(token, 2) {
		t.Fatal("remembered token must not be required against player 2")
	}
	if rsDragon := e.attackRequirements(dragon); rsDragon.Any() {
		t.Fatalf("a creature outside the Effect's remembered set must not be required: %+v", rsDragon)
	}
}

// TestMustAttackNamedDefenderBlockedLeavesAlternateOptional is the sol1
// review's case: a named requirement whose ONE defender no pair can reach
// makes the creature not required at all, instead of forcing an attack on a
// player no duty names.
//
// Three seats; the dragon carries a single `MustAttack$ RememberedPlayer`
// duty naming player 1, and a CantAttack restriction scoped to player 1
// removes exactly that pair. Attacking player 2 obeys zero requirements and
// so does not attacking, so CR 508.1d permits both: player 2's pair stays
// OFFERED (it is still a legal attack) but must not be marked Required, and
// the empty declaration must be accepted. Before the fix mustAttackRequired
// asked only whether SOME pair survived, so it marked the player-2 pair
// Required and the empty declaration was rejected.
func TestMustAttackNamedDefenderBlockedLeavesAlternateOptional(t *testing.T) {
	t.Parallel()
	e := New(seatZeroStart(Config{Seed: 3, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{
		mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40),
	}}))
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	hk := onBoardReady(t, e, 0, "Name:Dragon\nTypes:Creature\nPT:5/5\nOracle:x\n")
	if o := e.G.Obj(hk); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: dragon not on the battlefield")
	}
	// One named duty: attack player 1.
	e.AddContinuous(ContinuousEffect{
		Source: hk, Controller: 0, UntilEOT: true,
		Restriction:       "MustAttack",
		RestrictParams:    map[string]string{"Mode": "MustAttack", "ValidCreature": "Card.Self", "MustAttack": "RememberedPlayer"},
		RememberedPlayers: []state.PlayerID{1},
	})
	// ... and a restriction that blocks exactly that pair (Call for Aid's
	// shape: Target$ Player.IsRemembered against the effect's own capture).
	e.AddContinuous(ContinuousEffect{
		Source: hk, Controller: 0, UntilEOT: true,
		Restriction:       "CantAttack",
		RestrictParams:    map[string]string{"Mode": "CantAttack", "ValidCard": "Card.Self", "Target": "Player.IsRemembered"},
		RememberedPlayers: []state.PlayerID{1},
	})
	if !e.attackBlocked(hk, 1, 0) {
		t.Fatal("precondition: the named defender's pair is not blocked")
	}
	if e.attackBlocked(hk, 2, 0) {
		t.Fatal("precondition: the alternate defender's pair must stay legal")
	}
	if rs := e.attackRequirements(hk); rs.SatisfiedBy(1) != 1 || rs.SatisfiedBy(2) != 0 || rs.Broad || rs.Goad {
		t.Fatalf("precondition: expected exactly one named duty on player 1, got %+v", rs)
	}
	if e.mustAttackRequired(hk) {
		t.Fatal("dragon is required although no offered pair discharges its only duty")
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected a KAttackers decision, got %+v", d)
	}
	sawAlternate := false
	for _, opt := range d.Options {
		if opt.Obj != hk {
			continue
		}
		if opt.Player == 1 {
			t.Fatalf("the blocked named pair (player 1) was still offered: %+v", opt)
		}
		if opt.Player == 2 {
			sawAlternate = true
			if opt.Required {
				t.Fatal("the alternate pair (player 2) is marked Required though it discharges no duty")
			}
		}
	}
	if !sawAlternate {
		t.Fatalf("the legal alternate pair must stay offered; options=%+v", d.Options)
	}
	// Both maximal declarations are legal: the empty one, and the alternate.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player}); err != nil {
		t.Fatalf("the empty declaration obeys as many requirements as any other and must be legal: %v", err)
	}
	if e.G.Obj(hk).IsAttacking {
		t.Fatal("the dragon attacked although the declaration was empty")
	}
}

// TestMustAttackBlockedNamedDutyStillRequiredWhenBroad is the other half of
// the same rule: a BROAD duty (an unconditional "attacks each combat if
// able") is discharged by ANY defender, so a named duty whose defender is
// blocked does not release the creature -- it must still attack the
// alternate. This is what keeps the fix from turning every blocked named
// duty into a blanket exemption.
func TestMustAttackBlockedNamedDutyStillRequiredWhenBroad(t *testing.T) {
	t.Parallel()
	e := New(seatZeroStart(Config{Seed: 3, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{
		mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40),
	}}))
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	hk := onBoardReady(t, e, 0, "Name:Dragon\nTypes:Creature\nPT:5/5\nOracle:x\n")
	for _, ce := range []ContinuousEffect{
		{Source: hk, Controller: 0, UntilEOT: true, Restriction: "MustAttack",
			RestrictParams:    map[string]string{"Mode": "MustAttack", "ValidCreature": "Card.Self", "MustAttack": "RememberedPlayer"},
			RememberedPlayers: []state.PlayerID{1}},
		{Source: hk, Controller: 0, UntilEOT: true, Restriction: "CantAttack",
			RestrictParams:    map[string]string{"Mode": "CantAttack", "ValidCard": "Card.Self", "Target": "Player.IsRemembered"},
			RememberedPlayers: []state.PlayerID{1}},
		// The broad duty: no MustAttack$ player reference at all.
		{Source: hk, Controller: 0, UntilEOT: true, Restriction: "MustAttack",
			RestrictParams: map[string]string{"Mode": "MustAttack", "ValidCreature": "Card.Self"}},
	} {
		e.AddContinuous(ce)
	}
	if rs := e.attackRequirements(hk); !rs.Broad || rs.SatisfiedBy(1) != 1 {
		t.Fatalf("precondition: expected a broad duty beside the named one, got %+v", rs)
	}
	if !e.mustAttackRequired(hk) {
		t.Fatal("a broad duty with an available defender still requires the attack")
	}
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected a KAttackers decision, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Obj != hk {
			continue
		}
		if opt.Player != 2 {
			t.Fatalf("only the unblocked defender may be offered, got %+v", opt)
		}
		if !opt.Required {
			t.Fatal("the pair that discharges the broad duty is not marked Required")
		}
		idx = opt.Index
	}
	if idx < 0 {
		t.Fatalf("no dragon option at all: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player}); err == nil {
		t.Fatal("a creature under a satisfiable broad duty was allowed to skip its attack")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("the one legal declaration was rejected: %v", err)
	}
	if o := e.G.Obj(hk); !o.IsAttacking || o.Attacking != 2 {
		t.Fatalf("dragon did not attack player 2 (attacking=%v at %d)", o.IsAttacking, o.Attacking)
	}
}
