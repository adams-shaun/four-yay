package rules

// The Card.CastSa Spell.Warp and Card.CastSa Spell.Mayhem cast-provenance
// predicates (task mayplay-warp). Both are alternative-cast permissions, so
// both read the cast's pay-time CastInfo CastFlags (state.FlagWarped /
// state.FlagMayhem) and must be admitted at every site the ValidLKI row
// names: rules' castSaAdmits, the layer Affected$ match (matchesWithTypes),
// Count$ThisTurnCast_ (spellsCastThisTurnMatching) and a ValidLKI$
// replacement (replacementMatches).
//
// The carriers are the real corpus cards that print the tokens:
//   - full_bore.txt: `ConditionPresent$ Card.CastSa Spell.Warp`
//   - sandmans_quicksand.txt: `ConditionPresent$ Card.CastSa Spell.Mayhem`
//
// The brief's premise that Spell.Mayhem still fails closed was measured
// FALSE: it was closed by the earlier kw:Mayhem work (commits 5d952df6,
// db87a8f1) and is asserted here so a future regression of the flag path
// fails this test rather than the mayhem-specific one.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// castSaCarrier is one corpus card's token and the flag its cast stamps.
type castSaCarrier struct {
	name  string
	token string
	flag  uint64
	// param is the card parameter carrying the token, asserted in the
	// precondition so a corpus rename cannot silently make the test vacuous.
	param string
}

// TestCastSaWarpAndMayhemProvenanceAllSites drives both flag predicates
// through all four match sites the ValidLKI row names.
func TestCastSaWarpAndMayhemProvenanceAllSites(t *testing.T) {
	t.Parallel()
	carriers := []castSaCarrier{
		{name: "Full Bore", token: "CastSa Spell.Warp", flag: state.FlagWarped, param: "ConditionPresent"},
		{name: "Sandman's Quicksand", token: "CastSa Spell.Mayhem", flag: state.FlagMayhem, param: "ConditionPresent"},
	}
	reg := testutil.CorpusRegistry(t)

	e, _, _ := newFixtureDeck(t, 20260923, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	// Precondition: the two provenance bits under test must differ, or the
	// negative cases below would prove nothing.
	if state.FlagWarped == state.FlagMayhem {
		t.Fatal("precondition: FlagWarped and FlagMayhem must be distinct bits")
	}

	for _, c := range carriers {
		c := c
		t.Run(c.name, func(t *testing.T) {
			card, ok := reg.Lookup(c.name)
			if !ok {
				t.Fatalf("%s missing from corpus", c.name)
			}
			if len(card.Faces) == 0 {
				t.Fatalf("%s has no face", c.name)
			}
			// Precondition: the real corpus carrier must actually print the
			// token this test evaluates, or the test would pass on a card
			// that never exercises the predicate.
			if !faceCarriesParamToken(card.Faces[0], c.param, c.token) {
				t.Fatalf("precondition: %s does not carry %q in %s: %+v", c.name, c.token, c.param, card.Faces[0])
			}

			spell := e.G.AddObject(card, 0)
			spell.Zone = state.ZStack
			e.G.SetZone(state.ZStack, 0, []state.ObjID{spell.ID})
			e.emit(events.Event{Kind: events.PutOnStack, Obj: spell.ID,
				From: state.ZHand, To: state.ZStack, Player: 0})
			e.emit(events.Event{Kind: events.CastInfo, Obj: spell.ID,
				Counter: events.FlagsString(c.flag)})

			// Precondition: the pay-time CastInfo landed as CastFlags and
			// carries the bit under test (and not the sibling's).
			if spell.Zone != state.ZStack {
				t.Fatalf("precondition: spell zone = %v, want stack", spell.Zone)
			}
			if spell.CastFlags&c.flag == 0 {
				t.Fatalf("precondition: spell CastFlags = %#x, want bit %#x", spell.CastFlags, c.flag)
			}
			otherFlag := state.FlagMayhem
			if c.flag == state.FlagMayhem {
				otherFlag = state.FlagWarped
			}
			if spell.CastFlags&otherFlag != 0 {
				t.Fatalf("precondition: spell carries the sibling bit %#x too (%#x)", otherFlag, spell.CastFlags)
			}

			spec := "Card." + c.token

			// Site 1: rules' castSaAdmits — the ValidLKI strip.
			admitted, matches := e.castSaAdmits(spec, spell.ID)
			if !matches || admitted == "" || strings.Contains(admitted, c.token) {
				t.Fatalf("castSaAdmits did not resolve the paid cast: admitted=%q matches=%v", admitted, matches)
			}

			// Site 2: the layer Affected$ match.
			ce := ContinuousEffect{Source: spell.ID, Controller: 0,
				Affects: "Card.Self+" + c.token}
			if !e.matchesWithTypes(&ce, spell.ID, nil, state.ZStack) {
				t.Fatalf("Affected$ layer match rejected the paid %s cast", c.token)
			}
			// The same gate must reject a cast without the bit (negative
			// case at this site).
			noFlag := e.G.AddObject(card, 0)
			noFlag.Zone = state.ZStack
			e.G.SetZone(state.ZStack, 0, []state.ObjID{noFlag.ID})
			if noFlag.ID == spell.ID {
				t.Fatal("precondition: negative-case object must differ from the paid one")
			}
			if e.matchesWithTypes(&ce, noFlag.ID, nil, state.ZStack) {
				t.Fatalf("Affected$ layer match admitted an un-paid %s cast", c.token)
			}

			// Site 3: Count$ThisTurnCast_.
			counted := e.spellsCastThisTurnMatching(0, spec, 0)
			if len(counted) != 1 || counted[0] != spell.ID {
				t.Fatalf("Count$ThisTurnCast_ matches = %v, want the paid cast %d", counted, spell.ID)
			}
			if got := len(e.spellsCastThisTurnMatching(0, spec, spell.ID)); got != 0 {
				t.Fatalf("Count$ThisTurnCast_ excluded count = %d, want 0", got)
			}

			// Site 4: a ValidLKI$ replacement.
			move := events.Event{Kind: events.MoveZone, Obj: spell.ID,
				From: state.ZStack, To: state.ZGraveyard}
			repl := cards.Repl{Event: "Moved", Params: map[string]string{
				"Origin": "Stack", "Destination": "Graveyard", "ValidLKI": spec,
			}}
			if !e.replacementMatches(repl, spell.ID, move) {
				t.Fatalf("ValidLKI replacement did not admit the paid %s cast", c.token)
			}
			// Negative case at this site: clear the flag with a fresh
			// CastInfo (the pay-time CastInfo REPLACES the set) and confirm
			// the replacement now rejects. This is the precondition that the
			// two reads differ.
			e.emit(events.Event{Kind: events.CastInfo, Obj: noFlag.ID})
			if noFlag.CastFlags != 0 {
				t.Fatalf("negative-case precondition: CastInfo did not clear flags (%#x)", noFlag.CastFlags)
			}
			noFlagRepl := cards.Repl{Event: "Moved", Params: map[string]string{
				"Origin": "Stack", "Destination": "Graveyard", "ValidLKI": spec,
			}}
			noFlagMove := events.Event{Kind: events.MoveZone, Obj: noFlag.ID,
				From: state.ZStack, To: state.ZGraveyard}
			if e.replacementMatches(noFlagRepl, noFlag.ID, noFlagMove) {
				t.Fatalf("ValidLKI replacement admitted an un-paid %s cast", c.token)
			}

			// Leave the shared engine's stack zone empty for the next
			// subtest's own additive object.
			e.G.SetZone(state.ZStack, 0, nil)
		})
	}
}

// faceCarriesParamToken reports whether any ability, trigger, static or
// SVar of f carries token in param, or whether any SVar body contains it
// (the corpus stores Full Bore's DB ability as an SVar string).
func faceCarriesParamToken(f *cards.Face, param, token string) bool {
	for _, ab := range f.Abilities {
		if ab != nil && strings.Contains(ab.Params[param], token) {
			return true
		}
	}
	for _, tr := range f.Triggers {
		if strings.Contains(tr.Params[param], token) {
			return true
		}
	}
	for _, st := range f.Statics {
		if strings.Contains(st.Params[param], token) {
			return true
		}
	}
	for _, rp := range f.Repls {
		if strings.Contains(rp.Params[param], token) {
			return true
		}
	}
	for _, body := range f.SVars {
		if strings.Contains(body, token) {
			return true
		}
	}
	return false
}
