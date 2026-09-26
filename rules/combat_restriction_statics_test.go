package rules

// combatrestriction1: the three combat/sacrifice restriction statics.
//
// stat:CantAttack is enforced per (attacker, defender) pair (attackBlocked,
// rules/layers.go, consulted by askAttackers' option filter and
// validateAttackers), stat:CantSacrifice at every sacrifice candidate choke
// point (Engine.SacrificeBlocked, the effects.Host method: effSacrifice's
// eligible pool and object-target paths, effSacrificeAll, the
// cast/activation/mana/ward/unless Sac-cost walks), and stat:MustAttack by
// mustAttackRequired's board-wide activeStatics walk (which is what lets an
// AURA-carried requirement — Fealty to the Realm's
// `ValidCreature$ Creature.EnchantedBy` — reach the enchanted creature; the
// old solver walked only the considered creature's own face). CR 508.1d's
// "if able" is honoured by mustAttackRequired's attackDutyDischargeable gate: a
// required creature whose every pair a CantAttack blocks is NOT required,
// so the KAttackers decision never wedges.
//
// Every leaf pins a REAL corpus card through the ordinary offer/declare
// paths, the way rules/equip_cost_modifiers_test.go and
// rules/static_gaincontrol_test.go do; every fixture mutation goes through
// e.emit, so every test ends replay-verified.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// restrictionGame builds an n-seat corpus game (one seat per entry), parks
// each seat's hand cards where findAndMoveToHand can reach them, puts each
// seat's board cards on its battlefield BEFORE the turn boundary (so they
// are not summoning sick on turn 2), and parks the table at seat 0's turn-2
// Main1 — the same shape staticGainControlGame (static_gaincontrol_test.go)
// builds for two seats, generalised so the multi-defender tests can use
// three. Fixture bears are the caller's own card(t, staticBearFixture)
// pointer (matched by pointer, since card() parses fresh every call).
func restrictionGame(t *testing.T, seed uint64, hands, board [][]*cards.Card) (*Engine, Config) {
	t.Helper()
	if len(hands) != len(board) {
		t.Fatalf("hands/board seat mismatch")
	}
	var names []string
	var decks [][]*cards.Card
	for i := range hands {
		names = append(names, string(rune('a'+i)))
		deck := append([]*cards.Card{}, hands[i]...)
		deck = append(deck, board[i]...)
		deck = append(deck, mountainDeck(t, 40-len(deck))...)
		decks = append(decks, deck)
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: names, Decks: decks,
		Tokens: testutil.CorpusRegistry(t).Tokens})
	e := New(cfg)
	e.Advance()
	for seat := range hands {
		moved := 0
		want := make(map[*cards.Card]bool, len(board[seat]))
		for _, c := range board[seat] {
			want[c] = true
		}
		for _, id := range append(append([]state.ObjID(nil), e.G.Zone(state.ZHand, state.PlayerID(seat))...),
			e.G.Zone(state.ZLibrary, state.PlayerID(seat))...) {
			o := e.G.Obj(id)
			if o == nil || !want[o.Card] {
				continue
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZBattlefield})
			moved++
			if moved == len(board[seat]) {
				break
			}
		}
		if moved != len(board[seat]) {
			t.Fatalf("seat %d: only %d of %d board cards dealt", seat, moved, len(board[seat]))
		}
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	return e, cfg
}

// bearOnBoard finds seat p's fixture bear (matched by card pointer) on the
// battlefield.
func bearOnBoard(t *testing.T, e *Engine, p state.PlayerID, bear *cards.Card) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Card == bear {
			return id
		}
	}
	t.Fatalf("seat %d has no fixture bear on the battlefield", p)
	return 0
}

// attackerOptionsFor filters a pending KAttackers decision's options down to
// the ones naming creature id.
func attackerOptionsFor(e *Engine, id state.ObjID) []decision.Option {
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		return nil
	}
	var out []decision.Option
	for _, o := range d.Options {
		if o.Obj == id {
			out = append(out, o)
		}
	}
	return out
}

// drainThrough drives the engine to turn/active/step, answering only the
// decisions a quiet table poses on the way: priority passes and empty
// blocker declarations (no attackers worth blocking). Anything else is a
// fixture surprise and fails.
func drainThrough(t *testing.T, e *Engine, turn int32, active state.PlayerID, step state.Step) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == step {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended early at turn %d seat %d step %s", e.G.Turn, e.G.Active, e.G.Step)
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch d.Kind {
		case decision.KPriority:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
				t.Fatalf("pass priority: %v", err)
			}
		case decision.KBlockers, decision.KAttackers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
				t.Fatalf("declared no attackers/blockers: %v", err)
			}
		case decision.KChoose:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
				t.Fatalf("answered quiet choose: %v", err)
			}
		default:
			t.Fatalf("unexpected %v decision while draining to turn %d seat %d: %+v", d.Kind, turn, active, d)
		}
	}
	t.Fatalf("drain did not reach turn %d seat %d step %s", turn, active, step)
}

// TestFealtyToTheRealmEnchantedMustAttackEachCombat pins the aura-carried
// MustAttack requirement: Fealty to the Realm's
// `S:Mode$ MustAttack | ValidCreature$ Creature.EnchantedBy` is printed on
// the AURA's face, so the old face-only solver never saw it; the board-wide
// activeStatics walk does. With the monarch control static active (the same
// grant TestFealtyToTheRealmMonarchControlsEnchantedCreature pins), the
// enchanted bear is required — Option.Required on the wire, and
// validateAttackDeclaration rejects a declaration omitting it.
func TestFealtyToTheRealmEnchantedMustAttackEachCombat(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	ft, ok := reg.Lookup("Fealty to the Realm")
	if !ok {
		t.Fatal("corpus missing Fealty to the Realm")
	}
	e, cfg, bear := staticGainControlGame(t, 6110, []*cards.Card{ft}, 1)
	castControlAura(t, e, "Fealty to the Realm", bear)
	if got := e.G.Obj(bear).Controller; got != 0 {
		t.Fatalf("bearer controller = %d, want 0 (the monarch)", got)
	}
	// The fixture jumps turn 1→2 with seat 0 already active, so the bear's
	// summoning-sick mark (set when it entered seat 1's side on turn 1) was
	// never cleared by its controller's own turn boundary; the steal moved it
	// to seat 0. One replay-visible TurnChange on the parked turn clears the
	// mark for seat 0's battlefield, exactly as the boundary it skipped would
	// have.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn})
	if !e.mustAttackRequired(bear) {
		t.Fatal("enchanted bear not required: the aura-carried MustAttack static never reached the requirement solver")
	}
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepDeclareAttackers)
	opts := attackerOptionsFor(e, bear)
	if len(opts) != 1 || !opts[0].Required {
		t.Fatalf("bear attack options = %+v, want exactly one Required pair", opts)
	}
	// A declaration omitting the required bear is rejected (CR 508.1c/d).
	d := e.Pending()
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err == nil {
		t.Fatal("declaration omitting the required enchanted bear was accepted")
	}
	submitAttackers(t, e, bear)
	replayCheck(t, e, cfg)
}

// TestFealtyToTheRealmCantAttackScoping pins the pair-scoping reader. Fealty
// to the Realm's own CantAttack half is deliberately NOT the carrier here —
// it is vacuous in this engine (its control static keeps the bearer under
// the monarch's control, and askAttackers never offers a pair against the
// active player, so `Target$ You` has nothing to bite) — so the scoping is
// pinned on the other real corpus carrier of the same whitelisted shape, the
// Vow cycle: Vow of Lightning's
// `S:Mode$ CantAttack | ValidCard$ Creature.EnchantedBy | Target$ You,Planeswalker.YouCtrl`.
// In a three-seat game the vow's bearer (seat 1's bear, the vow's controller
// seat 0) may attack seat 2 but is never offered (and never declares) a pair
// against seat 0 — the "You" half of the comma list, with the unreadable
// walker half matching nobody.
func TestFealtyToTheRealmCantAttackScoping(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	vow, ok := reg.Lookup("Vow of Lightning")
	if !ok {
		t.Fatal("corpus missing Vow of Lightning")
	}
	bear := card(t, staticBearFixture)
	e, cfg := restrictionGame(t, 6111,
		[][]*cards.Card{{vow}, nil, nil},
		[][]*cards.Card{nil, {bear}, {bear}})
	bear1 := bearOnBoard(t, e, 1, bear)
	// Attach the vow (2R) to seat 1's bear.
	vowID := findAndMoveToHand(t, e, 0, "Vow of Lightning")
	addMana(t, e, 0, "CCRR")
	castFromPriority(t, e, vowID)
	answerKTarget(t, e, bear1)
	passUntilStackEmpty(t, e, 60)
	if e.G.Obj(vowID).AttachedTo != bear1 {
		t.Fatalf("vow AttachedTo = %d, want the bear %d", e.G.Obj(vowID).AttachedTo, bear1)
	}
	if !e.attackBlocked(bear1, 0, 0) || e.attackBlocked(bear1, 2, 0) {
		t.Fatalf("pair scoping wrong: blocked(1→0)=%v blocked(1→2)=%v", e.attackBlocked(bear1, 0, 0), e.attackBlocked(bear1, 2, 0))
	}
	// Seat 1's declare attackers: the bear is offered seat 2 and never seat 0.
	driveToStep(t, e, 3, 1, state.StepDeclareAttackers)
	opts := attackerOptionsFor(e, bear1)
	if len(opts) != 1 || opts[0].Player != 2 {
		t.Fatalf("vowed bear's attack options = %+v, want exactly the seat-2 pair", opts)
	}
	submitAttackers(t, e, bear1)
	replayCheck(t, e, cfg)
}

// TestCallForAidStolenCreaturesCantSacrifice pins the CantSacrifice
// restriction end to end on the card the static exists for: Call for Aid's
// DB$ Effect (RememberObjects$ TargetedPlayer & RememberedCard,
// StaticAbilities$ CantSac,CantAttack) registers
// `Mode$ CantSacrifice | ValidCard$ Card.IsRemembered+YouCtrl` for the turn,
// and every sacrifice path over the stolen creatures — the mana-ability Sac
// cost (Ashnod's Altar) and effSacrifice's player-target eligible pool
// (Innocent Blood's "each player sacrifices a creature") — offers and takes
// nothing.
func TestCallForAidStolenCreaturesCantSacrifice(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Call for Aid", "Ashnod's Altar", "Innocent Blood"} {
		if _, ok := reg.Lookup(name); !ok {
			t.Fatalf("corpus missing %s", name)
		}
	}
	call, _ := reg.Lookup("Call for Aid")
	altar, _ := reg.Lookup("Ashnod's Altar")
	blood, _ := reg.Lookup("Innocent Blood")
	bear := card(t, staticBearFixture)
	e, cfg := restrictionGame(t, 6112,
		[][]*cards.Card{{call, altar, blood}, nil},
		[][]*cards.Card{nil, {bear, bear}})

	// Ashnod's Altar enters, then Call for Aid steals both bears.
	altarID := findAndMoveToHand(t, e, 0, "Ashnod's Altar")
	moveToBattlefield(t, e, altarID)
	callID := findAndMoveToHand(t, e, 0, "Call for Aid")
	addMana(t, e, 0, "CCCCCR")
	castFromPriority(t, e, callID)
	targetPlayer(t, e, 1)
	passUntilStackEmpty(t, e, 60)
	stolen := findOnBoard(t, e, 0, "Grizzly Bears") // the steal moved both bears to seat 0

	// The mana-ability path: the altar's Sac<1/Creature> cost has exactly one
	// would-be candidate (the stolen bear matches the spec) and it is
	// sacrifice-blocked, so the ability is not offered even from an empty pool
	// (a Sac cost needs no mana — with the restriction absent it would be).
	if !effects.MatchesSpecFrom(e.G, "Creature", stolen, 0, altarID) {
		t.Fatal("stolen bear does not match the altar's Creature spec — fixture wrong")
	}
	if _, ok := findAbilityOption(e, altarID, 0); ok {
		t.Fatal("Ashnod's Altar offered sacrificing a CantSacrifice-blocked stolen bear")
	}

	// The effSacrifice player-target pool: Innocent Blood's "each player
	// sacrifices a creature" — seat 0's pool is exactly the stolen bears, all
	// blocked; nothing is asked, nothing emitted.
	bloodID := findAndMoveToHand(t, e, 0, "Innocent Blood")
	addMana(t, e, 0, "B")
	castFromPriority(t, e, bloodID)
	passUntilStackEmpty(t, e, 60)
	kept := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Card == bear {
			kept++
		}
	}
	if kept != 2 {
		t.Fatalf("%d stolen bears remain on seat 0's battlefield, want 2 (every sacrifice path blocked)", kept)
	}
	for _, ev := range e.L.Events {
		if events.IsSacrifice(ev) {
			t.Fatalf("a Sacrifice event was emitted under the restriction: %+v", ev)
		}
	}
	replayCheck(t, e, cfg)
}

// TestStiltManAnimateCantSacrificeLastsThroughItsNextTurn pins the complete
// Animate staticAbilities$ path on the real Stilt-Man script. Its trigger
// gains control of an opponent's noncreature artifact and the nested Animate
// grants Card.Self CantSacrifice. The artifact is not a legal sacrifice
// candidate while the grant is live; after Stilt-Man's controller's next turn
// ends, control and the restriction both expire and it can be sacrificed.
func TestStiltManAnimateCantSacrificeLastsThroughItsNextTurn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	stilt, ok := reg.Lookup("Stilt-Man, Towering Terror")
	if !ok {
		t.Fatal("corpus missing Stilt-Man, Towering Terror")
	}
	star := card(t, "Name:Target Relic\nTypes:Artifact\nPT:0/0\nOracle:x\n")
	e, cfg := restrictionGame(t, 6114,
		[][]*cards.Card{{stilt}, nil},
		[][]*cards.Card{{stilt}, {star}})
	stiltID := findOnBoard(t, e, 0, "Stilt-Man, Towering Terror")
	starID := findOnBoard(t, e, 1, "Target Relic")
	stiltObj := e.G.Obj(stiltID)
	if stiltObj == nil || stiltObj.Zone != state.ZBattlefield {
		t.Fatal("precondition: Stilt-Man is not on the battlefield")
	}
	trigger := cards.ResolveSVar(stiltObj.Face().SVars, "TrigGainControl")
	effects.Resolve(e, &effects.Ctx{Source: stiltID, Controller: 0,
		Targets: []state.Target{{Obj: starID}}, TargetsOffered: true,
		SVars: stiltObj.Face().SVars}, trigger)
	if got := e.G.Obj(starID).Controller; got != 0 {
		t.Fatalf("Stilt-Man did not gain control of Target Relic: got %d", got)
	}
	if e.SacrificeBlocked(starID, false) == false {
		t.Fatal("precondition: Stilt-Man's Animate did not register CantSacrifice")
	}
	if e.G.Obj(starID).Zone != state.ZBattlefield {
		t.Fatal("precondition: the live Animate grant moved the target before the sacrifice attempt")
	}

	// Turn 4 is seat 0's next turn. The restriction must survive turn 3
	// cleanup and expire only after turn 4 cleanup.
	drainThrough(t, e, 4, 0, state.StepMain1)
	if !e.SacrificeBlocked(starID, false) {
		t.Fatal("CantSacrifice expired before Stilt-Man controller's next turn ended")
	}
	drainThrough(t, e, 5, 1, state.StepMain1)
	if got := e.G.Obj(starID).Controller; got != 1 {
		t.Fatalf("control did not expire after the next turn: got %d", got)
	}
	starObj := e.G.Obj(starID)
	if starObj == nil || starObj.Zone != state.ZBattlefield || starObj.Controller != 1 {
		t.Fatalf("precondition after expiry: star=%+v", starObj)
	}
	if e.SacrificeBlocked(starID, false) {
		t.Fatal("CantSacrifice restriction outlived the controller's next turn")
	}
	// With the gate open, perform the same state mutation a sacrifice path emits.
	e.emit(events.Sacrifice(starID))
	if got := e.G.Obj(starID).Zone; got != state.ZGraveyard {
		t.Fatalf("Target Relic was not sacrificed after expiry: zone=%s", got)
	}
	replayCheck(t, e, cfg)
}

// TestCallForAidCantAttackRememberedPlayer pins the CantAttack half: the
// registered restriction's Target$ Player.IsRemembered resolves against the
// effect's captured player set (RememberObjects$ TargetedPlayer — the
// targeted opponent Call for Aid stole from), so the stolen creatures cannot
// be declared attacking THAT player for the rest of the turn but can attack
// any other defender; the UntilEOT registration expires at cleanup. Three
// seats, so "another defender" exists.
func TestCallForAidCantAttackRememberedPlayer(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	call, ok := reg.Lookup("Call for Aid")
	if !ok {
		t.Fatal("corpus missing Call for Aid")
	}
	bear := card(t, staticBearFixture)
	e, cfg := restrictionGame(t, 6113,
		[][]*cards.Card{{call}, nil, nil},
		[][]*cards.Card{nil, {bear}, {bear}})
	callID := findAndMoveToHand(t, e, 0, "Call for Aid")
	addMana(t, e, 0, "CCCCCR")
	castFromPriority(t, e, callID)
	targetPlayer(t, e, 1)
	passUntilStackEmpty(t, e, 60)
	stolen := findOnBoard(t, e, 0, "Grizzly Bears") // the steal moved both bears to seat 0
	if got := e.G.Obj(stolen).Controller; got != 0 {
		t.Fatalf("stolen bear controller = %d, want 0", got)
	}
	if !e.attackBlocked(stolen, 1, 0) || e.attackBlocked(stolen, 2, 0) {
		t.Fatalf("scoping wrong: blocked(→1)=%v blocked(→2)=%v", e.attackBlocked(stolen, 1, 0), e.attackBlocked(stolen, 2, 0))
	}
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepDeclareAttackers)
	opts := attackerOptionsFor(e, stolen)
	if len(opts) != 1 || opts[0].Player != 2 {
		t.Fatalf("stolen bear's attack options = %+v, want exactly the seat-2 pair", opts)
	}
	submitAttackers(t, e, stolen)
	// Expiry: the Effect is a sorcery source (UntilEOT); seat 0's cleanup
	// drops it, and the control grant (LoseControl$ EOT) returns the bear.
	drainThrough(t, e, 3, 1, state.StepUpkeep)
	if got := e.G.Obj(stolen).Controller; got != 1 {
		t.Fatalf("after the turn the bear's controller = %d, want 1 (returned)", got)
	}
	for _, ce := range e.active() {
		if ce.Restriction == "CantAttack" || ce.Restriction == "CantSacrifice" {
			t.Fatalf("restriction %s outlived its UntilEOT turn: %+v", ce.Restriction, ce)
		}
	}
	if e.attackBlocked(stolen, 0, 0) {
		t.Fatal("CantAttack restriction still biting after cleanup")
	}
	replayCheck(t, e, cfg)
}

// TestMustAttackIfAbleCantAttackBlocksRequirement pins CR 508.1d's "if able"
// gate: Valley Dasher (its own `S:Mode$ MustAttack | ValidCreature$ Card.Self`)
// is required every combat — but after Call for Aid's CantAttack blocks
// attacking the table's ONLY defender, the requirement is not counted at all:
// no Required option, no option for the dasher, an empty declaration accepted,
// and the KAttackers decision does not wedge.
func TestMustAttackIfAbleCantAttackBlocksRequirement(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Call for Aid", "Valley Dasher"} {
		if _, ok := reg.Lookup(name); !ok {
			t.Fatalf("corpus missing %s", name)
		}
	}
	call, _ := reg.Lookup("Call for Aid")
	dasher, _ := reg.Lookup("Valley Dasher")
	bear := card(t, staticBearFixture)
	e, cfg := restrictionGame(t, 6114,
		[][]*cards.Card{{call}, nil},
		[][]*cards.Card{{dasher}, {bear}})
	dasherID := findOnBoard(t, e, 0, "Valley Dasher")
	if !e.canAttack(dasherID) {
		t.Fatal("dasher cannot attack at all — fixture wrong (summoning sick?)")
	}
	callID := findAndMoveToHand(t, e, 0, "Call for Aid")
	addMana(t, e, 0, "CCCCCR")
	castFromPriority(t, e, callID)
	targetPlayer(t, e, 1)
	passUntilStackEmpty(t, e, 60)
	if !e.attackBlocked(dasherID, 1, 0) {
		t.Fatal("the dasher's only pair is not blocked — fixture wrong")
	}
	if e.mustAttackRequired(dasherID) {
		t.Fatal("dasher still required though its every pair is CantAttack-blocked (would wedge the decision)")
	}
	// The declare-attackers step resolves SILENTLY (no decision anyone could
	// answer differently exists), exactly like a no-attacker combat, and the
	// empty declaration is what the log records.
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected priority at main1, got %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("pass priority: %v", err)
	}
	for i := 0; i < 200 && e.G.Turn == 2 && !e.G.Over; i++ {
		p := e.Pending()
		if p == nil {
			break
		}
		if p.Kind == decision.KAttackers {
			t.Fatalf("a KAttackers decision was posed with every pair blocked: %+v", p)
		}
		if p.Kind != decision.KPriority {
			t.Fatalf("unexpected decision crossing the combat steps: %+v", p)
		}
		if err := e.Submit(decision.Intent{Seq: p.Seq, Player: p.Player, Choices: []int{0}}); err != nil {
			t.Fatalf("pass priority: %v", err)
		}
	}
	marked := false
	for _, ev := range e.L.Events {
		if ev.Kind != events.DeclareAttackers {
			continue
		}
		marked = true
		for _, id := range ev.IDs {
			if id == dasherID {
				t.Fatal("the blocked dasher was declared attacking")
			}
		}
	}
	if !marked {
		t.Fatal("no empty DeclareAttackers marker for seat 0's blocked combat")
	}
	replayCheck(t, e, cfg)
}
