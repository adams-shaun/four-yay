package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// rv2b: static AddKeyword$ lists written with Forge's "&" separator
// ("Vigilance & Lifelink") parsed as ONE bogus keyword, so every Equipment /
// Aura static whose grant carried more than one keyword granted nothing at
// all. The fix is cards.SplitKeywordList, the shared parser for the Forge
// ampersand grammar. Commas remain inside a keyword's parameters; type lists
// keep their separate grammar. This file pins the real corpus cards the
// defect broke and the rules-side reader wiring.

// equipGrants drives the Equip ability of eq onto bearer and waits for the
// stack to empty, so the static's Affected$ grant is live.
func equipGrants(t *testing.T, e *Engine, eq, bearer state.ObjID, symbols string) {
	t.Helper()
	addMana(t, e, 0, symbols)
	opt := abilityOption(t, e, eq, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("equip: want target decision, got %+v", d)
	}
	targetObject(t, e, bearer)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(eq).AttachedTo != bearer {
		t.Fatalf("equipment %d attached to %d, want %d", eq, e.G.Obj(eq).AttachedTo, bearer)
	}
}

// findOnBoard returns seat p's battlefield object named name, or fails.
func findOnBoard(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("no %s on seat %d's battlefield", name, p)
	return 0
}

// assertGrants checks the bearer has every wanted keyword, no derived keyword
// still carries an "&" (the pre-fix bogus joined string), and for protection
// grants that the engine's own protectedFrom predicate sees them.
func assertGrants(t *testing.T, e *Engine, bearer state.ObjID, want []string) {
	t.Helper()
	for _, k := range want {
		if !e.HasKeyword(bearer, k) {
			t.Errorf("bearer %d missing granted keyword %q (derived %v)", bearer, k, e.Keywords(bearer))
		}
	}
	for _, k := range e.Keywords(bearer) {
		if strings.Contains(k, "&") {
			t.Errorf("derived keyword %q is still the bogus joined string", k)
		}
	}
}

// TestEquipmentAmpersandGrantsRealKeywords drives four REAL corpus cards —
// the Equipment the defect reported (Basilisk Collar, Loxodon Warhammer,
// Lightning Greaves, Sword of Fire and Ice) — through a full equip and
// asserts the bearer has BOTH members of each "A & B" grant. Before the fix
// each grant arrived as a single bogus keyword ("Deathtouch & Lifelink")
// that HasKeyword never matched, so none of these grants worked.
func TestEquipmentAmpersandGrantsRealKeywords(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cases := []struct {
		equipment string
		mana      string
		want      []string
	}{
		{"Basilisk Collar", "CC", []string{"Deathtouch", "Lifelink"}},
		{"Loxodon Warhammer", "CCC", []string{"Trample", "Lifelink"}},
		{"Lightning Greaves", "", []string{"Haste", "Shroud"}},
		{"Sword of Fire and Ice", "CC", []string{"Protection from red", "Protection from blue"}},
	}
	for _, tc := range cases {
		t.Run(tc.equipment, func(t *testing.T) {
			e, _ := linkBoard(t, reg, []string{tc.equipment, "Grizzly Bears"}, []string{"Hill Giant"})
			eq := findOnBoard(t, e, 0, tc.equipment)
			bear := findOnBoard(t, e, 0, "Grizzly Bears")
			equipGrants(t, e, eq, bear, tc.mana)
			assertGrants(t, e, bear, tc.want)
			if tc.equipment == "Sword of Fire and Ice" {
				// The grant is not just a string: the engine's protection
				// predicate must see it. Hill Giant is red — blocked;
				// the bearer itself is green — not.
				giant := findOnBoard(t, e, 1, "Hill Giant")
				if !e.protectedFrom(bear, giant) {
					t.Error("bearer with Protection from red is not protected from a red source")
				}
				if e.protectedFrom(bear, bear) {
					t.Error("bearer with Protection from red reads as protected from a green source")
				}
			}
			if tc.equipment == "Basilisk Collar" {
				// Deathtouch is really on the bearer's derived set, so the
				// combat gate (rules/combat.go) treats any damage as lethal.
				if !derivedKeywordsRegistered(t, e, bear, "Deathtouch") {
					t.Error("Basilisk Collar's deathtouch grant is not in the bearer's registered keyword set")
				}
			}
		})
	}
}

// derivedKeywordsRegistered reports whether kwHead is among the bearer's
// registered derived keyword heads (rules/combat.go's combat gate reads the
// same set).
func derivedKeywordsRegistered(t *testing.T, e *Engine, id state.ObjID, head string) bool {
	t.Helper()
	d := e.Derived(id)
	for _, k := range d.Keywords {
		if strings.EqualFold(cards.KeywordHead(k), head) {
			return true
		}
	}
	return false
}

// TestBasiliskCollarBearerNonCombatDamageGainsLife pins the second half of
// the defect on real cards: the Collar's bearer's NON-COMBAT damage gains
// its controller life (CR 702.15a's rider rides granted lifelink as surely
// as printed). Prodigal Pyromancer with the Collar pings seat 1 for 1;
// seat 0 gains that 1. Before the fix the bearer's lifelink was the bogus
// joined keyword and nobody gained anything.
func TestBasiliskCollarBearerNonCombatDamageGainsLife(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Basilisk Collar", "Prodigal Pyromancer"}, nil)
	collar := findOnBoard(t, e, 0, "Basilisk Collar")
	pyro := findOnBoard(t, e, 0, "Prodigal Pyromancer")
	e.G.Obj(pyro).SummonSick = false
	equipGrants(t, e, collar, pyro, "CC")

	opt := abilityOption(t, e, pyro, 0)
	submitChoices(t, e, opt.Index)
	targetPlayer(t, e, 1)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Players[0].Life; got != 21 {
		t.Fatalf("seat 0 life = %d, want 21 (20 + 1 collar lifelink)", got)
	}
	if got := e.G.Players[1].Life; got != 19 {
		t.Fatalf("seat 1 life = %d, want 19 (20 - 1 ping)", got)
	}
}

// TestBatterskullGermHasVigilanceAndLifelink casts the real Batterskull: its
// Living Weapon ETB mints a 0/0 Phyrexian Germ and attaches the skull, and
// the skull's "Vigilance & Lifelink" static must reach the germ as two real
// keywords (the +4/+4 static has always applied — the germ's survival was
// never the bug; its keywords were).
func TestBatterskullGermHasVigilanceAndLifelink(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	skull := mustCorpusCard(t, reg, "Batterskull")
	cfg := seatZeroStart(Config{Seed: 9, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{skull}, mountainDeck(t, 59)...),
			mountainDeck(t, 60),
		},
		Tokens: reg.Tokens,
	})
	e := New(cfg)
	e.Advance()
	// Bridge the skull into seat 0's hand when the opening deal buried it, so
	// the cast is offered (newFixtureDeck's bridge, applied to a real card).
	found := false
	for _, cand := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(cand).Face().Name == "Batterskull" {
			found = true
		}
	}
	if !found {
		for _, cand := range e.G.Zone(state.ZLibrary, 0) {
			if e.G.Obj(cand).Face().Name == "Batterskull" {
				e.emit(events.Event{Kind: events.MoveZone, Obj: cand, From: state.ZLibrary, To: state.ZHand})
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("Batterskull not found in hand or library")
	}
	e.pending = nil
	e.Advance()
	addMana(t, e, 0, "GGGGG")
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 30)
	var germ state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken {
			germ = id
		}
	}
	if germ == 0 || e.G.Obj(germ).Zone != state.ZBattlefield {
		t.Fatal("Batterskull's germ token is not on the battlefield")
	}
	if e.G.Obj(skullID(e, "Batterskull")).AttachedTo != germ {
		t.Fatalf("Batterskull attached to %d, want germ %d", e.G.Obj(skullID(e, "Batterskull")).AttachedTo, germ)
	}
	if p := e.Power(germ); p != 4 {
		t.Fatalf("germ power = %d, want 4 (0 + Batterskull's AddPower$ 4)", p)
	}
	assertGrants(t, e, germ, []string{"Vigilance", "Lifelink"})
	replayCheck(t, e, cfg)
}

// skullID finds seat 0's battlefield Batterskull.
func skullID(e *Engine, name string) state.ObjID {
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	return 0
}

// TestKeywordListReadersShareTheParser enumerates the implemented readers of
// Forge keyword-list parameters. The parser itself, including the two
// comma-bearing corpus forms, is pinned in cards.TestSplitKeywordList.
//
//  1. rules.statKeywords — the S: static AddKeyword$ path (rules/layers.go).
//  2. rules.grantKeywords — the pure keyword-grant KW$ path feeding
//     decision.Grant (rules/legal.go).
//  3. effects Pump and PumpAll — both have no KW$ read of their own: they
//     pass their SA to registerPumpEffects, whose sole read calls
//     cards.SplitKeywordList. effects.TestPumpKeywordListReadersUseTheSharedParser
//     executes both paths directly.
//  4. effects GainControl — its AddKWs$ read calls cards.SplitKeywordList and
//     effects.TestGainControlKeywordListReaderUsesSharedParser executes it.
func TestKeywordListReadersShareTheParser(t *testing.T) {
	t.Parallel()
	// 1. The static keyword path must divide the Forge ampersand list.
	st := cards.Static{Mode: "Continuous", Params: map[string]string{
		"AddKeyword": "Vigilance & Lifelink",
	}}
	if got := statKeywords(st); len(got) != 2 || got[0] != "Vigilance" || got[1] != "Lifelink" {
		t.Errorf("statKeywords = %v, want [Vigilance Lifelink]", got)
	}

	// 2. The pure-grant path (grantKeywords feeds decision.Grant).
	if got := grantKeywords("Deathtouch & Lifelink"); len(got) != 2 || got[0] != "Deathtouch" || got[1] != "Lifelink" {
		t.Errorf("grantKeywords = %v", got)
	}
}
