package rules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// ---------------------------------------------------------------------------
// §4.4 Section-coverage guard
// ---------------------------------------------------------------------------

// TestQuietCoversWalkSections parses the two entry points that enumerate the
// walk's sections (legalActionsWalkWithWindow and battlefieldWalk) and
// asserts every section call in them is named in quietCoveredSections. A walk
// section added later therefore cannot silently bypass the proof: the call
// shows up and the test fails until the table accounts for it.
func TestQuietCoversWalkSections(t *testing.T) {
	// Collect the calls the two functions make on the walk object (w.X()) or
	// free functions that take w as their first argument (X(w)). Those are
	// the section entry points; e.*/tail.*/rec.* are engine helpers, not
	// sections a blocker must cover.
	collect := func(file, fn string) map[string]bool {
		t.Helper()
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		calls := map[string]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			fd, ok := n.(*ast.FuncDecl)
			if !ok || fd.Name == nil || fd.Name.Name != fn {
				return true
			}
			ast.Inspect(fd, func(n ast.Node) bool {
				ce, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch fun := ce.Fun.(type) {
				case *ast.SelectorExpr:
					if id, ok := fun.X.(*ast.Ident); ok && id.Name == "w" {
						calls[fun.Sel.Name] = true
					}
				case *ast.Ident:
					// A free section function X(w).
					if len(ce.Args) >= 1 {
						if id, ok := ce.Args[0].(*ast.Ident); ok && id.Name == "w" {
							calls[fun.Name] = true
						}
					}
				}
				return true
			})
			return false
		})
		return calls
	}

	calls := collect("legal.go", "legalActionsWalkWithWindow")
	for name := range collect("legal_walk_battlefield.go", "battlefieldWalk") {
		calls[name] = true
	}
	if len(calls) == 0 {
		t.Fatal("section guard precondition: no walk-section calls found (the parser or the function names drifted)")
	}
	for name := range calls {
		if !quietCoveredSections[name] {
			t.Errorf("walk section %q is not in quietCoveredSections (rules/quiet_proof.go); a new walk section must be covered by the quiet proof or explicitly exempted in the table", name)
		}
	}
	// The walk's own named section methods must all be present, so the table
	// cannot silently drop one the parser no longer sees.
	for _, name := range []string{
		"handWalk", "mayPlayLandWalk", "mayhemLandWalk", "mayPlaySpellWalk",
		"plotZoneWalk", "commandZoneWalk", "graveyardCastsWalk", "exileCastsWalk",
		"battlefieldWalk",
	} {
		if !calls[name] {
			t.Errorf("section guard precondition: %q not found in the walk entry points; the guard is not seeing the walk", name)
		}
	}
}

// ---------------------------------------------------------------------------
// §2.4 cost classifier field guard
// ---------------------------------------------------------------------------

// TestQuietCostClassifierCoversCostFields reflects over cost.Cost and fails
// when it gains a field the classifier does not name. A new cost component
// therefore cannot silently read as free (which would make the proof unsound,
// since a cost the classifier reads as {0} makes an ability look affordable).
func TestQuietCostClassifierCoversCostFields(t *testing.T) {
	typ := reflect.TypeOf(Cost{})
	var missing []string
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if !quietCostFieldNames[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("quietCostFloor does not classify cost.Cost field(s) %s; add each to quietCostFieldNames and to quietCostFloor's nonMana test (an unclassified field must mean nonMana)", strings.Join(missing, ", "))
	}
	// The reverse direction: a name in the allowlist that no longer exists is
	// dead bookkeeping and hides the drift above.
	for name := range quietCostFieldNames {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("quietCostFieldNames names %q, which cost.Cost no longer has", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Fixture helpers: the §2.1 contract
// ---------------------------------------------------------------------------

// quietContractOpts returns the walk's options and asserts the seed of the
// contract: when seatQuiet is true, the walk offered nothing but activate,
// pass and concede. It also returns whether the walk offered bare mana.
func quietContract(t *testing.T, e *Engine, p state.PlayerID) (opts []decision.Option, quiet, bareMana bool) {
	t.Helper()
	opts = e.legalActions(p)
	quiet = e.seatQuiet(p)
	bareMana = false
	for i := range opts {
		switch opts[i].Kind {
		case "activate":
			bareMana = true
		case "pass", "concede":
		default:
			if quiet {
				t.Fatalf("quiet proof contract violated for seat %d (turn %d step %v): proof quiet but walk offered %s %q obj %d (blocker %s)",
					p, e.G.Turn, e.G.Step, opts[i].Kind, opts[i].Label, opts[i].Obj, quietBlockerNames[e.quietBlocker(p)])
			}
		}
	}
	return opts, quiet, bareMana
}

// ---------------------------------------------------------------------------
// §6 Q1 item 7: one row per blocker. Each row asserts the proof is true in a
// quiet fixture and false once the one ingredient is added.
// ---------------------------------------------------------------------------

func TestQuietProofFixtures(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	type tc struct {
		name    string
		build   func(t *testing.T) (*Engine, state.PlayerID)
		blocker quietBlockerID
	}
	rows := []tc{
		{
			name: "plains only is quiet (bare mana in the walk)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				return quietBase(t, reg), 0
			},
			blocker: qbNone,
		},
		{
			name: "affordable instant in hand blocks",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				e := quietBaseWith(t, reg, []*cards.Card{lookup(t, reg, "Lightning Bolt")})
				addHand(t, e, 0, lookup(t, reg, "Lightning Bolt"))
				return e, 0
			},
			blocker: qbHandSpell,
		},
		{
			name: "flashback card in graveyard blocks",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				e := quietBaseWith(t, reg, []*cards.Card{lookup(t, reg, "Think Twice")})
				addZone(t, e, 0, lookup(t, reg, "Think Twice"), state.ZGraveyard)
				return e, 0
			},
			blocker: qbGraveRoute,
		},
		{
			name: "equipment with Equip {1} on the battlefield blocks",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				e := quietBaseWith(t, reg, []*cards.Card{lookup(t, reg, "Bonesplitter")})
				addZone(t, e, 0, lookup(t, reg, "Bonesplitter"), state.ZBattlefield)
				return e, 0
			},
			blocker: qbBattlefieldAbility,
		},
		{
			name: "command-zone object blocks",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				e := quietBaseWith(t, reg, []*cards.Card{lookup(t, reg, "Grizzly Bears")})
				addZone(t, e, 0, lookup(t, reg, "Grizzly Bears"), state.ZCommand)
				return e, 0
			},
			blocker: qbCommand,
		},
		{
			name: "plotted card in exile blocks",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				e := quietBaseWith(t, reg, []*cards.Card{lookup(t, reg, "Grizzly Bears")})
				id := addZone(t, e, 0, lookup(t, reg, "Grizzly Bears"), state.ZExile)
				e.G.Obj(id).PlottedTurn = e.G.Turn
				return e, 0
			},
			blocker: qbExileRoute,
		},
		{
			name: "self-carried may-play static in hand blocks (Omniscience)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Omniscience")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbHandSpell,
		},
		{
			name: "self-carried collect-evidence may-play static in hand blocks (Conspiracy Unraveler)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Conspiracy Unraveler")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbHandSpell,
		},
		{
			name: "spirit guide in hand is quiet (hand mana ability is the mana section)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Elvish Spirit Guide")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbNone,
		},
		{
			name: "cycling card in hand blocks through a granted ability",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Titanoth Rex")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbHandAbility,
		},
		{
			name: "foretell card in hand blocks (keyword action)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Behold the Multiverse")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbHandLand,
		},
		{
			name: "battlefield ability with a Sac cost blocks (nonMana)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Viscera Seer")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				addZone(t, e, 0, c, state.ZBattlefield)
				return e, 0
			},
			blocker: qbBattlefieldAbility,
		},
		{
			name: "adjustLandPlays keeps a land blocker after the first land",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				exp := lookup(t, reg, "Exploration")
				forest := lookup(t, reg, "Forest")
				e := quietBaseWith(t, reg, []*cards.Card{exp, forest})
				addZone(t, e, 0, exp, state.ZBattlefield)
				addHand(t, e, 0, forest)
				e.G.Players[0].LandsPlayed = 1
				return e, 0
			},
			blocker: qbHandLand,
		},
		{
			name: "may-play grant from a battlefield static blocks (board flag)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				// Grizzly Bears costs more than the seat's one untapped Plains,
				// so the affordable-hand-spell blocker does not preempt the
				// board flag the row is about: only the Omniscience may-play
				// grant opens it.
				om := lookup(t, reg, "Omniscience")
				bears := lookup(t, reg, "Grizzly Bears")
				e := quietBaseWith(t, reg, []*cards.Card{om, bears})
				addZone(t, e, 0, om, state.ZBattlefield)
				addHand(t, e, 0, bears)
				return e, 0
			},
			blocker: qbBoardMayPlay,
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			e, p := row.build(t)
			opts, quiet, _ := quietContract(t, e, p)
			if got := e.quietBlocker(p); got != row.blocker {
				t.Fatalf("quietBlocker = %s, want %s\nturn %d step %v options: %v",
					quietBlockerNames[got], quietBlockerNames[row.blocker], e.G.Turn, e.G.Step, optKinds(opts))
			}
			if row.blocker == qbNone && !quiet {
				t.Fatalf("fixture is meant to be quiet but the proof blocked")
			}
			if row.blocker != qbNone && quiet {
				t.Fatalf("fixture is meant to block with %s but the proof was quiet", quietBlockerNames[row.blocker])
			}
		})
	}
}

// quietBase is the proof's quiet fixture: seat 0 has one untapped Plains on
// the battlefield and an empty hand, so no hand land, no cast and no ability
// is open. The walk still offers the Plains' bare mana tap.
func quietBase(t *testing.T, reg *cards.Registry) *Engine {
	t.Helper()
	return quietBaseWith(t, reg, nil)
}

// quietBaseWith is quietBase with the named extra cards seated in seat 0's
// deck (so addZone/addHand can pull one out).
func quietBaseWith(t *testing.T, reg *cards.Registry, extras []*cards.Card) *Engine {
	t.Helper()
	mountain := lookup(t, reg, "Mountain")
	plains := lookup(t, reg, "Plains")
	deck0 := append([]*cards.Card{plains}, extras...)
	for len(deck0) < 40 {
		deck0 = append(deck0, mountain)
	}
	deck1 := make([]*cards.Card, 40)
	for i := range deck1 {
		deck1[i] = mountain
	}
	e := New(Config{Seed: 42, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{deck0, deck1}})
	e.Advance()
	toMain1(t, e)
	// Empty seat 0's hand, then seat the Plains untapped on the battlefield.
	for _, id := range append([]state.ObjID{}, e.G.Zone(state.ZHand, 0)...) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
	}
	pid := pullByName(t, e, 0, "Plains")
	e.emit(events.Event{Kind: events.MoveZone, Obj: pid, From: state.ZLibrary, To: state.ZBattlefield})
	if o := e.G.Obj(pid); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("quietBase precondition: Plains %d is not an untapped battlefield land: %+v", pid, o)
	}
	return e
}

// pullByName finds seat p's object for a face name in hand then library.
func pullByName(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				return id
			}
		}
	}
	t.Fatalf("fixture precondition: %q not found in seat %d's hand or library", name, p)
	return 0
}

// addHand seats card in seat p's hand, pulled from the deck.
func addHand(t *testing.T, e *Engine, p state.PlayerID, card *cards.Card) state.ObjID {
	t.Helper()
	id := pullByName(t, e, p, card.Faces[0].Name)
	from := e.G.Obj(id).Zone
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: state.ZHand})
	return id
}

// addZone seats card in seat p's zone, pulled from the deck.
func addZone(t *testing.T, e *Engine, p state.PlayerID, card *cards.Card, to state.Zone) state.ObjID {
	t.Helper()
	id := pullByName(t, e, p, card.Faces[0].Name)
	from := e.G.Obj(id).Zone
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: to})
	if o := e.G.Obj(id); o == nil || o.Zone != to {
		t.Fatalf("fixture precondition: %q did not land in %v: %+v", card.Faces[0].Name, to, o)
	}
	return id
}

func optKinds(opts []decision.Option) []string {
	out := make([]string, 0, len(opts))
	for i := range opts {
		out = append(out, opts[i].Kind+":"+opts[i].Label)
	}
	return out
}

// ---------------------------------------------------------------------------
// §4.2 item 5: corpus sweep
// ---------------------------------------------------------------------------

// TestQuietProofCorpusSweep builds one battlefield, hand and graveyard object
// per corpus card face that has facts, and asserts the §2.1 contract for
// every seat and both timing classes: when seatQuiet is true the full walk
// offered no non-mana option. It is the soundness gate for the blocker table.
//
// It is sharded by card index so no shard exceeds the 2 GB / 1 min budget;
// run every shard with -run TestQuietProofCorpusSweep.
func TestQuietProofCorpusSweep(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	n := reg.Len()
	if n == 0 {
		t.Fatal("corpus sweep precondition: the registry is empty")
	}
	const shards = 8
	for shard := 0; shard < shards; shard++ {
		t.Run(shardName(shard), func(t *testing.T) {
			sweepShard(t, reg, n, shard, shards)
		})
	}
}

func shardName(shard int) string {
	return string(rune('0'+shard)) + "-of-8"
}

// sweepShard visits every shards'th card by index, places it on the
// battlefield, in hand and in the graveyard in turn, and asserts the §2.1
// contract on both seats at both a sorcery-open and a non-open window.
func sweepShard(t *testing.T, reg *cards.Registry, n, shard, shards int) {
	t.Helper()
	mountain, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("corpus sweep precondition: Mountain missing")
	}
	for idx := shard; idx < n; idx += shards {
		c := reg.Card(idx)
		if c == nil || len(c.Faces) == 0 {
			continue
		}
		name := c.Faces[0].Name
		e := newSweepEngine(t, reg, c, mountain)
		if e == nil {
			continue
		}
		for _, zone := range []state.Zone{state.ZBattlefield, state.ZHand, state.ZGraveyard} {
			id := moveByNameQuiet(t, e, 0, name, zone)
			if id == 0 {
				continue
			}
			for p := state.PlayerID(0); p < 2; p++ {
				quietSweepCheck(t, e, p, name, zone)
			}
			moveQuiet(e, id, zone, state.ZLibrary)
		}
	}
}

// moveByNameQuiet is moveByName without the fatal: a card that is neither in
// hand nor library (a token-id card) is skipped by the sweep.
func moveByNameQuiet(t *testing.T, e *Engine, p state.PlayerID, name string, to state.Zone) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				return id
			}
		}
	}
	return 0
}

// moveQuiet moves id between two zones without a lookup, for the sweep's
// reset between positions.
func moveQuiet(e *Engine, id state.ObjID, from, to state.Zone) {
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: to})
}

// newSweepEngine is a two-seat game whose seat-0 deck is c plus Mountains and
// whose seat-1 deck is Mountains, driven to seat 0's turn-1 Main1. It returns
// nil for a card the engine refuses to seat (a token or a malformed script).
func newSweepEngine(t *testing.T, reg *cards.Registry, c, mountain *cards.Card) (e *Engine) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			e = nil
		}
	}()
	fill := func(n int) []*cards.Card {
		out := make([]*cards.Card, n)
		for i := range out {
			out[i] = mountain
		}
		return out
	}
	e = New(Config{Seed: 42, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{append([]*cards.Card{c}, fill(39)...), fill(40)}})
	e.Advance()
	toMain1(t, e)
	// Empty both hands so the only card under test is the one the shard
	// places; a hand full of filler lands would block on the hand-land
	// blocker and the proof would never be quiet.
	for p := state.PlayerID(0); p < 2; p++ {
		for _, id := range append([]state.ObjID{}, e.G.Zone(state.ZHand, p)...) {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
		}
	}
	return e
}

// quietSweepCheck asserts the contract for one (card, zone, seat) probe.
func quietSweepCheck(t *testing.T, e *Engine, p state.PlayerID, name string, zone state.Zone) {
	t.Helper()
	quiet := e.seatQuiet(p)
	opts := e.legalActions(p)
	if !quiet {
		return
	}
	for i := range opts {
		switch opts[i].Kind {
		case "activate", "pass", "concede":
		default:
			blk := e.quietBlocker(p)
			t.Fatalf("quiet proof unsound: %q in %s, seat %d (turn %d step %v stack %d): proof quiet (blocker %s) but walk offered %s %q obj %d",
				name, zone, p, e.G.Turn, e.G.Step, len(e.G.Stack), quietBlockerNames[blk], opts[i].Kind, opts[i].Label, opts[i].Obj)
		}
	}
}
