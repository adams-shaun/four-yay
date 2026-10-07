package effects

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

type digMultipleHost struct {
	*fakeHost
	choices []int
	chosen  []int
	arrange []int
	asked   []*decision.Decision
}

func (h *digMultipleHost) AskCount() uint64 { return uint64(h.askCount) }
func (h *digMultipleHost) TapeAnswer(d *decision.Decision) (decision.Intent, bool) {
	h.asked = append(h.asked, d)
	if d.ResumeKind == "digmultiple_chosen" {
		return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: h.chosen}, true
	}
	if d.Kind == decision.KArrange {
		if h.arrange == nil {
			return decision.Intent{}, false
		}
		return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: h.arrange}, true
	}
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: h.choices}, true
}
func digMultipleBoard(t *testing.T) (*digMultipleHost, []state.ObjID) {
	t.Helper()
	h := &digMultipleHost{fakeHost: newHost(t, 2)}
	var ids []state.ObjID
	for _, text := range []string{
		"Name:BearA\nTypes:Creature\nPT:2/2\nOracle:x\n",
		"Name:BearB\nTypes:Creature\nPT:2/2\nOracle:x\n",
		"Name:Isle\nTypes:Basic Land Island\nOracle:x\n",
		"Name:Blast\nTypes:Sorcery\nOracle:x\n",
	} {
		ids = append(ids, h.g.AddObject(mkCard(t, text), 0).ID)
	}
	h.g.SetZone(state.ZLibrary, 0, ids)
	if ids[0] == ids[1] || ids[1] == ids[2] || !MatchesSpecCtx(h.g, "Creature", ids[0], NewSpecContext(0, 0)) || MatchesSpecCtx(h.g, "Creature", ids[2], NewSpecContext(0, 0)) || !MatchesSpecCtx(h.g, "Land", ids[2], NewSpecContext(0, 0)) {
		t.Fatal("precondition: distinct creature and land in library window")
	}
	return h, ids
}
func TestDigMultipleSelectionAndRemainder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		choices []int
	}{
		{name: "positive", choices: []int{1, 2}},
		{name: "decline", choices: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, ids := digMultipleBoard(t)
			h.choices = tc.choices
			Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ DigMultiple | DigNum$ 4 | Reveal$ True | ChangeValid$ Creature,Land | Optional$ True | DestinationZone2$ Graveyard"))
			if len(h.asked) == 0 || h.asked[0].ResumeKind != "digmultiple" || len(h.asked[0].Options) != 3 || !h.asked[0].DistinctTypePicks {
				t.Fatalf("decision %+v", h.asked)
			}
			hand := h.g.Zone(state.ZHand, 0)
			if tc.name == "positive" && !reflect.DeepEqual(hand, []state.ObjID{ids[1], ids[2]}) {
				t.Fatalf("hand %v, want second bear and land", hand)
			}
			if tc.name == "decline" && len(hand) != 0 {
				t.Fatalf("decline hand %v", hand)
			}
			wantRest := []state.ObjID{ids[0], ids[3]}
			if tc.name == "decline" {
				wantRest = ids
			}
			if got := h.g.Zone(state.ZGraveyard, 0); !reflect.DeepEqual(got, wantRest) {
				t.Fatalf("rest %v, want %v", got, wantRest)
			}
			for _, ev := range h.log {
				if ev.Kind == events.MoveZone && (!ev.Secret || ev.Player != 0) {
					t.Fatalf("library move leaked: %+v", ev)
				}
			}
		})
	}
}
func TestDigMultipleOjerCorpus(t *testing.T) {
	_, ability, vars := corpusRiderSA(t, "Ojer Kaslem, Deepest Growth", "TrigDig")
	if ability.API != "DigMultiple" || ability.Params["DestinationZone"] != "Battlefield" || ability.Params["RestRandomOrder"] != "True" {
		t.Fatalf("precondition: corpus Ojer ability %+v", ability)
	}
	h, ids := digMultipleBoard(t)
	h.choices = []int{1, 2}
	c := &Ctx{Controller: 0, SVars: vars, TriggerContext: TriggerContext{TriggerAmount: 4}}
	if got := numText(h, c, compileDigMultiple(ability).Num, 1); got != 4 {
		t.Fatalf("precondition: Ojer damage count = %d, want 4", got)
	}
	Resolve(h, c, ability)
	if h.g.Obj(ids[1]).Zone != state.ZBattlefield || h.g.Obj(ids[2]).Zone != state.ZBattlefield || h.g.Obj(ids[0]).Zone != state.ZLibrary {
		t.Fatalf("Ojer destinations: %v %v %v, asks=%+v events=%+v", h.g.Obj(ids[0]).Zone, h.g.Obj(ids[1]).Zone, h.g.Obj(ids[2]).Zone, h.asked, h.log)
	}
	if h.asked[0].Max != 2 || h.asked[0].Options[0].Obj != ids[0] {
		t.Fatalf("Ojer decision %+v", h.asked[0])
	}
}
func TestDigMultiplePresenceCorpus(t *testing.T) {
	_, ability, _ := corpusRiderSA(t, "In the Presence of Ages", "")
	if ability.API != "DigMultiple" || ability.Params["DestinationZone2"] != "Graveyard" || ability.Params["Reveal"] != "True" {
		t.Fatalf("precondition: corpus Presence ability %+v", ability)
	}
	h, ids := digMultipleBoard(t)
	h.choices = []int{1, 2}
	Resolve(h, &Ctx{Controller: 0}, ability)
	if !reflect.DeepEqual(h.g.Zone(state.ZHand, 0), []state.ObjID{ids[1], ids[2]}) || !reflect.DeepEqual(h.g.Zone(state.ZGraveyard, 0), []state.ObjID{ids[0], ids[3]}) {
		t.Fatalf("Presence hand=%v grave=%v", h.g.Zone(state.ZHand, 0), h.g.Zone(state.ZGraveyard, 0))
	}
}

func TestDigMultipleBottomOrderAndPrivateReveal(t *testing.T) {
	h, ids := digMultipleBoard(t)
	// One chosen land, three unchosen cards: a genuine permutation, not a
	// one-card no-op. Only the choice is publicly revealed; the look is secret.
	h.choices = []int{2}
	h.arrange = []int{2, 0, 1}
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ DigMultiple | DigNum$ 4 | ChangeValid$ Creature,Land | Optional$ True"))
	if len(h.asked) != 2 || h.asked[1].Kind != decision.KArrange || len(h.asked[1].Options) != 3 {
		t.Fatalf("precondition: bottom arrange was not offered: %+v", h.asked)
	}
	if got := h.g.Zone(state.ZLibrary, 0); !reflect.DeepEqual(got, []state.ObjID{ids[3], ids[0], ids[1]}) {
		t.Fatalf("bottom order %v", got)
	}
	var private, public int
	for _, ev := range h.log {
		if ev.Kind == events.Note && len(ev.IDs) > 0 {
			if ev.Secret {
				private++
				if ev.Player != 0 || !reflect.DeepEqual(ev.IDs, ids) {
					t.Fatalf("private look %+v", ev)
				}
			} else {
				public++
				if !reflect.DeepEqual(ev.IDs, []state.ObjID{ids[2]}) {
					t.Fatalf("public reveal %+v", ev)
				}
			}
		}
	}
	if private != 1 || public != 1 {
		t.Fatalf("look/reveal counts private=%d public=%d", private, public)
	}
}

func TestDigMultipleOverlappingTypeAndMandatoryPick(t *testing.T) {
	h := &digMultipleHost{fakeHost: newHost(t, 2), choices: []int{0, 1}}
	dual := h.g.AddObject(mkCard(t, "Name:Dryad\nTypes:Land Creature Dryad\nPT:1/1\nOracle:x\n"), 0).ID
	bear := h.g.AddObject(mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0).ID
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{dual, bear})
	if !MatchesSpecCtx(h.g, "Land", dual, NewSpecContext(0, 0)) || !MatchesSpecCtx(h.g, "Creature", bear, NewSpecContext(0, 0)) || MatchesSpecCtx(h.g, "Land", bear, NewSpecContext(0, 0)) {
		t.Fatal("precondition: dual is land, bear is creature only")
	}
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ DigMultiple | DigNum$ 2 | ChangeValid$ Creature,Land | DestinationZone2$ Graveyard"))
	if len(h.asked) == 0 {
		t.Fatal("DigMultiple posed no choice")
	}
	d := h.asked[0]
	if d.Min != 2 || !d.DistinctTypesFit([]int{0, 1}) {
		t.Fatalf("mandatory, assignable decision %+v", d)
	}
	if err := d.Validate(decision.Intent{Player: 0, Choices: []int{0, 1}}); err != nil {
		t.Fatalf("dual-type answer rejected: %v", err)
	}
	if !reflect.DeepEqual(h.g.Zone(state.ZHand, 0), []state.ObjID{dual, bear}) {
		t.Fatalf("dual-type hand %v", h.g.Zone(state.ZHand, 0))
	}
}

func TestDigMultipleGreenSunDefersChosenAndImprintsRest(t *testing.T) {
	_, ability, vars := corpusRiderSA(t, "Green Sun's Twilight", "")
	if ability.API != "DigMultiple" || ability.Params["ChangeLater"] != "True" || ability.Params["ImprintRest"] != "True" {
		t.Fatalf("precondition: Green Sun params %+v", ability)
	}
	h, ids := digMultipleBoard(t)
	source := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0).ID
	h.choices = []int{1, 2}
	c := &Ctx{Controller: 0, Source: source, SVars: vars}
	c.X = 3
	// The printed SVar:X is the paid X amount on a real cast; supply a
	// runtime override in this direct effect fixture, without changing SA.
	c.SVars = map[string]string{"X": "Number$3"}
	if got := numText(h, c, compileDigMultiple(ability).Num, 1); got != 4 {
		t.Fatalf("precondition: X+1 = %d", got)
	}
	effDigMultiple(h, c, ability) // inspect the deferred piles before Resolve runs DBChangeZone.
	if !reflect.DeepEqual(h.g.Zone(state.ZLibrary, 0), ids) {
		t.Fatalf("ChangeLater moved library before subability: %v", h.g.Zone(state.ZLibrary, 0))
	}
	if len(c.Remembered) != 2 || c.Remembered[0].Obj != ids[1] || c.Remembered[1].Obj != ids[2] {
		t.Fatalf("selected remembered %v", c.Remembered)
	}
	if imprinted := h.g.Obj(source).Imprinted; !reflect.DeepEqual(imprinted, []state.ObjID{ids[0], ids[3]}) {
		t.Fatalf("rest imprinted %v", imprinted)
	}
	// Now drive the SAME corpus chain to assert its deferred subabilities
	// consume the two recorded piles, rather than merely storing them.
	full, fullIDs := digMultipleBoard(t)
	full.choices = []int{1, 2}
	fullSource := full.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0).ID
	fullCtx := &Ctx{Controller: 0, Source: fullSource, SVars: map[string]string{"X": "Number$3"}}
	Resolve(full, fullCtx, ability)
	if !reflect.DeepEqual(full.g.Zone(state.ZHand, 0), []state.ObjID{fullIDs[1], fullIDs[2]}) || len(full.g.Zone(state.ZLibrary, 0)) != 2 {
		t.Fatalf("deferred chain hand=%v library=%v", full.g.Zone(state.ZHand, 0), full.g.Zone(state.ZLibrary, 0))
	}
}

func TestDigMultipleSelectiveAdaptationChosenZone(t *testing.T) {
	_, ability, _ := corpusRiderSA(t, "Selective Adaptation", "")
	if ability.API != "DigMultiple" || ability.Params["ChooseAmount"] != "1" || ability.Params["ChosenZone"] != "Battlefield" {
		t.Fatalf("precondition: Selective Adaptation params %+v", ability)
	}
	h := &digMultipleHost{fakeHost: newHost(t, 2), choices: []int{0, 1}, chosen: []int{1}}
	flying := h.g.AddObject(mkCard(t, "Name:Bird\nTypes:Creature Bird\nPT:1/1\nK:Flying\nOracle:x\n"), 0).ID
	first := h.g.AddObject(mkCard(t, "Name:Knight\nTypes:Creature Knight\nPT:2/2\nK:First Strike\nOracle:x\n"), 0).ID
	other := h.g.AddObject(mkCard(t, "Name:Other\nTypes:Sorcery\nOracle:x\n"), 0).ID
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{flying, first, other})
	p := compileDigMultiple(ability)
	if !MatchesSpecCtx(h.g, p.Specs[0], flying, NewSpecContext(0, 0)) || !MatchesSpecCtx(h.g, p.Specs[1], first, NewSpecContext(0, 0)) || MatchesSpecCtx(h.g, p.Specs[1], other, NewSpecContext(0, 0)) {
		t.Fatal("precondition: two different keywords and ineligible remainder")
	}
	Resolve(h, &Ctx{Controller: 0}, ability)
	if len(h.asked) != 2 || h.asked[0].Min != 2 || h.asked[1].ResumeKind != "digmultiple_chosen" {
		t.Fatalf("selection decisions %+v", h.asked)
	}
	if !reflect.DeepEqual(h.g.Zone(state.ZBattlefield, 0), []state.ObjID{first}) || !reflect.DeepEqual(h.g.Zone(state.ZHand, 0), []state.ObjID{flying}) || !reflect.DeepEqual(h.g.Zone(state.ZGraveyard, 0), []state.ObjID{other}) {
		t.Fatalf("destinations battlefield=%v hand=%v graveyard=%v", h.g.Zone(state.ZBattlefield, 0), h.g.Zone(state.ZHand, 0), h.g.Zone(state.ZGraveyard, 0))
	}
}

// Exact measured ability/SVar carrier set, including the kind of each form.
func TestDigMultipleCorpusCarriers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var got []string
	visit := func(name, kind string, sa *cards.SA) {
		if sa != nil && sa.API == "DigMultiple" {
			if strings.HasPrefix(kind, "TrigDig") && sa.Kind != "DB" {
				t.Errorf("%s %s: expected DB, got %s", name, kind, sa.Kind)
			}
			if (kind == "SP" || kind == "AB") && sa.Kind != kind {
				t.Errorf("%s: expected %s, got %s", name, kind, sa.Kind)
			}
			got = append(got, name+"|"+kind)
			if cards.APICodeForName(sa.API) != cards.APIDigMultiple {
				t.Errorf("%s: opcode %d", name, sa.CompiledAPI())
			}
		}
	}
	for _, card := range reg.AllCards() {
		for _, f := range card.Faces {
			for _, sa := range f.Abilities {
				if sa != nil {
					visit(f.Name, sa.Kind, sa)
				}
			}
			keys := make([]string, 0, len(f.SVars))
			for k := range f.SVars {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if strings.Contains(f.SVars[k], "DigMultiple") {
					visit(f.Name, k, cards.ResolveSVar(f.SVars, k))
				}
			}
		}
	}
	sort.Strings(got)
	want := []string{
		"Benefaction of Rhonas|SP", "Gift of the Gargantuan|SP", "Green Sun's Twilight|SP", "Harper Recruiter|TrigDigMulti", "In the Presence of Ages|SP", "Kaalia, Zenith Seeker|TrigDigMulti", "Kiora, Master of the Depths|AB", "Ojer Kaslem, Deepest Growth|TrigDig", "Relentless Pursuit|SP", "Selective Adaptation|SP", "Explore the Vastlands|SP", "A-Nahiri, Heir of the Ancients|AB",
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DigMultiple carriers=%q, want %q", got, want)
	}
}
