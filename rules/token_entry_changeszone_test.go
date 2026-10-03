package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// A token minted straight onto the battlefield (events.TokenCreate /
// events.CardToken) must fire an "enters the battlefield" trigger, exactly
// like a real card's MoveZone entry. Before this ticket no token entry raised
// a Mode$ ChangesZone / ChangesZoneAll trigger at all: the mint event was
// event-kind-gated out of the zone-change matcher, its compiled-interest
// prefilter returned 0, the textual mask excluded it, and the mint carried
// no object id for ValidCard$ to read. The four gates are closed together
// (rules/trigmatch/zone.go, rules/trigger_eligibility.go, rules/emit.go).
//
// The control half (a real creature entering) passes before and after; the
// token half is the regression the fix targets. Both use real corpus cards,
// so no Forge script text is committed here (the search_library_test.go
// convention). The token is minted through the engine's own effToken path by
// an authored artifact (tokenForgeSrc), whose TokenScript$ names a real
// corpus token script.

// soulWardenTrigger asserts the compiled trigger line the life assertions
// ride on: a real scan (or a corpus change) cannot make the counts vacuous.
func soulWardenTrigger(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatalf("Soul Warden object %d missing a face", id)
	}
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Soul Warden in %s, want Battlefield", o.Zone)
	}
	triggers := o.Face().Triggers
	if len(triggers) == 0 {
		t.Fatalf("precondition: Soul Warden has no trigger, want one ChangesZone line")
	}
	tr := triggers[0]
	if tr.Mode != "ChangesZone" {
		t.Fatalf("precondition: Soul Warden trigger mode = %q, want ChangesZone", tr.Mode)
	}
	if got := tr.Params["Destination"]; got != "Battlefield" {
		t.Fatalf("precondition: Soul Warden Destination = %q, want Battlefield", got)
	}
	if got := tr.Params["ValidCard"]; got != "Creature.Other" {
		t.Fatalf("precondition: Soul Warden ValidCard = %q, want Creature.Other", got)
	}
	if got := tr.Params["Origin"]; got != "Any" {
		t.Fatalf("precondition: Soul Warden Origin = %q, want Any", got)
	}
}

// battlefieldToken finds the first battlefield token on seat p whose face
// name is exactly name; it fails if absent so a caller cannot assert on a
// missing mint.
func battlefieldToken(t *testing.T, e *Engine, p state.PlayerID, name string) *state.Object {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o != nil && o.Face() != nil && o.Face().Name == name {
			return o
		}
	}
	t.Fatalf("no %q on seat %d's battlefield", name, p)
	return nil
}

// TestTokenEntryFiresChangesZoneTriggers is the report's carrier shape: Soul
// Warden (Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield |
// ValidCard$ Creature.Other) must gain life for a real creature's entry
// (control, passes before the fix) AND for a minted creature token's entry
// (the regression, fails before the fix). The third half asserts the minted
// object really is a battlefield token, so the count is not vacuous.
func TestTokenEntryFiresChangesZoneTriggers(t *testing.T) {
	t.Parallel()
	warden := tokenReplCorpusCard(t, "Soul Warden")
	bears := tokenReplCorpusCard(t, "Grizzly Bears")
	maker := cardByName(t, tokenForgeSrc("g_1_1_squirrel"))

	e, cfg := tokenReplGame(t, 71, warden, bears, maker)
	wardenID := moveSeededCard(t, e, 0, warden, state.ZBattlefield)
	soulWardenTrigger(t, e, wardenID)
	makerID := moveSeededCard(t, e, 0, maker, state.ZBattlefield)

	// (a) Control: a REAL creature entering fires the trigger. This half
	// passes with and without the fix, so it proves the payoff path itself.
	lifeBefore := e.G.Players[0].Life
	moveSeededCard(t, e, 0, bears, state.ZBattlefield)
	zallDrain(t, e)
	if lifeBefore != 20 {
		t.Fatalf("control precondition: seat 0 life = %d, want 20", lifeBefore)
	}
	if got := e.G.Players[0].Life; got != 21 {
		t.Fatalf("real creature entry: life = %d, want 21 (Soul Warden trigger must fire)", got)
	}

	// (b) The regression: a DB$ Token mint is a zone change too. The token
	// is a creature (Squirrel Token), so Soul Warden must gain life again.
	lifeBeforeToken := e.G.Players[0].Life
	activateTokenForge(t, e, makerID)
	if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 1 {
		t.Fatalf("precondition: %d Squirrel Tokens minted, want 1", got)
	}
	if got := e.G.Players[0].Life; got != lifeBeforeToken+1 {
		t.Fatalf("token entry: life = %d after a token minted, want %d (Soul Warden must fire on a token entry)",
			got, lifeBeforeToken+1)
	}

	// (c) The minted object is really on the battlefield and is a token, so
	// (b) cannot be vacuous.
	tok := battlefieldToken(t, e, 0, "Squirrel Token")
	if !tok.IsToken {
		t.Fatalf("Squirrel Token object %d is not flagged IsToken", tok.ID)
	}
	if tok.Zone != state.ZBattlefield {
		t.Fatalf("Squirrel Token in %s, want Battlefield", tok.Zone)
	}
	replayCheck(t, e, cfg)
}

// TestTokenEntryFiresChangesZoneAllTriggers proves the ChangesZoneAll family
// shares the fix -- it rides the same trigmatch.ZoneChangeMatchesWithCapture matcher and
// the same mask/interest entries. Elvish Warmaster's
// ChangesZoneAll | ValidCards$ Elf.Other+YouCtrl line must see a minted Elf
// token enter and create its own Elf Warrior token. The Warmaster's own entry
// is "other Elves" so it does not trigger itself, and ActivationLimit$ 1
// latches after the first qualifying entry.
func TestTokenEntryFiresChangesZoneAllTriggers(t *testing.T) {
	t.Parallel()
	warmaster := tokenReplCorpusCard(t, "Elvish Warmaster")
	maker := cardByName(t, tokenForgeSrc("g_1_1_elf_warrior"))

	e, cfg := tokenReplGame(t, 73, warmaster, maker)
	wID := moveSeededCard(t, e, 0, warmaster, state.ZBattlefield)
	o := e.G.Obj(wID)
	if o == nil || o.Face() == nil || len(o.Face().Triggers) == 0 {
		t.Fatalf("precondition: Elvish Warmaster object %d missing its trigger", wID)
	}
	tr := o.Face().Triggers[0]
	if tr.Mode != "ChangesZoneAll" {
		t.Fatalf("precondition: Elvish Warmaster trigger mode = %q, want ChangesZoneAll", tr.Mode)
	}
	if got := tr.Params["ValidCards"]; got != "Elf.Other+YouCtrl" {
		t.Fatalf("precondition: Elvish Warmaster ValidCards = %q, want Elf.Other+YouCtrl", got)
	}
	if got := tr.Params["Destination"]; got != "Battlefield" {
		t.Fatalf("precondition: Elvish Warmaster Destination = %q, want Battlefield", got)
	}
	makerID := moveSeededCard(t, e, 0, maker, state.ZBattlefield)

	activateTokenForge(t, e, makerID)
	// The minted Elf Warrior's entry fires the Warmaster, which creates a
	// second Elf Warrior: one scripted mint + one Warmaster payoff = two.
	if got := countTokensNamedOnSeat(t, e, 0, "Elf Warrior Token"); got != 2 {
		t.Fatalf("Elvish Warmaster saw %d Elf Warrior Tokens, want 2 (its payoff token for the minted Elf's entry)", got)
	}
	replayCheck(t, e, cfg)
}
