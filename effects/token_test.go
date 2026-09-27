package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// tokenFixtures builds this file's own token cards -- authored here for the
// test, never copied from the corpus's own (GPL-3.0) tokenscripts under
// .cards/tokenscripts. Names and stems echo real Forge shapes (a Goblin
// token and a Deathtouch Wurm) closely enough to read naturally in a test
// failure, but the text is original.
func tokenFixtures(t *testing.T) map[string]*cards.Card {
	t.Helper()
	return map[string]*cards.Card{
		"r_1_1_goblin":                      mkCard(t, "Name:Goblin Token\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n"),
		"c_3_3_a_phyrexian_wurm_deathtouch": mkCard(t, "Name:Phyrexian Wurm Token\nTypes:Creature Phyrexian Wurm\nPT:3/3\nK:Deathtouch\nOracle:x\n"),
	}
}

// fixtureHostWithTokens is fixtureHost (context_test.go), a 2-seat game with
// one object already on it, plus Game.Tokens populated with tokenFixtures --
// what every effToken test in this file needs to have something to mint.
func fixtureHostWithTokens(t *testing.T) (*fakeHost, *Ctx) {
	t.Helper()
	h, c := fixtureHost(t)
	h.g.Tokens = tokenFixtures(t)
	return h, c
}

// countKind reports how many events of kind k are in h's captured log --
// this file's own two-argument variant of rules/replacement_updated_test.go's
// countKind(log, kind, id): effToken's tests care how many tokens got
// minted in total, not which one object a particular event names.
func countKind(h *fakeHost, k events.Kind) int {
	n := 0
	for _, ev := range h.log {
		if ev.Kind == k {
			n++
		}
	}
	return n
}

func TestTokenCreatesEachScriptTheGivenNumberOfTimes(t *testing.T) {
	h, c := fixtureHostWithTokens(t) // Game.Tokens: r_1_1_goblin, c_3_3_a_phyrexian_wurm_deathtouch
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token", Params: map[string]string{"TokenAmount": "2", "TokenScript": "r_1_1_goblin", "TokenOwner": "You"}})
	if n := countKind(h, events.TokenCreate); n != 2 {
		t.Fatalf("%d TokenCreate events", n)
	}
	bf := h.Game().Zone(state.ZBattlefield, c.Controller)
	if len(bf) != 2 || !h.Game().Obj(bf[0]).IsToken || h.Game().Obj(bf[0]).Face().Name != "Goblin Token" {
		t.Fatalf("battlefield %v", bf)
	}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token", Params: map[string]string{"TokenScript": "c_3_3_a_phyrexian_wurm_deathtouch,r_1_1_goblin", "RememberTokens": "True"}})
	if len(c.Remembered) != 2 || len(h.Game().Zone(state.ZBattlefield, c.Controller)) != 4 {
		t.Fatalf("remembered %v", c.Remembered)
	}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token", Params: map[string]string{"TokenScript": "no_such"}})
	if countKind(h, events.TokenCreate) != 4 {
		t.Fatal("unknown script created something")
	}
}

// TestTokenRememberedPersistsDefinedTargets pins the event-backed memory
// attached to the newly created token, rather than only the resolution Ctx.
func TestTokenRememberedPersistsDefinedTargets(t *testing.T) {
	h, c := fixtureHostWithTokens(t)
	// The source already has a real remembered object, matching the
	// resolution-local set an exile cost leaves for TokenRemembered$ ExiledCards.
	remembered := c.Source
	c.Remembered = []state.Target{{Obj: remembered}}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token", Params: map[string]string{
		"TokenScript": "r_1_1_goblin", "TokenRemembered": "Remembered",
	}})
	bf := h.Game().Zone(state.ZBattlefield, c.Controller)
	if len(bf) != 1 {
		t.Fatalf("battlefield = %v, want one token", bf)
	}
	token := h.Game().Obj(bf[0])
	if token == nil || !token.IsToken {
		t.Fatalf("setup did not create a token: %+v", token)
	}
	if len(token.Remembered) != 1 || token.Remembered[0].Obj != remembered {
		t.Fatalf("token Remembered = %+v, want remembered object %d", token.Remembered, remembered)
	}
	if !containsEvent(h.log, events.Choose, token.ID) {
		t.Fatalf("token memory was not event-backed: log = %+v", h.log)
	}
}

func TestTimotharTokenRememberedUsesExiledCards(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Timothar, Baron of Bats")
	if !ok {
		t.Fatal("Timothar missing from corpus")
	}
	var tokenSA *cards.SA
	var walk func(*cards.SA)
	walk = func(s *cards.SA) {
		if s == nil || tokenSA != nil {
			return
		}
		if s.API == "Token" {
			tokenSA = s
			return
		}
		walk(s.Sub)
	}
	for _, f := range card.Faces {
		for _, a := range f.Abilities {
			walk(a)
		}
		for _, tr := range f.Triggers {
			walk(tr.Effect)
		}
	}
	if tokenSA == nil {
		t.Fatal("Timothar has no compiled Token effect")
	}
	h, c := fixtureHostWithTokens(t)
	h.g.Tokens = reg.Tokens
	c.Remembered = []state.Target{{Obj: c.Source}}
	Resolve(h, c, tokenSA)
	bf := h.Game().Zone(state.ZBattlefield, c.Controller)
	if len(bf) != 1 || h.Game().Obj(bf[0]).Face().Name != "Bat Token" {
		t.Fatalf("Timothar token setup = %v, want one Bat token", bf)
	}
	bat := h.Game().Obj(bf[0])
	if len(bat.Remembered) != 1 || bat.Remembered[0].Obj != c.Source {
		t.Logf("Timothar params=%v log=%+v", tokenSA.Params, h.log)
		t.Fatalf("Bat remembered = %+v, want exiled object %d", bat.Remembered, c.Source)
	}
	if !containsEvent(h.log, events.Choose, bat.ID) {
		t.Fatalf("Timothar memory was not persisted by Choose: %+v", h.log)
	}
}

// TestHofriCopyPermanentTokenRemembered pins exactly ONE thing: that
// `TokenRemembered$` is read on the CopyPermanent mint path too, and is
// persisted on the minted token by the replay-visible Choose/"remembered"
// event. It deliberately does NOT claim Hofri Ghostforge works: that card's
// dies trigger also carries `AddSVars$ HofriTrigReturn` and `AddTriggers$
// TrigLeavesBattlefield`, which effCopyPermanent still skips behind a loud
// Note, so the Spirit copy has no leaves-the-battlefield return ability. The
// card stays open; see the (copyperm-grants) row in AGENTS.md's Known
// approximations.
func TestHofriCopyPermanentTokenRemembered(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Hofri Ghostforge")
	if !ok {
		t.Fatal("Hofri missing from corpus")
	}
	var copySA *cards.SA
	var walk func(*cards.SA)
	walk = func(s *cards.SA) {
		if s == nil || copySA != nil {
			return
		}
		if s.API == "CopyPermanent" {
			copySA = s
			return
		}
		walk(s.Sub)
	}
	for _, f := range card.Faces {
		for _, tr := range f.Triggers {
			walk(tr.Effect)
		}
	}
	if copySA == nil {
		t.Fatal("Hofri has no compiled CopyPermanent effect")
	}
	h, c := fixtureHostWithTokens(t)
	h.g.Tokens = reg.Tokens
	// The real effect's trigger referent is supplied by the rules engine; use
	// its same Defined group explicitly in this effects-level regression.
	params := make(map[string]string, len(copySA.Params)+1)
	for k, v := range copySA.Params {
		params[k] = v
	}
	params["Defined"] = "Remembered"
	// The real trigger's ConditionDefined$ is evaluated by the rules trigger
	// matcher; this effects-level test supplies that already-qualified context.
	delete(params, "ConditionDefined")
	delete(params, "ConditionPresent")
	copySA = &cards.SA{Kind: "DB", API: copySA.API, Params: params}
	c.Remembered = []state.Target{{Obj: c.Source}}
	Resolve(h, c, copySA)
	bf := h.Game().Zone(state.ZBattlefield, c.Controller)
	if len(bf) != 1 {
		t.Logf("Hofri params=%v log=%+v remembered=%+v", copySA.Params, h.log, c.Remembered)
		t.Fatalf("Hofri copy battlefield = %v, want one Spirit token", bf)
	}
	spirit := h.Game().Obj(bf[0])
	if !spirit.IsToken || len(spirit.Remembered) != 1 || spirit.Remembered[0].Obj != c.Source {
		t.Fatalf("Hofri token = %+v, want token remembering source", spirit)
	}
	if !containsEvent(h.log, events.Choose, spirit.ID) {
		t.Fatalf("Hofri token memory was not persisted: %+v", h.log)
	}
}

func containsEvent(log []events.Event, kind events.Kind, obj state.ObjID) bool {
	for _, ev := range log {
		if ev.Kind == kind && ev.Obj == obj && ev.Counter == "remembered" {
			return true
		}
	}
	return false
}

// TestTokenUnknownScriptNotesAndCreatesNothing pins down the exact totality
// behaviour TestTokenCreatesEachScriptTheGivenNumberOfTimes only checks the
// count for: an unrecognised TokenScript$ stem is a Note diagnostic naming
// the stem, not a silent no-op and never a panic (Resolve's own convention
// for every other unimplemented/unrecognised input in this package).
func TestTokenUnknownScriptNotesAndCreatesNothing(t *testing.T) {
	h, c := fixtureHostWithTokens(t)
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token", Params: map[string]string{"TokenScript": "no_such"}})
	if n := countKind(h, events.TokenCreate); n != 0 {
		t.Fatalf("%d TokenCreate events, want 0", n)
	}
	if len(h.log) != 1 || h.log[0].Kind != events.Note || h.log[0].Text != "unknown token script no_such" {
		t.Fatalf("log = %+v", h.log)
	}
	if bf := h.Game().Zone(state.ZBattlefield, c.Controller); len(bf) != 0 {
		t.Fatalf("battlefield = %v, want empty", bf)
	}
}

// TestTokenAmountZeroCreatesNothing: TokenAmount$ 0 is a legal (if useless)
// value. This checks the explicit-zero path specifically, which is distinct
// from Num's own default: a MISSING TokenAmount$ falls back to 1 (Num's def
// parameter, which TestTokenCreatesEachScriptTheGivenNumberOfTimes already
// exercises with TokenAmount$ absent on its RememberTokens$ call) -- an
// unparseable-but-present value falls to Num's own zero fallback instead
// (Num returns 0 for a present-but-unparseable value, not the default; only
// a MISSING key returns the default), which happens to read the same as
// this test's explicit "0" but is a different code path and not what this
// test is pinning down.
func TestTokenAmountZeroCreatesNothing(t *testing.T) {
	h, c := fixtureHostWithTokens(t)
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token", Params: map[string]string{"TokenAmount": "0", "TokenScript": "r_1_1_goblin"}})
	if n := countKind(h, events.TokenCreate); n != 0 {
		t.Fatalf("%d TokenCreate events, want 0", n)
	}
	if len(h.log) != 0 {
		t.Fatalf("log = %+v, want empty (TokenAmount$ 0 is not an unknown script -- no Note either)", h.log)
	}
	if bf := h.Game().Zone(state.ZBattlefield, c.Controller); len(bf) != 0 {
		t.Fatalf("battlefield = %v, want empty", bf)
	}
}

// TestTokenOwnerOpponentPicksTheNextAliveSeat covers TokenOwner$ Opponent in
// both a 2-player game (the only opponent) and a 4-player game (the next
// living seat after the controller, in APNAP order -- the same seat
// Defined$ Opponent's own first entry names, context_test.go's
// TestDefinedResolvesEachForm). The token must land on the OPPONENT's
// battlefield, not the controller's own.
func TestTokenOwnerOpponentPicksTheNextAliveSeat(t *testing.T) {
	for _, tc := range []struct {
		seats      int
		controller state.PlayerID
		wantOwner  state.PlayerID
	}{
		{2, 0, 1},
		{4, 1, 2},
	} {
		h := newHost(t, tc.seats)
		h.g.Tokens = tokenFixtures(t)
		c := &Ctx{Controller: tc.controller}

		Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
			Params: map[string]string{"TokenScript": "r_1_1_goblin", "TokenOwner": "Opponent"}})

		bf := h.Game().Zone(state.ZBattlefield, tc.wantOwner)
		if len(bf) != 1 {
			t.Fatalf("seats=%d controller=%d: opponent %d's battlefield = %v, want 1 token",
				tc.seats, tc.controller, tc.wantOwner, bf)
		}
		if o := h.Game().Obj(bf[0]); o.Owner != tc.wantOwner || o.Controller != tc.wantOwner {
			t.Fatalf("seats=%d controller=%d: token owner=%d controller=%d, want both %d",
				tc.seats, tc.controller, o.Owner, o.Controller, tc.wantOwner)
		}
		if own := h.Game().Zone(state.ZBattlefield, tc.controller); len(own) != 0 {
			t.Fatalf("seats=%d controller=%d: controller's own battlefield = %v, want empty",
				tc.seats, tc.controller, own)
		}
	}
}

// TestTokenOwnerDefaultsToYou: with no TokenOwner$ at all (the corpus's most
// common shape -- "You" is the default per the brief), the token belongs to
// the controller, mirroring TestTokenCreatesEachScriptTheGivenNumberOfTimes'
// own explicit "You" but proving the parameter is genuinely optional.
func TestTokenOwnerDefaultsToYou(t *testing.T) {
	h, c := fixtureHostWithTokens(t)
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token", Params: map[string]string{"TokenScript": "r_1_1_goblin"}})
	if bf := h.Game().Zone(state.ZBattlefield, c.Controller); len(bf) != 1 {
		t.Fatalf("controller's battlefield = %v, want 1 token", bf)
	}
}

// TestTokenOwnerUnrecognizedFormNotesAndDefaultsToController: a
// TokenOwner$ value the shared Defined$ player-selector grammar does not
// know at all still creates the token under the controller -- the brief's
// own stated fallback -- but says so with a Note, so a genuine fidelity gap
// is visible in the log rather than silently indistinguishable from the
// ordinary "You" default.
func TestTokenOwnerUnrecognizedFormNotesAndDefaultsToController(t *testing.T) {
	h, c := fixtureHostWithTokens(t)
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
		Params: map[string]string{"TokenScript": "r_1_1_goblin", "TokenOwner": "NoSuchSelector"}})

	if bf := h.Game().Zone(state.ZBattlefield, c.Controller); len(bf) != 1 {
		t.Fatalf("controller's battlefield = %v, want 1 token (unrecognised TokenOwner$ still "+
			"defaults to the controller)", bf)
	}
	want := "unrecognized TokenOwner NoSuchSelector, defaulting to the controller"
	found := false
	for _, ev := range h.log {
		if ev.Kind == events.Note && ev.Text == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("log = %+v, want a Note %q", h.log, want)
	}
}

// TestTokenOwnerQualifiedPlayerIsRememberedResolvesThroughGrammar (from
// main's qualified-spelling pin, re-aimed at the merged grammar): the
// QUALIFIED Player.<qualifier> spellings resolve through the shared Defined$
// grammar (tokenOwnerPlayers), not the controller fallback --
// Player.IsRemembered names the source permanent's persistent player-Remember
// list, so the token is minted under the remembered seat, not the resolving
// controller, with no fallback Note.
func TestTokenOwnerQualifiedPlayerIsRememberedResolvesThroughGrammar(t *testing.T) {
	h, c := fixtureHostWithTokens(t)
	c.Remembered = []state.Target{{Player: 1, IsPlayer: true}}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
		Params: map[string]string{"TokenScript": "r_1_1_goblin", "TokenOwner": "Player.IsRemembered"}})
	if bf := h.Game().Zone(state.ZBattlefield, 1); len(bf) != 1 {
		t.Fatalf("remembered seat 1's battlefield = %v, want 1 token (Player.IsRemembered "+
			"resolves through the shared grammar)", bf)
	}
	if bf := h.Game().Zone(state.ZBattlefield, c.Controller); len(bf) != 0 && c.Controller != 1 {
		t.Fatalf("resolving controller's battlefield = %v, want empty", bf)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "TokenOwner") {
			t.Fatalf("unexpected TokenOwner fallback Note in log: %+v", ev)
		}
	}
}

// TestTokenOwnerTargetedControllerStaysWithTheTargetsController is the leaf
// for the deck carrier generous_gift (and the 38-file corpus family):
// TokenOwner$ TargetedController resolves through the shared Defined$
// grammar to the controller of the resolution's object target -- NOT the
// resolving controller. The object has already left the battlefield by the
// time the chained Token resolves (Destroy runs first), so this also pins
// that the target's controller survives the zone change for an ordinary
// (owner-controlled) permanent.
func TestTokenOwnerTargetedControllerStaysWithTheTargetsController(t *testing.T) {
	h := newHost(t, 2)
	h.g.Tokens = tokenFixtures(t)
	// Seat 1's artifact, on the battlefield.
	victim := h.g.AddObject(mkCard(t, "Name:Fixture Relic\nTypes:Artifact\nOracle:x\n"), 1)
	victim.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 1, append(h.g.Zone(state.ZBattlefield, 1), victim.ID))
	if o := h.g.Obj(victim.ID); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: victim = %+v, want seat 1 battlefield", o)
	}

	// The real Generous Gift shape, authored inline (the Forge corpus script
	// is GPL and is never copied into a test).
	src := "Name:Generous Gift\nManaCost:2 W\nTypes:Instant\n" +
		"A:SP$ Destroy | ValidTgts$ Permanent | SubAbility$ DBToken\n" +
		"SVar:DBToken:DB$ Token | TokenScript$ r_1_1_goblin | TokenOwner$ TargetedController\n" +
		"Oracle:x\n"
	card := mkCard(t, src)
	c := &Ctx{Source: victim.ID, Controller: 0,
		Targets: []state.Target{{Obj: victim.ID}}}
	Resolve(h, c, card.Faces[0].Abilities[0])

	if o := h.g.Obj(victim.ID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("victim = %+v, want graveyard after Destroy", o)
	}
	bf := h.g.Zone(state.ZBattlefield, 1)
	if len(bf) != 1 {
		t.Fatalf("target's controller battlefield = %v, want 1 Elephant", bf)
	}
	tok := h.g.Obj(bf[0])
	if !tok.IsToken || tok.Face() == nil || tok.Face().Name != "Goblin Token" {
		t.Fatalf("token = %+v, want a minted Goblin Token", tok)
	}
	if tok.Controller != 1 || tok.Owner != 1 {
		t.Fatalf("token controller/owner = %d/%d, want both 1 (the destroyed permanent's controller)",
			tok.Controller, tok.Owner)
	}
	if own := h.g.Zone(state.ZBattlefield, 0); len(own) != 0 {
		t.Fatalf("caster's battlefield = %v, want empty (the caster must not keep the gift)", own)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unrecognized TokenOwner") {
			t.Fatalf("TokenOwner$ TargetedController fell back with a Note: %q", ev.Text)
		}
	}
}

// TestTokenOwnerPlayerMintsForEveryAlivePlayer covers the bare
// TokenOwner$ Player spelling ("each player creates ..."): in a 3-seat
// game EVERY seat -- the controller included -- creates its own token, each
// owned and controlled by that seat, in seat order from seat 0 (so the
// mint ids are deterministic and replay-stable regardless of who is
// resolving). A seat that has already lost creates nothing.
func TestTokenOwnerPlayerMintsForEveryAlivePlayer(t *testing.T) {
	h := newHost(t, 3)
	h.g.Tokens = tokenFixtures(t)
	// Seat 1 has already lost: "each player" means each ALIVE player.
	h.g.Players[1].Lost = true
	c := &Ctx{Controller: 2}

	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
		Params: map[string]string{"TokenScript": "r_1_1_goblin", "TokenOwner": "Player"}})

	if n := countKind(h, events.TokenCreate); n != 2 {
		t.Fatalf("%d TokenCreate events, want 2 (one per ALIVE seat: 0 and 2)", n)
	}
	// Mint order is seat order from seat 0, so the first mint is seat 0's.
	for _, p := range []state.PlayerID{0, 2} {
		bf := h.Game().Zone(state.ZBattlefield, p)
		if len(bf) != 1 {
			t.Fatalf("seat %d's battlefield = %v, want 1 token", p, bf)
		}
		o := h.Game().Obj(bf[0])
		if o.Owner != p || o.Controller != p {
			t.Fatalf("seat %d's token: owner=%d controller=%d, want both %d", p, o.Owner, o.Controller, p)
		}
		if !o.IsToken {
			t.Fatalf("seat %d's object is not a token", p)
		}
	}
	if bf := h.Game().Zone(state.ZBattlefield, 1); len(bf) != 0 {
		t.Fatalf("lost seat 1's battlefield = %v, want empty", bf)
	}
	// No diagnostic Note: the bare spelling is resolved, not defaulted.
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "TokenOwner") {
			t.Fatalf("unexpected TokenOwner Note in log: %+v", ev)
		}
	}
}

// TestTokenOwnerPlayerResolvesEveryAlivePlayer covers the adjacent corpus
// value (29 files, "each player creates"): TokenOwner$ Player now resolves
// through the shared grammar to every living seat, so each player gets the
// token rather than only the resolving controller. This is the fan-out the
// shared resolver gives for free -- it is why the fix is a class fix, not a
// TargetedController special case.
// TestTokenOwnerTargetedControllerUsesControllerLKI covers a stolen target:
// the target's owner is seat 0 but its controller is seat 1. Destroy resets
// the live object controller to the owner before the chained Token resolves;
// the token must still be created for the pre-destruction controller.
func TestTokenOwnerTargetedControllerUsesControllerLKI(t *testing.T) {
	h := newHost(t, 2)
	h.g.Tokens = tokenFixtures(t)
	victim := h.g.AddObject(mkCard(t, "Name:Stolen Relic\nTypes:Artifact\nOracle:x\n"), 0)
	victim.Zone = state.ZBattlefield
	victim.Controller = 1
	h.g.SetZone(state.ZBattlefield, 1, append(h.g.Zone(state.ZBattlefield, 1), victim.ID))
	if o := h.g.Obj(victim.ID); o == nil || o.Zone != state.ZBattlefield || o.Owner != 0 || o.Controller != 1 {
		t.Fatalf("precondition: victim = %+v, want owner 0/controller 1 on battlefield", o)
	}

	card := mkCard(t, "Name:Generous Gift\nManaCost:2 W\nTypes:Instant\n"+
		"A:SP$ Destroy | ValidTgts$ Permanent | SubAbility$ DBToken\n"+
		"SVar:DBToken:DB$ Token | TokenScript$ r_1_1_goblin | TokenOwner$ TargetedController\n"+
		"Oracle:x\n")
	c := &Ctx{Source: victim.ID, Controller: 0, Targets: []state.Target{{Obj: victim.ID}}}
	Resolve(h, c, card.Faces[0].Abilities[0])

	if o := h.g.Obj(victim.ID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("victim = %+v, want graveyard after Destroy", o)
	}
	bf := h.g.Zone(state.ZBattlefield, 1)
	if len(bf) != 1 {
		t.Fatalf("target controller battlefield = %v, want one token", bf)
	}
	if own := h.g.Zone(state.ZBattlefield, 0); len(own) != 0 {
		t.Fatalf("owner/caster battlefield = %v, want empty", own)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unrecognized TokenOwner") {
			t.Fatalf("TargetedController emitted fallback Note: %q", ev.Text)
		}
	}
}

// TestTokenOwnerPlayerPerPlayerTokenAmountAmount: with TokenAmount$ 2 each
// alive player creates TWO tokens (Edge Rover's "each player creates X ..."
// reads X PER player, not X in total), 2 seats -> 4 mints.
func TestTokenOwnerPlayerPerPlayerTokenAmountAmount(t *testing.T) {
	h := newHost(t, 2)
	h.g.Tokens = tokenFixtures(t)
	c := &Ctx{Controller: 0}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
		Params: map[string]string{"TokenAmount": "2", "TokenScript": "r_1_1_goblin", "TokenOwner": "Player"}})
	if n := countKind(h, events.TokenCreate); n != 4 {
		t.Fatalf("%d TokenCreate events, want 4 (2 per seat x 2 alive seats)", n)
	}
	for _, p := range []state.PlayerID{0, 1} {
		if got := len(h.Game().Zone(state.ZBattlefield, p)); got != 2 {
			t.Fatalf("seat %d has %d tokens, want 2", p, got)
		}
	}
}

func TestTokenOwnerPlayerResolvesEveryAlivePlayer(t *testing.T) {
	for _, seats := range []int{2, 4} {
		h := newHost(t, seats)
		h.g.Tokens = tokenFixtures(t)
		c := &Ctx{Controller: 0}
		Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
			Params: map[string]string{"TokenScript": "r_1_1_goblin", "TokenOwner": "Player"}})
		for p := state.PlayerID(0); p < state.PlayerID(seats); p++ {
			bf := h.Game().Zone(state.ZBattlefield, p)
			if len(bf) != 1 {
				t.Fatalf("seats=%d: player %d battlefield = %v, want 1 token", seats, p, bf)
			}
			if o := h.Game().Obj(bf[0]); o.Controller != p {
				t.Fatalf("seats=%d: token for player %d is controlled by %d", seats, p, o.Controller)
			}
		}
		for _, ev := range h.log {
			if ev.Kind == events.Note && strings.Contains(ev.Text, "unrecognized TokenOwner") {
				t.Fatalf("seats=%d: TokenOwner$ Player fell back with a Note: %q", seats, ev.Text)
			}
		}
	}
}

// TestTokenOwnerPlayerCreatesForEveryLivingSeat covers TokenOwner$ Player
// (Rendmaw, Creaking Nest's "each player creates a tapped 2/2 black Bird";
// 29 raw corpus lines): one token per LIVING player, each under its own
// seat, none noted as unrecognised. An eliminated seat creates nothing.
func TestTokenOwnerPlayerCreatesForEveryLivingSeat(t *testing.T) {
	for _, tc := range []struct {
		seats      int
		controller state.PlayerID
	}{
		{2, 0}, {4, 1},
	} {
		h := newHost(t, tc.seats)
		h.g.Tokens = tokenFixtures(t)
		c := &Ctx{Controller: tc.controller}

		Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
			Params: map[string]string{"TokenScript": "r_1_1_goblin", "TokenOwner": "Player"}})

		for _, ev := range h.log {
			if ev.Kind == events.Note && strings.Contains(ev.Text, "TokenOwner") {
				t.Fatalf("seats=%d controller=%d: unexpected Note %q", tc.seats, tc.controller, ev.Text)
			}
		}
		if n := countKind(h, events.TokenCreate); n != tc.seats {
			t.Fatalf("seats=%d controller=%d: %d TokenCreate events, want one per seat", tc.seats, tc.controller, n)
		}
		for p := range tc.seats {
			bf := h.Game().Zone(state.ZBattlefield, state.PlayerID(p))
			if len(bf) != 1 {
				t.Fatalf("seats=%d controller=%d: seat %d's battlefield = %v, want 1 token", tc.seats, tc.controller, p, bf)
			}
			if o := h.Game().Obj(bf[0]); o.Owner != state.PlayerID(p) {
				t.Fatalf("seats=%d controller=%d: seat %d's token owner=%d", tc.seats, tc.controller, p, o.Owner)
			}
		}
	}
}

// TestTokenOwnerPlayerSkipsAnEliminatedSeat preserves the Player selector's
// fan-out contract: eliminated seats do not receive a token.
func TestTokenOwnerPlayerSkipsAnEliminatedSeat(t *testing.T) {
	h := newHost(t, 4)
	h.g.Tokens = tokenFixtures(t)
	h.g.Players[2].Lost = true
	Resolve(h, &Ctx{Controller: 0}, &cards.SA{Kind: "DB", API: "Token",
		Params: map[string]string{"TokenScript": "r_1_1_goblin", "TokenOwner": "Player"}})
	for p := state.PlayerID(0); p < 4; p++ {
		want := 1
		if p == 2 {
			want = 0
		}
		if got := len(h.Game().Zone(state.ZBattlefield, p)); got != want {
			t.Fatalf("player %d battlefield = %v, want %d token(s)", p, h.Game().Zone(state.ZBattlefield, p), want)
		}
	}
}

// TestTokenAttackingTrueMarksTheDefender: with the firing Attacks trigger's
// referent capture in context (c.DefendingPlayer set, what
// rules/trigger_referents.go binds from the DeclareAttackers event), every
// token TokenAttacking$ True creates enters tapped and attacking that
// defender through the appended events.TokenAttacks kind -- and NO diagnostic
// Note runs.
func TestTokenAttackingTrueMarksTheDefender(t *testing.T) {
	h, c := fixtureHostWithTokens(t)
	c.DefendingPlayer = state.Target{IsPlayer: true, Player: 1}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
		Params: map[string]string{"TokenScript": "r_1_1_goblin", "TokenTapped": "True", "TokenAttacking": "True"}})
	bf := h.Game().Zone(state.ZBattlefield, c.Controller)
	if len(bf) != 1 {
		t.Fatalf("battlefield = %v, want 1 token", bf)
	}
	o := h.Game().Obj(bf[0])
	if !o.Tapped || !o.IsAttacking || o.Attacking != 1 {
		t.Fatalf("token tapped=%v attacking=%v defender=%d, want tapped, attacking seat 1", o.Tapped, o.IsAttacking, o.Attacking)
	}
	attacks := 0
	for _, ev := range h.log {
		if ev.Kind == events.TokenAttacks {
			attacks++
			if ev.Obj != bf[0] || len(ev.IDs) != 1 || ev.IDs[0] != 1 {
				t.Fatalf("TokenAttacks event = %+v, want Obj %d attacking seat 1", ev, bf[0])
			}
		}
	}
	if attacks != 1 {
		t.Fatalf("%d TokenAttacks events, want 1", attacks)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note {
			t.Fatalf("a correct defender context must not note: %+v", h.log)
		}
	}
}

// TestTokenAttackingWithoutDefenderDegradesLoud: an ACTIVATED AB$ Token
// rider (kavaron_harrier, militias_pride) has no trigger context, so there is
// no defender to attack: the token still enters (tapped, TokenTapped$ says
// so) but NOT attacking, under exactly ONE loud deterministic Note naming the
// limitation -- never a guessed defender, never silence.
func TestTokenAttackingWithoutDefenderDegradesLoud(t *testing.T) {
	h, c := fixtureHostWithTokens(t)
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
		Params: map[string]string{"TokenScript": "r_1_1_goblin", "TokenTapped": "True", "TokenAttacking": "True"}})
	bf := h.Game().Zone(state.ZBattlefield, c.Controller)
	if len(bf) != 1 {
		t.Fatalf("battlefield = %v, want 1 token", bf)
	}
	o := h.Game().Obj(bf[0])
	if !o.Tapped {
		t.Fatal("the token must still enter tapped (TokenTapped$ True is the ordinary path)")
	}
	if o.IsAttacking || o.Attacking != 0 {
		t.Fatalf("token attacking=%v defender=%d with no defender context, want not attacking", o.IsAttacking, o.Attacking)
	}
	notes := 0
	for _, ev := range h.log {
		if ev.Kind == events.Note {
			notes++
			if !strings.Contains(ev.Text, "no defending player in context") {
				t.Fatalf("degrade note = %q, want the no-defender-context limitation", ev.Text)
			}
		}
	}
	if notes != 1 {
		t.Fatalf("%d notes, want exactly one", notes)
	}
}

// TestTokenAttackingEmptyRememberedDegradesLoud: a `Remembered` selector
// whose remembered set holds no PLAYER (an empty set, or cards only) has no
// seat to attack, so the token enters unmarked under one Note per call. The
// non-True selector forms themselves ARE resolved now (tokenAttackingPlayers,
// pinned in token_attacking_selectors_test.go and end to end on Adeline,
// Resplendent Cathar in rules/adeline_token_attacking_test.go); this is the
// fail-closed empty-binding arm, not an unimplemented spelling.
func TestTokenAttackingEmptyRememberedDegradesLoud(t *testing.T) {
	h, c := fixtureHostWithTokens(t)
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
		Params: map[string]string{"TokenScript": "r_1_1_goblin", "TokenAttacking": "Remembered"}})
	bf := h.Game().Zone(state.ZBattlefield, c.Controller)
	if len(bf) != 1 {
		t.Fatalf("battlefield = %v, want 1 token", bf)
	}
	if o := h.Game().Obj(bf[0]); o.IsAttacking || o.Tapped {
		t.Fatalf("token tapped=%v attacking=%v, want unmarked", o.Tapped, o.IsAttacking)
	}
	notes := 0
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "TokenAttacking$ Remembered is not implemented") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("%d selector notes, want exactly one; log = %+v", notes, h.log)
	}
}
