package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
)

// factsFixture builds an httptest Scryfall double that answers
// /cards/named with the full named-lookup shape (six printed facts plus
// image_uris), counts named hits, and serves the images. Same shape as
// artFixture, plus the facts.
func factsFixture(t *testing.T, cards map[string]scryNamed) (*artCache, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	imgHits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/cards/named", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		name := r.URL.Query().Get("exact")
		card, ok := cards[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if card.ImageURIs.Normal != "" && !strings.HasPrefix(card.ImageURIs.Normal, "http") {
			card.ImageURIs.Normal = "http://" + r.Host + card.ImageURIs.Normal
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(card)
	})
	mux.HandleFunc("/img/", func(w http.ResponseWriter, r *http.Request) {
		imgHits++
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("fake-jpeg-bytes:" + r.URL.Path))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ac, err := newArtCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ac.client = srv.Client()
	ac.namedBaseURL = srv.URL + "/cards/named?exact="
	return ac, &hits
}

func getJSON(t *testing.T, h http.HandlerFunc, path string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, path, nil))
	var body map[string]any
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decoding %s: %v\n%s", path, err, w.Body.String())
		}
	}
	return w.Code, body
}

// A card fetched for art then asked for text costs ZERO extra Scryfall
// named requests: the sidecar was written from the art fetch's own
// response.
func TestCardTextServesSixFieldsFromTheArtRecord(t *testing.T) {
	ac, hits := factsFixture(t, map[string]scryNamed{
		"Llanowar Elves": {
			Name: "Llanowar Elves", ManaCost: "{G}", TypeLine: "Creature — Elf Druid",
			OracleText: "{T}: Add {G}.", Power: "1", Toughness: "1",
			ImageURIs: struct {
				Normal string `json:"normal"`
			}{Normal: "/img/llanowar.jpg"},
		},
	})

	code, art := getJSON(t, ac.named, "/art/named?exact="+url.QueryEscape("Llanowar Elves"))
	if code != http.StatusOK {
		t.Fatalf("art lookup: want 200, got %d", code)
	}
	if art["image_uris"].(map[string]any)["normal"] == "" {
		t.Fatalf("art record has no image: %+v", art)
	}

	code, body := getJSON(t, ac.text, "/cards/named?exact="+url.QueryEscape("Llanowar Elves"))
	if code != http.StatusOK {
		t.Fatalf("text lookup: want 200, got %d", code)
	}
	for field, want := range map[string]any{
		"name": "Llanowar Elves", "mana_cost": "{G}", "type_line": "Creature — Elf Druid",
		"oracle_text": "{T}: Add {G}.", "power": "1", "toughness": "1",
	} {
		if got := body[field]; got != want {
			t.Fatalf("%s = %v, want %v (full record: %+v)", field, got, want, body)
		}
	}
	// Exactly ONE Scryfall named request across both lookups.
	if hits.Load() != 1 {
		t.Fatalf("want exactly 1 Scryfall named hit across art+text, got %d", hits.Load())
	}
}

// The text lookup may come FIRST, before any art request: the same fetch
// writes both records and the art route still costs only one named hit.
func TestCardTextBeforeArtStillOneScryfallRequest(t *testing.T) {
	ac, hits := factsFixture(t, map[string]scryNamed{
		"Llanowar Elves": {
			Name: "Llanowar Elves", OracleText: "{T}: Add {G}.",
			ImageURIs: struct {
				Normal string `json:"normal"`
			}{Normal: "/img/llanowar.jpg"},
		},
	})
	code, body := getJSON(t, ac.text, "/cards/named?exact=Llanowar%20Elves")
	if code != http.StatusOK || body["oracle_text"] != "{T}: Add {G}." {
		t.Fatalf("text-first lookup: got %d %+v", code, body)
	}
	code, _ = getJSON(t, ac.named, "/art/named?exact=Llanowar%20Elves")
	if code != http.StatusOK {
		t.Fatalf("art lookup after text: want 200, got %d", code)
	}
	if hits.Load() != 1 {
		t.Fatalf("want exactly 1 Scryfall named hit across text+art, got %d", hits.Load())
	}
}

// A name Scryfall has never heard of is a known miss for text too: 404,
// recorded on disk (.miss), never re-asked.
func TestCardTextKnownMissIs404AndCached(t *testing.T) {
	ac, hits := factsFixture(t, map[string]scryNamed{})
	code, _ := getJSON(t, ac.text, "/cards/named?exact=Not+A+Card")
	if code != http.StatusNotFound {
		t.Fatalf("unknown name: want 404, got %d", code)
	}
	if _, err := os.Stat(ac.missPath(artKey("Not A Card"))); err != nil {
		t.Fatalf("miss not recorded on disk: %v", err)
	}
	code, _ = getJSON(t, ac.text, "/cards/named?exact=Not+A+Card")
	if code != http.StatusNotFound {
		t.Fatalf("repeated unknown name: want 404, got %d", code)
	}
	if hits.Load() != 1 {
		t.Fatalf("want exactly 1 Scryfall hit for the repeated miss, got %d", hits.Load())
	}
}

// A card cached for art BEFORE facts were kept has a .jpg but no sidecar:
// the first text lookup backfills it with one paced METADATA-ONLY request —
// no second image download.
func TestCardTextBackfillsALegacyArtCacheWithoutReFetchingTheImage(t *testing.T) {
	ac, hits := factsFixture(t, map[string]scryNamed{
		"Llanowar Elves": {
			Name: "Llanowar Elves", OracleText: "{T}: Add {G}.",
			ImageURIs: struct {
				Normal string `json:"normal"`
			}{Normal: "/img/llanowar.jpg"},
		},
	})
	key := artKey("Llanowar Elves")
	if err := os.WriteFile(ac.jpgPath(key), []byte("pre-existing-art"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, body := getJSON(t, ac.text, "/cards/named?exact=Llanowar%20Elves")
	if code != http.StatusOK || body["oracle_text"] != "{T}: Add {G}." {
		t.Fatalf("legacy backfill: got %d %+v", code, body)
	}
	sidecar, err := os.ReadFile(ac.factsPath(key))
	if err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}
	if !strings.Contains(string(sidecar), "Llanowar Elves") {
		t.Fatalf("sidecar content %q", sidecar)
	}
	if hits.Load() != 1 {
		t.Fatalf("want exactly 1 Scryfall named hit for the backfill, got %d", hits.Load())
	}
	// The pre-existing art bytes are untouched by the text lookup.
	b, err := os.ReadFile(ac.jpgPath(key))
	if err != nil || string(b) != "pre-existing-art" {
		t.Fatalf("legacy art disturbed: %q %v", b, err)
	}
}

// A multi-faced card carries its printed facts on the faces; the sidecar
// A multi-faced card carries its printed facts on the faces; for a lookup
// by the card's combined name (no face matches) the sidecar still takes the
// FRONT face. A lookup by one face's printed name takes THAT face — the
// back-face test below pins it.
func TestCardTextTakesTheFrontFaceOfADoubleFacedCard(t *testing.T) {
	ac, _ := factsFixture(t, map[string]scryNamed{
		"Delver of Secrets": {
			Name: "Delver of Secrets",
			ImageURIs: struct {
				Normal string `json:"normal"`
			}{Normal: "/img/delver.jpg"},
			CardFaces: []scryFace{
				{ManaCost: "{U}", TypeLine: "Creature — Human Wizard", OracleText: "At the beginning of your upkeep...", Power: "1", Toughness: "1"},
				{TypeLine: "Creature — Insect", OracleText: "Flying", Power: "3", Toughness: "2"},
			},
		},
	})
	code, body := getJSON(t, ac.text, "/cards/named?exact=Delver+of+Secrets")
	if code != http.StatusOK {
		t.Fatalf("want 200, got %d", code)
	}
	if body["oracle_text"] != "At the beginning of your upkeep..." || body["mana_cost"] != "{U}" || body["power"] != "1" {
		t.Fatalf("front face not served: %+v", body)
	}
	if body["toughness"] != "1" {
		t.Fatalf("front face toughness: %+v", body)
	}
}

// A lookup by the BACK face's printed name takes that face's facts: a
// transformed Delver of Secrets is on the wire as "Insectile Aberration",
// and its sidecar must carry the Insectile Aberration face (task
// fb-20260914T033246Z-3f1cc033, defect 3).
func TestCardTextTakesTheMatchedFaceOfADoubleFacedCard(t *testing.T) {
	ac, _ := factsFixture(t, map[string]scryNamed{
		"Insectile Aberration": {
			Name: "Delver of Secrets // Insectile Aberration",
			ImageURIs: struct {
				Normal string `json:"normal"`
			}{Normal: "/img/delver.jpg"},
			CardFaces: []scryFace{
				{Name: "Delver of Secrets", ManaCost: "{U}", TypeLine: "Creature — Human Wizard", OracleText: "At the beginning of your upkeep...", Power: "1", Toughness: "1"},
				{Name: "Insectile Aberration", TypeLine: "Creature — Insect", OracleText: "Flying", Power: "3", Toughness: "2"},
			},
		},
	})
	code, body := getJSON(t, ac.text, "/cards/named?exact=Insectile+Aberration")
	if code != http.StatusOK {
		t.Fatalf("want 200, got %d", code)
	}
	if body["oracle_text"] != "Flying" || body["power"] != "3" || body["toughness"] != "2" {
		t.Fatalf("back face not served: %+v", body)
	}
	if body["name"] != "Insectile Aberration" {
		t.Fatalf("matched face name not served: %+v", body)
	}
}

func TestCardTextRequiresTheExactParameter(t *testing.T) {
	ac, _ := factsFixture(t, map[string]scryNamed{})
	w := httptest.NewRecorder()
	ac.text(w, httptest.NewRequest(http.MethodGet, "/cards/named", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing exact: want 400, got %d", w.Code)
	}
}

// cardsmeta.go: the served index.html carries the injected tag, other files
// pass through byte-for-byte, the on-disk build is not consulted twice, and
// an injection is idempotent.
func TestWithCardMetaInjectsTheTagIntoIndexHTML(t *testing.T) {
	page := "<!doctype html><html><head><title>gorge</title></head><body></body></html>"
	inner := fstest.MapFS{
		"index.html":  &fstest.MapFile{Data: []byte(page)},
		"assets/a.js": &fstest.MapFile{Data: []byte("console.log(1)")},
		"favicon.ico": &fstest.MapFile{Data: []byte{0, 1, 2}},
	}
	fsys := withCardMeta(inner)

	f, err := fsys.Open("index.html")
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, cardMetaTag) {
		t.Fatalf("served index.html lacks the tag:\n%s", s)
	}
	// The rest of the page is intact and the tag sits in the head.
	if !strings.Contains(s, "<title>gorge</title></head>") {
		t.Fatalf("served index.html lost its body:\n%s", s)
	}
	head := s[:strings.Index(s, cardMetaTag)]
	if !strings.HasSuffix(head, "<head>\n") {
		t.Fatalf("tag not right after <head>: %q", head)
	}

	// Every other file passes through untouched.
	for name, want := range map[string]string{"assets/a.js": "console.log(1)", "favicon.ico": "\x00\x01\x02"} {
		f, err := fsys.Open(name)
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		got, err := io.ReadAll(f)
		f.Close()
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q (%v), want %q", name, got, err, want)
		}
	}

	// Idempotent: a build that already carries the tag is served as built.
	already := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html><head>" + cardMetaTag + "</head></html>")}}
	f2, err := withCardMeta(already).Open("index.html")
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := io.ReadAll(f2)
	f2.Close()
	if string(b2) != "<html><head>"+cardMetaTag+"</head></html>" {
		t.Fatalf("injection not idempotent: %s", b2)
	}

	// A build with no literal <head> still gets the tag (prepended; every
	// browser's parser relocates a top-of-document meta into the head).
	nohead := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html><body>x</body></html>")}}
	f3, err := withCardMeta(nohead).Open("index.html")
	if err != nil {
		t.Fatal(err)
	}
	b3, _ := io.ReadAll(f3)
	f3.Close()
	if !strings.HasPrefix(string(b3), cardMetaTag) {
		t.Fatalf("no-head fallback did not prepend: %s", b3)
	}
}

// A name the wrapped FS does not have (no web build) still errors through —
// webFS() checks index.html on the INNER fs before wrapping, so the nil
// "no build" contract is unchanged.
func TestWithCardMetaPassesThroughAMissingIndex(t *testing.T) {
	fsys := withCardMeta(fstest.MapFS{"other.txt": &fstest.MapFile{Data: []byte("x")}})
	if _, err := fsys.Open("index.html"); err == nil {
		t.Fatal("missing index.html should error, matching webFS()'s no-build contract")
	}
}
