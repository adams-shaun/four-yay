package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The four token-replacement stand-ins task tokrepl1 closes:
//   (a) an Optional$ True R: line now poses its apply/decline election
//       instead of silently declining (Flitwing Lyev, Detective);
//   (b) Type$ ReplaceController now rewrites the created token's controller
//       (an authored printed carrier; the corpus's only carrier, Crafty
//       Cutpurse, delivers its replacement through a DB$ Effect whose
//       CreateToken body effects/misc.go does not yet register live -- see
//       the report's Issues);
//   (c) an SVar-backed Amount$ resolves through the shared numeric grammar
//       instead of the loud Note;
//   (d) effToken's per-token riders now land on EVERY mint a CreateToken
//       replacement produces, not just the first.
//
// Every test asserts its own precondition (the replacement source is a
// battlefield permanent, the maker is where the ability is read, the two
// outcomes under comparison differ) so a vacuous setup fails loudly.

// handPriorityToSeat1 passes priority until the pending decision is seat 1's,
// the APNAP hand-off the opponent maker's activation needs (the existing
// addMana+passPriorityOnce flow assumes a fresh round from seat 0; after a
// stack drain the holder varies, so hand off conditionally).
func handPriorityToSeat1(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d != nil && d.Kind == decision.KPriority && d.Player == 1 {
			return
		}
		passPriorityOnce(t, e)
	}
	t.Fatalf("priority never reached seat 1 (pending %+v)", e.Pending())
}

// tokenControllerReplSrc is an authored printed ReplaceController carrier:
// while it is on the battlefield, every creature token created by ANY player
// enters under its controller's control (the Crafty Cutpurse shape, as a
// printed R: line the ordinary replacement collection reaches).
func tokenControllerReplSrc() string {
	return "Name:Token Controller Shift\nTypes:Enchantment\n" +
		"R:Event$ CreateToken | ActiveZones$ Battlefield | ValidToken$ Creature | ReplaceWith$ ShiftCtrl | Description$ Each creature token is created under your control instead.\n" +
		"SVar:ShiftCtrl:DB$ ReplaceToken | Type$ ReplaceController | NewController$ You\n" +
		"Oracle:x\n"
}

// tokenSVarAmountReplSrc is an authored doubler whose Amount$ is an SVar body
// (X:ReplaceCount$CounterNum/Twice) rather than a literal or op word -- the
// unpriceable-Amount$ shape that used to take the loud Note and leave the
// event verbatim.
func tokenSVarAmountReplSrc() string {
	return "Name:Token Doubler SVar\nTypes:Enchantment\n" +
		"R:Event$ CreateToken | ActiveZones$ Battlefield | ValidToken$ Creature | ReplaceWith$ Double | Description$ Instead create twice that many.\n" +
		"SVar:Double:DB$ ReplaceToken | Type$ Amount | Amount$ X\n" +
		"SVar:X:ReplaceCount$CounterNum/Twice\n" +
		"Oracle:x\n"
}

// tokenForgeTappedSrc is the authored maker with a TokenTapped$ rider, so a
// replacement that produces extras can be checked for rider propagation
// (Doubling Season doubles a CounterChange rider too, which would obscure the
// per-mint check; Tap is untouched by any replacement).
func tokenForgeTappedSrc(script string) string {
	return "Name:Token Forge Tapped\nTypes:Artifact\n" +
		"A:AB$ Token | Cost$ T | TokenScript$ " + script + " | TokenOwner$ You | TokenTapped$ True | SpellDescription$ Create a tapped token.\n" +
		"Oracle:x\n"
}

// TestTokenReplacementOptionalAsksAndAccepts pins (a): Flitwing Lyev's
// Optional$ True replacement poses a real KChoose (decline first, apply
// second) and an accepted election rewrites the Treasure into a Clue.
func TestTokenReplacementOptionalAsksAndAccepts(t *testing.T) {
	t.Parallel()
	lyev := tokenReplCorpusCard(t, "Flitwing, Lyev Detective")
	maker := cardByName(t, tokenForgeSrc("c_a_treasure_sac"))
	e, cfg := tokenReplGame(t, 101, lyev, maker)
	lyevID := moveSeededCard(t, e, 0, lyev, state.ZBattlefield)
	if o := e.G.Obj(lyevID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Flitwing Lyev not a battlefield permanent: %+v", o)
	}
	m := moveSeededCard(t, e, 0, maker, state.ZBattlefield)

	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, m, 0).Index)
	d := drainToChooseOrEmpty(t, e)
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no parked Optional election: the optional ReplaceToken arm did not ask (%+v)", d)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "decline" || d.Options[1].Kind != "apply" {
		t.Fatalf("election options = %+v, want [decline, apply]", d.Options)
	}
	// Accept: the Treasure becomes a Clue.
	submitChoices(t, e, d.Options[1].Index)
	if got := countTokensNamedOnSeat(t, e, 0, "Clue Token"); got != 1 {
		t.Fatalf("accepted election made %d Clue Tokens, want 1", got)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Treasure Token"); got != 0 {
		t.Fatalf("accepted election left %d Treasure Tokens, want 0", got)
	}
	replayCheck(t, e, cfg)
}

// TestTokenReplacementOptionalDeclineStandsVerbatin pins the decline arm of
// (a) through the SAME ask: answering the decline option leaves the original
// Treasure, and the ask was really posed (the assertion above), not the old
// silent decline.
func TestTokenReplacementOptionalDeclineStandsVerbatim(t *testing.T) {
	t.Parallel()
	lyev := tokenReplCorpusCard(t, "Flitwing Lyev, Detective")
	maker := cardByName(t, tokenForgeSrc("c_a_treasure_sac"))
	e, cfg := tokenReplGame(t, 107, lyev, maker)
	moveSeededCard(t, e, 0, lyev, state.ZBattlefield)
	m := moveSeededCard(t, e, 0, maker, state.ZBattlefield)

	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, m, 0).Index)
	d := drainToChooseOrEmpty(t, e)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("no parked Optional election: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	if got := countTokensNamedOnSeat(t, e, 0, "Treasure Token"); got != 1 {
		t.Fatalf("declined election made %d Treasure Tokens, want 1", got)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Clue Token"); got != 0 {
		t.Fatalf("declined election applied anyway: %d Clue Tokens", got)
	}
	replayCheck(t, e, cfg)
}

// TestTokenReplacementControllerShift pins (b): a Type$ ReplaceController
// replacement on seat 0's battlefield makes an opponent-created creature
// token enter under seat 0's control.
func TestTokenReplacementControllerShift(t *testing.T) {
	t.Parallel()
	shift := cardByName(t, tokenControllerReplSrc())
	maker := cardByName(t, tokenForgeSrc("g_1_1_squirrel"))
	e, cfg := tokenReplGameSeats(t, 109, []*cards.Card{shift}, []*cards.Card{maker})
	shiftID := moveSeededCard(t, e, 0, shift, state.ZBattlefield)
	if o := e.G.Obj(shiftID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the ReplaceController source is not a battlefield permanent: %+v", o)
	}
	m := moveSeededCard(t, e, 1, maker, state.ZBattlefield)
	// Seat 0 holds the first priority slot (seatZeroStart): pass it so seat
	// 1 can activate its maker.
	addMana(t, e, 0, "")
	passPriorityOnce(t, e)
	activateTokenForge(t, e, m)

	if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 1 {
		t.Fatalf("ReplaceController put %d tokens under the source's controller, want 1", got)
	}
	if got := countTokensNamedOnSeat(t, e, 1, "Squirrel Token"); got != 0 {
		t.Fatalf("the token stayed under its creator: %d tokens on seat 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestTokenReplacementSVarAmountDoubles pins (c): an Amount$ whose value is an
// SVar body (ReplaceCount$CounterNum/Twice) doubles the mint instead of
// taking the unpriceable-Note path.
func TestTokenReplacementSVarAmountDoubles(t *testing.T) {
	t.Parallel()
	dbl := cardByName(t, tokenSVarAmountReplSrc())
	maker := cardByName(t, tokenForgeSrc("g_1_1_squirrel"))
	e, cfg := tokenReplGame(t, 113, dbl, maker)
	dblID := moveSeededCard(t, e, 0, dbl, state.ZBattlefield)
	if o := e.G.Obj(dblID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the SVar-Amount replacement is not on the battlefield: %+v", o)
	}
	m := moveSeededCard(t, e, 0, maker, state.ZBattlefield)
	activateTokenForge(t, e, m)
	if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 2 {
		t.Fatalf("SVar Amount$ X doubled to %d Squirrel Tokens, want 2", got)
	}
	replayCheck(t, e, cfg)
}

// TestTokenReplacementRidersLandOnEveryExtra pins (d): the maker's
// TokenTapped$ rider lands on BOTH mints Doubling Season produces, not just
// the first.
func TestTokenReplacementRidersLandOnEveryExtra(t *testing.T) {
	t.Parallel()
	ds := tokenReplCorpusCard(t, "Doubling Season")
	maker := cardByName(t, tokenForgeTappedSrc("g_1_1_squirrel"))
	e, cfg := tokenReplGame(t, 127, ds, maker)
	dsID := moveSeededCard(t, e, 0, ds, state.ZBattlefield)
	if o := e.G.Obj(dsID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Doubling Season is not on the battlefield: %+v", o)
	}
	m := moveSeededCard(t, e, 0, maker, state.ZBattlefield)
	activateTokenForge(t, e, m)

	var tokens []*state.Object
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Squirrel Token" {
			tokens = append(tokens, o)
		}
	}
	if len(tokens) != 2 {
		t.Fatalf("Doubling Season made %d Squirrel Tokens, want 2 (precondition for the rider check)", len(tokens))
	}
	for i, o := range tokens {
		if !o.Tapped {
			t.Fatalf("mint %d of 2 is not tapped (the rider must land on every extra)", i)
		}
	}
	replayCheck(t, e, cfg)
}

// TestCraftyCutpurseRedirectsOpponentTokens is the corpus carrier end-to-end:
// Crafty Cutpurse enters (its own ChangesZone trigger resolves the TrigEffect
// chain), which registers the Effect-delivered CreateToken replacement
// (ReplacementEffects$ OppCreatEnters, body DB$ ReplaceToken |
// Type$ ReplaceController | NewController$ You) in effects/misc.go's live
// class -- the registration this test's fix adds. A token the OPPONENT then
// creates enters under Cutpurse's controller's control instead.
func TestCraftyCutpurseRedirectsOpponentTokens(t *testing.T) {
	t.Parallel()
	cutpurse := tokenReplCorpusCard(t, "Crafty Cutpurse")
	maker := cardByName(t, tokenForgeSrc("g_1_1_squirrel"))
	e, cfg := tokenReplGameSeats(t, 139, []*cards.Card{cutpurse}, []*cards.Card{maker})
	cutID := moveSeededCard(t, e, 0, cutpurse, state.ZBattlefield)
	if o := e.G.Obj(cutID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Crafty Cutpurse is not a battlefield permanent: %+v", o)
	}
	m := moveSeededCard(t, e, 1, maker, state.ZBattlefield)
	// Resolve Cutpurse's "when it enters" trigger first: the trigger goes on
	// the stack on the next engine step, and draining the stack resolves it,
	// executing the TrigEffect chain that registers the replacement. Only
	// after that registration is outstanding do we let the opponent mint.
	addMana(t, e, 0, "")
	passUntilStackEmpty(t, e, 20)
	if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 0 {
		t.Fatalf("draining Cutpurse's entry trigger minted %d tokens; nothing should exist yet", got)
	}
	// Preconditions the redirect depends on: seat 1 owns the maker's ability.
	if o := e.G.Obj(m); o == nil || o.Controller != 1 {
		t.Fatalf("precondition: the token maker is not seat 1's: %+v", o)
	}
	handPriorityToSeat1(t, e)
	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, m, 0).Index)
	passUntilStackEmpty(t, e, 20)
	if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 1 {
		t.Fatalf("Cutpurse did not redirect the opponent's token: %d Squirrel Tokens on seat 0, want 1", got)
	}
	if got := countTokensNamedOnSeat(t, e, 1, "Squirrel Token"); got != 0 {
		t.Fatalf("the token stayed under its creator: %d tokens on seat 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestCraftyCutpurseRegistrationExpiresAfterTheTurn pins the registration's
// LIFETIME, the other half of the effect-delivered shape: the effect's
// duration is "this turn" (no Duration$ on the SVar), so it survives its own
// source leaving the battlefield (a departure mid-turn does not end it --
// Forge's Effect entity is turn-scoped, not presence-scoped) and expires at
// the cleanup that ends the turn. On the NEXT turn the opponent's token is
// NOT redirected.
func TestCraftyCutpurseRegistrationExpiresAfterTheTurn(t *testing.T) {
	t.Parallel()
	cutpurse := tokenReplCorpusCard(t, "Crafty Cutpurse")
	maker := cardByName(t, tokenForgeSrc("g_1_1_squirrel"))
	e, cfg := tokenReplGameSeats(t, 149, []*cards.Card{cutpurse}, []*cards.Card{maker})
	cutID := moveSeededCard(t, e, 0, cutpurse, state.ZBattlefield)
	if o := e.G.Obj(cutID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Crafty Cutpurse is not a battlefield permanent: %+v", o)
	}
	m := moveSeededCard(t, e, 1, maker, state.ZBattlefield)
	addMana(t, e, 0, "")
	passUntilStackEmpty(t, e, 20)
	if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 0 {
		t.Fatalf("draining Cutpurse's entry trigger minted %d tokens; nothing should exist yet", got)
	}
	// Cutpurse leaves mid-turn (the logged move is its own departure).
	e.emit(events.Event{Kind: events.MoveZone, Obj: cutID, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	e.Advance()
	if o := e.G.Obj(cutID); o != nil && o.Zone == state.ZBattlefield {
		t.Fatalf("precondition: Cutpurse never left the battlefield")
	}
	// Cross to seat 1's turn: seat 0's cleanup ended the "this turn" effect.
	driveToTurn(t, e, e.G.Turn+1, 1)
	addMana(t, e, 1, "")
	submitChoices(t, e, abilityOption(t, e, m, 0).Index)
	passUntilStackEmpty(t, e, 20)
	got1 := countTokensNamedOnSeat(t, e, 1, "Squirrel Token")
	got0 := countTokensNamedOnSeat(t, e, 0, "Squirrel Token")
	if got1 != 1 {
		t.Fatalf("after the effect's turn ended, the opponent's token did not stay theirs: %d on seat 1 (seat 0: %d), want 1", got1, got0)
	}
	if got0 != 0 {
		t.Fatalf("the expired Cutpurse effect still redirected: %d tokens on seat 0", got0)
	}
	replayCheck(t, e, cfg)
}
