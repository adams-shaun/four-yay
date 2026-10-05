package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// faceHasType reports whether the named card's front face prints a type or
// subtype (case-insensitive). It is the tests' precondition check: a fixture
// that claims to serve "a Villain in your graveyard" must actually put a
// Villain there.
func faceHasType(t *testing.T, reg *cards.Registry, name, want string) bool {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		t.Fatalf("%s not in corpus", name)
	}
	for _, ty := range c.Faces[0].Types {
		if strings.EqualFold(strings.TrimSpace(ty), want) {
			return true
		}
	}
	return false
}

// castStep returns the card's own cast step, or fails.
func castStep(t *testing.T, it oraclegen.Item, name string) oraclegen.Step {
	t.Helper()
	for _, st := range it.Scenario.Steps {
		if st.Op == "cast" && st.Card == "p0:"+name {
			return st
		}
	}
	t.Fatalf("%s: no cast step in scenario", name)
	return oraclegen.Step{}
}

// TestOptionalTargetSlots: Fiery Annihilation's Equipment slot is TargetMin$0,
// so its fixture must cast with exactly the one mandatory creature target and
// omit the optional slot (no Equipment, no second target). Without the
// optional-slot handling the empty Equipment candidate list sinks the whole
// fixture and Generate returns a skip.
func TestOptionalTargetSlots(t *testing.T) {
	reg := loadGenRegistry(t)
	f := faceOf(t, reg, "Fiery Annihilation")
	slots := oraclegen.SlotSpecs(f)
	if len(slots) != 2 {
		t.Fatalf("Fiery Annihilation: %d slots, want 2 (%v)", len(slots), slots)
	}
	if slots[0].Optional {
		t.Errorf("slot 0 %q is optional, want mandatory", slots[0].Filter)
	}
	if !slots[1].Optional || !strings.Contains(slots[1].Filter, "Equipment") {
		t.Errorf("slot 1 %q optional=%v, want optional Equipment", slots[1].Filter, slots[1].Optional)
	}

	it, skip := Generate(reg, "Fiery Annihilation")
	if skip != nil {
		t.Fatalf("Fiery Annihilation: %s", skip.Reason)
	}
	st := castStep(t, it, "Fiery Annihilation")
	if len(st.Targets) != 1 {
		t.Fatalf("Fiery Annihilation cast targets = %v, want exactly the mandatory creature", st.Targets)
	}
	if !strings.Contains(st.Targets[0], "Grizzly Bears") {
		t.Errorf("Fiery Annihilation target = %q, want a creature", st.Targets[0])
	}
}

// TestRegistryTypedCandidates: a filter whose head is a subtype the fixed
// type list cannot see (Villain, an Elf, an Equipment) is served by a real
// card of that subtype from the registry, not a generic Grizzly Bears.
func TestRegistryTypedCandidates(t *testing.T) {
	reg := loadGenRegistry(t)

	// Villain in a graveyard (Decoy Ploy's first charm mode).
	decoy := faceOf(t, reg, "Decoy Ploy")
	slots := oraclegen.SlotSpecs(decoy)
	if len(slots) == 0 || !strings.Contains(slots[0].Filter, "Villain") {
		t.Fatalf("Decoy Ploy slot = %v, want a Villain filter first", slots)
	}
	fxs := oraclegen.Fixtures(reg, slots)
	if len(fxs) == 0 {
		t.Fatalf("Decoy Ploy: no fixture for %q", slots[0].Filter)
	}
	var villain string
	for _, n := range fxs[0].P0().Graveyard {
		if faceHasType(t, reg, n, "Villain") {
			villain = n
		}
	}
	if villain == "" {
		t.Fatalf("Decoy Ploy fixture graveyard = %v, want a real Villain", fxs[0].P0().Graveyard)
	}

	// A battlefield subtype (Elf you control) from the same registry path.
	elfSlots := []oraclegen.SlotSpec{{Filter: "Elf.YouCtrl"}}
	elfFxs := oraclegen.Fixtures(reg, elfSlots)
	if len(elfFxs) == 0 {
		t.Fatalf("Elf.YouCtrl: no fixture")
	}
	if got := elfFxs[0].P0().Battlefield; len(got) == 0 || !faceHasType(t, reg, got[0], "Elf") {
		t.Fatalf("Elf.YouCtrl fixture battlefield = %v, want a real Elf", got)
	}
}

// TestTokenFixture: For the Common Good targets a token you control, so the
// fixture must run a token-maker prelude and name the created token
// (pN:token:<subtype>) rather than a card in a zone.
func TestTokenFixture(t *testing.T) {
	reg := loadGenRegistry(t)
	it, skip := Generate(reg, "For the Common Good")
	if skip != nil {
		t.Fatalf("For the Common Good: %s", skip.Reason)
	}
	st := castStep(t, it, "For the Common Good")
	if len(st.Targets) != 1 || !strings.HasPrefix(st.Targets[0], "p0:token:") {
		t.Fatalf("For the Common Good targets = %v, want a p0:token:<subtype> ref", st.Targets)
	}
	// Precondition: the prelude actually makes the token -- a cast op for a
	// token-maker before the card's own cast.
	var maker string
	for _, step := range it.Scenario.Steps {
		if step.Op == "cast" && step.Card != "p0:For the Common Good" {
			maker = step.Card
		}
	}
	if maker == "" {
		t.Fatalf("For the Common Good: no token-maker prelude cast for %s", st.Targets[0])
	}
	if got := it.Setup["p0"].Hand; !containsName(got, strings.TrimPrefix(maker, "p0:")) {
		t.Fatalf("For the Common Good: token-maker %q not in hand %v", maker, got)
	}
}

// TestAuraFixture: Graceful Takedown's first slot is a creature you control
// that is enchanted, which only a prelude Aura cast produces.
func TestAuraFixture(t *testing.T) {
	reg := loadGenRegistry(t)
	it, skip := Generate(reg, "Graceful Takedown")
	if skip != nil {
		t.Fatalf("Graceful Takedown: %s", skip.Reason)
	}
	// Precondition: an Aura cast, with a target, precedes the card's cast.
	var aura, enchanted string
	for _, step := range it.Scenario.Steps {
		if step.Op == "cast" && step.Card != "p0:Graceful Takedown" {
			aura = step.Card
			if len(step.Targets) == 1 {
				enchanted = step.Targets[0]
			}
		}
	}
	if aura == "" || enchanted == "" {
		t.Fatalf("Graceful Takedown: no Aura prelude cast with a target (aura=%q target=%q)", aura, enchanted)
	}
	// Precondition: the enchanted creature is actually on p0's battlefield,
	// so the "enchanted" qualifier the cast relies on is real.
	name := strings.TrimPrefix(enchanted, "p0:")
	if !containsName(it.Setup["p0"].Battlefield, name) {
		t.Fatalf("Graceful Takedown: enchanted %q not on p0 battlefield %v", name, it.Setup["p0"].Battlefield)
	}
	st := castStep(t, it, "Graceful Takedown")
	if !containsName(st.Targets, enchanted) {
		t.Fatalf("Graceful Takedown targets = %v, want the enchanted %q", st.Targets, enchanted)
	}
}

// TestThisTurnEnteredFixture: Reenact the Crime needs a card that entered a
// graveyard this turn, which only a mid-game move op produces (the setup then
// carries no ThisTurnEntered stamp).
func TestThisTurnEnteredFixture(t *testing.T) {
	reg := loadGenRegistry(t)
	it, skip := Generate(reg, "Reenact the Crime")
	if skip != nil {
		t.Fatalf("Reenact the Crime: %s", skip.Reason)
	}
	st := castStep(t, it, "Reenact the Crime")
	if len(st.Targets) != 1 {
		t.Fatalf("Reenact the Crime targets = %v, want one graveyard card", st.Targets)
	}
	target := st.Targets[0]
	// Precondition: the prelude moves the targeted card into the graveyard.
	var moved string
	for _, step := range it.Scenario.Steps {
		if step.Op == "move" && step.Card == target && step.To == "graveyard" {
			moved = step.Card
		}
	}
	if moved == "" {
		for _, step := range it.Scenario.Steps {
			t.Logf("step op=%s card=%s to=%s", step.Op, step.Card, step.To)
		}
		t.Fatalf("Reenact the Crime: no move of %q to graveyard in the prelude", target)
	}
	// Precondition: the card starts outside the graveyard, so the move is a
	// real zone change and not a no-op.
	name := strings.TrimPrefix(target, "p0:")
	if containsName(it.Setup["p0"].Graveyard, name) {
		t.Fatalf("Reenact the Crime: %q already in the setup graveyard", name)
	}
	if !containsName(it.Setup["p0"].Hand, name) {
		t.Fatalf("Reenact the Crime: %q not staged in hand %v", name, it.Setup["p0"].Hand)
	}
}

// TestBoardHistoryCardsGenerate: the five cards of this ticket's cluster that
// the generator skipped before its board-history, token, Aura, optional-slot
// and registry-typed fixes now each get a scenario.
func TestBoardHistoryCardsGenerate(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{
		"Fiery Annihilation",
		"For the Common Good",
		"Graceful Takedown",
		"Decoy Ploy",
		"Reenact the Crime",
	} {
		if _, skip := Generate(reg, name); skip != nil {
			t.Errorf("%s: %s", name, skip.Reason)
		}
	}
}

func containsName(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
