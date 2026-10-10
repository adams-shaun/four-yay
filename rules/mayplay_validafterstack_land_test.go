package rules

import (
	"maps"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// A printed MayPlay grant's ValidAfterStack$ Spell.<...> qualifies SPELLS
// ("play lands and cast spells with mana value 4 or greater"): a land is
// played, never cast (CR 305.1), so the qualifier must not be asked of it.
// These tests drive the real Glarb, Calamity's Augur / Zask, Skittering
// Swarmlord / Serra Paragon statics.

// landGrantPriority advances e to seat 0's main-phase priority decision with
// plenty of mana of every colour so mana is never the reason for an absent
// option.
func landGrantPriority(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for c := range e.G.Players[0].Pool {
		e.G.Players[0].Pool[c] = 10
	}
	e.G.Step, e.G.Active, e.G.Priority, e.G.Turn = state.StepMain1, 0, 0, 1
	e.pending = nil
	e.Advance()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority = %+v, want priority for seat 0", d)
	}
	return d
}

func landGrantOptionIndex(d *decision.Decision, kind string, id state.ObjID) int {
	for _, o := range d.Options {
		if o.Kind == kind && o.Obj == id {
			return o.Index
		}
	}
	return -1
}

// glarbLibraryTop builds a game with Glarb on seat 0's battlefield and top
// as the top card of seat 0's library, returning the engine and top's object.
func glarbLibraryTop(t *testing.T, seed uint64, top *cards.Card) (*Engine, *state.Object, *state.Object) {
	t.Helper()
	e, _, _ := newFixtureDeck(t, seed, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	g := e.G.AddObject(mayPlayAfterStackCard(t, "Glarb, Calamity's Augur"), 0)
	g.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), g.ID))
	o := e.G.AddObject(top, 0)
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{o.ID}, e.G.Zone(state.ZLibrary, 0)...))
	if g.Zone != state.ZBattlefield || o.Zone != state.ZLibrary || e.G.Zone(state.ZLibrary, 0)[0] != o.ID {
		t.Fatal("precondition: Glarb on the battlefield and the probe on top of the library")
	}
	return e, g, o
}

func TestMayPlayValidAfterStackGlarbOffersTopLibraryLand(t *testing.T) {
	forest := card(t, "Name:Forest\nTypes:Land Forest\nOracle:x\n")
	e, _, top := glarbLibraryTop(t, 8201, forest)
	if !top.Face().IsLand() || top.Face().ManaValue() >= 4 {
		t.Fatal("precondition: the probe is a land whose mana value fails Spell.cmcGE4")
	}
	d := landGrantPriority(t, e)
	idx := landGrantOptionIndex(d, "play_land", top.ID)
	if idx < 0 {
		t.Fatalf("Glarb must offer the top-of-library land as play_land: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}
	if top.Zone != state.ZBattlefield || e.G.Players[0].LandsPlayed != 1 {
		t.Fatalf("land zone %v, LandsPlayed %d; want battlefield and 1", top.Zone, e.G.Players[0].LandsPlayed)
	}
}

func TestMayPlayValidAfterStackGlarbStillNarrowsSpells(t *testing.T) {
	cases := []struct {
		name     string
		wantCast bool
	}{{"Hill Giant", true}, {"Shock", false}}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := mayPlayAfterStackCard(t, tc.name)
			if mv := c.Faces[0].ManaValue(); (mv >= 4) != tc.wantCast || c.Faces[0].IsLand() {
				t.Fatalf("precondition: %s mana value %d, wantCast %v", tc.name, mv, tc.wantCast)
			}
			e, _, top := glarbLibraryTop(t, 8211+uint64(i), c)
			d := landGrantPriority(t, e)
			if got := landGrantOptionIndex(d, "cast", top.ID) >= 0; got != tc.wantCast {
				t.Fatalf("cast offered = %v, want %v: %+v", got, tc.wantCast, d.Options)
			}
			if landGrantOptionIndex(d, "play_land", top.ID) >= 0 {
				t.Fatalf("a nonland was offered as play_land: %+v", d.Options)
			}
		})
	}
}

func TestMayPlayValidAfterStackZaskOffersGraveyardLand(t *testing.T) {
	e, _, _ := newFixtureDeck(t, 8221, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	z := e.G.AddObject(mayPlayAfterStackCard(t, "Zask, Skittering Swarmlord"), 0)
	z.Zone = state.ZBattlefield
	forest := e.G.AddObject(card(t, "Name:Forest\nTypes:Land Forest\nOracle:x\n"), 0)
	insect := e.G.AddObject(card(t, "Name:Gnat\nManaCost:G\nTypes:Creature Insect\nPT:1/1\nOracle:x\n"), 0)
	bears := e.G.AddObject(mayPlayAfterStackCard(t, "Grizzly Bears"), 0)
	for _, o := range []*state.Object{forest, insect, bears} {
		o.Zone = state.ZGraveyard
	}
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), z.ID))
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{forest.ID, insect.ID, bears.ID})
	if z.Zone != state.ZBattlefield || !forest.Face().IsLand() ||
		!slices.Contains(insect.Face().Types, "Insect") || slices.Contains(bears.Face().Types, "Insect") {
		t.Fatal("precondition: Zask, a graveyard Forest, an Insect and a non-Insect")
	}
	d := landGrantPriority(t, e)
	if landGrantOptionIndex(d, "play_land", forest.ID) < 0 {
		t.Fatalf("Zask must offer the graveyard land: %+v", d.Options)
	}
	if landGrantOptionIndex(d, "cast", insect.ID) < 0 || landGrantOptionIndex(d, "cast", bears.ID) >= 0 {
		t.Fatalf("Zask must offer the Insect and not the non-Insect: %+v", d.Options)
	}
}

// The exemption lives in the ValidAfterStack$ gate itself: Affected$ is
// broadened to every card so only the qualifier can tell the two apart.
func TestMayPlayValidAfterStackGateExemptsLandsOnly(t *testing.T) {
	forest := card(t, "Name:Forest\nTypes:Land Forest\nOracle:x\n")
	e, glarb, land := glarbLibraryTop(t, 8231, forest)
	cheap := e.G.AddObject(mayPlayAfterStackCard(t, "Shock"), 0)
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{cheap.ID}, e.G.Zone(state.ZLibrary, 0)...))
	if cheap.Zone != state.ZLibrary || cheap.Face().ManaValue() >= 4 || land.Zone != state.ZLibrary {
		t.Fatal("precondition: a mana value 1 nonland and a land, both in the library")
	}
	for _, st := range glarb.Face().Statics {
		if st.Params["ValidAfterStack"] != "Spell.cmcGE4" {
			continue
		}
		params := maps.Clone(st.Params)
		params["Affected"] = "Card.YouCtrl"
		if applies, grants, _, _, _, _ := e.mayPlayStatic(params, cheap.ID, 0, glarb.ID); applies || grants {
			t.Fatalf("mv 1 nonland passed the ValidAfterStack gate: %v %v", applies, grants)
		}
		if applies, grants, _, _, _, _ := e.mayPlayStatic(params, land.ID, 0, glarb.ID); !applies || !grants {
			t.Fatalf("land rejected by the spell qualifier: %v %v", applies, grants)
		}
		return
	}
	t.Fatal("Glarb carrier static missing")
}

func TestMayPlayValidAfterStackSerraParagonKeepsGraveyardLand(t *testing.T) {
	e, _, _ := newFixtureDeck(t, 8241, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	s := e.G.AddObject(mayPlayAfterStackCard(t, "Serra Paragon"), 0)
	s.Zone = state.ZBattlefield
	forest := e.G.AddObject(card(t, "Name:Forest\nTypes:Land Forest\nOracle:x\n"), 0)
	forest.Zone = state.ZGraveyard
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), s.ID))
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{forest.ID})
	if s.Zone != state.ZBattlefield || !forest.Face().IsLand() {
		t.Fatal("precondition: Serra Paragon and a graveyard land")
	}
	d := landGrantPriority(t, e)
	if landGrantOptionIndex(d, "play_land", forest.ID) < 0 {
		t.Fatalf("Serra Paragon must still offer the graveyard land: %+v", d.Options)
	}
}

// The effect-delivered twin (MayPlayValidAfterStack$ on a ContinuousEffect)
// applies the same land exemption: a grant covering an Equipment and a land
// admits the land, still rejects a nonland that is not an Equipment.
func TestEffectGrantValidAfterStackExemptsLands(t *testing.T) {
	e := combatEngine(t)
	var ids []state.ObjID
	probe := func(c *cards.Card) *state.Object {
		o := e.G.AddObject(c, 0)
		o.Zone = state.ZExile
		ids = append(ids, o.ID)
		return o
	}
	equipment := probe(mayPlayAfterStackCard(t, "Bonesplitter"))
	land := probe(card(t, "Name:Forest\nTypes:Land Forest\nOracle:x\n"))
	bears := probe(mayPlayAfterStackCard(t, "Grizzly Bears"))
	e.G.SetZone(state.ZExile, 0, ids)
	if !slices.Contains(equipment.Face().Types, "Equipment") || !land.Face().IsLand() ||
		slices.Contains(bears.Face().Types, "Equipment") || bears.Face().IsLand() {
		t.Fatal("precondition: an Equipment, a land and a nonland non-Equipment in exile")
	}
	grant := state.ContinuousEffect{
		Source:                 equipment.ID,
		Controller:             0,
		Affects:                "Card.IsRemembered",
		MayPlay:                true,
		MayPlayValidAfterStack: "Spell.Equipment",
		Remembered:             ids,
	}
	if !e.effectGrantMatches(&grant, equipment.ID) || !e.effectGrantMatches(&grant, land.ID) {
		t.Fatal("the Equipment and the land must both be covered")
	}
	if e.effectGrantMatches(&grant, bears.ID) {
		t.Fatal("a nonland non-Equipment passed the Spell.Equipment qualifier")
	}
}
