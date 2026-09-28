package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

func TestMustAttackYouBindsRegistrationController(t *testing.T) {
	t.Parallel()
	deck := func() []*cards.Card {
		out := make([]*cards.Card, 40)
		for i := range out {
			out[i] = card(t, "Name:Mountain\nTypes:Basic Land\nOracle:x\n")
		}
		return out
	}
	e := New(Config{Seed: 11, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{deck(), deck(), deck()}})
	cardDef := choiceCorpusCard(t, "Alluring Siren")
	source := onBoardReadyCard(t, e, 0, cardDef)
	token := onBoardReady(t, e, 1, "Name:Remembered Creature\nTypes:Creature\nPT:2/2\nOracle:x\n")
	if o := e.G.Obj(token); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: remembered creature must be on battlefield")
	}
	line := cardDef.Faces[0].SVars["MustAttack"]
	if !strings.Contains(line, "MustAttack$ You") || !strings.Contains(line, "ValidCreature$ Creature.IsRemembered") {
		t.Fatalf("precondition: Alluring Siren MustAttack SVar is not its You requirement: %q", line)
	}
	if len(cardDef.Faces[0].Abilities) != 1 {
		t.Fatalf("precondition: expected Alluring Siren's one compiled activated ability, got %d", len(cardDef.Faces[0].Abilities))
	}
	ability := cardDef.Faces[0].Abilities[0]
	if ability.API != "Effect" || ability.Params["RememberObjects"] != "Targeted" || ability.Params["StaticAbilities"] != "MustAttack" {
		t.Fatalf("precondition: Alluring Siren compiled ability = %+v", ability)
	}
	ctx := &effects.Ctx{Source: source, Controller: 0, SVars: cardDef.Faces[0].SVars,
		Targets: []state.Target{{Obj: token}}, OfferedSA: ability}
	effects.Resolve(e, ctx, ability)
	regs := 0
	for _, ce := range e.active() {
		if ce.Restriction == "MustAttack" {
			regs++
			if len(ce.Remembered) != 1 || ce.Remembered[0] != token {
				t.Fatalf("Alluring Siren captured %v, want targeted creature %d", ce.Remembered, token)
			}
			if ce.Controller != 0 {
				t.Fatalf("Alluring Siren registration controller = %d, want 0", ce.Controller)
			}
		}
	}
	if regs != 1 {
		t.Fatalf("expected Alluring Siren's real ability to register MustAttack once, got %d", regs)
	}

	rs := e.attackRequirements(token)
	if !rs.any() || rs.named[0] != 1 {
		t.Fatalf("MustAttack$ You should require the registration controller 0, got %+v", rs)
	}
	if rs.named[1] != 0 || !e.requiredForDefender(token, 0) || e.requiredForDefender(token, 1) {
		t.Fatalf("precondition/assertion: controller and opponent must differ; requirements=%+v", rs.named)
	}
}

func TestMustAttackYouStaticUsesSourceController(t *testing.T) {
	t.Parallel()
	deck := func() []*cards.Card {
		out := make([]*cards.Card, 40)
		for i := range out {
			out[i] = card(t, "Name:Mountain\nTypes:Basic Land\nOracle:x\n")
		}
		return out
	}
	e := New(Config{Seed: 13, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{deck(), deck(), deck()}})
	source := onBoardReady(t, e, 0, "Name:Static Source\nTypes:Creature\nPT:2/2\nOracle:x\nS:Mode$ MustAttack | ValidCreature$ Creature | MustAttack$ You\n")
	creature := onBoardReady(t, e, 1, "Name:Attacker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	if e.G.Obj(source).Zone != state.ZBattlefield || e.G.Obj(creature).Zone != state.ZBattlefield {
		t.Fatal("precondition: static source and affected creature must be on battlefield")
	}
	if e.G.Obj(source).Controller == e.G.Obj(creature).Controller {
		t.Fatal("precondition: static controller and affected creature controller must differ")
	}
	rs := e.attackRequirements(creature)
	if !rs.any() || rs.named[0] != 1 || rs.named[1] != 0 {
		t.Fatalf("static MustAttack$ You should name source controller 0: %+v", rs.named)
	}
	if !e.requiredForDefender(creature, 0) || e.requiredForDefender(creature, 1) {
		t.Fatalf("static requirement names wrong defender: %+v", rs.named)
	}
}

func TestMustAttackRememberedBindsUniqueCapturedPlayer(t *testing.T) {
	t.Parallel()
	deck := func() []*cards.Card {
		out := make([]*cards.Card, 40)
		for i := range out {
			out[i] = card(t, "Name:Mountain\nTypes:Basic Land\nOracle:x\n")
		}
		return out
	}
	e := New(Config{Seed: 12, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{deck(), deck(), deck()}})
	source := onBoardReady(t, e, 0, "Name:Source\nTypes:Creature\nPT:2/2\nOracle:x\n")
	token := onBoardReady(t, e, 0, "Name:Remembered Creature\nTypes:Creature\nPT:2/2\nOracle:x\n")
	if o := e.G.Obj(token); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: remembered creature must be on battlefield")
	}
	cardDef := choiceCorpusCard(t, "Dulcet Sirens")
	line := cardDef.Faces[0].SVars["MustAttack"]
	if !strings.Contains(line, "MustAttack$ Remembered") || !strings.Contains(line, "ValidCreature$ Creature.IsRemembered") {
		t.Fatalf("precondition: Dulcet Sirens must carry the exact Remembered player form: %q", line)
	}
	db := cards.ResolveSVar(cardDef.Faces[0].SVars, "DBEffect")
	if db == nil || db.API != "Effect" || db.Params["RememberObjects"] != "ParentTarget & Targeted" || db.Params["StaticAbilities"] != "MustAttack" {
		t.Fatalf("precondition: Dulcet Sirens compiled DBEffect = %+v", db)
	}
	ability := cardDef.Faces[0].Abilities[0]
	if ability.API != "Pump" || ability.Sub == nil || ability.Sub.API != "Effect" || ability.Sub.Line != db.Line {
		t.Fatalf("precondition: Dulcet Sirens root/sub ability chain = %+v / %+v", ability, ability.Sub)
	}
	playerTarget := state.Target{Player: 2, IsPlayer: true}
	ctx := &effects.Ctx{Source: source, Controller: 0, SVars: cardDef.Faces[0].SVars,
		Targets: []state.Target{{Obj: token}}, TargetsOffered: true, OfferedSA: ability,
		SubPreAsk: map[string][]state.Target{db.Line: {playerTarget}}}
	effects.Resolve(e, ctx, ability)

	regs := 0
	for _, ce := range e.active() {
		if ce.Restriction == "MustAttack" {
			regs++
			if len(ce.RememberedPlayers) != 1 || ce.RememberedPlayers[0] != 2 {
				t.Fatalf("captured player = %v, want unique player 2", ce.RememberedPlayers)
			}
			if len(ce.Remembered) != 1 || ce.Remembered[0] != token {
				t.Fatalf("ParentTarget object capture = %v, want parent creature %d (Targeted is the player half)", ce.Remembered, token)
			}
		}
	}
	if regs != 1 {
		t.Fatalf("expected one MustAttack registration, got %d", regs)
	}
	rs := e.attackRequirements(token)
	if !rs.any() || rs.named[2] != 1 || rs.named[0] != 0 {
		t.Fatalf("MustAttack$ Remembered should require distinct captured player 2, got %+v", rs)
	}
	if !e.requiredForDefender(token, 2) || e.requiredForDefender(token, 0) {
		t.Fatalf("wrong defender required: %+v", rs.named)
	}
}
