package rules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// potential_kinds_test.go pins aph-web-manual-only-plays' server half:
// rules.PotentialActions projects EVERY real play kind the priority offer walk
// (legalActionsPriced) can emit, not only "cast", "ability" and "play_land".
// A mana-costed non-cast play is offered FLOAT-FIRST (the walk prices it
// against the floating pool), so on an empty pool it is absent from the
// priority options; before this ticket a Room unlock, a morph turn-face-up, a
// specialize or a max-speed granted ability was then neither offered nor
// projected, and the web client -- which keeps the manual mana taps visible
// under auto-pay exactly while such a play is reachable (web/src/lib/
// manualmana.ts) -- hid the very taps that would make it appear.

// notAPlayKinds are the option kinds every priority window offers and which
// are never a play: the mana tap, pass and concede.
var notAPlayKinds = map[string]bool{"activate": true, "pass": true, "concede": true}

// measuredLegalActionKinds is the MEASURED vocabulary of legalActionsPriced
// (read off rules/legal.go by legalActionsPricedKinds below). A new kind in
// the walk fails TestPotentialActionsProjectsEveryPlayKind until it is added
// here AND classified: projected by potentialPlayKind, or never a play.
var measuredLegalActionKinds = []string{
	"ability", "activate", "cast", "concede", "granted", "pass", "play_land",
	"specialize", "station", "turn_face_up", "unlock",
}

// legalActionsPricedKinds parses the legal.go walk family -- the entry body
// in legal.go (the one (*Engine) method that constructs the legalWalk
// walker) plus every (*legalWalk) *Walk section method in legal_walk*.go,
// which the entry calls in order -- and returns every option Kind the walk
// can emit: a `Kind: "<lit>"` field of a composite literal, the literal first
// argument of the walk's add(kind, ...) closure (the entry's local one and
// the sections' legalWalk.add binding), and any `<x>.Kind = "<lit>"`
// assignment. A Kind the walk computes (anything but the add closure's own
// `kind` parameter) fails the test: the ratchet only works while every
// emitted kind is a literal it can read.
func legalActionsPricedKinds(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("legal*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob rules/legal*.go: %v (%d files)", err, len(files))
	}
	fset := token.NewFileSet()
	var bodies []*ast.BlockStmt
	haveEntry := false
	for _, name := range files {
		f, perr := parser.ParseFile(fset, name, nil, 0)
		if perr != nil {
			t.Fatalf("parse rules/%s: %v", name, perr)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Recv == nil {
				continue
			}
			// legalActionsPriced delegates (legalActionsPriced ->
			// legalActionsWalk -> the entry) to the method that constructs the
			// legalWalk walker; its sections are the (*legalWalk) *Walk methods
			// the entry calls (legal_walk*.go). The entry is found by that
			// construction, never by name, so a rename of any walk method
			// leaves this ratchet untouched.
			recvName := ""
			if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
				if id, ok := star.X.(*ast.Ident); ok {
					recvName = id.Name
				}
			}
			if recvName == "Engine" && constructsLegalWalk(fn.Body) {
				bodies = append(bodies, fn.Body)
				haveEntry = true
			}
			if recvName == "legalWalk" && strings.HasSuffix(fn.Name.Name, "Walk") {
				bodies = append(bodies, fn.Body)
			}
		}
	}
	if !haveEntry {
		t.Fatal("rules/legal*.go has no (*Engine) method constructing legalWalk; the shared walk entry moved or was restructured -- update legalActionsPricedKinds' entry detection")
	}
	seen := map[string]bool{}
	lit := func(n ast.Expr, where string) {
		switch v := n.(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				s, err := strconv.Unquote(v.Value)
				if err != nil {
					t.Fatalf("%s: %v", where, err)
				}
				seen[s] = true
				return
			}
		case *ast.Ident:
			if v.Name == "kind" { // the add closure's own parameter
				return
			}
		}
		t.Errorf("%s: legalActionsPriced emits a non-literal Kind at %s; classify it by hand", where, fset.Position(n.Pos()))
	}
	for _, body := range bodies {
		ast.Inspect(body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.KeyValueExpr:
				if id, ok := x.Key.(*ast.Ident); ok && id.Name == "Kind" {
					lit(x.Value, "Kind field")
				}
			case *ast.CallExpr:
				var fname string
				switch id := x.Fun.(type) {
				case *ast.Ident:
					fname = id.Name // the entry's local add closure
				case *ast.SelectorExpr:
					fname = id.Sel.Name // the sections' legalWalk.add method
				}
				if fname == "add" && len(x.Args) > 0 {
					lit(x.Args[0], "add(kind, ...)")
				}
			case *ast.AssignStmt:
				for i, l := range x.Lhs {
					if sel, ok := l.(*ast.SelectorExpr); ok && sel.Sel.Name == "Kind" && i < len(x.Rhs) {
						lit(x.Rhs[i], "Kind assignment")
					}
				}
			}
			return true
		})
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// constructsLegalWalk reports whether body contains a CompositeLit of type
// legalWalk -- the shape of the ONE place the engine builds the shared walk
// walker (rules/legal.go, the body legalActionsPriced delegates to). Matching
// the construction, not a method name, is what keeps this ratchet silent
// across renames of the walk entry.
func constructsLegalWalk(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if cl, ok := n.(*ast.CompositeLit); ok {
			if id, ok := cl.Type.(*ast.Ident); ok && id.Name == "legalWalk" {
				found = true
			}
		}
		return true
	})
	return found
}

// TestPotentialActionsProjectsEveryPlayKind is the vocabulary ratchet: the
// kinds legalActionsPriced emits are exactly the measured list, and every one
// of them is either projected by PotentialActions or one of the three
// never-a-play kinds -- never both, never neither.
func TestPotentialActionsProjectsEveryPlayKind(t *testing.T) {
	t.Parallel()
	got := legalActionsPricedKinds(t)
	if !slices.Equal(got, measuredLegalActionKinds) {
		t.Fatalf("legalActionsPriced emits kinds %v, measured %v: classify the new kind (potentialPlayKind or notAPlayKinds) and update the list", got, measuredLegalActionKinds)
	}
	for _, k := range got {
		if potentialPlayKind(k) == notAPlayKinds[k] {
			t.Errorf("kind %q: projected=%v, never-a-play=%v -- every real play is projected, and only the tap, pass and concede are not", k, potentialPlayKind(k), notAPlayKinds[k])
		}
	}
}

// potentialHas reports whether seat 0's projection carries kind on obj (and,
// when mode is non-empty, that mode).
func potentialHas(e *Engine, kind string, obj state.ObjID, mode string) bool {
	for _, a := range e.PotentialActions(0) {
		if a.Kind == kind && a.Obj == obj && (mode == "" || a.Mode == mode) {
			return true
		}
	}
	return false
}

// offeredNow reports whether seat 0's real (floating-pool) offer walk carries
// kind on obj -- the priority decision's own option list.
func offeredNow(e *Engine, kind string, obj state.ObjID) bool {
	for _, o := range e.legalActions(0) {
		if o.Kind == kind && o.Obj == obj {
			return true
		}
	}
	return false
}

// assertFloatGated is the shared shape: with an empty floating pool the play
// is NOT offered (float-first), and it IS projected, because the seat's
// untapped sources could pay it.
func assertFloatGated(t *testing.T, e *Engine, kind string, obj state.ObjID, mode string) {
	t.Helper()
	if total := e.G.Players[0].Pool.Total(); total != 0 {
		t.Fatalf("precondition: floating pool %d, want empty", total)
	}
	if offeredNow(e, kind, obj) {
		t.Fatalf("precondition: %s on %d is offered on an empty pool; the fixture does not exercise the float-first gap: %+v", kind, obj, e.legalActions(0))
	}
	if !potentialHas(e, kind, obj, mode) {
		t.Fatalf("%s on %d (mode %q) is float-gated but missing from PotentialActions: %+v", kind, obj, mode, e.PotentialActions(0))
	}
}

// TestPotentialActionsFloatGatedRoomUnlock: the real corpus Dazzling Theater
// on the battlefield, its Prop Room door ({2}{W}) locked, three untapped
// Plains and an empty pool in main 1. The unlock is priced against the
// floating pool, so it is not offered yet; the projection must carry it.
func TestPotentialActionsFloatGatedRoomUnlock(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Dazzling Theater")}, nil)
	room := moveByName(t, e, 0, "Dazzling Theater", state.ZBattlefield)
	if roomLockedFace(e.G.Obj(room)) == nil {
		t.Fatal("precondition: Dazzling Theater has no locked door")
	}
	e.G.Players[0].LandsPlayed = 1
	for i := 0; i < 3; i++ {
		onBoard(t, e, 0, jitteSnapshotPlains)
	}
	assertFloatGated(t, e, "unlock", room, "")
}

// TestPotentialActionsFloatGatedGrantedAbility: a max-speed (CR 702.179e)
// granted "{2}: Draw a card." on an artifact whose controller has speed 4,
// with two untapped Plains and an empty pool. The granted offer is priced
// against the floating pool; the projection must carry it as "granted".
func TestPotentialActionsFloatGatedGrantedAbility(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Step = state.StepMain1
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.Players[0].LandsPlayed = 1
	e.G.Players[0].Speed = maxSpeed
	onBoard(t, e, 0, jitteSnapshotPlains)
	onBoard(t, e, 0, jitteSnapshotPlains)
	engine := onBoard(t, e, 0, "Name:Probe Engine\nTypes:Artifact\n"+
		"S:Mode$ Continuous | Affected$ Card.Self | Condition$ MaxSpeed | AddAbility$ ABDraw | Description$ Max speed — {2}: Draw a card.\n"+
		"SVar:ABDraw:AB$ Draw | Cost$ 2 | NumCards$ 1 | Secondary$ True | SpellDescription$ Draw a card.\n"+
		"Oracle:Max speed — {2}: Draw a card.\n")
	assertFloatGated(t, e, "granted", engine, "")
}

// TestPotentialActionsFloatGatedTurnFaceUp: the real corpus Kin-Tree Warden
// cast face down (morph {G}), the leftover floating mana then dropped (a
// fixture write: this test reads projections only and never replays), and one
// untapped Forest. The turn-face-up special action (CR 708.6) is priced
// against the floating pool; the projection must carry it once the
// hypothetical pool can pay the {G}.
func TestPotentialActionsFloatGatedTurnFaceUp(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := manifestEngine(t, reg, "Kin-Tree Warden")
	id := morphDownCast(t, e, "Kin-Tree Warden", "morphed", "CCCG", 1)
	if !e.G.Obj(id).FaceDown {
		t.Fatal("precondition: Kin-Tree Warden is not face down")
	}
	e.G.Players[0].Pool = state.Mana{}
	onBoard(t, e, 0, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n")
	assertFloatGated(t, e, "turn_face_up", id, "")
}

// TestPotentialActionsFloatGatedSpecialize: a Specialize {1} creature (the
// inline two-face shape specialize_test.go uses) beside one untapped
// Mountain with an empty pool, as a sorcery. The special action is priced
// against the floating pool; the projection must carry it, face index as its
// Mode exactly like the option.
func TestPotentialActionsFloatGatedSpecialize(t *testing.T) {
	t.Parallel()
	c, diags := cards.ParseBytes("specialize.txt", []byte("Name:Front\nAlternateMode:Specialize\nTypes:Creature Druid\n"+
		"K:Specialize:1\nSPECIALIZE:WHITE\nName:White Form\nManaCost:W\nTypes:Creature\n"))
	if len(diags) != 0 {
		t.Fatalf("parse diags: %+v", diags)
	}
	e, id := specializeEngine(t, c, 1)
	assertFloatGated(t, e, "specialize", id, "1")
}

// TestPotentialActionsOfferedPlaysStayProjected pins the real-pool half the
// widening must not disturb: once the pool floats the cost, each play is both
// offered and projected (the web's "already offered" identity check relies on
// the projection naming what the decision offers).
func TestPotentialActionsOfferedPlaysStayProjected(t *testing.T) {
	t.Parallel()
	c, diags := cards.ParseBytes("specialize.txt", []byte("Name:Front\nAlternateMode:Specialize\nTypes:Creature Druid\n"+
		"K:Specialize:1\nSPECIALIZE:WHITE\nName:White Form\nManaCost:W\nTypes:Creature\n"))
	if len(diags) != 0 {
		t.Fatalf("parse diags: %+v", diags)
	}
	e, id := specializeEngine(t, c, 0)
	addMana(t, e, 0, "C")
	if !offeredNow(e, "specialize", id) {
		t.Fatalf("precondition: specialize not offered with {C} floating: %+v", e.legalActions(0))
	}
	if !potentialHas(e, "specialize", id, "1") {
		t.Fatalf("an offered specialize is missing from PotentialActions: %+v", e.PotentialActions(0))
	}
	for _, a := range e.PotentialActions(0) {
		if notAPlayKinds[a.Kind] {
			t.Fatalf("PotentialActions projected a never-a-play kind: %+v", a)
		}
	}
}
