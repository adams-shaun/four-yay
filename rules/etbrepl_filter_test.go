package rules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kw:ETBReplacement's trailing colon fields pin (task etbrepl-filter). The
// keyword parameter is
//
//	K:ETBReplacement:<layer>:<SVar>:<Mandatory|Optional>:<validZone>:<filter>
//
// and the LAST field is the entering-card filter the replacement must match.
// The expansion historically hardcoded ValidCard$ Card.Self, so a card like
// Veiled Ascension ("Face-down creatures you control enter with a flying
// counter on them") fired on its OWN entry instead of the creatures it names.
//
// The tests compile the real corpus scripts FRESH (a temp cardsfolder run
// through cards.CompileDir), never the shared ir.gob.gz cache: the cache's
// staleness rule is keyed on cards.lock, which a Go-side keyword-expansion
// change (cards/kw_etbreplacement.go) does not touch, so a cached registry
// would still decode the old expansion and the test would pass vacuously.
// Same pattern as altcast_test.go's freshEncoreRegistry.

// freshCorpusRegistry compiles a temp cardsfolder holding the named real
// corpus scripts (paths relative to the corpus root), plus a bare
// tokenscripts directory. Returns a registry whose Tokens map is empty --
// none of these cards mints a token.
func freshCorpusRegistry(t *testing.T, rels ...string) *cards.Registry {
	t.Helper()
	dir := t.TempDir()
	cf := filepath.Join(dir, "cardsfolder")
	tk := filepath.Join(dir, "tokenscripts")
	if err := os.MkdirAll(cf, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tk, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range rels {
		b, err := os.ReadFile(filepath.Join("../.cards/cardsfolder", rel))
		if err != nil {
			t.Fatalf("corpus copy %s: %v", rel, err)
		}
		if err := os.WriteFile(filepath.Join(cf, filepath.Base(rel)), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg, _, err := cards.CompileDir(cf)
	if err != nil {
		t.Fatalf("fresh compile: %v", err)
	}
	return reg
}

// etbreplEngine is manifestEngine's fresh-compile twin: a seatZeroStart
// two-seat game where seat 0's deck is the named corpus cards (plus Forests)
// and seat 1's deck is ten Grizzly Bears (plus Forests), parked at Main1.
func etbreplEngine(t *testing.T, reg *cards.Registry, hand ...string) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range hand {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for len(deck) < 40 {
		deck = append(deck, forest)
	}
	opp := make([]*cards.Card, 0, 40)
	for i := 0; i < 10; i++ {
		opp = append(opp, bear)
	}
	for len(opp) < 40 {
		opp = append(opp, forest)
	}
	cfg := seatZeroStart(Config{Seed: 7701, Names: []string{"etbrepl", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// TestETBReplacementFilterVeiledAscension pins BOTH faces of the Veiled
// Ascension defect on the real corpus card:
//
//   - casting Veiled Ascension leaves ZERO Flying counters on the
//     ENCHANTMENT (the pre-fix expansion scoped the replacement to
//     Card.Self, so the body acted on its own source), and
//   - a face-down creature that enters while Veiled Ascension is already on
//     the battlefield gains ONE Flying counter from the replacement.
//
// The second half needs the derived face-down override: at replacement-match
// time the entering object has not yet had FaceDown folded onto it, so
// `Creature.faceDown+YouCtrl` would fail closed without it.
func TestETBReplacementFilterVeiledAscension(t *testing.T) {
	t.Parallel()
	reg := freshCorpusRegistry(t,
		"v/veiled_ascension.txt", "g/grizzly_bears.txt", "f/forest.txt", "m/mountain.txt")
	e, cfg := etbreplEngine(t, reg, "Veiled Ascension", "Grizzly Bears")

	castVanilla(t, e, "Veiled Ascension", "WWWW")

	// Consequence 2: the enchantment must NOT counter itself. Find it on the
	// battlefield by name.
	var veil state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Veiled Ascension" {
			veil = id
		}
	}
	if veil == 0 {
		t.Fatal("Veiled Ascension did not reach the battlefield")
	}
	if n := counterCount(e.G.Obj(veil), "Flying"); n != 0 {
		t.Fatalf("Veiled Ascension flying counters = %d, want 0 (self-scoped replacement)", n)
	}

	// Consequence 1: a face-down creature entering AFTER the cast matches.
	// cloakBear emits the exact logged MoveZone effCloak emits ("entered_cloaked").
	bear := cloakBear(t, e, 0)
	o := e.G.Obj(bear)
	if o.Zone != state.ZBattlefield || !o.FaceDown {
		t.Fatalf("cloaked bear: zone %s FaceDown %v", o.Zone, o.FaceDown)
	}
	if n := counterCount(o, "Flying"); n != 1 {
		t.Fatalf("entering face-down bear flying counters = %d, want 1 (the replacement)", n)
	}
	replayCheck(t, e, cfg)
}

// TestETBReplacementFilterVeiledAscensionFaceDown$TrueShape pins the
// manifest marker too, not only the cloak literal: a plain "entered_face_down"
// MoveZone entry must match the same filter. A third face-down marker cannot
// be missed because the matcher reads events.IsFaceDownEntry, the one
// predicate covering both.
func TestETBReplacementFilterVeiledAscensionManifestMarker(t *testing.T) {
	t.Parallel()
	reg := freshCorpusRegistry(t,
		"v/veiled_ascension.txt", "g/grizzly_bears.txt", "f/forest.txt", "m/mountain.txt")
	e, cfg := etbreplEngine(t, reg, "Veiled Ascension", "Grizzly Bears")
	castVanilla(t, e, "Veiled Ascension", "WWWW")

	// Move one of seat 0's Grizzly Bears face down with the manifest marker.
	var bear state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
				bear = id
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, Player: 0,
					From: z, To: state.ZBattlefield,
					Counter: events.FaceDownEntryCounter, Secret: true})
				break
			}
		}
		if bear != 0 {
			break
		}
	}
	if bear == 0 {
		t.Fatal("no Grizzly Bears to manifest")
	}
	if n := counterCount(e.G.Obj(bear), "Flying"); n != 1 {
		t.Fatalf("manifested bear flying counters = %d, want 1", n)
	}
	replayCheck(t, e, cfg)
}

// TestETBReplacementFilterGiada pins a NON-faceDown non-Self filter on a
// second real card with a distinct SVar body: Giada, Font of Hope's
// `Creature.Angel+YouCtrl+Other`, whose body reads `CounterNum$ X` with
// `X = Count$Valid Angel.YouCtrl`. A non-Angel must get nothing, Giada's own
// entry must not double-apply (the `Other` predicate), and a later Angel must
// receive counters.
func TestETBReplacementFilterGiada(t *testing.T) {
	t.Parallel()
	reg := freshCorpusRegistry(t,
		"g/giada_font_of_hope.txt", "s/serra_angel.txt",
		"g/grizzly_bears.txt", "f/forest.txt", "m/mountain.txt", "p/plains.txt")
	e, cfg := etbreplEngine(t, reg, "Giada, Font of Hope", "Serra Angel", "Grizzly Bears")

	// Giada's own entry: `Other` excludes the source, so it must get 0.
	castVanilla(t, e, "Giada, Font of Hope", "WW")
	var giada state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Giada, Font of Hope" {
			giada = id
		}
	}
	if giada == 0 {
		t.Fatal("Giada did not reach the battlefield")
	}
	if n := counterCount(e.G.Obj(giada), "P1P1"); n != 0 {
		t.Fatalf("Giada p1p1 counters = %d, want 0 (Other excludes her own entry)", n)
	}

	// A later Angel enters: it must receive at least one (Giada counts as an
	// Angel already controlled). Answer any replacement/trigger asks by
	// passing priority.
	angel := searchMoveByName(t, e, "Serra Angel", state.ZHand)
	addMana(t, e, 0, "WWWWW")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == angel {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Serra Angel: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 30)
	ao := e.G.Obj(angel)
	if ao.Zone != state.ZBattlefield {
		t.Fatalf("Serra Angel zone = %s, want battlefield", ao.Zone)
	}
	if n := counterCount(ao, "P1P1"); n < 1 {
		t.Fatalf("entering Angel p1p1 counters = %d, want >= 1 (Giada's replacement)", n)
	}

	// A non-Angel entering gets none.
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
	addMana(t, e, 0, "GG")
	d = e.Pending()
	idx = -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bear {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Grizzly Bears: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 30)
	if n := counterCount(e.G.Obj(bear), "P1P1"); n != 0 {
		t.Fatalf("entering Bear p1p1 counters = %d, want 0 (non-Angel)", n)
	}
	replayCheck(t, e, cfg)
}
