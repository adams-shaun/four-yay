package rules

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

func TestPhyrexianGrimoirePumpAllRememberPumped(t *testing.T) {
	reg := choiceCorpusRegistry(t)
	grimoire := choiceCorpusCard(t, "Phyrexian Grimoire")
	const aSrc = "Name:Grave One\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	const bSrc = "Name:Grave Two\nTypes:Creature Bear\nPT:3/3\nOracle:x\n"
	const cSrc = "Name:Below Top Two\nTypes:Creature Bear\nPT:4/4\nOracle:x\n"
	e := corpusEngine(t, reg, []*cards.Card{grimoire, card(t, aSrc), card(t, bSrc), card(t, cSrc)}, nil)

	ability := grimoire.Faces[0].Abilities[0]
	if ability.API != "PumpAll" || strings.TrimSpace(ability.Params["PumpZone"]) != "Graveyard" ||
		ability.Params["ValidCards"] != "Card.TopGraveyard2+YouCtrl" ||
		!strings.EqualFold(strings.TrimSpace(ability.Params["RememberPumped"]), "True") {
		t.Fatalf("precondition: compiled Grimoire ability is not the expected PumpAll: %+v", ability)
	}
	src := findCardObj(t, e, 0, "Phyrexian Grimoire", state.ZHand)
	first := addToGraveyard(t, e, 0, aSrc)
	second := addToGraveyard(t, e, 0, bSrc)
	control := moveSeeded(t, e, 0, cSrc, state.ZBattlefield)
	grave := e.G.Zone(state.ZGraveyard, 0)
	if len(grave) < 2 || grave[len(grave)-2] != first || grave[len(grave)-1] != second || e.G.Obj(control).Zone != state.ZBattlefield {
		t.Fatalf("precondition: expected ordered graveyard fixtures %d,%d and battlefield control %d, got graveyard %v", first, second, control, grave)
	}

	// The corpus's TopGraveyard2 qualifier is retained and asserted above,
	// but that filter predicate is not currently implemented by MatchesSpec.
	// Keep the real PumpAll/PumpZone carrier and constrain this regression to
	// its supported Card filter so the test isolates RememberPumped.
	pumpOnly := *ability
	pumpOnly.Params = make(map[string]string, len(ability.Params))
	for key, value := range ability.Params {
		pumpOnly.Params[key] = value
	}
	pumpOnly.Params["ValidCards"] = "Card"
	pumpOnly.Sub = nil
	if !effects.MatchesSpecCtx(e.G, pumpOnly.Params["ValidCards"], first, effects.SpecContext{You: 0}) ||
		!effects.MatchesSpecCtx(e.G, pumpOnly.Params["ValidCards"], second, effects.SpecContext{You: 0}) {
		t.Fatal("precondition: both distinct graveyard cards must match the exercised PumpAll filter")
	}
	ctx := &effects.Ctx{Source: src, Controller: 0, SVars: grimoire.Faces[0].SVars}
	effects.Resolve(e, ctx, &pumpOnly)
	want := []state.ObjID{first, second}
	got := rememberedIDs(ctx.Remembered)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ctx remembered = %v, want top two eligible cards in zone order %v", got, want)
	}
	if gotPersistent := rememberedIDs(e.G.Obj(src).Remembered); !reflect.DeepEqual(gotPersistent, want) {
		t.Fatalf("source event-backed remembered = %v, want %v", gotPersistent, want)
	}
	for _, id := range got {
		if e.G.Obj(id).Zone != state.ZGraveyard {
			t.Fatalf("precondition: remembered object %d was not pumped in the graveyard", id)
		}
	}
}

func TestPumpAllRememberPumpedBattlefield(t *testing.T) {
	t.Parallel()
	reg := choiceCorpusRegistry(t)
	const sourceSrc = "Name:Pump Source\nTypes:Artifact\nOracle:x\n"
	const bearSrc = "Name:Pump Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	const otherSrc = "Name:Other Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e := corpusEngine(t, reg, []*cards.Card{card(t, sourceSrc), card(t, bearSrc), card(t, otherSrc)}, []*cards.Card{card(t, bearSrc)})
	src := findCardObj(t, e, 0, "Pump Source", state.ZHand)
	first := moveSeeded(t, e, 0, bearSrc, state.ZBattlefield)
	second := moveSeeded(t, e, 0, otherSrc, state.ZBattlefield)
	enemy := moveSeeded(t, e, 1, bearSrc, state.ZBattlefield)
	if first == second || first == enemy || second == enemy || e.G.Obj(first).Zone != state.ZBattlefield || e.G.Obj(second).Zone != state.ZBattlefield || e.G.Obj(enemy).Zone != state.ZBattlefield {
		t.Fatal("precondition: three distinct battlefield creatures are required")
	}
	ability := &cards.SA{Kind: "DB", API: "PumpAll", Params: map[string]string{
		"ValidCards": "Creature.YouCtrl", "NumAtt": "+1", "RememberPumped": " True ",
	}}
	ctx := &effects.Ctx{Source: src, Controller: 0}
	effects.Resolve(e, ctx, ability)
	want := []state.ObjID{first, second}
	if got := rememberedIDs(ctx.Remembered); !reflect.DeepEqual(got, want) {
		t.Fatalf("ctx remembered = %v, want matching battlefield objects in zone order %v", got, want)
	}
	if got := rememberedIDs(e.G.Obj(src).Remembered); !reflect.DeepEqual(got, want) {
		t.Fatalf("source event-backed remembered = %v, want %v", got, want)
	}
	for _, id := range want {
		found := false
		for _, ce := range e.active() {
			if ce.Source == id && ce.AddPower == 1 {
				found = true
			}
		}
		if !found {
			t.Fatalf("matching creature %d was remembered but no +1/+0 pump was registered", id)
		}
	}
}

func TestParamCensusReadsPumpAllRememberPumped(t *testing.T) {
	t.Parallel()
	base, d := measureParamCensus(t, nil)
	if d == nil || !d.api["PumpAll"]["RememberPumped"] {
		t.Fatal("param census does not derive the RememberPumped$ read for api:PumpAll")
	}
	for card, labels := range base.labels {
		for _, label := range labels {
			if label == "param:api:PumpAll.RememberPumped" {
				t.Fatalf("%s is reported unsupported for param:api:PumpAll.RememberPumped despite the read", card)
			}
		}
	}
}

func rememberedIDs(targets []state.Target) []state.ObjID {
	ids := make([]state.ObjID, 0, len(targets))
	for _, target := range targets {
		if !target.IsPlayer {
			ids = append(ids, target.Obj)
		}
	}
	return ids
}
