package rules

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// corpusKeywordCard keeps these proofs tied to the actual Forge scripts whose
// coverage entries this ticket retires, rather than lookalike handwritten cards.
func corpusKeywordCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	// The checked-in IR is intentionally a cache. Parse the live GPL corpus
	// at test time so keyword expansion added by this change is exercised.
	paths := map[string]string{
		"Vein Ripper": "v/vein_ripper.txt", "Artisan of Kozilek": "a/artisan_of_kozilek.txt",
		"Fury": "f/fury.txt", "Shriekmaw": "s/shriekmaw.txt", "Dauthi Voidwalker": "d/dauthi_voidwalker.txt",
		"Emrakul, the World Anew": "e/emrakul_the_world_anew.txt", "Emrakul, the Aeons Torn": "e/emrakul_the_aeons_torn.txt", "Geyadrone Dihada": "g/geyadrone_dihada.txt", "Yavimaya Scion": "y/yavimaya_scion.txt", "Guardian of the Guildpact": "g/guardian_of_the_guildpact.txt", "Frenemy of the Guildpact": "f/frenemy_of_the_guildpact.txt", "Kitesail Larcenist": "k/kitesail_larcenist.txt", "Auntie Ool, Cursewretch": "a/auntie_ool_cursewretch.txt", "The Serpent Society": "t/the_serpent_society.txt", "Karazikar, the Eye Tyrant": "k/karazikar_the_eye_tyrant.txt", "Jon Irenicus, Shattered One": "j/jon_irenicus_shattered_one.txt", "Vislor Turlough": "v/vislor_turlough.txt", "Herald of Hoofbeats": "h/herald_of_hoofbeats.txt", "Gollum, Obsessed Stalker": "g/gollum_obsessed_stalker.txt", "Behind the Scenes": "b/behind_the_scenes.txt", "Ixhel, Scion of Atraxa": "i/ixhel_scion_of_atraxa.txt", "Prosthetic Injector": "p/prosthetic_injector.txt", "Necrogen Communion": "n/necrogen_communion.txt", "Time Beetle": "t/time_beetle.txt", "Sepulchral Primordial": "s/sepulchral_primordial.txt",
	}
	path, ok := paths[name]
	if !ok {
		t.Fatalf("no corpus path for %q", name)
	}
	c, ds := cards.Parse(filepath.Join("..", ".cards", "cardsfolder", path))
	if len(ds) != 0 {
		t.Fatalf("parse %s: %v", name, ds)
	}
	if ds = c.Link(); len(ds) != 0 {
		t.Fatalf("link %s: %v", name, ds)
	}
	return c
}

func TestWardVeinRipperCountersAnUnpaidTargetingSpell(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	warded := onBoardCard(t, e, 0, corpusKeywordCard(t, "Vein Ripper"))
	cause := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.PutOnStack, Obj: cause, Player: 1, From: state.ZLibrary, To: state.ZStack})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: cause, IDs: []state.ObjID{warded}})
	e.putTriggersOnStack()
	e.resolveTop()
	if e.Pending() == nil {
		t.Fatal("Ward did not ask the targeting spell's controller to pay")
	}
	// The second option is decline.
	if err := e.Submit(decision.Intent{Seq: e.Pending().Seq, Player: 1, Choices: []int{1}}); err != nil {
		t.Fatal(err)
	}
	if got := e.G.Obj(cause).Zone; got != state.ZGraveyard {
		t.Fatalf("unpaid ward spell zone = %s, want graveyard", got)
	}
}

func TestWardVeinRipperAcceptsItsRealSacrificePayment(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	warded := onBoardCard(t, e, 0, corpusKeywordCard(t, "Vein Ripper"))
	sac := onBoard(t, e, 1, "Name:Payment\nTypes:Creature\nPT:1/1\nOracle:x\n")
	cause := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.PutOnStack, Obj: cause, Player: 1, From: state.ZLibrary, To: state.ZStack})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: cause, IDs: []state.ObjID{warded}})
	e.putTriggersOnStack()
	e.resolveTop()
	if err := e.Submit(decision.Intent{Seq: e.Pending().Seq, Player: 1, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	d := e.Pending()
	if d == nil || d.ResumeKind != "ward_sac" {
		t.Fatalf("Vein Ripper ward did not ask for sacrifice: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	if got := e.G.Obj(sac).Zone; got != state.ZGraveyard {
		t.Fatalf("ward payment creature zone = %s, want graveyard", got)
	}
	if got := e.G.Obj(cause).Zone; got != state.ZStack {
		t.Fatalf("paid ward moved targeting spell to %s, want stack", got)
	}
}

func TestWardKitesailLarcenistChargesTheNonzeroPayer(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	warded := onBoardCard(t, e, 0, corpusKeywordCard(t, "Kitesail Larcenist"))
	cause := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.PutOnStack, Obj: cause, Player: 1, From: state.ZLibrary, To: state.ZStack})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: cause, IDs: []state.ObjID{warded}})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 1, Amount: 1})
	e.putTriggersOnStack()
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_pay" {
		t.Fatalf("Kitesail ward did not ask to pay: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	if got := e.G.Players[1].Pool.Total(); got != 0 {
		t.Fatalf("ward payer pool = %d, want 0 after paying", got)
	}
	if got := e.G.Obj(cause).Zone; got != state.ZStack {
		t.Fatalf("paid ward moved targeting spell to %s, want stack", got)
	}
}

func TestWardBlightMayUseATappedCreatureAndPoisonLoses(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	warded := onBoardCard(t, e, 0, corpusKeywordCard(t, "Auntie Ool, Cursewretch"))
	blighted := onBoard(t, e, 1, "Name:Tapped payment\nTypes:Creature\nPT:3/3\nOracle:x\n")
	e.emit(events.Event{Kind: events.Tap, Obj: blighted})
	cause := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.PutOnStack, Obj: cause, Player: 1, From: state.ZLibrary, To: state.ZStack})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: cause, IDs: []state.ObjID{warded}})
	e.putTriggersOnStack()
	e.resolveTop()
	if err := e.Submit(decision.Intent{Seq: e.Pending().Seq, Player: 1, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	d := e.Pending()
	if d == nil || len(d.Options) != 1 || d.Options[0].Obj != blighted {
		t.Fatalf("tapped Blight candidate = %+v, want tapped creature", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	if got := e.G.Obj(blighted).Counter("M1M1"); got != 2 {
		t.Fatalf("Blight counters = %d, want 2", got)
	}

	e2 := combatEngine(t)
	serpent := onBoardCard(t, e2, 0, corpusKeywordCard(t, "The Serpent Society"))
	e2.emit(events.Event{Kind: events.PlayerCounterChange, Player: 1, Counter: "POISON", Amount: 5})
	cause = e2.G.Zone(state.ZLibrary, 1)[0]
	e2.emit(events.Event{Kind: events.PutOnStack, Obj: cause, Player: 1, From: state.ZLibrary, To: state.ZStack})
	e2.emit(events.Event{Kind: events.TargetsChosen, Obj: cause, IDs: []state.ObjID{serpent}})
	e2.putTriggersOnStack()
	e2.resolveTop()
	if err := e2.Submit(decision.Intent{Seq: e2.Pending().Seq, Player: 1, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	if !e2.G.Players[1].Lost || e2.G.Players[1].Counter("POISON") != 10 {
		t.Fatalf("five poison Ward payment: lost/counters = %v/%d, want true/10", e2.G.Players[1].Lost, e2.G.Players[1].Counter("POISON"))
	}
}

// TestWardUsesTargetingStackObjectsController pins CR 702.21a's distinction
// between an activated ability's source characteristics and the controller of
// the ability object on the stack. A later gain-control event can leave these
// different; AbilityPush already records that controller independently.
func TestWardUsesTargetingStackObjectsController(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	warded := onBoardCard(t, e, 0, corpusKeywordCard(t, "Vein Ripper"))
	source := onBoard(t, e, 0, "Name:Borrowed source\nTypes:Creature\nPT:1/1\nA:AB$ Draw | Cost$ T\nOracle:x\n")
	payment := onBoard(t, e, 1, "Name:Ward payment\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.emit(events.Event{Kind: events.AbilityPush, Obj: source, Player: 1})
	if len(e.G.Stack) != 1 {
		t.Fatalf("ability stack = %v, want one ability", e.G.Stack)
	}
	ability := e.G.Stack[0]
	if e.G.Obj(ability).Controller != 1 || e.G.Obj(ability).Source != source || e.G.Obj(source).Controller != 0 {
		t.Fatalf("stack/source controllers = ability %+v source %+v", e.G.Obj(ability), e.G.Obj(source))
	}
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: ability, IDs: []state.ObjID{warded}})
	e.putTriggersOnStack()
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Player != 1 {
		t.Fatalf("Ward did not charge the opponent-controlled stack ability: pending=%+v payment=%d", d, payment)
	}
}

// TestWardDoesNotTriggerForFriendlyTargetingStackObject is the inverse of
// TestWardUsesTargetingStackObjectsController: source control and the stack
// object's controller differ here too, but the stack object is controlled by
// Ward's controller. CR 702.21a must not trigger Ward in that case.
func TestWardDoesNotTriggerForFriendlyTargetingStackObject(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	warded := onBoardCard(t, e, 0, corpusKeywordCard(t, "Vein Ripper"))
	source := onBoard(t, e, 1, "Name:Borrowed source\nTypes:Creature\nPT:1/1\nA:AB$ Draw | Cost$ T\nOracle:x\n")
	e.emit(events.Event{Kind: events.AbilityPush, Obj: source, Player: 0})
	if len(e.G.Stack) != 1 {
		t.Fatalf("ability stack = %v, want one ability", e.G.Stack)
	}
	ability := e.G.Stack[0]
	if e.G.Obj(ability).Controller != 0 || e.G.Obj(ability).Source != source || e.G.Obj(source).Controller != 1 {
		t.Fatalf("stack/source controllers = ability %+v source %+v", e.G.Obj(ability), e.G.Obj(source))
	}
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: ability, IDs: []state.ObjID{warded}})
	if e.putTriggersOnStack() || len(e.G.Stack) != 1 || e.Pending() != nil {
		t.Fatalf("friendly-controlled stack ability incorrectly triggered Ward: stack=%v pending=%+v", e.G.Stack, e.Pending())
	}
}

func TestWardManaPaymentActivatesManaAbilities(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	warded := onBoardCard(t, e, 0, corpusKeywordCard(t, "Kitesail Larcenist"))
	land := onBoard(t, e, 1, "Name:Ward Island\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ U\nOracle:x\n")
	cause := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.PutOnStack, Obj: cause, Player: 1, From: state.ZLibrary, To: state.ZStack})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: cause, IDs: []state.ObjID{warded}})
	e.putTriggersOnStack()
	e.resolveTop()
	if err := e.Submit(decision.Intent{Seq: e.Pending().Seq, Player: 1, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	d := e.Pending()
	if d == nil || d.ResumeKind != "ward_mana" || len(d.Options) != 2 || d.Options[0].Obj != land {
		t.Fatalf("Ward mana window = %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	d = e.Pending()
	if d == nil || d.ResumeKind != "ward_mana" || len(d.Options) != 1 || d.Options[0].Kind != "done" {
		t.Fatalf("Ward mana window after activation = %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	if e.G.Obj(cause).Zone != state.ZStack || !e.G.Obj(land).Tapped {
		t.Fatalf("paid Ward source/cause = tapped %v, zone %s", e.G.Obj(land).Tapped, e.G.Obj(cause).Zone)
	}
}

func TestAnnihilatorArtisanSacrificesThePrintedAmount(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	a := onBoardCard(t, e, 0, corpusKeywordCard(t, "Artisan of Kozilek"))
	e.G.Obj(a).SummonSick = false
	p1 := onBoard(t, e, 1, "Name:One\nTypes:Artifact\nOracle:x\n")
	p2 := onBoard(t, e, 1, "Name:Two\nTypes:Artifact\nOracle:x\n")
	p3 := onBoard(t, e, 1, "Name:Three\nTypes:Artifact\nOracle:x\n")
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{a}})
	e.putTriggersOnStack()
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("annihilator did not ask its defending player to choose sacrifices: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{1, 2}}); err != nil {
		t.Fatal(err)
	}
	if e.G.Obj(p1).Zone != state.ZBattlefield || e.G.Obj(p2).Zone != state.ZGraveyard || e.G.Obj(p3).Zone != state.ZGraveyard {
		t.Fatalf("Artisan of Kozilek did not sacrifice the chosen two permanents: %s %s %s", e.G.Obj(p1).Zone, e.G.Obj(p2).Zone, e.G.Obj(p3).Zone)
	}
}

func TestDoubleStrikeFearAndShadowUseCorpusCombatKeywords(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	fury := onBoardCard(t, e, 0, corpusKeywordCard(t, "Fury"))
	fearBlocker := onBoard(t, e, 1, "Name:White\nManaCost:W\nTypes:Creature\nPT:1/1\nOracle:x\n")
	if e.actsThisDamageStep(fury, true) != true || e.actsThisDamageStep(fury, false) != true {
		t.Fatal("Fury's double strike must deal damage in both steps")
	}
	shriek := onBoardCard(t, e, 0, corpusKeywordCard(t, "Shriekmaw"))
	e.G.Obj(shriek).IsAttacking = true
	e.G.Obj(shriek).Attacking = 1
	if e.canBlock(fearBlocker, shriek) {
		t.Fatal("nonblack nonartifact creature blocked Shriekmaw with fear")
	}
	voidwalker := onBoardCard(t, e, 0, corpusKeywordCard(t, "Dauthi Voidwalker"))
	e.G.Obj(voidwalker).IsAttacking, e.G.Obj(voidwalker).Attacking = true, 1
	if e.canBlock(fearBlocker, voidwalker) {
		t.Fatal("non-Shadow creature blocked Dauthi Voidwalker")
	}
}

func TestHorsemanshipCanBlockOnlyHorsemanshipAttackers(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	// Herald of Hoofbeats is a real corpus carrier of the printed
	// K:Horsemanship (CR 702.31). Seat 1 attacks seat 0, so seat 0's creatures
	// are the prospective blockers.
	plainBlocker := onBoard(t, e, 0, "Name:White\nManaCost:W\nTypes:Creature\nPT:1/1\nOracle:x\n")
	// A second real Herald on the defending side supplies a printed-
	// horsemanship blocker, independent of the layer-7 grant.
	horsemanshipBlocker := onBoardCard(t, e, 0, corpusKeywordCard(t, "Herald of Hoofbeats"))
	// A Knight under seat 1's control, to exercise the Herald's layer-7 static
	// (Knight.YouCtrl+Other) -- the grant the combat read depends on.
	knight := onBoard(t, e, 1, "Name:Knight\nManaCost:W\nTypes:Creature Knight\nPT:2/2\nOracle:x\n")
	heraid := onBoardCard(t, e, 1, corpusKeywordCard(t, "Herald of Hoofbeats"))
	// Layer-7: the seat-1 Herald's static gives seat 1's other Knight
	// horsemanship.
	if !e.HasKeyword(knight, "Horsemanship") {
		t.Fatal("Herald of Hoofbeats' static did not grant another Knight Horsemanship")
	}
	if !e.HasKeyword(heraid, "Horsemanship") {
		t.Fatal("Herald of Hoofbeats does not have its own printed Horsemanship")
	}
	// CR 702.31b, direction 1: a creature without horsemanship cannot block a
	// horsemanship attacker.
	e.G.Obj(heraid).IsAttacking, e.G.Obj(heraid).Attacking = true, 0
	if e.canBlock(plainBlocker, heraid) {
		t.Fatal("plain creature blocked a horsemanship attacker")
	}
	// CR 702.31b, direction 2: a horsemanship creature can block a
	// horsemanship attacker.
	if !e.canBlock(horsemanshipBlocker, heraid) {
		t.Fatal("horsemanship creature could not block a horsemanship attacker")
	}
	// CR 702.31b, direction 3 (the asymmetric half): a horsemanship creature
	// can block a creature WITHOUT horsemanship.
	plainAttacker := onBoard(t, e, 1, "Name:White\nManaCost:W\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.G.Obj(plainAttacker).IsAttacking, e.G.Obj(plainAttacker).Attacking = true, 0
	if !e.canBlock(horsemanshipBlocker, plainAttacker) {
		t.Fatal("horsemanship creature could not block a plain attacker (rule is asymmetric)")
	}
}

func TestProtectionUsesAllLiveColourQualities(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	emrakul := onBoardCard(t, e, 0, corpusKeywordCard(t, "Emrakul, the World Anew"))
	spell := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.PutOnStack, Obj: spell, Player: 1, From: state.ZLibrary, To: state.ZStack})
	if !e.protectedFrom(emrakul, spell) {
		t.Fatal("Emrakul's protection from spells did not recognize a spell")
	}
	scion := onBoardCard(t, e, 0, corpusKeywordCard(t, "Yavimaya Scion"))
	artifact := onBoard(t, e, 1, "Name:Artifact source\nTypes:Artifact\nOracle:x\n")
	if !e.protectedFrom(scion, artifact) {
		t.Fatal("parameterized Protection:Artifact did not use the shared object-spec grammar")
	}
	guardian := onBoardCard(t, e, 0, corpusKeywordCard(t, "Guardian of the Guildpact"))
	mono := onBoard(t, e, 1, "Name:Mono\nManaCost:G\nTypes:Creature\nPT:1/1\nOracle:x\n")
	if !e.protectedFrom(guardian, mono) {
		t.Fatal("Guardian's Protection:Card.MonoColor did not match a monocolored source")
	}
	frenemy := onBoardCard(t, e, 0, corpusKeywordCard(t, "Frenemy of the Guildpact"))
	eons := onBoardCard(t, e, 0, corpusKeywordCard(t, "Emrakul, the Aeons Torn"))
	coloredSpell := onBoard(t, e, 1, "Name:Colored spell\nManaCost:R\nTypes:Instant\nOracle:x\n")
	e.emit(events.Event{Kind: events.PutOnStack, Obj: coloredSpell, Player: 1, From: state.ZBattlefield, To: state.ZStack})
	if !e.protectedFrom(eons, coloredSpell) {
		t.Fatal("Spell.nonColorless protection did not recognize a colored spell")
	}
	geyadrone := onBoardCard(t, e, 0, corpusKeywordCard(t, "Geyadrone Dihada"))
	corrupt := onBoard(t, e, 1, "Name:Corrupt source\nTypes:Artifact\nOracle:x\n")
	e.emit(events.Event{Kind: events.CounterChange, Obj: corrupt, Counter: "CORRUPTION", Amount: 1})
	if !e.protectedFrom(geyadrone, corrupt) {
		t.Fatal("counter-qualified Protection did not recognize a corrupted permanent")
	}
	enemy := onBoard(t, e, 1, "Name:Enemy pair\nManaCost:U G\nTypes:Creature\nPT:1/1\nOracle:x\n")
	ally := onBoard(t, e, 1, "Name:Ally pair\nManaCost:W U\nTypes:Creature\nPT:1/1\nOracle:x\n")
	if !e.protectedFrom(frenemy, enemy) || e.protectedFrom(frenemy, ally) {
		t.Fatal("Frenemy's Protection:Card.EnemyColor did not distinguish enemy and allied pairs")
	}
}

func TestGoadKarazikarEnforcesEveryGoaderAtDeclaration(t *testing.T) {
	t.Parallel()
	e := New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b", "c", "d"}, Decks: [][]*cards.Card{
		mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40),
	}}))
	e.G.Active = 2
	e.G.Step = state.StepDeclareAttackers
	kar := corpusKeywordCard(t, "Karazikar, the Eye Tyrant")
	goad := cards.ResolveSVar(kar.Faces[0].SVars, "DBGoad")
	if goad == nil || goad.API != "Goad" {
		t.Fatal("Karazikar's real Goad subability did not compile")
	}
	victim := onBoardReady(t, e, 2, "Name:Victim\nTypes:Creature\nPT:1/1\nOracle:x\n")
	for _, goader := range []state.PlayerID{0, 1} {
		effects.Resolve(e, &effects.Ctx{Controller: goader, Targets: []state.Target{{Obj: victim}}}, goad)
	}
	if got := e.G.Obj(victim).Goads; len(got) != 2 || got[0].Player != 0 || got[1].Player != 1 {
		t.Fatalf("goad relationships = %v, want [0 1]", got)
	}
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers || len(d.Options) != 1 || d.Options[0].Obj != victim || d.Options[0].Player != 3 {
		t.Fatalf("two-goader attack options = %+v, want only victim attacking player 3", d)
	}
	// CR 508.1d travels on the wire: the goaded option is marked Required so
	// a rules-ignorant seat can build the legal declaration (validateAttack
	// Declaration rejects the omission).
	if !d.Options[0].Required {
		t.Fatal("the goaded creature's attack option is not marked Required")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 2}); err == nil {
		t.Fatal("goaded creature was allowed to skip its required attack")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 2, Choices: []int{d.Options[0].Index}}); err != nil {
		t.Fatalf("legal non-goader attack rejected: %v", err)
	}
	if !e.G.Obj(victim).IsAttacking || e.G.Obj(victim).Attacking != 3 {
		t.Fatal("goaded creature did not attack the only non-goader")
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 1})
	if got := e.G.Obj(victim).Goads; len(got) != 1 || got[0].Player != 1 {
		t.Fatalf("first goader expiry = %v, want [1]", got)
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: 2})
	if got := e.G.Obj(victim).Goads; len(got) != 0 {
		t.Fatalf("second goader expiry = %v, want none", got)
	}
}

func TestGoadDurationsUseRealJonAndVislorScripts(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	victim := onBoardReady(t, e, 1, "Name:Victim\nTypes:Creature\nPT:1/1\nOracle:x\n")
	jon := onBoardCard(t, e, 0, corpusKeywordCard(t, "Jon Irenicus, Shattered One"))
	jonGoad := cards.ResolveSVar(e.G.Obj(jon).Face().SVars, "DBGoad")
	effects.Resolve(e, &effects.Ctx{Source: jon, Controller: 0, Targets: []state.Target{{Obj: victim}}}, jonGoad)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 1})
	if got := e.G.Obj(victim).Goads; len(got) != 1 || got[0].Duration != "Permanent" {
		t.Fatalf("Jon's permanent goad expired at next turn: %v", got)
	}

	vislor := onBoardCard(t, e, 0, corpusKeywordCard(t, "Vislor Turlough"))
	// Vislor's chain donates itself (GainControl) before DBGoad resolves, so
	// the goad is made by Vislor's original controller while the opponent
	// controls it. The control transfer goes through the logged ControlChange
	// event, never a direct Controller write.
	e.emit(events.Event{Kind: events.ControlChange, Obj: vislor, Player: 1})
	vislorGoad := cards.ResolveSVar(e.G.Obj(vislor).Face().SVars, "DBGoad")
	effects.Resolve(e, &effects.Ctx{Source: vislor, Controller: 0, Targets: []state.Target{{Obj: victim}}}, vislorGoad)
	if got := e.G.Obj(vislor).Goads; len(got) != 1 || got[0].Duration != "AsLongAsControl" || got[0].Source != vislor || got[0].Controller != 1 || got[0].Player != 0 {
		t.Fatalf("Vislor's conditional goad = %v", got)
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Goad && ev.Obj == vislor {
			found = ev.Player == 0 && ev.Text == "AsLongAsControl" && len(ev.IDs) == 1 && ev.IDs[0] == vislor && ev.Amount == 2
		}
	}
	if !found {
		t.Fatal("Vislor's goad event did not preserve its conditional lifetime")
	}
	// "For as long as they control it": control returning ends the goad, and
	// a later return to the same opponent does not revive it.
	e.emit(events.Event{Kind: events.ControlChange, Obj: vislor, Player: 0})
	if got := e.G.Obj(vislor).Goads; len(got) != 0 {
		t.Fatalf("Vislor's goad survived losing control: %v", got)
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: vislor, Player: 1})
	if got := e.G.Obj(vislor).Goads; len(got) != 0 {
		t.Fatalf("Vislor's ended goad revived on a later control change: %v", got)
	}
}

func TestProtectionThisTurnCastIgnoresUncastEntries(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	emrakul := onBoardCard(t, e, 0, corpusKeywordCard(t, "Emrakul, the World Anew"))
	lib := e.G.Zone(state.ZLibrary, 1)
	cast, reanimated, flickered := lib[0], lib[1], lib[2]

	e.emit(events.Event{Kind: events.PutOnStack, Obj: cast, Player: 1, From: state.ZLibrary, To: state.ZStack})
	e.emit(events.Event{Kind: events.MoveZone, Obj: cast, From: state.ZStack, To: state.ZBattlefield})
	if !e.protectedFrom(emrakul, cast) {
		t.Fatal("a permanent cast this turn is not matched by Permanent.ThisTurnCast")
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: reanimated, From: state.ZLibrary, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.MoveZone, Obj: reanimated, From: state.ZGraveyard, To: state.ZBattlefield})
	if o := e.G.Obj(reanimated); o.Zone != state.ZBattlefield || !o.EnteredThisTurn {
		t.Fatalf("reanimated fixture zone=%s entered=%v", o.Zone, o.EnteredThisTurn)
	}
	if e.protectedFrom(emrakul, reanimated) {
		t.Fatal("a permanent that entered this turn without being cast is protected against")
	}

	e.emit(events.Event{Kind: events.PutOnStack, Obj: flickered, Player: 1, From: state.ZLibrary, To: state.ZStack})
	e.emit(events.Event{Kind: events.MoveZone, Obj: flickered, From: state.ZStack, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.MoveZone, Obj: flickered, From: state.ZBattlefield, To: state.ZExile})
	e.emit(events.Event{Kind: events.MoveZone, Obj: flickered, From: state.ZExile, To: state.ZBattlefield})
	if e.protectedFrom(emrakul, flickered) {
		t.Fatal("a cast permanent flickered back as a new object is still treated as cast")
	}

	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: 1})
	if e.protectedFrom(emrakul, cast) {
		t.Fatal("a permanent cast last turn is still treated as cast this turn")
	}
}

// TestAttackersMaxExposesTheCeiling pins the wire half of CR 508.1j: when an
// AttackRestrict static (Silent Arbiter's shape) caps the whole declaration,
// the KAttackers decision's Max is that ceiling, not the option count, so a
// rules-ignorant client capped at Max can never assemble a declaration the
// engine would reject for size. Without a ceiling in force maxAttackers
// returns the int maximum and Max stays len(opts) (today's value, pinned
// everywhere else by construction).
func TestAttackersMaxExposesTheCeiling(t *testing.T) {
	t.Parallel()
	e := New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}))
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	onBoard(t, e, 0, "Name:Arbiter\nManaCost:4\nTypes:Artifact Creature Construct\nPT:1/5\n"+
		"S:Mode$ AttackRestrict | MaxAttackers$ 1 | Description$ No more than one creature can attack each combat.\nOracle:x\n")
	onBoardReady(t, e, 0, "Name:Rusher A\nTypes:Creature\nPT:2/2\nOracle:x\n")
	onBoardReady(t, e, 0, "Name:Rusher B\nTypes:Creature\nPT:2/2\nOracle:x\n")
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers || len(d.Options) != 2 {
		t.Fatalf("arbiter attack options = %+v, want one option per creature", d)
	}
	if d.Max != 1 {
		t.Fatalf("KAttackers Max = %d, want the MaxAttackers$ 1 ceiling", d.Max)
	}
	both := decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0, 1}}
	if err := d.Validate(both); err == nil {
		t.Fatal("a two-attacker declaration passed the ceiling-capped decision's own Max")
	}
	if err := e.Submit(both); err == nil {
		t.Fatal("a two-attacker declaration was accepted under a MaxAttackers$ 1 ceiling")
	}
	one := decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0}}
	if err := e.Submit(one); err != nil {
		t.Fatalf("a one-attacker declaration rejected under its own ceiling: %v", err)
	}
}

func TestSkulkBlocksOnlyGreaterPowerBlockers(t *testing.T) {
	t.Parallel()
	// Gollum, Obsessed Stalker is a real corpus carrier of the printed
	// K:Skulk (CR 702.110a: a creature with skulk can't be blocked by
	// creatures with greater power). Seat 1 attacks seat 0, so seat 0's
	// creatures are the prospective blockers.
	e := combatEngine(t)
	gollum := onBoardCard(t, e, 1, corpusKeywordCard(t, "Gollum, Obsessed Stalker"))
	e.G.Obj(gollum).IsAttacking, e.G.Obj(gollum).Attacking = true, 0
	if !e.HasKeyword(gollum, "Skulk") {
		t.Fatal("Gollum, Obsessed Stalker does not read as carrying printed Skulk")
	}
	// The equality boundary is the one a naive >= write breaks: CR 702.110a
	// bars only GREATER power, so an equal-power creature blocks freely.
	equal := onBoard(t, e, 0, "Name:Equal\nManaCost:1\nTypes:Creature\nPT:1/1\nOracle:x\n")
	lesser := onBoard(t, e, 0, "Name:Lesser\nManaCost:1\nTypes:Creature\nPT:0/5\nOracle:x\n")
	greater := onBoard(t, e, 0, "Name:Greater\nManaCost:3\nTypes:Creature\nPT:5/5\nOracle:x\n")
	if e.canBlock(greater, gollum) {
		t.Fatal("a 5/5 blocked a 1/1 attacker with skulk")
	}
	if !e.canBlock(equal, gollum) {
		t.Fatal("an equal-power creature could not block a skulk attacker")
	}
	if !e.canBlock(lesser, gollum) {
		t.Fatal("a lesser-power creature could not block a skulk attacker")
	}
	// Derived power, not printed PT: a 1/1 printed blocker holding three
	// +1/+1 counters is a 4/4 for CR 702.110a's comparison and must be
	// refused (printed PT alone would still admit it).
	e.emit(events.Event{Kind: events.CounterChange, Obj: equal, Counter: "P1P1", Amount: 3})
	if e.canBlock(equal, gollum) {
		t.Fatal("printed PT compared instead of derived power (a 1/1 with three +1/+1 counters still blocked)")
	}
	// And the attacker side is derived too: grow the 1/1 attacker to a 5/5
	// with four counters and the same 5/5 blocker is suddenly legal, because
	// its power no longer EXCEEDS the attacker's.
	e.emit(events.Event{Kind: events.CounterChange, Obj: gollum, Counter: "P1P1", Amount: 4})
	if !e.canBlock(greater, gollum) {
		t.Fatal("a 5/5 could not block a grown-to-5/5 skulk attacker")
	}
	// The 4/4 blocker is no longer excluded either: CR 702.110a bars only
	// GREATER power, and 4 < 5.
	if !e.canBlock(equal, gollum) {
		t.Fatal("a 4/4 could not block a grown-to-5/5 skulk attacker")
	}
}

func TestTimeBeetleSkulkEvadesBiggerBlocker(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	attacker := onBoardCard(t, e, 1, corpusKeywordCard(t, "Time Beetle"))
	e.G.Obj(attacker).IsAttacking, e.G.Obj(attacker).Attacking = true, 0
	if e.G.Obj(attacker).Face().Name != "Time Beetle" || e.Derived(attacker).Power != 1 {
		t.Fatal("Time Beetle precondition did not produce the expected 1-power attacker")
	}
	if !e.HasKeyword(attacker, "Skulk") {
		t.Fatal("Time Beetle does not read as carrying printed Skulk")
	}
	blocker := onBoard(t, e, 0, "Name:Big Blocker\nManaCost:3\nTypes:Creature\nPT:2/2\nOracle:x\n")
	if e.canBlock(blocker, attacker) {
		t.Fatal("a 2/2 blocked Time Beetle's 1/1 skulk attack")
	}
}

func TestSkulkDoesNotBlockSmaller(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	attacker := onBoardCard(t, e, 1, corpusKeywordCard(t, "Time Beetle"))
	e.G.Obj(attacker).IsAttacking, e.G.Obj(attacker).Attacking = true, 0
	// Assert the fixture before leaning on it: this test is about the
	// PRINTED keyword on this exact card, so a fixture that lost the
	// keyword (or was the wrong card) must fail loudly, not pass silently.
	if got := e.G.Obj(attacker).Face().Name; got != "Time Beetle" {
		t.Fatalf("fixture is %q, want Time Beetle", got)
	}
	if !e.HasKeyword(attacker, "Skulk") {
		t.Fatal("Time Beetle does not read as carrying printed Skulk")
	}
	// A SMALLER blocker (0 power against the attacker's 1) is admitted:
	// CR 702.110a bars only GREATER power, so the boundary below the
	// attacker is legal. This is the "DoesNotBlockSmaller" half.
	smaller := onBoard(t, e, 0, "Name:Small Blocker\nManaCost:1\nTypes:Creature\nPT:0/5\nOracle:x\n")
	if e.Derived(smaller).Power >= e.Derived(attacker).Power {
		t.Fatalf("skulk smaller-boundary precondition changed: blocker power %d, attacker power %d",
			e.Derived(smaller).Power, e.Derived(attacker).Power)
	}
	if !e.canBlock(smaller, attacker) {
		t.Fatal("a smaller creature could not block Time Beetle's skulk attack")
	}
	// The necessary Skulk property, proven by contrast: one GREATER-power
	// blocker is refused against the Skulk attacker and admitted against
	// an otherwise identical attacker WITHOUT the keyword. Removing the
	// Skulk read in canBlock makes the first assertion fail, so this test
	// cannot pass on the unfixed engine.
	greater := onBoard(t, e, 0, "Name:Big Blocker\nManaCost:3\nTypes:Creature\nPT:2/2\nOracle:x\n")
	plain := onBoard(t, e, 1, "Name:Plain Beetle\nManaCost:1\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.G.Obj(plain).IsAttacking, e.G.Obj(plain).Attacking = true, 0
	if e.HasKeyword(plain, "Skulk") {
		t.Fatal("control attacker unexpectedly has Skulk; the contrast proves nothing")
	}
	if e.canBlock(greater, attacker) {
		t.Fatal("a greater-power blocker blocked Time Beetle's skulk attack")
	}
	if !e.canBlock(greater, plain) {
		t.Fatal("the same greater-power blocker was refused against a non-skulk attacker")
	}
}

func TestGrantedSkulkStaticBlocksGreaterPowerBlockers(t *testing.T) {
	t.Parallel()
	// Behind the Scenes is a real corpus carrier of the layer-7 grant
	// (S:AddKeyword$ Skulk); HasKeyword reads the layer-derived keyword
	// list, so a granted skulk needs no path of its own beyond canBlock.
	e := combatEngine(t)
	onBoardCard(t, e, 1, corpusKeywordCard(t, "Behind the Scenes"))
	smalls := onBoard(t, e, 1, "Name:Small\nManaCost:1\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.G.Obj(smalls).IsAttacking, e.G.Obj(smalls).Attacking = true, 0
	if !e.HasKeyword(smalls, "Skulk") {
		t.Fatal("Behind the Scenes' static did not grant another creature Skulk")
	}
	// A seat-0 creature (not a Knight, so Behind the Scenes' Knight static
	// does not also apply) with greater power must be refused; an equal-power
	// one admitted.
	big := onBoard(t, e, 0, "Name:Big\nManaCost:3\nTypes:Creature\nPT:5/5\nOracle:x\n")
	tiny := onBoard(t, e, 0, "Name:Tiny\nManaCost:1\nTypes:Creature\nPT:1/1\nOracle:x\n")
	if e.canBlock(big, smalls) {
		t.Fatal("a greater-power creature blocked a small creature granted skulk by Behind the Scenes")
	}
	if !e.canBlock(tiny, smalls) {
		t.Fatal("an equal-power creature could not block a creature granted skulk")
	}
	// The granting enchantment leaving the battlefield ends the grant: the
	// static is re-derived per read, so the bigger creature can block again.
	enchanter := e.G.Zone(state.ZBattlefield, 1)
	for _, id := range enchanter {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Behind the Scenes" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
		}
	}
	if e.HasKeyword(smalls, "Skulk") {
		t.Fatal("skulk survived its granting enchantment leaving the battlefield")
	}
	if !e.canBlock(big, smalls) {
		t.Fatal("a greater-power creature still blocked after the grant ended")
	}
}
