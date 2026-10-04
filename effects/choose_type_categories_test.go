package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// These tests pin task ct1's non-creature ChooseType option lists: every
// Type$ category this build can enumerate now offers its REAL list through
// the existing "choosetype" ask, instead of a loud Note and a
// nonsensical creature-type fallback. Each test asserts its own precondition
// (the objects exist where the rule reads them; the option list actually
// differs from a creature list) so a vacuous setup fails loudly.

// categoryAsk drives one resolution-time ChooseType with the given Type$/extra
// params and returns the single posed decision, failing if the ask did not
// happen.
func categoryAsk(t *testing.T, h *chooseTypeHost, src state.ObjID, c *Ctx, line string) *decision.Decision {
	t.Helper()
	Resolve(h, c, sa(t, line))
	if len(h.asks) != 1 {
		t.Fatalf("expected exactly one posed ask for %q, got %d", line, len(h.asks))
	}
	return h.asks[0]
}

// optionsAre compares the posed options' labels to want exactly.
func optionsAre(t *testing.T, got []decision.Option, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("option count = %d (%v), want %d (%v)", len(got), optionLabelsOf(got), len(want), want)
	}
	for i := range want {
		if got[i].Kind != "type" || got[i].Label != want[i] {
			t.Fatalf("option %d = %+v, want kind \"type\" label %q", i, got[i], want[i])
		}
	}
}

func optionLabelsOf(opts []decision.Option) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		out = append(out, o.Label)
	}
	return out
}

// hasNotePrefix reports whether the resolution logged a Note with the given
// prefix.
func hasNotePrefix(h *fakeHost, prefix string) bool {
	for _, e := range h.log {
		if e.Kind == events.Note && len(e.Text) >= len(prefix) && e.Text[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// TestChooseTypeLandOffersBasicAndNonbasicLandTypes pins Type$ Land (Vision
// Charm, Barbarian Guides, Shimmer): the union of the basic and nonbasic land
// types, so both Plains and Desert are offerable.
func TestChooseTypeLandOffersBasicAndNonbasicLandTypes(t *testing.T) {
	h := &chooseTypeHost{}
	h.g = state.NewGame(names(2))
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Land\nOracle:x\n"), 0).ID
	d := categoryAsk(t, h, src, &Ctx{Source: src, Controller: 0},
		"SP$ ChooseType | Defined$ You | Type$ Land")
	labels := optionLabelsOf(d.Options)
	has := func(s string) bool {
		for _, l := range labels {
			if l == s {
				return true
			}
		}
		return false
	}
	if !has("Plains") || !has("Desert") || !has("Urza's") {
		t.Fatalf("Type$ Land list %v must carry both basic (Plains) and nonbasic (Desert, Urza's) land types", labels)
	}
	if !has("Island") || !has("Forest") || !has("Swamp") || !has("Mountain") {
		t.Fatalf("Type$ Land list %v is missing a basic land type", labels)
	}
}

// TestChooseTypeNonbasicLandExcludesTheBasicTypes pins Type$ Nonbasic Land
// (March from Velis Vel): a nonbasic land type is offerable, a basic one is
// not.
func TestChooseTypeNonbasicLandExcludesTheBasicTypes(t *testing.T) {
	h := &chooseTypeHost{}
	h.g = state.NewGame(names(2))
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Land\nOracle:x\n"), 0).ID
	d := categoryAsk(t, h, src, &Ctx{Source: src, Controller: 0},
		"SP$ ChooseType | Defined$ You | Type$ Nonbasic Land")
	for _, o := range d.Options {
		switch o.Label {
		case "Plains", "Island", "Swamp", "Mountain", "Forest":
			t.Fatalf("nonbasic land list offered the basic type %q: %v", o.Label, optionLabelsOf(d.Options))
		}
	}
	if len(d.Options) == 0 || d.Options[0].Label != "Cave" {
		t.Fatalf("nonbasic land list = %v, want the sorted nonbasic types starting Cave", optionLabelsOf(d.Options))
	}
}

// TestChooseTypeCardFiltersByValidTypes pins Type$ Card | ValidTypes$
// Artifact,Creature,Land (Turnabout's tap/untap all): the ask offers exactly
// the named card types, not the whole vocabulary.
func TestChooseTypeCardFiltersByValidTypes(t *testing.T) {
	h := &chooseTypeHost{}
	h.g = state.NewGame(names(2))
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0).ID
	d := categoryAsk(t, h, src, &Ctx{Source: src, Controller: 0},
		"SP$ ChooseType | Defined$ You | Type$ Card | ValidTypes$ Artifact,Creature,Land")
	optionsAre(t, d.Options, "Artifact", "Creature", "Land")
	if d.Prompt != "Choose a card type" {
		t.Fatalf("prompt = %q, want the card-type ask", d.Prompt)
	}
}

// TestChooseTypeCardExpandsNonlandAndAppliesInvalidTypes pins the two filter
// shapes the corpus uses on Type$ Card: ValidTypes$ Land,Nonland (Gollum,
// Scheming Guide; Jukai Liberator) expands Nonland to every nonland card
// type, and InvalidTypes$ Instant,Sorcery,Kindred (Creeping Renaissance)
// removes named types.
func TestChooseTypeCardExpandsNonlandAndAppliesInvalidTypes(t *testing.T) {
	h := &chooseTypeHost{}
	h.g = state.NewGame(names(2))
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0).ID
	d := categoryAsk(t, h, src, &Ctx{Source: src, Controller: 0},
		"SP$ ChooseType | Defined$ You | Type$ Card | ValidTypes$ Land,Nonland | InvalidTypes$ Kindred,Sorcery")
	labels := optionLabelsOf(d.Options)
	has := func(s string) bool {
		for _, l := range labels {
			if l == s {
				return true
			}
		}
		return false
	}
	if !has("Land") || !has("Artifact") || !has("Creature") {
		t.Fatalf("Land,Nonland list %v must carry Land and the nonland types", labels)
	}
	if has("Kindred") || has("Sorcery") {
		t.Fatalf("InvalidTypes$ removal failed: %v", labels)
	}
}

// TestChooseTypePlaneswalkerOffersPlaneswalkerTypes pins Type$ Planeswalker
// (Deification, Leori Sparktouched Hunter): the real planeswalker-subtype
// vocabulary, never a creature type.
func TestChooseTypePlaneswalkerOffersPlaneswalkerTypes(t *testing.T) {
	h := &chooseTypeHost{}
	h.g = state.NewGame(names(2))
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Enchantment\nOracle:x\n"), 0).ID
	d := categoryAsk(t, h, src, &Ctx{Source: src, Controller: 0},
		"SP$ ChooseType | Defined$ You | Type$ Planeswalker")
	labels := optionLabelsOf(d.Options)
	has := func(s string) bool {
		for _, l := range labels {
			if l == s {
				return true
			}
		}
		return false
	}
	if !has("Jace") || !has("Chandra") {
		t.Fatalf("planeswalker list %v is missing Jace/Chandra", labels)
	}
	if has("Human") || has("Goblin") {
		t.Fatalf("planeswalker list leaked a creature type: %v", labels)
	}
	if d.Prompt != "Choose a planeswalker type" {
		t.Fatalf("prompt = %q, want the planeswalker-type ask", d.Prompt)
	}
}

// TestChooseTypeSharedOffersTypesSharedByExiledObjects pins Type$ Shared |
// TypesFromDefined$ ExiledWith (Apex Observatory): the option list is the
// CARD types shared by every object exiled with the source -- here two
// artifact creatures sharing Artifact and Creature (the creature subtype
// Golem is not a card type) -- even though both also share the Creature
// subtype.
func TestChooseTypeSharedOffersTypesSharedByExiledObjects(t *testing.T) {
	h := &chooseTypeHost{}
	h.g = state.NewGame(names(2))
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Artifact\nOracle:x\n"), 0)
	srcID := src.ID
	// Precondition: exactly two objects exiled with the source, and they
	// share at least one type word.
	aID := h.g.AddObject(mkCard(t, "Name:A\nTypes:Artifact Creature Golem\nPT:1/1\nOracle:x\n"), 0).ID
	bID := h.g.AddObject(mkCard(t, "Name:B\nTypes:Artifact Creature Golem\nPT:2/2\nOracle:x\n"), 0).ID
	h.g.Obj(aID).ExiledWith = srcID
	h.g.Obj(bID).ExiledWith = srcID
	if h.g.Obj(aID).ExiledWith != srcID || h.g.Obj(bID).ExiledWith != srcID {
		t.Fatalf("setup: exiled-with association not recorded")
	}
	d := categoryAsk(t, h, srcID, &Ctx{Source: srcID, Controller: 0},
		"SP$ ChooseType | Type$ Shared | TypesFromDefined$ ExiledWith")
	optionsAre(t, d.Options, "Artifact", "Creature")
}

// TestChooseTypeCreatureInTargetedDeckOffersDeckCreatureTypes pins Type$
// CreatureInTargetedDeck (Aswan Jaguar): the option list is the creature
// subtypes present in the targeted opponent's library.
func TestChooseTypeCreatureInTargetedDeckOffersDeckCreatureTypes(t *testing.T) {
	h := &chooseTypeHost{}
	h.g = state.NewGame(names(2))
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Creature Cat\nPT:2/2\nOracle:x\n"), 0).ID
	// Precondition: the OPPONENT (seat 1) owns the creatures, and seat 1 has
	// no other creature subtypes, so the list can only come from the deck.
	elfID := h.g.AddObject(mkCard(t, "Name:Elf\nTypes:Creature Elf\nPT:1/1\nOracle:x\n"), 1).ID
	goblinID := h.g.AddObject(mkCard(t, "Name:Goblin\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n"), 1).ID
	h.g.SetZone(state.ZLibrary, 1, []state.ObjID{elfID, goblinID})
	if got := len(h.g.Zone(state.ZLibrary, 1)); got != 2 {
		t.Fatalf("setup: opponent library has %d cards, want the 2 creatures", got)
	}
	d := categoryAsk(t, h, src, &Ctx{Source: src, Controller: 0,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}},
		"SP$ ChooseType | Type$ CreatureInTargetedDeck")
	optionsAre(t, d.Options, "Elf", "Goblin")
}
