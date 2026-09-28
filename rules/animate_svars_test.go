// The sVars$ grant on a DB$ Animate body, pinned end to end on the real
// corpus carrier the ticket named: Guardian Scalelord's BackupAbilities SVar
// is `DB$ Animate | Keywords$ Flying | Triggers$ AttackTrig | sVars$ AE`, and
// the card's own SVar table carries `AE` as the nested declaration
// `SVar:AE:SVar:HasAttackEffect:TRUE` (cards/../g/guardian_scalelord.txt). The
// resolved Animate must grant the named bodies through the same layer-6
// AddSVars machinery the Whip of Erebos arm uses (rules/animate_leavebattle-
// field_test.go), expanding the nested form under BOTH names, alongside the
// Keywords$ half. The other participant is a freely-authored fixture; no
// corpus .txt is inlined.
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// guardianScalelordAnimEngine seeds the REAL corpus Guardian Scalelord and a
// freely-authored fixture creature on seat 0's battlefield, then returns the
// engine, the fixture creature id and the card. The Animate body is resolved
// by the caller against Guardian Scalelord's own SVar table, the way its
// Backup keyword would execute the named SVar.
func guardianScalelordAnimEngine(t *testing.T) (*Engine, state.ObjID, *cards.Card) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	gs := lookup(t, reg, "Guardian Scalelord")
	fixture := card(t, "Name:Animate Fixture\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, _ := corpusEngineCfg(t, reg, []*cards.Card{gs, fixture}, nil)
	fixtureID := moveByName(t, e, 0, "Animate Fixture", state.ZBattlefield)

	// PRECONDITION: the fixture is really on the battlefield under seat 0 and
	// carries none of the animation's grants yet, so the assertions below
	// cannot pass on a target the animation never touched.
	o := e.G.Obj(fixtureID)
	if o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("fixture precondition: obj=%+v, want battlefield under seat 0", o)
	}
	if e.HasKeyword(fixtureID, "Flying") {
		t.Fatal("fixture precondition: already has Flying before the animation")
	}
	if _, ok := e.GrantedSVar(fixtureID, "AE"); ok {
		t.Fatal("fixture precondition: already carries the AE sVars grant")
	}
	if _, ok := e.GrantedSVar(fixtureID, "HasAttackEffect"); ok {
		t.Fatal("fixture precondition: already carries the HasAttackEffect grant")
	}
	return e, fixtureID, gs
}

// TestGuardianScalelordAnimateGrantsSVars: resolving the card's real
// BackupAbilities Animate body against its own SVar table grants the named
// `sVars$ AE` bodies — the verbatim `SVar:HasAttackEffect:TRUE` declaration
// under `AE`, the expanded marker under `HasAttackEffect` — and the sibling
// Keywords$ Flying grant lands in the same registration.
func TestGuardianScalelordAnimateGrantsSVars(t *testing.T) {
	t.Parallel()
	e, fixtureID, gs := guardianScalelordAnimEngine(t)
	sa := cards.ResolveSVar(gs.Faces[0].SVars, "BackupAbilities")
	if sa == nil {
		t.Fatal("Guardian Scalelord BackupAbilities SVar unresolved")
	}
	if got := strings.TrimSpace(sa.Params["sVars"]); got != "AE" {
		t.Fatalf("BackupAbilities sVars$ = %q, want AE (the real corpus body)", got)
	}
	if body, ok := gs.Faces[0].SVars["AE"]; !ok || body != "SVar:HasAttackEffect:TRUE" {
		t.Fatalf("Guardian Scalelord SVar AE = %q, %v; want the nested declaration", body, ok)
	}

	effects.Resolve(e, &effects.Ctx{Source: fixtureID, Controller: 0,
		SVars: gs.Faces[0].SVars}, sa)

	if v, ok := e.GrantedSVar(fixtureID, "AE"); !ok || v != "SVar:HasAttackEffect:TRUE" {
		t.Fatalf("GrantedSVar(AE) = %q, %v; want the verbatim sVars$ grant", v, ok)
	}
	if v, ok := e.GrantedSVar(fixtureID, "HasAttackEffect"); !ok || v != "TRUE" {
		t.Fatalf("GrantedSVar(HasAttackEffect) = %q, %v; want the expanded nested marker", v, ok)
	}
	if !e.HasKeyword(fixtureID, "Flying") {
		t.Fatal("the Keywords$ half of the same Animate did not grant Flying")
	}
}

// TestGuardianScalelordAnimateMissingSVarIsLoud: a face table without the
// named body fails closed with ONE loud Note and grants nothing. This both
// proves the grant is driven by the SVar table READ (not a constant) and
// guards the "nothing happens" direction against a silently unregistered
// handler.
func TestGuardianScalelordAnimateMissingSVarIsLoud(t *testing.T) {
	t.Parallel()
	e, fixtureID, gs := guardianScalelordAnimEngine(t)
	sa := cards.ResolveSVar(gs.Faces[0].SVars, "BackupAbilities")
	if sa == nil {
		t.Fatal("Guardian Scalelord BackupAbilities SVar unresolved")
	}
	// A table with the body absent: the read must refuse it, one note.
	table := map[string]string{}
	for k, v := range gs.Faces[0].SVars {
		if k != "AE" {
			table[k] = v
		}
	}

	effects.Resolve(e, &effects.Ctx{Source: fixtureID, Controller: 0, SVars: table}, sa)

	if _, ok := e.GrantedSVar(fixtureID, "AE"); ok {
		t.Fatal("AE grant landed though its SVar body was absent (must fail closed)")
	}
	notes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "sVars$ AE has no SVar body") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("missing sVars$ body produced %d notes, want exactly 1", notes)
	}
}
