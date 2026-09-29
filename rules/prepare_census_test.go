package rules

// Prepare-mode modal pair census (ticket agent-20260928T214048Z-a90202e9).
//
// Forge encodes the Secrets of Strixhaven "prepared spell" pairs with a
// card-level `AlternateMode:Prepare`: a permanent front (which enters
// prepared and may cast a copy of its back-face spell while prepared) and a
// spell back. rules/legal.go's modalSpellBack now treats a Prepare pair like
// a Modal DFC (modalSpellFacePair), so the back-face spell is offered from
// hand with its own cost, timing, targets and restriction and resolves to the
// graveyard (CR 712.3a).
//
// The census below is the class-wide ledger for that shape. It pins the exact
// set of Prepare cards in the corpus, in both directions, and it pins the two
// sub-classes the offer has to distinguish:
//
//   - inline backs: the ALTERNATE block names a real Instant/Sorcery face, so
//     modalSpellBack must return it (33 corpus cards today);
//   - `CopyFaceFrom` stubs: the ALTERNATE block is only a directive this
//     parser does not resolve, so the back face parses nameless with no types
//     or cost. modalSpellBack must return nil for those, or the offer would
//     put a free, empty-named cast on the stack (21 corpus cards today;
//     resolving CopyFaceFrom is a separate parser ticket, reported in the
//     ticket report's Issues section).
//
// A counted census is a ratchet: a Prepare card added to the corpus, an inline
// back turning into a stub (or the reverse), or the helper's classification
// drifting fails here and names the exact card.

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// prepareCensus is the measured corpus set of AlternateMode:Prepare cards, as
// "Front // Back" for an inline back or bare "Front" for an unresolved
// CopyFaceFrom stub. Sorted. Measured 2026-09-28 at FORGE_REF
// 95f04e8a04c8925fa97cb226fc3341cabcc90a53.
var prepareCensus = []string{
	"Abigale, Poet Laureate // Heroic Stanza",
	"Adventurous Eater // Have a Bite",
	"Blazing Firesinger",
	"Bloodline Recollector // Ancestral Craving",
	"Campus Composer // Aqueous Aria",
	"Cheerful Osteomancer",
	"Crescendo Conductor",
	"Defacing Duskmage // Vandal's Edit",
	"Dirgur Focusmage",
	"Eccentric Pestfinder // Turn Stones",
	"Eiganjo Dynastorian",
	"Elite Interceptor // Rejoinder",
	"Emeritus of Abundance",
	"Emeritus of Conflict",
	"Emeritus of Ideation",
	"Emeritus of Truce",
	"Emeritus of Woe",
	"Encouraging Aviator",
	"Galathul Galecaller // Corvid Squall",
	"Goblin Glasswright // Craft with Pride",
	"Grave Researcher",
	"Harmonized Trio",
	"Honorbound Page // Forum's Favor",
	"Infirmary Healer",
	"Inspired Skypainter // Maestro's Gift",
	"Jadzi, Steward of Fate // Oracle's Gift",
	"Joined Researchers",
	"Kirol, History Buff // Pack a Punch",
	"Landscape Painter // Vibrant Idea",
	"Leech Collector // Bloodletting",
	"Lluwen, Exchange Student // Pest Friend",
	"Lorehold Archivist // Restore Relic",
	"Maelstrom Artisan // Rocket Volley",
	"Naktamun Lorespinner",
	"Paradox Shaper // Omit Variables",
	"Pigment Wrangler // Striking Palette",
	"Prudent Fateseer // Peer Review",
	"Quill-Blade Laureate // Twofold Intent",
	"Sanar, Unfinished Genius // Wild Idea",
	"Scathing Shadelock // Venomous Words",
	"Scheming Silvertongue",
	"Skycoach Conductor // All Aboard",
	"Spellbook Seeker",
	"Spiritcall Enthusiast // Scrollboost",
	"Stensian Sanguinist",
	"Stingerquill Voxmancer // Vicious Verse",
	"Striding Shotcaller // Run the Play",
	"Strife Scholar // Awaken the Ages",
	"Studious First-Year",
	"Tam, Observant Sequencer // Deep Sight",
	"Vastlands Scavenger // Bind to Life",
	"Vigorbloom Vanguard // Seed Suture",
	"Woodwork Prodigy // Soul Tether",
	"Yavimaya Bloomsage",
}

// prepareLabel renders a Prepare card as the census compares it: the front
// name, plus " // <back>" when the back face is a resolved, named face.
func prepareLabel(c *cards.Card) string {
	if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
		return "<empty>"
	}
	front := c.Faces[0].Name
	if len(c.Faces) == 2 && c.Faces[1] != nil && c.Faces[1].Name != "" {
		return front + " // " + c.Faces[1].Name
	}
	return front
}

// TestPrepareCensusMatchesCorpus pins the exact AlternateMode:Prepare set in
// the corpus in both directions: a card the build newly sees is a finding
// until it is classified here, and a pinned card the corpus no longer carries
// is a stale entry.
func TestPrepareCensusMatchesCorpus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	var measured []string
	for _, c := range reg.Cards {
		if c != nil && c.AlternateMode == "Prepare" {
			measured = append(measured, prepareLabel(c))
		}
	}
	sort.Strings(measured)

	want := append([]string(nil), prepareCensus...)
	sort.Strings(want)

	if len(measured) != 54 {
		t.Errorf("prepare census: %d AlternateMode:Prepare cards in the corpus, want 54", len(measured))
	}

	inWant := map[string]bool{}
	for _, w := range want {
		inWant[w] = true
	}
	inMeasured := map[string]bool{}
	for _, m := range measured {
		inMeasured[m] = true
	}
	for _, m := range measured {
		if !inWant[m] {
			t.Errorf("prepare census: corpus card %q is not in the pinned census (classify it)", m)
		}
	}
	for _, w := range want {
		if !inMeasured[w] {
			t.Errorf("prepare census: pinned %q is no longer an AlternateMode:Prepare corpus card (delete it)", w)
		}
	}
}

// TestPrepareBackFaceHelperCensus pins modalSpellBack's classification over
// every Prepare card: an inline Instant/Sorcery back must be returned (the
// offer's precondition), and an unresolved CopyFaceFrom stub must be rejected
// (otherwise the hand walk would offer a free, empty-named cast). This is the
// class-wide assertion behind TestSetAudit_sos_TamObservantSequencer_CastBackFace;
// it fails for any inline back the fix does not reach, not just Tam.
func TestPrepareBackFaceHelperCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	var inline, stubs int
	for _, c := range reg.Cards {
		if c == nil || c.AlternateMode != "Prepare" || len(c.Faces) != 2 || c.Faces[0] == nil || c.Faces[1] == nil {
			continue
		}
		o := &state.Object{Card: c, Zone: state.ZHand, FaceIdx: 0}
		got := modalSpellBack(o)
		back := c.Faces[1]
		if back.Name == "" {
			// Unresolved CopyFaceFrom stub: the helper must reject it. If it
			// does not, the hand walk offers "Cast " with a free cost.
			stubs++
			if got != nil {
				t.Errorf("prepare helper: %q stub back %+v was returned by modalSpellBack, want nil",
					c.Faces[0].Name, back)
			}
			continue
		}
		// Inline back: a real named face, so the helper must return exactly
		// the back face and the card must be a spell half (Instant/Sorcery).
		inline++
		if got == nil || got.Name != back.Name {
			t.Errorf("prepare helper: %q back = %+v, want %q", c.Faces[0].Name, got, back.Name)
			continue
		}
		if !back.IsInstant() && !back.IsSorcery() {
			t.Errorf("prepare helper: %q inline back %q is neither Instant nor Sorcery", c.Faces[0].Name, back.Name)
		}
	}
	if inline != 33 || stubs != 21 {
		t.Errorf("prepare helper census: %d inline backs, %d CopyFaceFrom stubs, want 33/21", inline, stubs)
	}
}
