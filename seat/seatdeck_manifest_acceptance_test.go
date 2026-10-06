package seat

// Cross-interface privacy acceptance for the seat deck manifest contract
// (docs/superpowers/specs/2026-09-29-seat-deck-manifest.md, ticket
// seat-deck-03-acceptance). The per-interface unit pins live beside the code
// they cover (deck/manifest_test.go, rules/deck_manifest_test.go,
// view/own_deck_test.go, internal/manabrew/own_deck_test.go,
// host/manabrewhttp/own_deck_test.go, host/httpapi/own_deck_test.go); this
// file audits the CONTRACT end to end on one match: every owner interface
// agrees on the owner manifest at the same sequence, no other projection or
// the redacted event stream carries the owner's private card names, and the
// manifest stays static while the library it describes moves underneath it.
//
// The fixture is authored inline (Ruling P9): Forge scripts are GPL and must
// never be committed, so a synthetic card is parsed from bytes here exactly
// as deck/manifest_test.go does.

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/view"
)

// parseAcceptanceCard compiles one inline card face for the acceptance
// fixture. Unknown parameters are fine: the manifest reads only the face
// name.
func parseAcceptanceCard(t *testing.T, script string) *cards.Card {
	t.Helper()
	c, diags := cards.ParseBytes("seatdeck-acceptance.txt", []byte(script))
	if len(diags) != 0 {
		t.Fatalf("parse %q: %v", script, diags)
	}
	c.Link()
	return c
}

// acceptanceFixture is the compact genesis list the brief asks for: duplicate
// main-deck names, one legendary commander, and a two-card sideboard. The
// card names are deliberately unique to this fixture so a leak assertion can
// search any wire blob for them without false positives.
type acceptanceFixture struct {
	cfg       rules.Config
	twin      *cards.Card // main x4 (duplicate collapse)
	twin2     *cards.Card // same printed name, separate object -> one counted row
	commander *cards.Card
	filler    *cards.Card
	side      *cards.Card
}

func newAcceptanceFixture(t *testing.T) acceptanceFixture {
	t.Helper()
	twin := parseAcceptanceCard(t, "Name:Acceptance Twin\nTypes:Creature\nPT:2/2\n")
	twin2 := parseAcceptanceCard(t, "Name:Acceptance Twin\nTypes:Creature\nPT:2/2\n")
	commander := parseAcceptanceCard(t, "Name:Acceptance Commander\nTypes:Legendary Creature\nPT:3/3\n")
	filler := parseAcceptanceCard(t, "Name:Acceptance Filler\nTypes:Creature\nPT:1/1\n")
	side := parseAcceptanceCard(t, "Name:Acceptance Sideboard\nTypes:Creature\nPT:1/1\n")

	// main[0] is the commander, so the declared index {0} resolves against the
	// legal-commander gate.
	main := []*cards.Card{commander, twin, twin, twin, twin2, twin2, filler, filler, filler, filler, filler, filler, filler, filler}
	opp := make([]*cards.Card, len(main))
	for i := range opp {
		opp[i] = filler
	}
	cfg := rules.Config{
		Seed:       4242,
		Format:     rules.FormatCommander,
		Names:      []string{"acceptance-owner", "acceptance-opponent"},
		Decks:      [][]*cards.Card{main, opp},
		Sideboards: [][]*cards.Card{{side, side}, nil},
		Commanders: [][]int{{0}, nil},
	}
	return acceptanceFixture{cfg: cfg, twin: twin, twin2: twin2, commander: commander, filler: filler, side: side}
}

// acceptancePrivateNames are the fixture card names that must never reach
// another projection, the redacted log, or the match metadata. Acceptance
// Filler is NOT here: the opponent plays it too, so it is not owner-private.
// Acceptance Commander is NOT here either: a commander is seated in the
// PUBLIC command zone (CR 903.6), so its name is legitimately visible to
// every spectator — the manifest leak this file guards against is the hidden
// main/sideboard contents, and TestSeatDeckManifestCommanderZoneIsPublic
// pins that the exclusion is a rules fact, not an oversight.
var acceptancePrivateNames = []string{"Acceptance Twin", "Acceptance Sideboard"}

// TestSeatDeckManifestFixtureShape pins the fixture's manifest itself before
// any interface claim leans on it: duplicate printed names collapse to one
// counted row, the commander rides Main counted AND names Commanders in
// declared order, and the sideboard is its own canonical list.
func TestSeatDeckManifestFixtureShape(t *testing.T) {
	t.Parallel()
	f := newAcceptanceFixture(t)
	e := rules.New(f.cfg)
	m := e.OwnDeck(0)
	if m == nil {
		t.Fatal("precondition: genesis built no owner manifest")
	}
	want := deck.Manifest{
		Name: "acceptance-owner",
		Main: []deck.ManifestRow{
			{Name: "Acceptance Commander", Count: 1},
			{Name: "Acceptance Filler", Count: 8},
			{Name: "Acceptance Twin", Count: 5},
		},
		Sideboard:  []deck.ManifestRow{{Name: "Acceptance Sideboard", Count: 2}},
		Commanders: []string{"Acceptance Commander"},
	}
	if got := *m; !manifestEqual(got, want) {
		t.Fatalf("fixture manifest = %#v, want %#v", got, want)
	}
	// Precondition for the privacy leaves: the names really are distinct and
	// searchable, and the commander really is seated in the command zone (the
	// legal-commander gate admitted it).
	if !slices.Contains(m.Commanders, "Acceptance Commander") {
		t.Fatalf("commander not named: %#v", m)
	}
	if n := len(e.G.Players[0].Commanders); n != 1 {
		t.Fatalf("precondition: genesis seated %d commanders, want 1", n)
	}
	for _, name := range acceptancePrivateNames {
		if !manifestNames(m, name) {
			t.Fatalf("fixture manifest is missing %q; a leak search would be vacuous", name)
		}
	}
}

// TestSeatDeckManifestCommanderZoneIsPublic pins the control for the privacy
// exclusion: the commander name IS publicly visible because the command zone
// is public, so an opponent seeing it is correct rules behaviour and not the
// manifest leaking. Without this, a future reader could not tell why the
// commander is absent from acceptancePrivateNames.
func TestSeatDeckManifestCommanderZoneIsPublic(t *testing.T) {
	t.Parallel()
	f := newAcceptanceFixture(t)
	e := rules.New(f.cfg)
	e.Advance()
	opp := view.ProjectFor(e.G, e, 1, view.Seat, nil)
	for _, p := range opp.Players {
		if p.ID != 0 {
			continue
		}
		for _, c := range p.Command {
			if c.Name == "Acceptance Commander" {
				return
			}
		}
	}
	t.Fatalf("commander not publicly visible in seat 0's command zone: %+v", opp.Players)
}

func manifestEqual(a, b deck.Manifest) bool {
	if a.Name != b.Name || !slices.Equal(a.Main, b.Main) || !slices.Equal(a.Sideboard, b.Sideboard) || !slices.Equal(a.Commanders, b.Commanders) {
		return false
	}
	return true
}

func manifestNames(m *deck.Manifest, name string) bool {
	for _, r := range m.Main {
		if r.Name == name {
			return true
		}
	}
	for _, r := range m.Sideboard {
		if r.Name == name {
			return true
		}
	}
	return slices.Contains(m.Commanders, name)
}

// TestSeatDeckManifestEveryOwnerInterfaceAgrees is the brief's cross-interface
// agreement leaf: at ONE sequence, the engine's genesis manifest, the native
// HTTP/SSE seat projection (view.ProjectFor, the same projection the /view
// endpoint and seat stream frames serve), the ordinary seat/board adapter
// (seat.BoardFromView + botpolicy.BoardFromGame, the two halves a bot uses),
// and the hosted EnvSeat view/board all carry the identical owner manifest.
// The ManaBrew rendering is compared in cmd/gorged's cross-interface test,
// where the negotiated transport is actually mounted.
func TestSeatDeckManifestEveryOwnerInterfaceAgrees(t *testing.T) {
	t.Parallel()
	f := newAcceptanceFixture(t)
	e := rules.New(f.cfg)
	e.Advance() // genesis: shuffle + deal

	engineManifest := e.OwnDeck(0)
	if engineManifest == nil {
		t.Fatal("precondition: engine has no owner manifest")
	}

	// The one projection every in-process seat path is built from.
	v := view.ProjectFor(e.G, e, 0, view.Seat, nil)
	if v.OwnDeck == nil {
		t.Fatal("native seat projection omitted the owner manifest")
	}
	if !manifestEqual(*v.OwnDeck, *engineManifest) {
		t.Fatalf("native seat view manifest = %#v, engine = %#v", v.OwnDeck, engineManifest)
	}

	// Ordinary seat/board adapters: both halves must agree with the view and
	// the engine. BoardFromView reads the projected view; BoardFromGame reads
	// the engine handle directly through the same OwnDeck accessor.
	boardView := BoardFromView(v)
	boardGame := botpolicy.BoardFromGame(e.G, e, 0)
	if boardView.OwnDeck == nil || boardGame.OwnDeck == nil {
		t.Fatalf("board halves dropped the manifest: view=%#v game=%#v", boardView.OwnDeck, boardGame.OwnDeck)
	}
	if !manifestEqual(*boardView.OwnDeck, *engineManifest) {
		t.Fatalf("BoardFromView manifest = %#v, engine = %#v", boardView.OwnDeck, engineManifest)
	}
	if !manifestEqual(*boardGame.OwnDeck, *engineManifest) {
		t.Fatalf("BoardFromGame manifest = %#v, engine = %#v", boardGame.OwnDeck, engineManifest)
	}

	// Hosted EnvSeat input: the host builds Env.View from the same projection
	// (host/botenv.go envData) and Env.Board from BoardFromGameInto; the
	// decisive property is that the Env's manifest is the actor's, so an
	// adapter reading Env.View.OwnDeck or Env.Board.OwnDeck sees it too.
	envView := view.ProjectFor(e.G, e, 0, view.Seat, nil)
	envBoardBuf := botpolicy.NewBoard(2)
	envBoard := botpolicy.BoardFromGameInto(e.G, e, 0, &envBoardBuf)
	if envView.OwnDeck == nil || envBoard.OwnDeck == nil {
		t.Fatalf("EnvSeat input dropped the manifest: view=%#v board=%#v", envView.OwnDeck, envBoard.OwnDeck)
	}
	if !manifestEqual(*envView.OwnDeck, *engineManifest) || !manifestEqual(*envBoard.OwnDeck, *engineManifest) {
		t.Fatalf("EnvSeat manifest diverged: view=%#v board=%#v engine=%#v", envView.OwnDeck, envBoard.OwnDeck, engineManifest)
	}

	// The two seats' manifests are genuinely different lists, so a comparison
	// that silently returned seat 1's data for seat 0 would fail below.
	opp := view.ProjectFor(e.G, e, 1, view.Seat, nil)
	if opp.OwnDeck == nil || opp.OwnDeck.Name == engineManifest.Name {
		t.Fatalf("precondition: seats do not have distinct manifests (%#v vs %#v)", opp.OwnDeck, engineManifest)
	}
}

// TestSeatDeckManifestNeverLeaksToAnotherProjection pins the spec's security
// requirements: the owner's private card names must not appear in the
// opponent's seat view, the public spectator view, the omniscient spectator
// view, or the log redacted for another viewer. The owner's own seat view is
// the control, so the search is not vacuous.
func TestSeatDeckManifestNeverLeaksToAnotherProjection(t *testing.T) {
	t.Parallel()
	f := newAcceptanceFixture(t)
	e := rules.New(f.cfg)
	e.Advance()

	owner := view.ProjectFor(e.G, e, 0, view.Seat, nil)
	if owner.OwnDeck == nil || !projectionMentions(owner, acceptancePrivateNames) {
		t.Fatalf("precondition: owner projection is not a positive control: %#v", owner.OwnDeck)
	}

	opp := view.ProjectFor(e.G, e, 1, view.Seat, nil)
	if opp.OwnDeck == nil {
		t.Fatal("opponent seat view omitted ITS OWN manifest")
	}
	if projectionMentions(opp, acceptancePrivateNames) {
		t.Fatalf("opponent seat projection leaked the owner's private names")
	}
	if opp.Decision != nil {
		t.Fatalf("opponent projection carried the owner's decision: %#v", opp.Decision)
	}

	for _, vis := range []view.Visibility{view.Public, view.Omniscient} {
		spect := view.ProjectFor(e.G, e, view.NoSeat, vis, nil)
		if spect.OwnDeck != nil {
			t.Fatalf("%s spectator view carried a manifest: %#v", vis, spect.OwnDeck)
		}
		raw, err := json.Marshal(spect)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"own_deck"`) {
			t.Fatalf("%s spectator JSON carries an own_deck key: %s", vis, raw)
		}
	}

	// The PUBLIC (non-omniscient) spectator hides hands, so it must carry no
	// owner-private card name at all. The omniscient spectator legitimately
	// sees hands, so a twin in the owner's hand is visible to it by design
	// (that is the existing hand-spoiling rule, not a manifest leak); the
	// manifest-absence assertion above is the omniscient claim.
	public := view.ProjectFor(e.G, e, view.NoSeat, view.Public, nil)
	if projectionMentions(public, acceptancePrivateNames) {
		t.Fatal("public spectator view leaked the owner's private names")
	}

	// Redacted events: the opponent's and a spectator's view of the log must
	// carry no owner-private card name. The owner's own redaction is the
	// positive control (its shuffles name its own library objects).
	oppEvents := view.RedactEventsFor(e.G, e.L.Events, 1, view.Seat)
	if logMentions(t, e, oppEvents, acceptancePrivateNames) {
		t.Fatal("opponent-redacted log leaked owner-private names")
	}
	spectEvents := view.RedactEventsFor(e.G, e.L.Events, view.NoSeat, view.Public)
	if logMentions(t, e, spectEvents, acceptancePrivateNames) {
		t.Fatal("public-redacted log leaked owner-private names")
	}
}

func projectionMentions(v view.View, names []string) bool {
	// Marshal the projection exactly as the wire would: a field that is not
	// declared on View cannot reach a client, and a search over the JSON is
	// the strongest statement this test can make about that.
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	for _, name := range names {
		if strings.Contains(string(raw), name) {
			return true
		}
	}
	return false
}

// logMentions reports whether any redacted event's public text carries one of
// names. A hidden-zone move's Obj is zeroed by redaction, but Note/Reveal
// text or a leaked Name field is what this catches.
func logMentions(t *testing.T, e *rules.Engine, evs []events.Event, names []string) bool {
	t.Helper()
	for _, ev := range evs {
		for _, name := range names {
			if strings.Contains(ev.Text, name) {
				return true
			}
		}
	}
	return false
}
