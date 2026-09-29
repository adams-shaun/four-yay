package rules

// Prepare-mode modal pair census (ticket agent-20260928T214048Z-a90202e9,
// corrected by agent-20260928T215803Z-ddee4eed).
//
// Forge encodes the Secrets of Strixhaven "prepared spell" pairs with a
// card-level `AlternateMode:Prepare`: a permanent front (which enters
// prepared and may cast a COPY of its back-face spell while prepared) and a
// spell back. A Prepare pair is NOT a modal DFC: CR 722.3 says preparation
// cards "can't be cast using the alternative characteristics found within
// their inset frames", so rules/legal.go's modalSpellBack must NOT offer the
// inset spell from hand. The only way to cast it is CR 722.3c's copy made in
// exile while the front permanent is prepared (rules/legal.go mode
// "prepared_copy", pinned by rules/prepared_test.go).
//
// The census below is the class-wide ledger for that shape. It pins the exact
// set of Prepare cards in the corpus, in both directions, and it pins the two
// sub-classes a hand-walk gate would otherwise have to distinguish:
//
//   - inline backs: the ALTERNATE block names a real Instant/Sorcery face
//     (33 corpus cards today);
//   - `CopyFaceFrom` stubs: the ALTERNATE block is only a directive this
//     parser does not resolve, so the back face parses nameless with no types
//     or cost (21 corpus cards today; resolving CopyFaceFrom is a separate
//     parser ticket).
//
// BOTH sub-classes must be refused by modalSpellBack (CR 722.3), so the
// 33/21 split is a corpus measurement only -- it does not imply any inline
// back is castable from hand.
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
// every Prepare card: CR 722.3 makes the inset prepare spell uncasteable from
// hand in BOTH sub-classes, so the helper must return nil for every inline
// back (33) and every unresolved CopyFaceFrom stub (21). This is the
// class-wide assertion behind
// TestSetAudit_sos_TamObservantSequencer_PrepareSpellNotCastableFromHand; it
// fails for any prepare spell the hand walk would wrongly offer, not just Tam.
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
			// Unresolved CopyFaceFrom stub. If the helper returned it, the hand
			// walk would offer "Cast " with a free cost.
			stubs++
		} else {
			// Inline back: a real named Instant/Sorcery face. CR 722.3 still
			// forbids casting it from hand.
			inline++
			if !back.IsInstant() && !back.IsSorcery() {
				t.Errorf("prepare helper: %q inline back %q is neither Instant nor Sorcery", c.Faces[0].Name, back.Name)
			}
		}
		if got != nil {
			t.Errorf("prepare helper: %q back %+v was returned by modalSpellBack, want nil (CR 722.3)",
				c.Faces[0].Name, back)
		}
	}
	if inline != 33 || stubs != 21 {
		t.Errorf("prepare helper census: %d inline backs, %d CopyFaceFrom stubs, want 33/21", inline, stubs)
	}
}
