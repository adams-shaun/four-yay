package rules

// stat:AttackRestrict scoping (attacker ceiling). Every test below drives the
// REAL declare-attackers decision (askAttackers -> Engine.Pending) on a
// three-seat table with the named corpus carrier on seat 0 and real attackers
// on the active seat, so the offered option Groups, the raised per-defender
// GroupLimits and both enforcement paths (Decision.Validate and
// validateAttackDeclaration) are read from the decision the engine actually
// poses -- not a hand-built stand-in. The cap-above-one shape (Crawlspace,
// "no more than two creatures can attack you") is what the decision-wide
// single GroupLimit could not express; the IsPresent$ gate transition is
// Mirri, Weatherlight Duelist's "as long as CARDNAME is tapped".

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// attackRestrictTable builds a three-seat table with the named corpus
// restriction on seat 0 and n ready attackers on seat 1, active in the
// declare-attackers step. Both living opponents (seats 0 and 2) are attack
// targets, so a ValidDefender$ scoping is distinguishable from a global
// ceiling: two options can exist per attacker.
func attackRestrictTable(t *testing.T, restrictName string, nAttackers int) (*Engine, []state.ObjID) {
	t.Helper()
	cfg := Config{Seed: 716, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}}
	e := New(cfg)
	onBoardCard(t, e, 0, corpusCard(t, restrictName))
	var ids []state.ObjID
	for i := 0; i < nAttackers; i++ {
		id := onBoard(t, e, 1, "Name:Rusher\nTypes:Creature\nPT:2/2\nOracle:x\n")
		e.G.Obj(id).SummonSick = false
		ids = append(ids, id)
	}
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	return e, ids
}

// optionIndicesAt returns the indices of d's options whose defender is p,
// in the engine's offer order.
func optionIndicesAt(d *decision.Decision, p state.PlayerID) []int {
	var out []int
	for _, o := range d.Options {
		if o.Player == p {
			out = append(out, o.Index)
		}
	}
	return out
}

// TestAttackRestrictScopedLimitTwoOfferedDecision drives Crawlspace's real
// static
//
//	S:Mode$ AttackRestrict | MaxAttackers$ 2 | ValidDefender$ You
//
// on seat 0 with four attackers on seat 1. The scoped ceiling is per
// defender, so it must NOT be folded into the global maxAttackers() (CR
// 508.1j bounds only whole-declaration restrictions), and the raised cap must
// reach the wire: the decision's Max stays the option count while the
// restricted defender's Group carries its own cap of 2 through GroupLimits --
// the field a single decision-wide GroupLimit could not express. Attacks at
// the OTHER opponent stay uncapped, which is what proves the scoping.
func TestAttackRestrictScopedLimitTwoOfferedDecision(t *testing.T) {
	t.Parallel()
	e, _ := attackRestrictTable(t, "Crawlspace", 4)
	if sv := e.activeStatics("AttackRestrict"); len(sv) == 0 {
		t.Fatal("precondition: Crawlspace's AttackRestrict static is not active")
	}
	if e.maxAttackers() < 4 {
		t.Fatalf("scoped ceiling folded into the global maxAttackers: got %d, want unrestricted", e.maxAttackers())
	}
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("no declare-attackers decision: %+v", d)
	}
	restricted := optionIndicesAt(d, 0)
	other := optionIndicesAt(d, 2)
	if len(restricted) != 4 || len(other) != 4 {
		t.Fatalf("precondition: options at seat 0 = %d, at seat 2 = %d, want 4 each", len(restricted), len(other))
	}
	if d.Max < 4 {
		t.Fatalf("scoped ceiling narrowed the decision Max to %d, want the option count", d.Max)
	}
	// The group and its raised cap must travel on the offered decision.
	group := d.Options[restricted[0]].Group
	if group == "" {
		t.Fatal("options at the scoped defender carry no Group")
	}
	for _, i := range other {
		if g := d.Options[i].Group; g != "" {
			t.Fatalf("option at the unrestricted opponent carries Group %q", g)
		}
	}
	if got := d.GroupLimits[group]; got != 2 {
		t.Fatalf("GroupLimits[%q] = %d, want Crawlspace's MaxAttackers$ 2 (a scoped limit above one reduced to the default one)", group, got)
	}
	if got := d.GroupCapFor(group); got != 2 {
		t.Fatalf("GroupCapFor(%q) = %d, want 2", group, got)
	}
	// Decision.Validate and the engine's declaration check share the cap.
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: 1, Choices: restricted[:2]}); err != nil {
		t.Fatalf("decision rejected two attackers at the restricted defender: %v", err)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: 1, Choices: restricted[:3]}); err == nil {
		t.Fatal("decision accepted three attackers at a MaxAttackers$ 2 defender")
	}
	if err := e.validateAttackDeclaration(d, decision.Intent{Choices: restricted[:2]}); err != nil {
		t.Fatalf("engine rejected two attackers at the restricted defender: %v", err)
	}
	if err := e.validateAttackDeclaration(d, decision.Intent{Choices: restricted[:3]}); err == nil {
		t.Fatal("engine accepted three attackers at a MaxAttackers$ 2 defender")
	}
	// The other defender really is uncapped (the scoping, not a global cap).
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: 1, Choices: other[:3]}); err != nil {
		t.Fatalf("decision rejected three attackers at the unrestricted opponent: %v", err)
	}
	if err := e.validateAttackDeclaration(d, decision.Intent{Choices: other[:3]}); err != nil {
		t.Fatalf("engine rejected three attackers at the unrestricted opponent: %v", err)
	}
}

// TestAttackRestrictScopedLimitOneStaysMutuallyExclusive pins the limit-one
// scoped shape (Judoon Enforcers' "no more than one creature can attack you")
// on the real offered decision: the restricted defender's options share a
// Group at the default cap of one, and both enforcement paths reject two
// while accepting one.
func TestAttackRestrictScopedLimitOneStaysMutuallyExclusive(t *testing.T) {
	t.Parallel()
	e, _ := attackRestrictTable(t, "Judoon Enforcers", 3)
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("no declare-attackers decision: %+v", d)
	}
	restricted := optionIndicesAt(d, 0)
	if len(restricted) != 3 {
		t.Fatalf("precondition: %d options at the restricted defender, want 3", len(restricted))
	}
	group := d.Options[restricted[0]].Group
	if group == "" {
		t.Fatal("options at the scoped defender carry no Group")
	}
	if got := d.GroupCapFor(group); got != 1 {
		t.Fatalf("GroupCapFor(%q) = %d, want the default cap of one", group, got)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: 1, Choices: restricted[:1]}); err != nil {
		t.Fatalf("decision rejected one attacker at the limit-one defender: %v", err)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: 1, Choices: restricted[:2]}); err == nil {
		t.Fatal("decision accepted two attackers at a MaxAttackers$ 1 defender")
	}
	if err := e.validateAttackDeclaration(d, decision.Intent{Choices: restricted[:2]}); err == nil {
		t.Fatal("engine accepted two attackers at a MaxAttackers$ 1 defender")
	}
}

// TestAttackRestrictSilentArbiterIsGlobalAndOffered pins the unscoped shape
// (Silent Arbiter's "no more than one creature can attack each combat"): the
// global maxAttackers()/decision Max carries the ceiling, and the decision
// refuses a two-attacker declaration through its own Max without any Group.
func TestAttackRestrictSilentArbiterIsGlobalAndOffered(t *testing.T) {
	t.Parallel()
	e, _ := attackRestrictTable(t, "Silent Arbiter", 3)
	if got := e.maxAttackers(); got != 1 {
		t.Fatalf("global ceiling = %d, want 1", got)
	}
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("no declare-attackers decision: %+v", d)
	}
	if d.Max != 1 {
		t.Fatalf("decision Max = %d, want the global ceiling 1", d.Max)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{0, 1}}); err == nil {
		t.Fatal("decision accepted two attackers under a global MaxAttackers$ 1")
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{0}}); err != nil {
		t.Fatalf("decision rejected one attacker under the global ceiling: %v", err)
	}
}

// TestAttackRestrictPresentGateTransition drives Mirri, Weatherlight
// Duelist's gated static
//
//	S:Mode$ AttackRestrict | IsPresent$ Card.Self+tapped |
//	        MaxAttackers$ 1 | ValidDefender$ You
//
// through the offered decision's Group as the source taps. The gate is a
// TRANSITION: untapped, seat 0's defender is uncapped and no option carries a
// Group; the moment Mirri taps, the same decision re-derives the cap and the
// restricted defender's options gain the limit-one Group. Before the gate was
// read the static was live while untapped (over-restriction).
func TestAttackRestrictPresentGateTransition(t *testing.T) {
	t.Parallel()
	e, _ := attackRestrictTable(t, "Mirri, Weatherlight Duelist", 3)
	mirri := e.G.Zone(state.ZBattlefield, 0)[0]
	if o := e.G.Obj(mirri); o == nil || o.Tapped {
		t.Fatal("precondition: Mirri must be untapped on the battlefield")
	}
	if _, ok := e.attackRestrictLimit(0); ok {
		t.Fatal("Mirri's tapped-only gate held while untapped")
	}
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("no declare-attackers decision: %+v", d)
	}
	for _, i := range optionIndicesAt(d, 0) {
		if g := d.Options[i].Group; g != "" {
			t.Fatalf("untapped Mirri already grouped the restricted defender's options (%q)", g)
		}
	}
	// Tap Mirri: the gate now holds and the cap must appear on a fresh offer.
	e.G.Obj(mirri).Tapped = true
	limit, ok := e.attackRestrictLimit(0)
	if !ok || limit != 1 {
		t.Fatalf("attackRestrictLimit(0) = (%d,%v), want (1,true) once Mirri is tapped", limit, ok)
	}
	e.askAttackers()
	d = e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("no declare-attackers decision after tapping Mirri: %+v", d)
	}
	restricted := optionIndicesAt(d, 0)
	if len(restricted) != 3 {
		t.Fatalf("precondition: %d options at the restricted defender after the tap, want 3", len(restricted))
	}
	group := d.Options[restricted[0]].Group
	if group == "" {
		t.Fatal("tapped Mirri's gate did not group the restricted defender's options")
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: 1, Choices: restricted[:2]}); err == nil {
		t.Fatal("decision accepted two attackers while Mirri's gate capped the defender at one")
	}
	if err := e.validateAttackDeclaration(d, decision.Intent{Choices: restricted[:2]}); err == nil {
		t.Fatal("engine accepted two attackers while Mirri's gate capped the defender at one")
	}
}

// TestAttackRestrictScopedLimitBotRepair proves the raised scoped cap drives
// the bot's own repair on the REAL offered decision: an answer naming all
// four attackers at the MaxAttackers$ 2 defender is trimmed to two by
// botpolicy.Clamp, and the repaired answer passes both Decision.Validate and
// the engine's declaration check. The pre-fix decision carried no raised cap,
// so Clamp left the over-cap answer untouched and Submit rejected it forever
// (the deterministic-bot livelock class the one-home rule exists for). The
// actual policy answer (newTestBot) must satisfy the same two checks.
func TestAttackRestrictScopedLimitBotRepair(t *testing.T) {
	t.Parallel()
	e, _ := attackRestrictTable(t, "Crawlspace", 4)
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("no declare-attackers decision: %+v", d)
	}
	restricted := optionIndicesAt(d, 0)
	if len(restricted) != 4 {
		t.Fatalf("precondition: %d options at the restricted defender, want 4", len(restricted))
	}
	if got := d.GroupLimits[d.Options[restricted[0]].Group]; got != 2 {
		t.Fatalf("precondition: the raised scoped cap did not reach the decision: GroupLimits = %d", got)
	}
	// A rules-ignorant greedy answer naming all four at the capped defender.
	over := decision.Intent{Seq: d.Seq, Player: 1, Choices: append([]int(nil), restricted...)}
	if err := d.Validate(over); err == nil {
		t.Fatal("precondition: the over-cap raw answer must be rejected by Validate")
	}
	repaired := botpolicy.Clamp(d, over)
	if err := d.Validate(repaired); err != nil {
		t.Fatalf("Clamp left an answer Validate rejects: choices %v, err %v", repaired.Choices, err)
	}
	if err := e.validateAttackDeclaration(d, repaired); err != nil {
		t.Fatalf("Clamp left an answer the engine rejects: choices %v, err %v", repaired.Choices, err)
	}
	atDefender := 0
	for _, c := range repaired.Choices {
		if d.Options[c].Player == 0 {
			atDefender++
		}
	}
	if atDefender != 2 {
		t.Fatalf("repaired answer kept %d attackers at the capped defender, want 2 (choices %v)", atDefender, repaired.Choices)
	}
	// The policy's own answer must clear the same two checks.
	in := newTestBot(1).answer(e, d)
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer failed Decision.Validate: choices %v, err %v", in.Choices, err)
	}
	if err := e.validateAttackDeclaration(d, in); err != nil {
		t.Fatalf("bot answer failed the engine's declaration check: choices %v, err %v", in.Choices, err)
	}
}
