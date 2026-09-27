package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// These tests pin the shared AtEOT$ end-of-turn rider (effects/scheduleAtEOT)
// on the real corpus carriers of the four hook families: Puppeteer Clique
// (ChangeZone -> Animate, YourExile), Valduk (Token Exile) and Krovikan
// Elementalist (Pump Sacrifice). Every game replays byte-identically from its
// log.

// ateotEngine builds a two-seat corpus engine whose seat 0 deck opens with
// the named fixtures, and seat 1's deck opens with two Grizzly Bears (for the
// opponent-graveyard target) over Mountains.
func ateotEngine(t *testing.T, reg *cards.Registry, fixtures ...string) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range fixtures {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := []*cards.Card{bear, bear}
	for len(opp) < 40 {
		opp = append(opp, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 9205,
		Names: []string{"ateot", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

func ateotFind(t *testing.T, e *Engine, name string, seat state.PlayerID) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, seat) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == name {
				return id
			}
		}
	}
	t.Fatalf("corpus fixture %q absent from seat %d hand/library", name, seat)
	return 0
}

// ateotTo moves a fixture card to a zone and re-asks priority.
func ateotTo(t *testing.T, e *Engine, id state.ObjID, from, to state.Zone) {
	t.Helper()
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: to})
	e.pending = nil
	e.priorityRound()
}

// ateotDriveToStep drives to a step, answering the combat declarations (empty
// attack/block) and any other non-priority decision with its first option so
// the turn can reach the target step.
func ateotDriveToStep(t *testing.T, e *Engine, turn int32, active state.PlayerID, step state.Step) {
	t.Helper()
	for i := 0; i < 6000; i++ {
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == step {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before reaching turn %d seat %d step %s", turn, active, step)
		}
		d := e.Pending()
		if d == nil {
			continue
		}
		switch d.Kind {
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
				t.Fatalf("submit empty combat declaration: %v", err)
			}
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit: %v", err)
			}
		default:
			if len(d.Options) == 0 {
				t.Fatalf("empty non-priority decision %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit: %v", err)
			}
		}
	}
	t.Fatalf("did not reach turn %d seat %d step %s", turn, active, step)
}

// TestPuppeteerCliqueExilesTheReanimatedCreatureAtEOT is the flagship pin: the
// Clique's ETB trigger moves an opponent's graveyard creature onto the
// battlefield under the Clique's controller, the chained Animate grants Haste
// (Duration$ Permanent) with AtEOT$ YourExile, and at the beginning of the
// next end step the delayed registration exiles the reanimated creature while
// the Clique itself stays.
func testPuppeteerCliqueExilesTheReanimatedCreatureAtEOT(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := ateotEngine(t, reg, "Puppeteer Clique")

	// Seed the opponent's graveyard target before the Clique enters.
	grave := ateotFind(t, e, "Grizzly Bears", 1)
	ateotTo(t, e, grave, state.ZHand, state.ZGraveyard)
	graveOwner := e.G.Obj(grave).Owner
	clique := ateotFind(t, e, "Puppeteer Clique", 0)

	// Enter the Clique: the ETB trigger fires and its target ask arrives while
	// the trigger resolves. Answer the ask with the seeded graveyard bear.
	ateotTo(t, e, clique, state.ZLibrary, state.ZBattlefield)
	graveAnswered := false
	for n := 0; n < 30 && len(e.G.Stack) > 0; n++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while draining the Clique trigger")
		}
		if d.Kind == decision.KTarget {
			idx := -1
			for _, o := range d.Options {
				if o.Obj == grave {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("graveyard bear %d not offered: %+v", grave, d.Options)
			}
			submitChoices(t, e, idx)
			graveAnswered = true
			continue
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %+v while draining the Clique trigger", d)
		}
		pidx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pidx = o.Index
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pidx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	if !graveAnswered {
		t.Fatal("the Clique's reanimate target ask never arrived")
	}
	passUntilStackEmpty(t, e, 20)

	// The reanimated creature is on the battlefield under the Clique's
	// controller, with the Haste grant applied.
	re := e.G.Obj(grave)
	if re == nil || re.Zone != state.ZBattlefield {
		t.Fatalf("reanimated bear zone = %v, want battlefield", re)
	}
	if re.Controller != 0 {
		t.Fatalf("reanimated bear controller = %d, want seat 0", re.Controller)
	}
	if !e.HasKeyword(grave, "Haste") {
		t.Fatal("reanimated bear did not gain Haste from the chained Animate")
	}
	if e.HasKeyword(clique, "Haste") {
		t.Fatal("Puppeteer Clique incorrectly gained Haste from Defined$ Remembered")
	}
	if own := e.G.Obj(grave).Owner; own != graveOwner {
		t.Fatalf("reanimate changed ownership: %d -> %d", graveOwner, own)
	}
	if z := e.G.Obj(clique).Zone; z != state.ZBattlefield {
		t.Fatalf("the Clique itself moved: %s", z)
	}
	replayCheck(t, e, cfg)

	// Drive to the beginning of the next end step: the delayed registration
	// fires and exiles the reanimated creature only.
	ateotDriveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	for e.putTriggersOnStack() {
		answerTriggerOrders(t, e)
		passUntilStackEmpty(t, e, 20)
	}
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(grave).Zone; z == state.ZBattlefield {
		t.Fatalf("reanimated bear survived the end step (zone %s), want it exiled", z)
	}
	if z := e.G.Obj(clique).Zone; z != state.ZBattlefield {
		t.Fatalf("the Clique itself was exiled (zone %s), want it kept", z)
	}
	// DBCleanup (ClearRemembered$ True) still ran: the source's persistent
	// remembered set is empty at the end step.
	if src := e.G.Obj(clique); src != nil && len(src.Remembered) != 0 {
		t.Fatalf("DBCleanup did not clear the Clique's remembered set: %+v", src.Remembered)
	}
	replayCheck(t, e, cfg)
}

// TestFeralLightningTokensAreExiledAtEOT pins the Token hook: Feral
// Lightning's AtEOT$ Exile rider schedules each of its three Elemental tokens
// through the shared reader, and at the beginning of the next end step all
// three are exiled.
func TestAtEOTFeralLightningTokensAreExiled(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := ateotEngine(t, reg, "Feral Lightning")
	feral := ateotFind(t, e, "Feral Lightning", 0)
	ateotTo(t, e, feral, state.ZLibrary, state.ZHand)

	// Resolve the real printed SP ability directly: three Elemental tokens,
	// each scheduled by the AtEOT$ Exile rider.
	card := searchCorpusCard(t, reg, "Feral Lightning")
	if len(card.Faces[0].Abilities) == 0 {
		t.Fatal("Feral Lightning has no printed ability")
	}
	effects.Resolve(e, &effects.Ctx{Source: feral, Controller: 0,
		SVars: card.Faces[0].SVars}, card.Faces[0].Abilities[0])
	for e.putTriggersOnStack() {
		answerTriggerOrders(t, e)
		passUntilStackEmpty(t, e, 20)
	}
	passUntilStackEmpty(t, e, 20)

	// All three Elemental tokens are on the battlefield.
	var toks []state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.IsToken && o.Zone == state.ZBattlefield && o.Controller == 0 {
			toks = append(toks, o.ID)
		}
	}
	if len(toks) != 3 {
		t.Fatalf("Feral Lightning minted %d tokens, want 3", len(toks))
	}
	replayCheck(t, e, cfg)

	// The next end step exiles every token.
	ateotDriveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	for e.putTriggersOnStack() {
		answerTriggerOrders(t, e)
		passUntilStackEmpty(t, e, 20)
	}
	passUntilStackEmpty(t, e, 20)
	for _, id := range toks {
		if z := e.G.Obj(id).Zone; z == state.ZBattlefield {
			t.Fatalf("token %d survived the end step (zone %s), want it exiled", id, z)
		}
	}
	replayCheck(t, e, cfg)
}

// TestKrovikanElementalistSacrificesAtEOT pins the Pump hook: "Target creature
// you control gains flying until end of turn. Sacrifice it at the beginning of
// the next end step."
func TestAtEOTKrovikanElementalistSacrifices(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := ateotEngine(t, reg, "Krovikan Elementalist")
	el := ateotFind(t, e, "Krovikan Elementalist", 0)
	ateotTo(t, e, el, state.ZLibrary, state.ZBattlefield)
	bear := ateotFind(t, e, "Grizzly Bears", 0)
	ateotTo(t, e, bear, state.ZLibrary, state.ZBattlefield)

	// Activate the {U}{U} ability targeting the bear.
	addMana(t, e, 0, "UU")
	d := e.Pending()
	aidx := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == el {
			aidx = o.Index
		}
	}
	if aidx < 0 {
		t.Fatalf("no Krovikan Elementalist activation offered: %+v", d.Options)
	}
	submitChoices(t, e, aidx)
	if d = e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending %+v, want the pump target ask", d)
	}
	tidx := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			tidx = o.Index
		}
	}
	if tidx < 0 {
		t.Fatalf("bear %d not offered: %+v", bear, d.Options)
	}
	submitChoices(t, e, tidx)
	passUntilStackEmpty(t, e, 20)
	if !e.HasKeyword(bear, "Flying") {
		t.Fatal("the pumped bear did not gain Flying")
	}
	replayCheck(t, e, cfg)

	// The end step sacrifices the pumped creature; Krovikan stays.
	ateotDriveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	for e.putTriggersOnStack() {
		answerTriggerOrders(t, e)
		passUntilStackEmpty(t, e, 20)
	}
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(bear).Zone; z != state.ZGraveyard {
		t.Fatalf("pumped bear zone = %s, want graveyard (sacrificed)", z)
	}
	if z := e.G.Obj(el).Zone; z != state.ZBattlefield {
		t.Fatalf("Krovikan itself was sacrificed (zone %s), want it kept", z)
	}
	replayCheck(t, e, cfg)
}

// TestAtEOTOutOfScopeValueStaysLoud pins the loud degrade: a body carrying an
// out-of-scope value (here the end-of-combat family, on Kari Zev's real Token
// trigger) records exactly one Note naming the value while the token is still
// minted.
func TestAtEOTOutOfScopeValueStaysLoud(t *testing.T) {
	reg := searchTestRegistry(t)
	e, _ := ateotEngine(t, reg, "Kari Zev, Skyship Raider")
	kari := ateotFind(t, e, "Kari Zev, Skyship Raider", 0)
	ateotTo(t, e, kari, state.ZLibrary, state.ZBattlefield)
	card := searchCorpusCard(t, reg, "Kari Zev, Skyship Raider")
	sa := cards.ResolveSVar(card.Faces[0].SVars, "TrigToken")
	if sa == nil {
		t.Fatal("Kari Zev TrigToken SVar unresolved")
	}
	before := len(e.G.Objs)
	effects.Resolve(e, &effects.Ctx{Source: kari, Controller: 0,
		SVars: card.Faces[0].SVars}, sa)
	notes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "AtEOT$ ExileCombat is not implemented") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("AtEOT$ ExileCombat produced %d notes, want exactly 1", notes)
	}
	minted := 0
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.IsToken && o.Zone == state.ZBattlefield {
			minted++
		}
	}
	if minted == 0 || len(e.G.Objs) <= before {
		t.Fatalf("the out-of-scope AtEOT$ suppressed the token mint (objs %d -> %d)", before, len(e.G.Objs))
	}
}

// TestAtEOTPurphorosHandMoveIsSacrificedAtEOT pins the exact-Hand ChangeZone
// dispatch (the round-2 MAJOR finding): an AtEOT$ rider on an Origin$ Hand
// ChangeZone with no object selector used to return through
// effChangeZoneHand before any schedule call existed, dropping the rider
// silently -- no DelayedRegister, no loud Note -- for four corpus carriers
// (Kavaron Consumed, Ilharg, Purphoros, Planebound Accomplice). Purphoros's
// printed {2}{R} ability ("put a red creature card ... onto the battlefield.
// Sacrifice it at the beginning of the next end step") is the real carrier:
// the hand_move ask is answered with a Goblin Piker, the piker enters, the
// DelayedRegister is recorded for it, and the next end step sacrifices it
// while Purphoros stays.
func TestAtEOTPurphorosHandMoveIsSacrificedAtEOT(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := ateotEngine(t, reg, "Purphoros, Bronze-Blooded", "Goblin Piker")
	purph := ateotFind(t, e, "Purphoros, Bronze-Blooded", 0)
	ateotTo(t, e, purph, state.ZLibrary, state.ZBattlefield)
	gob := ateotFind(t, e, "Goblin Piker", 0)
	if e.G.Obj(gob).Zone != state.ZHand {
		ateotTo(t, e, gob, state.ZLibrary, state.ZHand)
	}

	// Resolve the printed {2}{R} ability directly (the Feral Lightning
	// style): the cost machinery is not what this pin exercises.
	card := searchCorpusCard(t, reg, "Purphoros, Bronze-Blooded")
	var sa *cards.SA
	for _, ab := range card.Faces[0].Abilities {
		if ab.API == "ChangeZone" {
			sa = ab
		}
	}
	if sa == nil {
		t.Fatal("Purphoros has no ChangeZone ability")
	}
	effects.Resolve(e, &effects.Ctx{Source: purph, Controller: 0,
		SVars: card.Faces[0].SVars}, sa)
	d := e.Pending()
	// The explicit Optional$ You marker poses Forge's confirm-before-pick
	// gate first (optional_confirm1): accept it, then the hand_move pick
	// follows.
	if d == nil || d.ResumeKind != "hand_move_confirm" {
		t.Fatalf("pending %+v, want the hand_move_confirm gate (Optional$ You asks before the pick)", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hand_move" {
		t.Fatalf("pending %+v, want the hand_move KChoose (Optional$ You keeps the ask alive)", d)
	}
	gidx := -1
	for _, o := range d.Options {
		if o.Obj == gob {
			gidx = o.Index
		}
	}
	if gidx < 0 {
		t.Fatalf("Goblin Piker %d not offered: %+v", gob, d.Options)
	}
	submitChoices(t, e, gidx)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(gob); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("the picked piker is %+v, want on seat 0's battlefield", o)
	}
	registrations := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedRegister && ev.Obj == gob {
			registrations++
		}
	}
	if registrations != 1 {
		t.Fatalf("the hand move emitted %d DelayedRegister events for the piker, want exactly 1", registrations)
	}
	replayCheck(t, e, cfg)

	// The next end step sacrifices the moved creature; Purphoros stays.
	ateotDriveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	for e.putTriggersOnStack() {
		answerTriggerOrders(t, e)
		passUntilStackEmpty(t, e, 20)
	}
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(gob).Zone; z != state.ZGraveyard {
		t.Fatalf("the hand-moved piker survived the end step (zone %s), want it sacrificed to the graveyard", z)
	}
	if z := e.G.Obj(purph).Zone; z != state.ZBattlefield {
		t.Fatalf("Purphoros itself moved at the end step (zone %s), want it kept", z)
	}
	replayCheck(t, e, cfg)
}

// TestAtEOTCrazedArmodonDestroysItself pins the Destroy rider, the one value
// the shared reader needs a new builtin body for (__kwAtEOTDestroy): the
// Armodon's {G} ability pumps itself (Defined$ Self) and destroys it at the
// beginning of the next end step.
func TestAtEOTCrazedArmodonDestroysItself(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := ateotEngine(t, reg, "Crazed Armodon")
	arm := ateotFind(t, e, "Crazed Armodon", 0)
	ateotTo(t, e, arm, state.ZLibrary, state.ZBattlefield)

	addMana(t, e, 0, "G")
	d := e.Pending()
	aidx := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == arm {
			aidx = o.Index
		}
	}
	if aidx < 0 {
		t.Fatalf("no Crazed Armodon activation offered: %+v", d.Options)
	}
	submitChoices(t, e, aidx)
	passUntilStackEmpty(t, e, 20)
	if !e.HasKeyword(arm, "Trample") {
		t.Fatal("the Armodon did not gain Trample from its pump")
	}
	replayCheck(t, e, cfg)

	ateotDriveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	for e.putTriggersOnStack() {
		answerTriggerOrders(t, e)
		passUntilStackEmpty(t, e, 20)
	}
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(arm).Zone; z != state.ZGraveyard {
		t.Fatalf("the Armodon survived the end step (zone %s), want it destroyed to the graveyard", z)
	}
	replayCheck(t, e, cfg)
}

// TestAtEOTDestroyRegistrationTracksIncarnation pins the round-2 finding on
// the fold: the __kwAtEOTDestroy promise must be incarnation-tracked like
// the dash/warp bodies it sits beside (a Destroy-rider permanent that left
// and returned as a new incarnation is not destroyed by the stale promise),
// while the encore sacrifice body keeps its untracked convention.
func TestAtEOTDestroyRegistrationTracksIncarnation(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := ateotEngine(t, reg)
	id := e.G.Zone(state.ZHand, 0)[0]
	inc := e.G.Obj(id).Incarnation
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: id, Player: 0,
		Step: state.StepEnd, Counter: "__kwAtEOTDestroy"})
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: id, Player: 0,
		Step: state.StepEnd, Counter: "__kwEncoreSacrifice"})
	if len(e.G.Delayed) != 2 {
		t.Fatalf("delayed registrations = %d, want 2", len(e.G.Delayed))
	}
	last := e.G.Delayed[len(e.G.Delayed)-2]
	if last.Execute != "__kwAtEOTDestroy" || !last.TrackSource || last.SourceIncarnation != inc {
		t.Fatalf("__kwAtEOTDestroy registration %+v, want incarnation-tracked (incarnation %d)", last, inc)
	}
	encore := e.G.Delayed[len(e.G.Delayed)-1]
	if encore.Execute != "__kwEncoreSacrifice" || encore.TrackSource {
		t.Fatalf("__kwEncoreSacrifice registration %+v, want untracked (the CopyPermanent convention)", encore)
	}
	replayCheck(t, e, cfg)
}
