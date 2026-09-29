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
// set of Prepare cards in the corpus, in both directions, and it pins the
// class-wide refusal a hand-walk gate would otherwise have to reproduce:
// every one of the 54 backs must be refused by modalSpellBack (CR 722.3), so
// none of them is castable from hand.
//
// 33 of the 54 cards name a real Instant/Sorcery face inline in their
// ALTERNATE block. The other 21 encode their inset spell with a
// `CopyFaceFrom:<Card>` directive, which cards/parse.go now resolves by
// copying the referenced card's front-face characteristics onto the stub
// (ticket agent-20260928T215303Z-ab37989c); after resolution every back is a
// named face too, so the census compares all 54 as "Front // Back".
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
// "Front // Back". Sorted. Measured 2026-09-28 at FORGE_REF
// 95f04e8a04c8925fa97cb226fc3341cabcc90a53; every back is now a resolved
// named face (the 21 CopyFaceFrom stubs resolve as of
// agent-20260928T215303Z-ab37989c).
var prepareCensus = []string{
	"Abigale, Poet Laureate // Heroic Stanza",
	"Adventurous Eater // Have a Bite",
	"Blazing Firesinger // Seething Song",
	"Bloodline Recollector // Ancestral Craving",
	"Campus Composer // Aqueous Aria",
	"Cheerful Osteomancer // Raise Dead",
	"Crescendo Conductor // Boltwave",
	"Defacing Duskmage // Vandal's Edit",
	"Dirgur Focusmage // Braingeyser",
	"Eccentric Pestfinder // Turn Stones",
	"Eiganjo Dynastorian // Replenish",
	"Elite Interceptor // Rejoinder",
	"Emeritus of Abundance // Regrowth",
	"Emeritus of Conflict // Lightning Bolt",
	"Emeritus of Ideation // Ancestral Recall",
	"Emeritus of Truce // Swords to Plowshares",
	"Emeritus of Woe // Demonic Tutor",
	"Encouraging Aviator // Jump",
	"Galathul Galecaller // Corvid Squall",
	"Goblin Glasswright // Craft with Pride",
	"Grave Researcher // Reanimate",
	"Harmonized Trio // Brainstorm",
	"Honorbound Page // Forum's Favor",
	"Infirmary Healer // Stream of Life",
	"Inspired Skypainter // Maestro's Gift",
	"Jadzi, Steward of Fate // Oracle's Gift",
	"Joined Researchers // Secret Rendezvous",
	"Kirol, History Buff // Pack a Punch",
	"Landscape Painter // Vibrant Idea",
	"Leech Collector // Bloodletting",
	"Lluwen, Exchange Student // Pest Friend",
	"Lorehold Archivist // Restore Relic",
	"Maelstrom Artisan // Rocket Volley",
	"Naktamun Lorespinner // Wheel of Fortune",
	"Paradox Shaper // Omit Variables",
	"Pigment Wrangler // Striking Palette",
	"Prudent Fateseer // Peer Review",
	"Quill-Blade Laureate // Twofold Intent",
	"Sanar, Unfinished Genius // Wild Idea",
	"Scathing Shadelock // Venomous Words",
	"Scheming Silvertongue // Sign in Blood",
	"Skycoach Conductor // All Aboard",
	"Spellbook Seeker // Careful Study",
	"Spiritcall Enthusiast // Scrollboost",
	"Stensian Sanguinist // Exsanguinate",
	"Stingerquill Voxmancer // Vicious Verse",
	"Striding Shotcaller // Run the Play",
	"Strife Scholar // Awaken the Ages",
	"Studious First-Year // Rampant Growth",
	"Tam, Observant Sequencer // Deep Sight",
	"Vastlands Scavenger // Bind to Life",
	"Vigorbloom Vanguard // Seed Suture",
	"Woodwork Prodigy // Soul Tether",
	"Yavimaya Bloomsage // Channel",
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
// hand, so the helper must return nil for all 54 backs. It also pins the
// parser resolution of the 21 `CopyFaceFrom` stub backs (ticket
// agent-20260928T215303Z-ab37989c): after resolution every back is a real,
// named Instant/Sorcery face, never a nameless stub. This is the class-wide
// assertion behind
// TestSetAudit_sos_TamObservantSequencer_PrepareSpellNotCastableFromHand; it
// fails for any prepare spell the hand walk would wrongly offer, not just Tam.
func TestPrepareBackFaceHelperCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	var named int
	for _, c := range reg.Cards {
		if c == nil || c.AlternateMode != "Prepare" || len(c.Faces) != 2 || c.Faces[0] == nil || c.Faces[1] == nil {
			continue
		}
		o := &state.Object{Card: c, Zone: state.ZHand, FaceIdx: 0}
		got := modalSpellBack(o)
		back := c.Faces[1]
		// Every back must now be a named Instant/Sorcery: the 21 CopyFaceFrom
		// stubs resolve to the referenced spell's characteristics. A nameless
		// back means the resolution pass missed a reference.
		if back.Name == "" {
			t.Errorf("prepare helper: %q back parsed nameless, want a resolved CopyFaceFrom name", c.Faces[0].Name)
			continue
		}
		named++
		if !back.IsInstant() && !back.IsSorcery() {
			t.Errorf("prepare helper: %q back %q is neither Instant nor Sorcery", c.Faces[0].Name, back.Name)
		}
		if got != nil {
			t.Errorf("prepare helper: %q back %+v was returned by modalSpellBack, want nil (CR 722.3)",
				c.Faces[0].Name, back)
		}
	}
	if named != 54 {
		t.Errorf("prepare helper census: %d resolved named backs, want 54", named)
	}
}
