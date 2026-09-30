package searchbench

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/searchbench/statespec"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Synthetic StateSpec fixtures: FDN card names only, hand-written, no
// 17lands rows. Seat A is on the play, so A takes the odd turns.

func deck(extra ...string) []string {
	out := append([]string(nil), extra...)
	for len(out) < 40 {
		out = append(out, "Forest")
	}
	return out
}

func bdeck(extra ...string) []string {
	out := append([]string(nil), extra...)
	for len(out) < 40 {
		out = append(out, "Plains")
	}
	return out
}

// turnSpec is B's end step of turn 6: A's turn 7 comes next (upstream's
// eot_rollover form). labels is spliced in verbatim.
func turnSpec(labels string) string {
	a, _ := json.Marshal(deck("Llanowar Elves", "Guarded Heir", "Pacifism", "Giant Growth", "Dwynen's Elite", "Savannah Lions"))
	b, _ := json.Marshal(bdeck("Serra Angel", "Savannah Lions"))
	return `{"version":1,"turn":6,"activePlayer":"B","phase":"END","step":"END_TURN",
	"enterMode":"PRIORITY_FRESH","priorityPlayer":"B","startingPlayer":"A",
	"players":{
	 "A":{"name":"A","life":18,"decklist":` + string(a) + `,"decklistSource":"exact","landsPlayed":0,
	  "hand":["Giant Growth","Forest"],"handUnknown":0,"graveyard":["Savannah Lions"],"libraryTop":["Dwynen's Elite"],
	  "battlefield":[
	   {"name":"Forest","count":3,"tapped":true,"sick":false,"damage":0,"faceDown":false},
	   {"name":"Llanowar Elves","id":"elves","count":1,"tapped":false,"sick":false,"damage":0,"faceDown":false},
	   {"name":"Guarded Heir","id":"heir","count":1,"tapped":false,"sick":false,"damage":0,"faceDown":false,"counters":{"P1P1":1}},
	   {"name":"Pacifism","id":"paci","count":1,"tapped":false,"sick":false,"damage":0,"faceDown":false,"attachTo":"B:lions"}]},
	 "B":{"name":"B","life":14,"decklist":` + string(b) + `,"decklistSource":"belief","landsPlayed":1,
	  "hand":["Plains"],"handUnknown":0,
	  "battlefield":[
	   {"name":"Plains","count":4,"tapped":true,"sick":false,"damage":0,"faceDown":false},
	   {"name":"Serra Angel","id":"angel","count":1,"tapped":false,"sick":true,"damage":0,"faceDown":false},
	   {"name":"Savannah Lions","id":"lions","count":1,"tapped":false,"sick":false,"damage":0,"faceDown":false},
	   {"tokenClass":"CatToken3","id":"cat","count":2,"tapped":false,"sick":true,"damage":0,"faceDown":false}]}},
	"provenance":{"source":"synthetic","tier":"T0"},
	"labels":` + labels + `}`
}

// blockSpec is B's turn 8, declare attackers, with the angel and one cat
// attacking A (upstream's declare_attackers form).
func blockSpec(labels string) string {
	s := turnSpec(labels)
	if !strings.Contains(s, `"step":"END_TURN",
	"enterMode":"PRIORITY_FRESH"`) {
		panic("fixture edit would miss")
	}
	s = strings.Replace(s, `"turn":6,"activePlayer":"B","phase":"END","step":"END_TURN",
	"enterMode":"PRIORITY_FRESH"`, `"turn":8,"activePlayer":"B","phase":"COMBAT","step":"DECLARE_ATTACKERS",
	"enterMode":"PRIORITY_HELD","attackers":[{"attacker":"B:angel","defender":"player:A"},{"attacker":"B:cat#2","defender":"player:A"}]`, 1)
	// A's forests untapped in its own turn 7; the angel and cats are no
	// longer sick in B's turn.
	s = strings.Replace(s, `{"name":"Forest","count":3,"tapped":true`, `{"name":"Forest","count":3,"tapped":false`, 1)
	s = strings.Replace(s, `"id":"angel","count":1,"tapped":false,"sick":true`, `"id":"angel","count":1,"tapped":false,"sick":false`, 1)
	s = strings.Replace(s, `"id":"cat","count":2,"tapped":false,"sick":true`, `"id":"cat","count":2,"tapped":false,"sick":false`, 1)
	return s
}

const (
	spellLabels = `{"user_turn":4,"global_turn":7,"lands":[{"name":"Forest","key":"Play Forest"}],
	 "casts":[{"name":"Dwynen's Elite","key":"Cast Dwynen's Elite"}],"attacks":{"A:heir":false},
	 "bridge":{"decisionPlayer":"A","decideFrom":{"turn":7,"step":"PRECOMBAT_MAIN"}}}`
	holdLabels = `{"user_turn":4,"global_turn":7,"lands":[],"casts":[],"attacks":{"A:heir":true},
	 "bridge":{"decisionPlayer":"A","decideFrom":{"turn":7,"step":"PRECOMBAT_MAIN"}}}`
	blockLabels = `{"user_turn":4,"opp_turn":4,"global_turn":8,"blocks":[["A:heir","B:cat#2",true]],"block_pairing":"unique",
	 "bridge":{"decisionPlayer":"A","decideFrom":{"turn":8,"step":"DECLARE_BLOCKERS"}}}`
)

func mustSpec(t *testing.T, s string) *statespec.Spec {
	t.Helper()
	spec, err := statespec.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	if errs := spec.Validate(); len(errs) > 0 {
		t.Fatalf("invalid fixture: %v", errs)
	}
	return spec
}

func refusalCode(err error) string {
	var r *Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

func TestStateSpecDecodesStrictlyWithUpstreamDefaults(t *testing.T) {
	spec := mustSpec(t, turnSpec(spellLabels))
	if p := spec.Players["A"].Battlefield[1]; p.Count != 1 || p.ID != "elves" {
		t.Fatalf("perm %+v", p)
	}
	min := `{"players":{"A":{"decklist":[],"battlefield":[{"name":"Forest"}]},"B":{"decklist":[],"battlefield":[]}}}`
	s, err := statespec.Parse([]byte(min))
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != 1 || s.Turn != 1 || s.ActivePlayer != "A" || s.Step != "PRECOMBAT_MAIN" || s.EnterMode != "PRIORITY_FRESH" ||
		s.Players["A"].Life != 20 || s.Players["A"].DecklistSource != "exact" || s.Players["A"].Battlefield[0].Count != 1 ||
		s.Provenance.Source != "synthetic" {
		t.Fatalf("defaults %+v", s)
	}
	for name, bad := range map[string]string{
		"top":      `{"bogus":1,"players":{}}`,
		"player":   `{"players":{"A":{"bogus":1},"B":{}}}`,
		"perm":     `{"players":{"A":{"battlefield":[{"name":"Forest","bogus":1}]},"B":{}}}`,
		"trailing": `{"players":{}} {}`,
		"ai":       `{"ai":{},"players":{}}`,
	} {
		if _, err := statespec.Parse([]byte(bad)); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
	// Validation mirrors upstream's messages.
	errs := s.Validate()
	if len(errs) == 0 || !strings.Contains(strings.Join(errs, "|"), "decklist has 0 cards") {
		t.Fatalf("validate %v", errs)
	}
	for name, mut := range map[string][2]string{
		"attach": {`"attachTo":"B:lions"`, `"attachTo":"B:nobody"`},
		"untap":  {`"step":"END_TURN"`, `"step":"UNTAP"`},
		"phase":  {`"phase":"END"`, `"phase":"COMBAT"`},
	} {
		sp, err := statespec.Parse([]byte(strings.Replace(turnSpec(spellLabels), mut[0], mut[1], 1)))
		if err != nil {
			t.Fatal(err)
		}
		if len(sp.Validate()) == 0 {
			t.Errorf("%s: validated", name)
		}
	}
}

func TestStateSpecAliases(t *testing.T) {
	spec := mustSpec(t, turnSpec(spellLabels))
	al := spec.Aliases()
	for ref, want := range map[string]statespec.Alias{
		"A:elves": {Seat: "A", Index: 1, Copy: 1}, "B:cat": {Seat: "B", Index: 3, Copy: 1},
		"B:cat#1": {Seat: "B", Index: 3, Copy: 1}, "B:cat#2": {Seat: "B", Index: 3, Copy: 2},
	} {
		if got, ok := al[ref]; !ok || got != want {
			t.Errorf("%s: %+v %v", ref, got, ok)
		}
	}
	if _, ok := al["B:cat#3"]; ok {
		t.Errorf("B:cat#3 resolved")
	}
	if len(al) != 8 {
		t.Errorf("%d aliases", len(al))
	}
}

func TestEveryFDNTokenClassResolves(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tk := range fdnTokens {
		if _, err := ResolveToken(reg, tk.class, ""); err != nil {
			t.Errorf("%s: %v", tk.class, err)
		}
	}
	if _, err := ResolveToken(reg, "NoSuchToken", ""); refusalCode(err) != "unknown token" {
		t.Errorf("unknown class: %v", err)
	}
	if _, err := ResolveToken(reg, "", "Beast"); refusalCode(err) != "unknown token" {
		t.Errorf("bare token name: %v", err)
	}
}

func TestMaterializePlacesTheSpecWithoutFiringAnything(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	spec := mustSpec(t, turnSpec(spellLabels))
	m, err := Materialize(reg, spec, 11)
	if err != nil {
		t.Fatal(err)
	}
	e := m.Engine
	if msg := checkStaged(m); msg != "" {
		t.Fatal(msg)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush || ev.Kind == events.AbilityPush || ev.Kind == events.PutOnStack {
			t.Fatalf("staging logged %v", ev.Kind)
		}
	}
	if m.Tokens["B:cat"] != "w_1_1_cat" {
		t.Fatalf("cat token %q", m.Tokens["B:cat"])
	}
	if o := e.G.Obj(m.Alias["A:paci"]); o.AttachedTo != m.Alias["B:lions"] {
		t.Fatalf("pacifism attached to %d", o.AttachedTo)
	}
	if o := e.G.Obj(m.Alias["A:heir"]); o.Counter("P1P1") != 1 || o.SummonSick {
		t.Fatalf("heir counters %d sick %v", o.Counter("P1P1"), o.SummonSick)
	}
	for _, ref := range []string{"B:angel", "B:cat#1", "B:cat#2"} {
		if o := e.G.Obj(m.Alias[ref]); !o.SummonSick || o.EnteredThisTurn {
			t.Fatalf("%s sick %v entered %v", ref, o.SummonSick, o.EnteredThisTurn)
		}
	}
	for _, id := range m.Perms["A"][0] {
		if !e.G.Obj(id).Tapped {
			t.Fatalf("forest untapped")
		}
	}
	lib := e.G.Zone(state.ZLibrary, 0)
	if objName(e, lib[0]) != "Dwynen's Elite" {
		t.Fatalf("library top %s", objName(e, lib[0]))
	}
	// The Guarded Heir ETB (two Knight tokens) did not happen, and B's
	// Serra Angel keeps its sickness into A's turn 7.
	r, err := Reach(e, DecisionSpell, mustOpts(t, spec, DecisionSpell))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(e.G.Zone(state.ZBattlefield, 0)) + len(e.G.Zone(state.ZBattlefield, 1)); got != 15 {
		t.Fatalf("battlefield %d, want 14 staged + the preLand", got)
	}
	if !r.PreLandPlayed || e.G.Players[0].LandsPlayed != 1 {
		t.Fatalf("preLand %v lands played %d", r.PreLandPlayed, e.G.Players[0].LandsPlayed)
	}
	if o := e.G.Obj(m.Alias["B:angel"]); !o.SummonSick {
		t.Fatalf("B's angel lost its sickness in A's turn")
	}
}

func mustOpts(t *testing.T, spec *statespec.Spec, kind DecisionType) ReachOptions {
	t.Helper()
	o, err := ItemReachOptions(spec, kind, 3)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func buildItem(t *testing.T, s string, kind DecisionType) (*Materialized, *Canon, ItemLabel, error) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	spec := mustSpec(t, s)
	if err := PrecheckKind(spec, kind); err != nil {
		return nil, nil, ItemLabel{}, err
	}
	m, err := Materialize(reg, spec, 5)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Reach(m.Engine, kind, mustOpts(t, spec, kind))
	if err != nil {
		return m, nil, ItemLabel{}, err
	}
	c, err := BuildCanon(m, r)
	if err != nil {
		return m, nil, ItemLabel{}, err
	}
	l, err := LabelItem(m, c)
	return m, c, l, err
}

func indexOf(c *Canon, label string) int {
	for i, o := range c.Options {
		if o.Label == label {
			return i
		}
	}
	return -1
}

func TestSpellItem(t *testing.T) {
	m, c, l, err := buildItem(t, turnSpec(spellLabels), DecisionSpell)
	if err != nil {
		t.Fatal(err)
	}
	e, d := m.Engine, c.Decision
	if d.Kind != decision.KPriority || d.Player != 0 || e.G.Turn != 7 || e.G.Step != state.StepMain1 {
		t.Fatalf("reached %s for %d at %d %v", d.Kind, d.Player, e.G.Turn, e.G.Step)
	}
	if c.Options[0].Label != "Pass" || c.Options[0].Act {
		t.Fatalf("index 0 %+v", c.Options[0])
	}
	hunter, growth := indexOf(c, "Cast Dwynen's Elite"), indexOf(c, "Cast Giant Growth")
	if hunter < 0 || growth < 0 {
		t.Fatalf("canon %q", c.Labels())
	}
	if !l.Act || len(l.Strict) != 1 || l.Strict[0] != hunter || len(l.Members) != 1 {
		t.Fatalf("label %+v (canon %q)", l, c.Labels())
	}
	// Project: pass, and every payment action onto its card's cast.
	pass := passIntent(d)
	if i, act, err := c.Project(e, pass); err != nil || i != 0 || act {
		t.Fatalf("project pass %d %v %v", i, act, err)
	}
	pays := e.EnsurePaymentActions()
	if len(pays) == 0 {
		t.Fatalf("no payment actions")
	}
	for _, pa := range pays {
		if len(pa.Plans) == 0 {
			continue
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Payment: &decision.PaymentSelection{ActionID: pa.ID, Plan: pa.Plans[0]}}
		i, act, err := c.Project(e, in)
		if err != nil || !act || c.Options[i].Label != "Cast "+objName(e, pa.Cast.Object) {
			t.Fatalf("project %s: %d %v %v", pa.Label, i, act, err)
		}
	}
	// A mana activation has no canonical option.
	for _, o := range d.Options {
		if o.Kind == "activate" {
			if _, _, err := c.Project(e, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err == nil {
				t.Fatalf("mana activation projected")
			}
			break
		}
	}
}

func TestHoldItem(t *testing.T) {
	_, c, l, err := buildItem(t, turnSpec(holdLabels), DecisionHold)
	if err != nil {
		t.Fatal(err)
	}
	if l.Act || len(l.Members) != 1 || l.Members[0] != 0 || indexOf(c, "Cast Giant Growth") < 0 {
		t.Fatalf("label %+v canon %q", l, c.Labels())
	}
	// A cast recorded that turn makes it no hold item.
	if _, _, _, err := buildItem(t, turnSpec(spellLabels), DecisionHold); refusalCode(err) != "cast" {
		t.Fatalf("hold with a cast: %v", err)
	}
}

func TestAttackItem(t *testing.T) {
	m, c, l, err := buildItem(t, turnSpec(holdLabels), DecisionAttack)
	if err != nil {
		t.Fatal(err)
	}
	e, d := m.Engine, c.Decision
	if d.Kind != decision.KAttackers || e.G.Turn != 7 || c.FocusAlias != "A:heir" {
		t.Fatalf("reached %s at %d focus %s", d.Kind, e.G.Turn, c.FocusAlias)
	}
	if len(c.Options) != 2 || c.Options[0].Act || !c.Options[1].Act {
		t.Fatalf("canon %q", c.Labels())
	}
	if !l.Act || len(l.Members) != 1 || l.Members[0] != 1 {
		t.Fatalf("label %+v", l)
	}
	var heir, other []int
	for _, o := range d.Options {
		if o.Obj == c.Focus {
			heir = append(heir, o.Index)
		} else {
			other = append(other, o.Index)
		}
	}
	if i, act, _ := c.Project(e, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: heir[:1]}); i != 1 || !act {
		t.Fatalf("heir attacks: %d %v", i, act)
	}
	if i, act, _ := c.Project(e, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: other}); i != 0 || act {
		t.Fatalf("heir stays home: %d %v", i, act)
	}
	// A recorded cast makes it no attack item; a closed defender refuses.
	if _, _, _, err := buildItem(t, turnSpec(spellLabels), DecisionAttack); refusalCode(err) != "cast" {
		t.Fatalf("attack with a cast: %v", err)
	}
}

func TestBlockItem(t *testing.T) {
	m, c, l, err := buildItem(t, blockSpec(blockLabels), DecisionBlock)
	if err != nil {
		t.Fatal(err)
	}
	e, d := m.Engine, c.Decision
	if d.Kind != decision.KBlockers || d.Player != 0 || e.G.Turn != 8 || e.G.Step != state.StepDeclareBlockers {
		t.Fatalf("reached %s for %d at %d %v", d.Kind, d.Player, e.G.Turn, e.G.Step)
	}
	if c.FocusAlias != "A:elves" && c.FocusAlias != "A:heir" {
		t.Fatalf("focus %s", c.FocusAlias)
	}
	// The labels name only the heir, so X is the heir; it cannot block the
	// flying angel.
	if c.FocusAlias != "A:heir" || strings.Join(c.Labels(), "|") != "No block|Block B:cat#2" {
		t.Fatalf("focus %s canon %q", c.FocusAlias, c.Labels())
	}
	if !l.Act || len(l.Members) != 1 || l.Members[0] != 1 {
		t.Fatalf("label %+v", l)
	}
	for _, o := range d.Options {
		if o.Obj == c.Focus {
			if i, act, err := c.Project(e, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil || i != 1 || !act {
				t.Fatalf("project block %d %v %v", i, act, err)
			}
		}
	}
	if i, act, _ := c.Project(e, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); i != 0 || act {
		t.Fatalf("no block projected %d %v", i, act)
	}
	// Staged attackers: the angel (vigilance) untapped, the cat tapped.
	if o := e.G.Obj(m.Alias["B:angel"]); !o.IsAttacking || o.Tapped {
		t.Fatalf("angel attacking %v tapped %v", o.IsAttacking, o.Tapped)
	}
	if o := e.G.Obj(m.Alias["B:cat#2"]); !o.IsAttacking || !o.Tapped {
		t.Fatalf("cat attacking %v tapped %v", o.IsAttacking, o.Tapped)
	}
}

func TestBeginStepEntersTheStep(t *testing.T) {
	// A's own upkeep of turn 7, run from its start: the draw happens.
	s := strings.Replace(turnSpec(spellLabels), `"turn":6,"activePlayer":"B","phase":"END","step":"END_TURN",
	"enterMode":"PRIORITY_FRESH"`, `"turn":7,"activePlayer":"A","phase":"BEGINNING","step":"UPKEEP",
	"enterMode":"BEGIN_STEP"`, 1)
	s = strings.Replace(s, `"id":"angel","count":1,"tapped":false,"sick":true`, `"id":"angel","count":1,"tapped":false,"sick":false`, 1)
	s = strings.Replace(s, `"id":"cat","count":2,"tapped":false,"sick":true`, `"id":"cat","count":2,"tapped":false,"sick":false`, 1)
	s = strings.Replace(s, `{"name":"Forest","count":3,"tapped":true`, `{"name":"Forest","count":3,"tapped":false`, 1)
	if !strings.Contains(s, `"step":"UPKEEP"`) {
		t.Fatal("fixture edit missed")
	}
	m, c, _, err := buildItem(t, s, DecisionSpell)
	if err != nil {
		t.Fatal(err)
	}
	if indexOf(c, "Cast Dwynen's Elite") < 0 {
		t.Fatalf("the upkeep entry did not draw: %q", c.Labels())
	}
	_ = m
}

func TestMaterializeAndReachAreDeterministic(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	spec := mustSpec(t, turnSpec(spellLabels))
	heads := map[uint64]string{}
	for _, seed := range []uint64{1, 1, 2} {
		m, err := Materialize(reg, spec, seed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Reach(m.Engine, DecisionSpell, mustOpts(t, spec, DecisionSpell)); err != nil {
			t.Fatal(err)
		}
		h := m.Engine.L.Head()
		if prev, ok := heads[seed]; ok && prev != h {
			t.Fatalf("seed %d: heads %s %s", seed, prev, h)
		}
		heads[seed] = h
	}
	if heads[1] == heads[2] {
		t.Fatalf("seed does not reach the library order")
	}
}

func TestReachRefusesAWrongTurn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	spec := mustSpec(t, turnSpec(spellLabels))
	m, err := Materialize(reg, spec, 1)
	if err != nil {
		t.Fatal(err)
	}
	o := mustOpts(t, spec, DecisionBlock)
	o.Turn = 7 // A's own turn: no block decision for A in it
	if _, err := Reach(m.Engine, DecisionBlock, o); refusalCode(err) != "reached" && refusalCode(err) != "wrong turn" {
		t.Fatalf("block in A's turn: %v", err)
	}
	m, _ = Materialize(reg, spec, 1)
	o = mustOpts(t, spec, DecisionSpell)
	o.PreLand = "Island"
	if _, err := Reach(m.Engine, DecisionSpell, o); refusalCode(err) != "preland" {
		t.Fatalf("absent preLand: %v", err)
	}
}
