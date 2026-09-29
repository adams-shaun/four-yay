package main

import (
	"testing"
	"time"
)

// TestManabrewCardTextServesTheBackFaceOfADFCByItsOwnName pins Q9's rule 5
// (Oracle text, matched by face) end to end: the CardText seam returns the
// SAME per-face text a client's /cards/named lookup would, keyed by the
// exact back-face name a DFC's redacted CardView carries after it
// transforms.
func TestManabrewCardTextServesTheBackFaceOfADFCByItsOwnName(t *testing.T) {
	ac, hits := factsFixture(t, map[string]scryNamed{
		"Insectile Aberration": {
			Name: "Delver of Secrets // Insectile Aberration",
			ImageURIs: struct {
				Normal string `json:"normal"`
			}{Normal: "/img/delver.jpg"},
			CardFaces: []scryFace{
				{Name: "Delver of Secrets", OracleText: "At the beginning of your upkeep..."},
				{Name: "Insectile Aberration", OracleText: "Flying"},
			},
		},
	})
	ct := newManabrewCardText(ac)

	// Cold: a miss, never blocking (the fixture server is reachable, so if
	// this were a synchronous fetch it would still return promptly, but the
	// point under test is that the FIRST call never observes the fetched
	// text -- only a queued background fill can produce it).
	if text, ok := ct.Text("Insectile Aberration"); ok || text != "" {
		t.Fatalf("cold lookup: want (\"\", false), got (%q, %v)", text, ok)
	}

	// Wait for the queued background fill to settle the sidecar on disk,
	// then the seam must read the BACK face's text, not the front face's.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if text, ok := ct.Text("Insectile Aberration"); ok {
			if text != "Flying" {
				t.Fatalf("back face text = %q, want %q", text, "Flying")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background fill never settled the sidecar")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if hits.Load() != 1 {
		t.Fatalf("want exactly 1 Scryfall named hit, got %d", hits.Load())
	}

	// The FRONT face's own name is a separate cache key: still a miss until
	// it, too, is fetched (by name, not by association with its sibling).
	if text, ok := ct.Text("Delver of Secrets"); ok {
		t.Fatalf("front face by its own name: want a miss before its own fetch, got (%q, %v)", text, ok)
	}
}

// TestManabrewCardTextMissNeverBlocksAndIsNotRefetched pins Q9's rules 3
// and 4: a name Scryfall does not know is a known miss (never re-queued)
// and Text never touches the network or a clock synchronously -- it always
// returns immediately.
func TestManabrewCardTextMissNeverBlocksAndIsNotRefetched(t *testing.T) {
	ac, hits := factsFixture(t, map[string]scryNamed{})
	ct := newManabrewCardText(ac)

	start := time.Now()
	text, ok := ct.Text("Not A Real Card")
	if ok || text != "" {
		t.Fatalf("unknown name: want (\"\", false), got (%q, %v)", text, ok)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("Text blocked for %s; must return immediately", elapsed)
	}

	// Give the queued background fetch time to record the miss on disk.
	deadline := time.Now().Add(5 * time.Second)
	for !fileExists(ac.missPath(artKey("Not A Real Card"))) {
		if time.Now().After(deadline) {
			t.Fatal("background fetch never recorded the miss")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// A second call, now that the miss is on disk, must not queue another
	// fetch: it takes the fast known-404 path.
	if text, ok := ct.Text("Not A Real Card"); ok || text != "" {
		t.Fatalf("repeated miss: want (\"\", false), got (%q, %v)", text, ok)
	}
	time.Sleep(50 * time.Millisecond) // let any wrongly-queued fetch fire
	if hits.Load() != 1 {
		t.Fatalf("want exactly 1 Scryfall named hit for the repeated miss, got %d", hits.Load())
	}
}

// TestManabrewCardTextNilCacheAndEmptyNameAreMisses pins the defensive
// edges: a nil *manabrewCardText, a seam built over a nil cache, and an
// empty name (which the translator never sends for a real card, but a
// face-down/hidden card's absent name must never reach a lookup) are all
// misses, never a panic and never a Scryfall request.
func TestManabrewCardTextNilCacheAndEmptyNameAreMisses(t *testing.T) {
	var nilSeam *manabrewCardText
	if text, ok := nilSeam.Text("Anything"); ok || text != "" {
		t.Fatalf("nil seam: want (\"\", false), got (%q, %v)", text, ok)
	}

	emptyCache := newManabrewCardText(nil)
	if text, ok := emptyCache.Text("Anything"); ok || text != "" {
		t.Fatalf("nil art cache: want (\"\", false), got (%q, %v)", text, ok)
	}

	ac, hits := factsFixture(t, map[string]scryNamed{
		"Goblin Guide": {Name: "Goblin Guide", OracleText: "Haste"},
	})
	ct := newManabrewCardText(ac)
	if text, ok := ct.Text(""); ok || text != "" {
		t.Fatalf("empty name: want (\"\", false), got (%q, %v)", text, ok)
	}
	time.Sleep(50 * time.Millisecond)
	if hits.Load() != 0 {
		t.Fatalf("empty name must never reach Scryfall, got %d hits", hits.Load())
	}
}

// TestManabrewCardTextReadsAnExistingSidecarWithNoFetch pins the pure-read
// hit path used once the prewarm or a prior fetch has already written the
// sidecar: no Scryfall server is even reachable here, so any code path that
// tried to fetch would fail loudly instead of silently succeeding.
func TestManabrewCardTextReadsAnExistingSidecarWithNoFetch(t *testing.T) {
	dir := t.TempDir()
	ac, err := newArtCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	ac.namedBaseURL = "http://127.0.0.1:1/unreachable?exact=" // never reachable
	key := artKey("Llanowar Elves")
	if err := ac.writeFacts(key, cardFacts{Name: "Llanowar Elves", OracleText: "{T}: Add {G}."}); err != nil {
		t.Fatal(err)
	}
	ct := newManabrewCardText(ac)
	text, ok := ct.Text("Llanowar Elves")
	if !ok || text != "{T}: Add {G}." {
		t.Fatalf("existing sidecar: got (%q, %v), want (\"{T}: Add {G}.\", true)", text, ok)
	}
}
