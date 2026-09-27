// unearth_test.go — CR 702.84 (Unearth) proof tests, driven by real corpus
// cards (the .cards/cardsfolder scripts for Ashnod's Harvester and Priest of
// Fell Rites), never a name-sharing fixture. The card text is read from the
// worktree's .cards corpus at test time; no Forge script text is committed.
//
// The fresh registry is necessary: testutil.CorpusRegistry decodes the shared
// ir.gob.gz cache, whose staleness rule is keyed on cards.lock, which a Go-side
// keyword-expansion change (cards/kw_unearth.go) does not touch -- so a cached
// registry decodes these cards WITHOUT the K:Unearth expansion, exactly the
// reason altcast_test.go's freshEncoreRegistry exists.
package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// freshUnearthRegistry compiles a minimal corpus fresh (no shared cache) from
// two real K:Unearth carriers. Their scripts are copied verbatim out of the
// worktree's gitignored .cards corpus into a temp dir; nothing is committed.
func freshUnearthRegistry(t *testing.T) *cards.Registry {
	t.Helper()
	dir := t.TempDir()
	cf := filepath.Join(dir, "cardsfolder")
	if err := os.MkdirAll(cf, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{
		{"../.cards/cardsfolder/a/ashnods_harvester.txt", "ashnods_harvester.txt"},
		{"../.cards/cardsfolder/p/priest_of_fell_rites.txt", "priest_of_fell_rites.txt"},
	} {
		b, err := os.ReadFile(pair[0])
		if err != nil {
			t.Fatalf("corpus copy %s: %v", pair[0], err)
		}
		if err := os.WriteFile(filepath.Join(cf, pair[1]), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg, _, err := cards.CompileDir(cf)
	if err != nil {
		t.Fatalf("fresh compile: %v", err)
	}
	return reg
}

// unearthAbilityOption returns the pending priority decision's graveyard
// activation option for id, failing the test when absent. Ashnod's Harvester
// carries exactly one graveyard ability (its unearth), so Obj alone selects
// it; cards with more than one use unearthAbilityOptionLabel.
func unearthAbilityOption(t *testing.T, e *Engine, id state.ObjID) int {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id {
			return o.Index
		}
	}
	t.Fatalf("no graveyard ability option for %s: %+v", e.G.Obj(id).Face().Name, d.Options)
	return -1
}

// unearthAbilityOptionLabel selects the ability option whose label contains
// "Unearth" -- needed for a carrier that also has a printed graveyard
// activated ability (Priest of Fell Rites).
func unearthAbilityOptionLabel(t *testing.T, e *Engine, id state.ObjID) int {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id && strings.Contains(o.Label, "Unearth") {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no unearth ability option for %s: %+v", e.G.Obj(id).Face().Name, d.Options)
	}
	return idx
}

// TestUnearthKeywordExpandsToGraveyardAbility is the parser/compiler half: the
// K:Unearth line must expand to exactly one graveyard-zone ChangeZone ability
// tagged Unearth, and the expansion must be idempotent across a second Link.
func TestUnearthKeywordExpandsToGraveyardAbility(t *testing.T) {
	c := card(t, "Name:Test Unearther\nManaCost:1 B\nTypes:Creature Zombie\nPT:2/2\nK:Unearth:1 B\nOracle:x\n")
	count := func(f *cards.Face) int {
		n := 0
		for _, a := range f.Abilities {
			if a.Params["Unearth"] == "True" {
				n++
			}
		}
		return n
	}
	f := c.Faces[0]
	if got := count(f); got != 1 {
		t.Fatalf("after Link: %d Unearth abilities, want 1", got)
	}
	var ua *cards.SA
	for _, a := range f.Abilities {
		if a.Params["Unearth"] == "True" {
			ua = a
		}
	}
	if ua.API != "ChangeZone" {
		t.Fatalf("unearth ability API %q, want ChangeZone", ua.API)
	}
	if ua.Params["ActivationZone"] != "Graveyard" {
		t.Fatalf("ActivationZone %q, want Graveyard", ua.Params["ActivationZone"])
	}
	if ua.Params["Origin"] != "Graveyard" || ua.Params["Destination"] != "Battlefield" {
		t.Fatalf("unearth zones %s -> %s, want Graveyard -> Battlefield", ua.Params["Origin"], ua.Params["Destination"])
	}
	if !strings.Contains(ua.Params["Cost"], "1") || !strings.Contains(ua.Params["Cost"], "B") {
		t.Fatalf("unearth cost %q, want the printed 1 B", ua.Params["Cost"])
	}
	// Idempotence: a second Link must not double-expand.
	if d := c.Link(); len(d) != 0 {
		t.Fatalf("second Link: %v", d)
	}
	if got := count(c.Faces[0]); got != 1 {
		t.Fatalf("after second Link: %d Unearth abilities, want 1 (expansion is not idempotent)", got)
	}
}

// TestUnearthPrimitiveIsRegistered pins the coverage-census half: kw:Unearth
// must leave effects.Supported()'s unsupported set, and a real corpus carrier
// must no longer name it as a missing primitive.
func TestUnearthPrimitiveIsRegistered(t *testing.T) {
	supported := effects.Supported()
	if !supported["kw:Unearth"] {
		t.Fatal(`effects.Supported() is missing "kw:Unearth"`)
	}
	reg := freshUnearthRegistry(t)
	c, ok := reg.Lookup("Ashnod's Harvester")
	if !ok {
		t.Fatal("fresh registry lacks Ashnod's Harvester")
	}
	if missing := reg.Unsupported(c, supported); containsStr(missing, "kw:Unearth") {
		t.Fatalf("Ashnod's Harvester still names kw:Unearth as unsupported: %v", missing)
	}
}

// TestUnearthActivatesFromGraveyardGainsHasteAndExilesAtEndStep is the core
// CR 702.84a proof on Ashnod's Harvester (K:Unearth:1 B): it starts in the
// graveyard, its unearth cost is paid, the permanent enters, it has haste, and
// the next end step's delayed trigger exiles it -- with the registration
// consumed, not orphaned.
func TestUnearthActivatesFromGraveyardGainsHasteAndExilesAtEndStep(t *testing.T) {
	reg := freshUnearthRegistry(t)
	e, cfg, _ := altCostEngineReg(t, 951, reg, []string{"Ashnod's Harvester"}, nil, nil)
	id := findCardObj(t, e, 0, "Ashnod's Harvester", state.ZGraveyard)
	if o := e.G.Obj(id); o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: card in %s, want graveyard", o.Zone)
	}
	addMana(t, e, 0, "CB") // unearth {1}{B}
	submitChoices(t, e, unearthAbilityOption(t, e, id))
	passUntilStackEmpty(t, e, 40)

	o := e.G.Obj(id)
	// Precondition: the move happened, so the haste/exile assertions are not
	// vacuous over a card still in the graveyard.
	if o.Zone != state.ZBattlefield {
		t.Fatalf("after unearth: zone %s, want battlefield", o.Zone)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after unearth %d, want 0 (cost unpaid)", e.G.Players[0].Pool.Total())
	}
	if !e.HasKeyword(id, "Haste") {
		t.Fatal("unearthed Ashnod's Harvester has no haste (CR 702.84a)")
	}
	// Precondition for the end-step assertion: the live promise exists.
	if !hasUnearthPromise(e, id) {
		t.Fatal("no live __kwUnearthExile registration after the unearth entry")
	}

	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o.Zone != state.ZExile {
		t.Fatalf("after the end step: zone %s, want exile (CR 702.84a)", o.Zone)
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("unearth registration not consumed: %d pending", len(e.G.Delayed))
	}
	replayCheck(t, e, cfg)
}

// TestUnearthExilesInsteadOfLeavingToHand covers CR 702.84b's replacement for
// an ordinary leave to hand (a bounce): the intended destination is hand, the
// actual destination must be exile, and the end-step promise must not later
// move the exiled card again.
func TestUnearthExilesInsteadOfLeavingToHand(t *testing.T) {
	unearthRedirectTest(t, 952, state.ZHand)
}

// TestUnearthExilesInsteadOfLeavingToGraveyard is the same replacement for a
// would-be graveyard departure (a destroy/dies move).
func TestUnearthExilesInsteadOfLeavingToGraveyard(t *testing.T) {
	unearthRedirectTest(t, 953, state.ZGraveyard)
}

func unearthRedirectTest(t *testing.T, seed uint64, intended state.Zone) {
	t.Helper()
	reg := freshUnearthRegistry(t)
	e, cfg, _ := altCostEngineReg(t, seed, reg, []string{"Ashnod's Harvester"}, nil, nil)
	id := findCardObj(t, e, 0, "Ashnod's Harvester", state.ZGraveyard)
	addMana(t, e, 0, "CB")
	submitChoices(t, e, unearthAbilityOption(t, e, id))
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: unearthed card in %s, want battlefield", o.Zone)
	}
	if !hasUnearthPromise(e, id) {
		t.Fatal("precondition: no live __kwUnearthExile registration to drive the replacement")
	}
	// The intended departure, emitted directly (the same raw-MoveZone setup
	// the dash command-zone test uses). The replacement must redirect it.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: intended})
	e.pending = nil
	e.priorityRound()
	if o := e.G.Obj(id); o.Zone != state.ZExile {
		t.Fatalf("leave to %s: zone %s, want exile (CR 702.84b)", intended, o.Zone)
	}
	// No orphaned promise: the end-step registration must fire once, find the
	// card already off the battlefield (a different incarnation/zone), and be
	// consumed without moving it again.
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o.Zone != state.ZExile {
		t.Fatalf("after the end step following an early exile: zone %s, want exile", o.Zone)
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("orphaned unearth registration after the early exile: %d pending", len(e.G.Delayed))
	}
	replayCheck(t, e, cfg)
}

// TestUnearthPriestOfFellRitesOfferAndResolution runs the same core behaviour
// on the briefly-reported carrier, which also has a printed graveyard
// activated ability: the unearth offer must exist (label-selected) and the
// returned permanent must have haste. It pays {3}{W}{B}.
func TestUnearthPriestOfFellRitesOfferAndResolution(t *testing.T) {
	reg := freshUnearthRegistry(t)
	e, cfg, _ := altCostEngineReg(t, 954, reg, []string{"Priest of Fell Rites"}, nil, nil)
	id := findCardObj(t, e, 0, "Priest of Fell Rites", state.ZGraveyard)
	if o := e.G.Obj(id); o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: card in %s, want graveyard", o.Zone)
	}
	addMana(t, e, 0, "CCCWB")
	submitChoices(t, e, unearthAbilityOptionLabel(t, e, id))
	passUntilStackEmpty(t, e, 40)
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("after unearth: zone %s, want battlefield", o.Zone)
	}
	if !e.HasKeyword(id, "Haste") {
		t.Fatal("unearthed Priest of Fell Rites has no haste")
	}
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o.Zone != state.ZExile {
		t.Fatalf("after the end step: zone %s, want exile", o.Zone)
	}
	replayCheck(t, e, cfg)
}

// hasUnearthPromise reports whether a live __kwUnearthExile registration names
// the object's current incarnation.
func hasUnearthPromise(e *Engine, id state.ObjID) bool {
	o := e.G.Obj(id)
	if o == nil {
		return false
	}
	for i := range e.G.Delayed {
		dt := &e.G.Delayed[i]
		if dt.Source == id && dt.Execute == "__kwUnearthExile" &&
			dt.TrackSource && dt.SourceIncarnation == o.Incarnation {
			return true
		}
	}
	return false
}
