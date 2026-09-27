package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// sVarHasToken asserts the raw SVar body carries the exact parameter token.
// It keeps this real-card test tied to what the corpus script actually
// says: a corpus re-pin that drops or renames a parameter must fail the
// test instead of silently invalidating its coverage.
func sVarHasToken(t *testing.T, raw, key, want string) {
	t.Helper()
	for _, tok := range strings.Split(raw, "|") {
		if strings.TrimSpace(tok) == want {
			return
		}
	}
	t.Fatalf("precondition: SVar %q = %q, want token %q", key, raw, want)
}

func TestAlhammarretNameCardChooseFromDefinedCards(t *testing.T) {
	e, reg := nameCardEngine(t, "Alhammarret, High Arbiter")
	// Script precondition (the ticket's coverage target): the ETB reveal
	// remembers the revealed cards and chains into a NameCard sub-ability
	// whose choices are drawn from that memory, filtered to nonlands.
	card, ok := reg.Lookup("Alhammarret, High Arbiter")
	if !ok {
		t.Fatal("corpus precondition: Alhammarret, High Arbiter missing")
	}
	face := card.Faces[0]
	reveal, hasReveal := face.SVars["RevealHand"]
	if !hasReveal {
		t.Fatal("corpus precondition: RevealHand SVar missing")
	}
	sVarHasToken(t, reveal, "RevealHand", "DB$ RevealHand")
	sVarHasToken(t, reveal, "RevealHand", "RememberRevealed$ True")
	sVarHasToken(t, reveal, "RevealHand", "SubAbility$ DBNameCard")
	nameSVar, hasName := face.SVars["DBNameCard"]
	if !hasName {
		t.Fatal("corpus precondition: DBNameCard SVar missing")
	}
	sVarHasToken(t, nameSVar, "DBNameCard", "DB$ NameCard")
	sVarHasToken(t, nameSVar, "DBNameCard", "ValidCards$ Card.nonLand")
	sVarHasToken(t, nameSVar, "DBNameCard", "ChooseFromDefinedCards$ Remembered")
	sVarHasToken(t, nameSVar, "DBNameCard", "SubAbility$ DBCleanup")
	arbiter := e.G.Zone(state.ZHand, 0)[0]
	var opponentHand []state.ObjID
	for _, name := range []string{"Grizzly Bears", "Counterspell", "Forest"} {
		card, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus precondition: %q missing", name)
		}
		obj := e.G.AddObject(card, 1)
		obj.Zone = state.ZHand
		opponentHand = append(opponentHand, obj.ID)
	}
	e.G.SetZone(state.ZHand, 1, opponentHand)
	if got := len(e.G.Zone(state.ZHand, 1)); got != 3 {
		t.Fatalf("precondition: opponent hand has %d cards, want 3", got)
	}
	if _, ok := reg.Lookup("Lightning Bolt"); !ok {
		t.Fatal("corpus precondition: unrelated nonland Lightning Bolt missing")
	}
	if _, ok := reg.Lookup("Forest"); !ok {
		t.Fatal("corpus precondition: Forest missing")
	}
	// Prove the tested names differ and that the unrelated nonland is not
	// among the revealed cards.
	revealed, _ := reg.Lookup("Grizzly Bears")
	excluded, _ := reg.Lookup("Lightning Bolt")
	if revealed.Faces[0].Name == excluded.Faces[0].Name {
		t.Fatal("precondition: revealed and excluded names must differ")
	}
	e.G.Players[0].Pool[state.MC] = 5
	e.G.Players[0].Pool[state.MU] = 2
	e.beginCast(0, decision.Option{Kind: "cast", Obj: arbiter})
	if got := e.G.Obj(arbiter).Zone; got != state.ZStack {
		t.Fatalf("Alhammarret zone after cast = %s, want stack", got)
	}
	e.resolveTop()
	d := pendingNameAsk(t, e, "Alhammarret entry")
	want := []string{"Grizzly Bears", "Counterspell"}
	if len(d.Options) != len(want) {
		t.Fatalf("name options = %d, want exactly %d revealed nonlands", len(d.Options), len(want))
	}
	for _, name := range want {
		if labelIndex(d, name) < 0 {
			t.Errorf("name ask omitted revealed nonland %q", name)
		}
	}
	for _, name := range []string{"Forest", "Lightning Bolt"} {
		if labelIndex(d, name) >= 0 {
			t.Errorf("name ask offered ineligible %q", name)
		}
	}
	if answer := newTestBot(7).answer(e, d); d.Validate(answer) != nil {
		t.Fatalf("bot answer %+v failed restricted name decision validation: %v", answer, d.Validate(answer))
	}
	idx := labelIndex(d, "Grizzly Bears")
	if idx < 0 {
		t.Fatal("cannot answer name ask with Grizzly Bears")
	}
	submitChoices(t, e, idx)
	if got := e.G.Obj(arbiter).ChosenName; got != "Grizzly Bears" {
		t.Fatalf("Alhammarret ChosenName = %q, want Grizzly Bears", got)
	}
	if d := e.Pending(); d != nil && d.ResumeKind == "name" {
		t.Fatalf("name resolution did not finish: %+v", d)
	}
}

func TestAlhammarretNameCardEmptyDefinedSetDoesNotBroaden(t *testing.T) {
	e, reg := nameCardEngine(t, "Alhammarret, High Arbiter")
	arbiter := e.G.Zone(state.ZHand, 0)[0]
	forest, ok := reg.Lookup("Forest")
	if !ok {
		t.Fatal("corpus precondition: Forest missing")
	}
	land := e.G.AddObject(forest, 1)
	land.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, []state.ObjID{land.ID})
	if len(e.G.Zone(state.ZHand, 1)) != 1 || e.G.Obj(e.G.Zone(state.ZHand, 1)[0]).Face().Name != "Forest" {
		t.Fatal("precondition: opponent's only revealed card must be Forest")
	}
	e.G.Players[0].Pool[state.MC] = 5
	e.G.Players[0].Pool[state.MU] = 2
	e.beginCast(0, decision.Option{Kind: "cast", Obj: arbiter})
	if e.G.Obj(arbiter).Zone != state.ZStack {
		t.Fatal("precondition: Alhammarret did not reach the stack")
	}
	e.resolveTop()
	if e.G.Obj(arbiter).Zone != state.ZBattlefield {
		t.Fatalf("Alhammarret zone after resolving ETB = %s, want battlefield", e.G.Obj(arbiter).Zone)
	}
	if e.G.Obj(arbiter).ChosenName != "" {
		t.Fatalf("empty eligible set chose %q", e.G.Obj(arbiter).ChosenName)
	}
	if d := e.Pending(); d != nil && d.ResumeKind == "name" {
		t.Fatalf("empty eligible set broadened into a name ask: %+v", d)
	}
}
