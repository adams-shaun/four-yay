package main

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

func TestSignatureCollapsesSeedNumbers(t *testing.T) {
	a := signature("livelock", "livelock detected (repeating cycle): object 12, kind priority, cycle: [seq 40 priority player=1]")
	b := signature("livelock", "livelock detected (repeating cycle): object 99, kind priority, cycle: [seq 7123 priority player=0]")
	if a != b {
		t.Fatalf("same cycle on different seeds must share a signature:\n%s\n%s", a, b)
	}
}

func TestGenerateIsDeterministicMonoColourSixty(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	p, err := buildPool(reg)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := loadCov(t.TempDir() + "/none.json")
	gen := func() genDeck { return generate(rand.New(rand.NewPCG(7, 9)), p, c) }
	d1, d2 := gen(), gen()
	if len(d1.Cards) != 60 {
		t.Fatalf("deck has %d cards, want 60", len(d1.Cards))
	}
	for i := range d1.Cards {
		if d1.Cards[i] != d2.Cards[i] {
			t.Fatalf("generation not deterministic at %d: %s vs %s", i, d1.Cards[i], d2.Cards[i])
		}
	}
	cards, err := resolveDeck(reg, d1)
	if err != nil {
		t.Fatal(err)
	}
	lands := 0
	ci := -1
	for i, n := range colourNames {
		if n == d1.Colour {
			ci = i
		}
	}
	for _, cd := range cards {
		if cd.Faces[0].IsLand() {
			lands++
		}
		if id := identity(cd); id != 0 && id != 1<<ci {
			t.Fatalf("%s has identity %b outside mono-%s", cardName(cd), id, d1.Colour)
		}
	}
	if lands < 20 {
		t.Fatalf("deck has %d lands, want at least 20", lands)
	}
}

// TestBoardGuardRecordsBigboard pins the harness-side board-size watchdog:
// a game whose live object count exceeds -max-objects ends as its own
// "bigboard" kind (never "hang" or "livelock", so triage can tell a runaway
// token engine from an engine bug), and the cap is inert below the limit
// and when disabled. Two 60-card decks put 120 live objects in the arena at
// genesis, so a cap of 100 fires at the first decision and one of 100000
// never does.
func TestBoardGuardRecordsBigboard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	p, err := buildPool(reg)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := loadCov(t.TempDir() + "/none.json")
	r := rand.New(rand.NewPCG(3, 5))
	decks := []genDeck{generate(r, p, c), generate(r, p, c)}

	f, _ := playOne(reg, decks, 11, 3, 20000, 100, false, false)
	if f == nil || f.Kind != "bigboard" {
		t.Fatalf("failure = %+v, want kind bigboard", f)
	}
	if !strings.Contains(f.Diag, "exceeds -max-objects 100") || !strings.HasPrefix(f.Sig, "bigboard: ") {
		t.Fatalf("bigboard record lacks its diagnostic: sig %q diag %q", f.Sig, f.Diag)
	}
	if boardGuard(0) != nil {
		t.Fatalf("-max-objects 0 must disable the guard")
	}
	// Under the cap the guard is inert: the 3-turn cap ends the game as a
	// plain stall (not a failure record).
	if f, _ := playOne(reg, decks, 11, 3, 20000, 100000, false, false); f != nil {
		t.Fatalf("game under the object cap recorded %+v", f)
	}
}

// TestAbilityInventory pins the usable-ability inventory on real corpus
// cards: Gilded Goose carries an activated ability, a mana ability and an
// ETB trigger; a spell's own SP ability and statics are excluded, and a
// keyword-expanded activation (Cycling) counts.
func TestAbilityInventory(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	look := func(n string) []abilitySlot {
		c, ok := reg.Lookup(n)
		if !ok {
			t.Fatalf("%s not in corpus", n)
		}
		return abilityInventory(c)
	}
	goose := look("Gilded Goose")
	var got []string
	for _, s := range goose {
		got = append(got, fmt.Sprintf("%s:%s:%v", s.key, s.desc, s.mana))
	}
	want := []string{"f0/a0:Token:false", "f0/a1:Mana:true", "f0/t0:ChangesZone:false"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Gilded Goose inventory = %v, want %v", got, want)
	}
	if inv := look("Lightning Bolt"); len(inv) != 0 {
		t.Fatalf("Lightning Bolt's spell ability must not be in the inventory: %v", inv)
	}
	if inv := look("Street Wraith"); len(inv) != 1 || inv[0].desc != "Draw/Cycling" {
		t.Fatalf("Street Wraith inventory = %+v, want its expanded Cycling activation", inv)
	}
	if inv := look("Delver of Secrets"); len(inv) != 1 || inv[0].key != "f0/t0" {
		t.Fatalf("Delver of Secrets inventory = %+v, want its front-face upkeep trigger", inv)
	}
}

// TestAbilitiesUsedFromPlayedGame plays a real bot game with Mind Stone (a
// {T}: Add {C} mana ability plus a draw activation) and Solemn Simulacrum (an
// ETB and a dies trigger), and checks the log walk credits Mind Stone's mana
// ability (the Tap+ManaAdd proxy) and a Solemn trigger (the minted
// TriggerPush object), and credits only keys in each card's inventory.
func TestAbilitiesUsedFromPlayedGame(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var d genDeck
	d.Colour = "C"
	for i := 0; i < 60; i++ {
		switch {
		case i < 22:
			d.Cards = append(d.Cards, "Wastes")
		case i < 41:
			d.Cards = append(d.Cards, "Mind Stone")
		default:
			d.Cards = append(d.Cards, "Solemn Simulacrum")
		}
	}
	f, gc := playOne(reg, []genDeck{d, d}, 5, 14, 20000, 0, false, false)
	if f != nil {
		t.Fatalf("game failed: %s", f.Diag)
	}
	for card, keys := range gc.used {
		c, _ := reg.Lookup(card)
		inv := abilityDescs(c)
		for k := range keys {
			if _, ok := inv[k]; !ok {
				t.Errorf("%s credited key %s outside its inventory %v", card, k, inv)
			}
		}
	}
	if !gc.cast["Mind Stone"] || !gc.cast["Solemn Simulacrum"] {
		t.Fatalf("setup: expected both cards cast, cast=%v", gc.cast)
	}
	if !gc.used["Mind Stone"]["f0/a0"] {
		t.Errorf("Mind Stone's mana ability not credited: used=%v", gc.used)
	}
	if !gc.used["Solemn Simulacrum"]["f0/t0"] {
		t.Errorf("Solemn Simulacrum's ETB trigger not credited: used=%v", gc.used)
	}
	if !gc.used["Wastes"]["f0/a0"] {
		t.Errorf("Wastes' mana ability not credited: used=%v", gc.used)
	}
	// A land play counts as the land being played: LandPlayed carries no
	// object, so played() finds the land by its preceding move.
	if !gc.cast["Wastes"] {
		t.Errorf("Wastes was never credited as played: cast=%v", gc.cast)
	}
	if !gc.offered["Mind Stone"]["f0/a1"] {
		t.Errorf("Mind Stone's draw activation was never recorded as offered: offered=%v", gc.offered)
	}
}

// TestManaAbilityChoosingColourCredited pins the exact mana credit: a
// "one mana of any colour" rock asks its colour between the Tap and the
// ManaAdd, which the old log proxy stopped at, so Manalith and every
// Darksteel Ingot-shaped source read as never used. The engine hook
// (rules.Engine.ManaAbilityHook) credits the activation itself.
func TestManaAbilityChoosingColourCredited(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var d genDeck
	d.Colour = "C"
	for i := 0; i < 60; i++ {
		switch {
		case i < 20:
			d.Cards = append(d.Cards, "Wastes")
		case i < 40:
			d.Cards = append(d.Cards, "Manalith")
		default:
			d.Cards = append(d.Cards, "Solemn Simulacrum")
		}
	}
	f, gc := playOne(reg, []genDeck{d, d}, 5, 14, 20000, 0, false, false)
	if f != nil {
		t.Fatalf("game failed: %s", f.Diag)
	}
	if !gc.cast["Manalith"] {
		t.Fatalf("setup: Manalith never cast")
	}
	if !gc.used["Manalith"]["f0/a0"] {
		t.Errorf("Manalith's any-colour mana ability not credited: used=%v", gc.used["Manalith"])
	}
}

// TestBookkeepingTriggersExcluded: Chrome Mox's Static$ True DBCleanup /
// DBForget riders (no TriggerDescription$) are Forge's internal
// state-tracking, not abilities; its imprint ETB and mana ability stay.
func TestBookkeepingTriggersExcluded(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Chrome Mox")
	if !ok {
		t.Fatal("Chrome Mox not in corpus")
	}
	var got []string
	for _, s := range abilityInventory(c) {
		got = append(got, s.key+":"+s.desc)
	}
	if want := "f0/a0:ManaReflected f0/t0:ChangesZone"; strings.Join(got, " ") != want {
		t.Fatalf("Chrome Mox inventory = %v, want %s", got, want)
	}
	// A Static$ True triggered MANA ability is a real ability and stays.
	cg, ok := reg.Lookup("Crypt Ghast")
	if !ok {
		t.Fatal("Crypt Ghast not in corpus")
	}
	found := false
	for _, s := range abilityInventory(cg) {
		if s.desc == "TapsForMana" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Crypt Ghast's triggered mana ability dropped from %v", abilityInventory(cg))
	}
}

// TestExploreSeatIsDeterministicAndRecorded: an -explore game plays one
// exploration seat (by seed parity), replays byte-identically, and a
// failure record carries the flag so -repro rebuilds the same seats.
func TestExploreSeatIsDeterministicAndRecorded(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	p, err := buildPool(reg)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := loadCov(t.TempDir() + "/none.json")
	r := rand.New(rand.NewPCG(3, 5))
	decks := []genDeck{generate(r, p, c), generate(r, p, c)}
	for _, seed := range []uint64{21, 22} {
		f, gc := playOne(reg, decks, seed, 12, 20000, 0, true, true)
		if f != nil {
			t.Fatalf("explore game seed %d failed: %s %s", seed, f.Kind, f.Diag)
		}
		_, gc2 := playOne(reg, decks, seed, 12, 20000, 0, false, true)
		if fmt.Sprint(gc.used) != fmt.Sprint(gc2.used) || fmt.Sprint(gc.cast) != fmt.Sprint(gc2.cast) {
			t.Fatalf("explore game seed %d is not deterministic", seed)
		}
	}
	// The 100-object cap forces a (bigboard) failure record.
	f, _ := playOne(reg, decks, 11, 3, 20000, 100, false, true)
	if f == nil || !f.Explore {
		t.Fatalf("failure record %+v does not carry Explore", f)
	}
}

// TestCovUsedBackwardCompatible: a state file written before Used existed
// loads with an empty Used map, and full coverage needs a cast plus every key.
func TestCovUsedBackwardCompatible(t *testing.T) {
	path := t.TempDir() + "/old.json"
	if err := os.WriteFile(path, []byte(`{"games":3,"included":{"X":1},"cast":{"X":1},"ability":{},"fails":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := loadCov(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Used == nil || len(c.Used) != 0 {
		t.Fatalf("Used = %v, want empty non-nil", c.Used)
	}
	if !c.full("X", nil) {
		t.Fatalf("a cast card with no abilities is fully covered")
	}
	keys := []string{"f0/a0", "f0/t0"}
	if c.full("X", keys) {
		t.Fatalf("no ability used yet: not full")
	}
	c.Used["X"] = map[string]int64{"f0/a0": 1}
	if got := c.missing("X", keys); len(got) != 1 || got[0] != "f0/t0" {
		t.Fatalf("missing = %v", got)
	}
	w := c.weight(poolCard{name: "X", keys: keys})
	c.Used["X"]["f0/t0"] = 1
	if !c.full("X", keys) {
		t.Fatalf("every key used and cast: full")
	}
	if w2 := c.weight(poolCard{name: "X", keys: keys}); w != 4*w2 {
		t.Fatalf("weight with a missing ability %v, want 4x the full weight %v", w, w2)
	}
}

// TestAutoPaySeatsModes pins the -autopay seat selection: off arms no seat,
// all arms every production seat (the explore seat only with
// -explore-autopay), and mixed is a pure per-seat function of the game seed
// that yields both answers across seeds.
func TestAutoPaySeatsModes(t *testing.T) {
	if _, err := parseAutoPay("bogus", false); err == nil {
		t.Fatalf("an unknown -autopay mode must be rejected")
	}
	off, _ := parseAutoPay("off", true)
	all, _ := parseAutoPay("all", false)
	allExp, _ := parseAutoPay("all", true)
	mixed, _ := parseAutoPay("mixed", false)
	for seed := uint64(0); seed < 16; seed++ {
		if got := seatList(off.seats(seed, 2, exploreSeat(seed))); got != nil {
			t.Fatalf("off armed seats %v", got)
		}
	}
	if got := fmt.Sprint(all.seats(5, 2, 1)); got != "[true false]" {
		t.Fatalf("all with explore seat 1 = %s, want the production seat only", got)
	}
	if got := fmt.Sprint(allExp.seats(5, 2, 1)); got != "[true true]" {
		t.Fatalf("all with -explore-autopay = %s, want both seats", got)
	}
	if got := fmt.Sprint(all.seats(5, 2, -1)); got != "[true true]" {
		t.Fatalf("all without an explore seat = %s, want both seats", got)
	}
	seen := map[bool]bool{}
	for seed := uint64(0); seed < 64; seed++ {
		a := mixed.seats(seed, 2, 1)
		if fmt.Sprint(a) != fmt.Sprint(mixed.seats(seed, 2, 1)) {
			t.Fatalf("mixed is not deterministic at seed %d", seed)
		}
		if a[1] {
			t.Fatalf("mixed armed the explore seat without -explore-autopay (seed %d)", seed)
		}
		seen[a[0]] = true
	}
	if !seen[true] || !seen[false] {
		t.Fatalf("mixed never varied seat 0 across 64 seeds: %v", seen)
	}
}

// TestAutoPayGameSubmitsPlans plays a real game whose casts are all V1-plan
// shapes (Forests, Llanowar Elves, Grizzly Bears, Hill Giant): with -autopay
// all both seats cast through submitted plans, none is reversed, and the game
// replays (verify); off seats see the same plans offered and submit none. A
// forced failure record carries the mode and seats, and -repro's rebuild of
// them (autoPayOf) round-trips, while an off record stays free of the fields.
func TestAutoPayGameSubmitsPlans(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var d genDeck
	d.Colour = "G"
	for i := 0; i < 60; i++ {
		switch {
		case i < 24:
			d.Cards = append(d.Cards, "Forest")
		case i < 36:
			d.Cards = append(d.Cards, "Llanowar Elves")
		case i < 50:
			d.Cards = append(d.Cards, "Grizzly Bears")
		default:
			d.Cards = append(d.Cards, "Hill Giant")
		}
	}
	decks := []genDeck{d, d}
	all, _ := parseAutoPay("all", false)
	f, gc := playGame(reg, decks, 9, 14, 20000, 0, true, false, all)
	if f != nil {
		t.Fatalf("auto-pay game failed: %s %s", f.Kind, f.Diag)
	}
	if gc.ap.Planned == 0 || gc.ap.PriorityPlan == 0 {
		t.Fatalf("auto-pay seats submitted no plan: %s", gc.ap)
	}
	if gc.ap.PlannedReversed != 0 || gc.ap.ManualCastPlan != 0 {
		t.Fatalf("a planned cast was reversed or a planned object cast by hand: %s", gc.ap)
	}
	if !gc.cast["Grizzly Bears"] {
		t.Fatalf("setup: Grizzly Bears never cast: %v", gc.cast)
	}
	t.Logf("all: %s", gc.ap)
	_, gcOff := playGame(reg, decks, 9, 14, 20000, 0, false, false, autoPay{mode: "off"})
	t.Logf("off: %s", gcOff.ap)
	// Lazy publication builds the payment extension only for a consumer. An
	// off run has no auto-pay seat, so no seat's extension is built and the
	// manual-seat counters are legitimately zero -- the off run is the cheap
	// path. (Before lazy publication every priority built one eagerly.)
	if gcOff.ap.Planned != 0 || gcOff.ap.Priority != 0 || gcOff.ap.ManualSeatPriorityPlan != 0 {
		t.Fatalf("off seats must build no plans and submit none: %s", gcOff.ap)
	}
	// The 100-object cap forces a (bigboard) failure record.
	f, _ = playGame(reg, decks, 11, 3, 20000, 100, false, false, all)
	if f == nil || f.AutoPay != "all" || fmt.Sprint(f.AutoPaySeats) != "[0 1]" {
		t.Fatalf("failure record %+v does not carry the auto-pay configuration", f)
	}
	if got, err := autoPayOf(*f); err != nil || got != all {
		t.Fatalf("autoPayOf(%q) = %+v, %v; want %+v", f.AutoPay, got, err, all)
	}
	f, _ = playOne(reg, decks, 11, 3, 20000, 100, false, false)
	b, _ := json.Marshal(f)
	if strings.Contains(string(b), "autopay") {
		t.Fatalf("an off record must stay byte-compatible with pre-flag records: %s", b)
	}
}

// TestPlanContractFailureKinds pins the plan-contract records: a reversed
// planned cast (planrev) outranks a fallback window (planfb), each signature
// names the reason and the card, and a probe with neither records nothing.
func TestPlanContractFailureKinds(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var d genDeck
	d.Colour = "G"
	for i := 0; i < 60; i++ {
		if i < 24 {
			d.Cards = append(d.Cards, "Forest")
		} else {
			d.Cards = append(d.Cards, "Grizzly Bears")
		}
	}
	dk, err := resolveDeck(reg, d)
	if err != nil {
		t.Fatal(err)
	}
	e := rules.New(rules.Config{Names: []string{"a", "b"}, Decks: [][]*cards.Card{dk, dk}, Tokens: reg.Tokens, Seed: 3})
	e.Advance()
	var bear state.ObjID
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if cardName(e.G.Obj(id).Card) == "Grizzly Bears" {
			bear = id
			break
		}
	}
	p := &apProbe{e: e}
	if _, _, _, ok := p.contractFailure(); ok {
		t.Fatalf("a probe with no violation recorded one")
	}
	p.firstFB = &fallbackSeen{card: bear, reason: "cost_changed", planID: "x"}
	if kind, _, sig, ok := p.contractFailure(); !ok || kind != "planfb" || sig != "planfb: cost_changed {Grizzly Bears}" {
		t.Fatalf("fallback record = %v %q %q", ok, kind, sig)
	}
	p.firstRev = &reversal{pc: plannedCast{obj: bear}, note: "cast aborted: cost no longer payable 2"}
	if kind, diag, sig, ok := p.contractFailure(); !ok || kind != "planrev" || sig != "planrev: cast aborted: cost no longer payable # (plan-only) {Grizzly Bears}" || !strings.Contains(diag, "reversed") {
		t.Fatalf("reversal record = %v %q %q", ok, kind, sig)
	}
}
