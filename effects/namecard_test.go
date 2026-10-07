package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// namecardUniverse is the fixed three-card universe every test below pins:
// a nonland creature, a nonbasic land and a basic land. It is built from
// inline scripts (never committed Forge data) and linked so the type walk
// reads the printed type line.
func namecardUniverse(t *testing.T) []*cards.Card {
	t.Helper()
	return []*cards.Card{
		mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"),
		mkCard(t, "Name:Wasteland\nTypes:Land\nOracle:x\n"),
		mkCard(t, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"),
	}
}

func namecardGameWithUniverse(t *testing.T) *state.Game {
	t.Helper()
	g := state.NewGame(names(2))
	g.NameUniverse = cards.UniverseOf(namecardUniverse(t))
	return g
}

// TestNameChoicesFiltersBySpec pins the ONE shared builder's filter
// semantics: an empty spec is unrestricted (Pithing Needle names any card,
// a land included), Card.nonLand removes every land (Phyrexian Revoker,
// Cabal Therapy) and a compound spec restricts on both halves.
func TestNameChoicesFiltersBySpec(t *testing.T) {
	g := namecardGameWithUniverse(t)

	all := NameChoices(g, "", "")
	if !containsName(all, "Wasteland") || !containsName(all, "Forest") || !containsName(all, "Bear") {
		t.Fatalf("unrestricted NameChoices = %v, want Bear, Forest and Wasteland", all)
	}
	if len(all) != 3 {
		t.Fatalf("unrestricted NameChoices = %v, want exactly the 3 universe names", all)
	}

	nonland := NameChoices(g, "Card.nonLand", "")
	if containsName(nonland, "Wasteland") || containsName(nonland, "Forest") {
		t.Fatalf("Card.nonLand offered a land: %v", nonland)
	}
	if !containsName(nonland, "Bear") {
		t.Fatalf("Card.nonLand omitted the creature: %v", nonland)
	}

	nonbasic := NameChoices(g, "Card.Land+nonBasic", "")
	if containsName(nonbasic, "Forest") {
		t.Fatalf("Card.Land+nonBasic offered a basic land: %v", nonbasic)
	}
	if !containsName(nonbasic, "Wasteland") {
		t.Fatalf("Card.Land+nonBasic omitted the nonbasic land: %v", nonbasic)
	}
}

// TestNameCardWithoutUniverseKeepsTheR9StandIn pins the no-host degradation
// AND its replay contract: a host with no corpus universe completes
// deterministically by naming the top of the caster's own library -- the exact
// pre-feature NameCard behaviour, so a log an older binary wrote reproduces
// byte for byte (host/persist.go sidecar.NameUniverse). It must never post an
// unanswerable ask.
func TestNameCardWithoutUniverseKeepsTheR9StandIn(t *testing.T) {
	h := newHost(t, 2)
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0).ID
	// The precondition the assertion depends on: a real library top whose
	// name differs from the source's, so "Source" cannot pass by accident.
	lib := h.g.AddObject(mkCard(t, "Name:Grizzly Bears\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0).ID
	h.Emit(events.Event{Kind: events.MoveZone, Obj: lib, From: state.ZHand, To: state.ZLibrary, Player: 0})
	if top := h.g.Zone(state.ZLibrary, 0); len(top) != 1 || h.g.Obj(top[0]).Face().Name != "Grizzly Bears" {
		t.Fatalf("precondition: library top = %v, want [Grizzly Bears]", top)
	}
	Resolve(h, &Ctx{Controller: 0, Source: src}, sa(t, "SP$ NameCard"))
	// The handler ran: the legacy path still records its deterministic name as
	// a Choose event (the ChosenName fold). A test that only checked the
	// absence of an ask would pass with the primitive unregistered.
	if h.g.Obj(src).ChosenName != "Grizzly Bears" {
		t.Fatalf("no-universe NameCard ChosenName = %q, want the library-top legacy stand-in Grizzly Bears", h.g.Obj(src).ChosenName)
	}
	found := false
	for _, ev := range h.log {
		if ev.Kind == events.Choose && ev.Counter == "name" && ev.Text == "Grizzly Bears" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no Choose name event for the legacy stand-in; log=%+v", h.log)
	}
}

// TestNameDescriptionOnlyStaysUnrestrictedButSafetyMaps pins the
// ValidDescription$ semantics the brief names. Forge reads ValidDescription$
// as prompt TEXT, not a filter (ChooseCardNameEffect.resolve; the
// ignoredParamKeys citation), so a description is never a second filter while
// ValidCards$ is present. When ValidCards$ is absent, the builder maps the
// recognised description forms to their predicate as a SAFETY fallback -- it
// only narrows, never widens, so a description-only script is never offered a
// list that contradicts its own wording; an unrecognised description keeps
// Forge's unrestricted "Card" default.
func TestNameDescriptionOnlyStaysUnrestrictedButSafetyMaps(t *testing.T) {
	g := namecardGameWithUniverse(t)

	// Safety map: a recognised description with no ValidCards$ narrows.
	nonland := NameChoices(g, "", "nonland")
	if containsName(nonland, "Wasteland") || containsName(nonland, "Forest") {
		t.Fatalf("ValidDescription$ nonland alone offered a land: %v", nonland)
	}
	if !containsName(nonland, "Bear") {
		t.Fatalf("ValidDescription$ nonland alone omitted the creature: %v", nonland)
	}

	// An UNrecognised description falls back to Forge's unrestricted default.
	all := NameChoices(g, "", "whatever the prompt says")
	if len(all) != 3 {
		t.Fatalf("unrecognised ValidDescription$ narrowed the universe: %v", all)
	}

	// Compound description forms map to evaluable predicates: "nonartifact,
	// nonland" leaves only the nonartifact nonland creature here.
	if got := NameChoices(g, "", "nonartifact, nonland"); len(got) != 1 || !containsName(got, "Bear") {
		t.Fatalf("ValidDescription$ nonartifact, nonland = %v, want [Bear]", got)
	}
	// A description whose predicate matches NOTHING in the universe is the
	// totality case (R-9): the ask is never emptied, it falls back to the
	// full universe. "noncreature, nonland" excludes all three cards here
	// (Bear is a creature, the other two are lands).
	if got := NameChoices(g, "", "noncreature, nonland"); len(got) != 3 {
		t.Fatalf("empty-matching ValidDescription$ = %v, want the totality fallback (3 names)", got)
	}

	// ValidCards$ still wins outright when both are present (Forge filters on
	// ValidCards$ only).
	both := NameChoices(g, "Card.Creature", "nonland")
	if len(both) != 1 || !containsName(both, "Bear") {
		t.Fatalf("ValidCards$ Card.Creature with a differing description = %v, want [Bear]", both)
	}
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func containsLabel(opts []decision.Option, want string) bool {
	for _, o := range opts {
		if o.Label == want {
			return true
		}
	}
	return false
}

// TestNameChoicesUnrestrictedPrefersThePersistedSnapshot closes the sol3
// MINOR: the snapshot preference in nameUniverseSnapshot was only covered on
// the FILTERED path, where the persisted list also acts as an allowlist. The
// UNRESTRICTED path -- Pithing Needle, the brief's headline card, which
// carries no ValidCards$ -- returns the snapshot directly, and nothing pinned
// that. Without the preference a corpus ADDITION that sorts before an
// existing name renumbers every later option, so a persisted numeric answer
// replays as a different card name.
//
// The probe is the same shape the host replay test uses: a "!"-prefixed name
// is forced to sort first in the current corpus, while the persisted
// NameUniverseNames still holds the list the match offered.
func TestNameChoicesUnrestrictedPrefersThePersistedSnapshot(t *testing.T) {
	g := namecardGameWithUniverse(t)
	pinned := NameChoices(g, "", "")
	g.NameUniverseNames = append([]string(nil), pinned...)

	// The corpus grows after the match was recorded.
	g.NameUniverse = cards.UniverseOf(append(universeCards(g.NameUniverse), mkCard(t, "Name:!Newcomer\nTypes:Creature\nPT:1/1\nOracle:x\n")))

	got := NameChoices(g, "", "")
	if len(got) != len(pinned) {
		t.Fatalf("unrestricted NameChoices returned %d names (%v) after a corpus addition, want the persisted %d (%v)", len(got), got, len(pinned), pinned)
	}
	for i := range pinned {
		if got[i] != pinned[i] {
			t.Fatalf("option %d = %q, persisted %q: a corpus addition renumbered a recorded name choice", i, got[i], pinned[i])
		}
	}
	if containsName(got, "!Newcomer") {
		t.Fatal("unrestricted NameChoices offered a card added after the match was recorded")
	}

	// With no persisted list (a live match) the same call DOES see the
	// addition -- the preference is a replay pin, not a corpus freeze.
	g.NameUniverseNames = nil
	if live := NameChoices(g, "", ""); !containsName(live, "!Newcomer") {
		t.Fatalf("a live (unpinned) unrestricted NameChoices missed the new card: %v", live)
	}
}

// universeCards lists u's cards in order.
func universeCards(u *cards.Universe) []*cards.Card {
	out := make([]*cards.Card, u.Len())
	for i := range out {
		out[i] = u.Card(i)
	}
	return out
}
