package rules

// Task wildgrowth1: Effect-delivered replacements with a SetChosenNumber$
// binding -- the "that creature enters with an additional +1/+1 counter on
// it for each ..." trigger family (torgal_a_fine_hound, communal_brewing,
// wildgrowth_archaic). Before the fix the trigger fired, the DB$ Effect
// registered nothing, and the log carried one line:
//
//	note: continuous replacement unimplemented (ETBCreat)
//
// because effEffect's registration branch knew only the DamageDone-with-body
// shape (Taii Wakeen) and nothing anywhere read SetChosenNumber$. The fix
// binds the number ONCE at Effect creation against the trigger's own context
// (torgal's Dog/Wolf board count, communal's ingredient counters on itself,
// wildgrowth's TriggeredCard$Converge fire-time snapshot), registers the
// Event$ Moved + PutCounter-body replacement for real, and threads the
// frozen binding through replCtx into the body's Count$ChosenNumber head.
//
// Every fixture is a real Forge script SHAPE written inline per the repo's
// licensing rule (never a .cards/ .txt); each chain below is its corpus
// carrier's exact SVar chain. The two-distinct-values discipline is kept:
// every binding test asserts two different inputs produce two different
// counters, because the defect was a silent zero and a single-value test can
// pass by coincidence.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const torgalSrc = "Name:Torgal, A Fine Hound\nManaCost:1 G\nTypes:Legendary Creature Wolf\nPT:2/2\n" +
	"T:Mode$ SpellCast | ValidCard$ Creature.Human | ValidActivatingPlayer$ You | ActivatorThisTurnCast$ EQ1 | Execute$ TrigEffect | TriggerZones$ Battlefield | TriggerDescription$ Whenever you cast your first Human creature spell each turn, that creature enters with an additional +1/+1 counter on it for each Dog and/or Wolf you control.\n" +
	"SVar:TrigEffect:DB$ Effect | RememberObjects$ TriggeredCard | SetChosenNumber$ X | ReplacementEffects$ ETBCreat | ExileOnMoved$ Stack\n" +
	"SVar:ETBCreat:Event$ Moved | ValidCard$ Card.IsRemembered | Destination$ Battlefield | ReplaceWith$ DBPutP1P1 | ReplacementResult$ Updated\n" +
	"SVar:DBPutP1P1:DB$ PutCounter | Defined$ ReplacedCard | CounterType$ P1P1 | ETB$ True | CounterNum$ Count$ChosenNumber\n" +
	"SVar:X:Count$Valid Dog.YouCtrl,Wolf.YouCtrl\n" +
	"Oracle:x\n"

const wildgrowthSrc = "Name:Wildgrowth Archaic\nManaCost:2G 2G\nTypes:Creature Avatar\nPT:2/2\n" +
	"T:Mode$ SpellCast | ValidCard$ Creature | ValidActivatingPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigEffect | TriggerDescription$ Whenever you cast a creature spell, that creature enters with X additional +1/+1 counters on it, where X is the number of colors of mana spent to cast it.\n" +
	"SVar:TrigEffect:DB$ Effect | RememberObjects$ TriggeredCard | SetChosenNumber$ Y | ReplacementEffects$ ETBCreat | ExileOnMoved$ Stack\n" +
	"SVar:ETBCreat:Event$ Moved | ValidCard$ Card.IsRemembered | Destination$ Battlefield | ReplaceWith$ DBPutP1P1 | ReplacementResult$ Updated\n" +
	"SVar:DBPutP1P1:DB$ PutCounter | Defined$ ReplacedCard | CounterType$ P1P1 | ETB$ True | CounterNum$ Count$ChosenNumber\n" +
	"SVar:Y:TriggeredCard$Converge\n" +
	"Oracle:x\n"

const brewingSrc = "Name:Communal Brewing\nManaCost:2 G\nTypes:Enchantment\n" +
	"T:Mode$ SpellCast | ValidCard$ Creature | ValidActivatingPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigEffect | TriggerDescription$ Whenever you cast a creature spell, that creature enters with X additional +1/+1 counters on it, where X is the number of ingredient counters on CARDNAME.\n" +
	"SVar:TrigEffect:DB$ Effect | RememberObjects$ TriggeredCard | SetChosenNumber$ Y | ReplacementEffects$ ETBCreat | ExileOnMoved$ Stack\n" +
	"SVar:ETBCreat:Event$ Moved | ValidCard$ Card.IsRemembered | Destination$ Battlefield | ReplaceWith$ DBPutP1P1 | ReplacementResult$ Updated\n" +
	"SVar:DBPutP1P1:DB$ PutCounter | Defined$ ReplacedCard | CounterType$ P1P1 | ETB$ True | CounterNum$ Count$ChosenNumber\n" +
	"SVar:Y:Count$CardCounters.INGREDIENT\n" +
	"Oracle:x\n"

// taiiSrc is Taii Wakeen, Perfect Shot's AB$ Effect chain -- the stretch
// leg: the registered DamageDone replacement's body rewrites the held
// event's amount by Plus.Y, where Y is the Effect's own frozen
// SetChosenNumber$ binding (the {X} the activation paid).
const taiiSrc = "Name:Taii Wakeen, Perfect Shot\nManaCost:R W\nTypes:Legendary Creature Human Mercenary\nPT:2/3\n" +
	"A:AB$ Effect | Cost$ X T | ReplacementEffects$ RepDamage | SetChosenNumber$ X | SpellDescription$ If a source you control would deal noncombat damage to a permanent or player this turn, it deals that much damage plus X instead.\n" +
	"SVar:X:Count$xPaid\n" +
	"SVar:RepDamage:Event$ DamageDone | ActiveZones$ Command | ValidSource$ Card.YouCtrl,Emblem.YouCtrl | ValidTarget$ Permanent,Player | IsCombat$ False | ReplaceWith$ DmgPlusX | Description$ If a source you control would deal noncombat damage to a permanent or player this turn, it deals that much damage plus X instead.\n" +
	"SVar:DmgPlusX:DB$ ReplaceEffect | VarName$ DamageAmount | VarValue$ ReplaceCount$DamageAmount/Plus.Y\n" +
	"SVar:Y:Count$ChosenNumber\n" +
	"Oracle:x\n"

const wgWolfSrc = "Name:Fixture Wolf\nManaCost:1 G\nTypes:Creature Wolf\nPT:2/2\nOracle:x\n"
const wgHumanSrc = "Name:Fixture Human\nManaCost:1 G\nTypes:Creature Human Soldier\nPT:2/2\nOracle:x\n"
const wgElfSrc = "Name:Fixture Elf\nManaCost:1 G\nTypes:Creature Elf\nPT:2/2\nOracle:x\n"
const wgBearSrc = "Name:Fixture Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
const wgBoltSrc = "Name:Fixture Bolt\nManaCost:2 R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 2\nOracle:x\n"

// mysticShapeSrc is Mystic Reflection's SP$ Effect shape, body API Clone --
// the non-PutCounter Moved body the widened gate must NOT silence: it keeps
// its loud "continuous replacement unimplemented" Note (Done-3).
const mysticShapeSrc = "Name:Mystic Reflection Shape\nManaCost:1 U\nTypes:Instant\n" +
	"A:SP$ Effect | ValidTgts$ Creature.nonLegendary | TgtPrompt$ Choose target nonlegendary creature | RememberObjects$ Targeted | ReplacementEffects$ ReplaceETB | SpellDescription$ x\n" +
	"SVar:ReplaceETB:Event$ Moved | Destination$ Battlefield | ValidCard$ Creature,Planeswalker | ReplaceWith$ EnterAsCopy | Layer$ Copy | ReplacementResult$ Updated | Description$ x\n" +
	"SVar:EnterAsCopy:DB$ Clone | Defined$ Remembered | CloneTarget$ ReplacedCard\n" +
	"Oracle:x\n"

// activeMovedReplacements returns the live Effect-created replacements whose
// event is Moved -- empty once the entry's ExileOnMoved$ Stack sweep has
// ended the effect.
func activeMovedReplacements(e *Engine) []*state.ContinuousEffect {
	var out []*state.ContinuousEffect
	for i := range e.continuous {
		if ce := &e.continuous[i]; ce.ReplacementEvent == "Moved" {
			out = append(out, ce)
		}
	}
	return out
}

// assertNoUnimplementedNote fails if the log carries the pre-fix
// "continuous replacement unimplemented" Note.
func assertNoUnimplementedNote(t *testing.T, e *Engine) {
	t.Helper()
	if hasNote(e, "continuous replacement unimplemented") {
		t.Fatal("the ETBCreat replacement was not registered: the unimplemented Note is in the log")
	}
}

// TestTorgalEntersWithDogWolfCounters pins the Dog/Wolf count binding: the
// first Human creature spell you cast enters with exactly one additional
// +1/+1 counter per Dog/Wolf you control -- one wolf vs three wolves, two
// distinct values on purpose.
func TestTorgalEntersWithDogWolfCounters(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		wolves int
		want   int32
	}{
		{name: "one wolf (Torgal counts himself)", wolves: 1, want: 2},
		{name: "three wolves (Torgal counts himself)", wolves: 3, want: 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srcs := []string{torgalSrc, wgHumanSrc, wgElfSrc}
			for i := 0; i < tc.wolves; i++ {
				srcs = append(srcs, wgWolfSrc)
			}
			e, cfg, find := etbConfig(t, seedTossSeat0(211), srcs, nil)
			torgal := putCreature(t, e, 0, torgalSrc)
			for i := 0; i < tc.wolves; i++ {
				putCreature(t, e, 0, wgWolfSrc)
			}
			humanID := find("Fixture Human", 0)
			addMana(t, e, 0, "GG")
			castObj(t, e, humanID)
			if got := e.G.Obj(humanID).Counter("P1P1"); got != tc.want {
				t.Fatalf("Human entered with %d P1P1, want %d (one per Dog/Wolf)", got, tc.want)
			}
			if len(activeMovedReplacements(e)) != 0 {
				t.Fatalf("the ETBCreat effect survived its own entry: %+v", activeMovedReplacements(e))
			}
			assertNoUnimplementedNote(t, e)
			if torgal == 0 {
				t.Fatal("unreachable")
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestWildgrowthArchaicEntersWithConvergeCounters pins the converge binding:
// the creature spell enters with one additional +1/+1 counter per colour of
// mana spent to cast it -- one colour ({1}{G} paid GG) vs two ({1}{G} paid
// RG: the generic absorbed the R), two distinct values on purpose. A second
// creature cast the same turn creates its OWN trigger, effect and binding --
// the expired first effect must not linger in the registry to re-upgrade
// anything (the ExileOnMoved$ Stack sweep is what ends it).
func TestWildgrowthArchaicEntersWithConvergeCounters(t *testing.T) {
	t.Parallel()
	t.Run("one colour of mana", func(t *testing.T) {
		e, cfg, find := etbConfig(t, seedTossSeat0(217), []string{wildgrowthSrc, wgElfSrc, wgWolfSrc}, nil)
		putCreature(t, e, 0, wildgrowthSrc)
		elfID := find("Fixture Elf", 0)
		addMana(t, e, 0, "GG") // {1}{G} paid G pip + G generic = one colour
		castObj(t, e, elfID)
		if got := e.G.Obj(elfID).Counter("P1P1"); got != 1 {
			t.Fatalf("Elf entered with %d P1P1, want 1 (one colour spent)", got)
		}
		if len(activeMovedReplacements(e)) != 0 {
			t.Fatalf("the ETBCreat effect survived its own entry: %+v", activeMovedReplacements(e))
		}
		assertNoUnimplementedNote(t, e)
		replayCheck(t, e, cfg)
	})
	t.Run("two colours of mana", func(t *testing.T) {
		e, cfg, find := etbConfig(t, seedTossSeat0(223), []string{wildgrowthSrc, wgElfSrc, wgWolfSrc}, nil)
		putCreature(t, e, 0, wildgrowthSrc)
		elfID := find("Fixture Elf", 0)
		addMana(t, e, 0, "RG") // {1}{G} paid G pip + R generic = two colours
		castObj(t, e, elfID)
		if got := e.G.Obj(elfID).Counter("P1P1"); got != 2 {
			t.Fatalf("Elf entered with %d P1P1, want 2 (two colours spent)", got)
		}
		assertNoUnimplementedNote(t, e)
		replayCheck(t, e, cfg)
	})
	t.Run("the ended effect does not upgrade the next entry", func(t *testing.T) {
		e, cfg, find := etbConfig(t, seedTossSeat0(229), []string{wildgrowthSrc, wgElfSrc, wgWolfSrc}, nil)
		putCreature(t, e, 0, wildgrowthSrc)
		elfID := find("Fixture Elf", 0)
		addMana(t, e, 0, "RG")
		castObj(t, e, elfID)
		if got := e.G.Obj(elfID).Counter("P1P1"); got != 2 {
			t.Fatalf("Elf entered with %d P1P1, want 2", got)
		}
		if len(activeMovedReplacements(e)) != 0 {
			t.Fatalf("the first cast's effect survived its entry: %+v", activeMovedReplacements(e))
		}
		// A second creature cast the same turn is its own trigger, its own
		// effect, its own binding: exactly one colour's worth, never the
		// first cast's stale two.
		wolfID := find("Fixture Wolf", 0)
		addMana(t, e, 0, "GG")
		castObj(t, e, wolfID)
		if got := e.G.Obj(wolfID).Counter("P1P1"); got != 1 {
			t.Fatalf("Wolf entered with %d P1P1, want 1 (its own cast's one colour)", got)
		}
		replayCheck(t, e, cfg)
	})
}

// TestCommunalBrewingEntersWithIngredientCounters pins the ingredient-counter
// binding: the number is read off the enchantment itself at trigger time --
// one ingredient counter vs three, two distinct values on purpose.
func TestCommunalBrewingEntersWithIngredientCounters(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ing  int32
		want int32
	}{
		{"one ingredient counter", 1, 1},
		{"three ingredient counters", 3, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, find := etbConfig(t, seedTossSeat0(233), []string{brewingSrc, wgElfSrc, wgWolfSrc}, nil)
			brewing := putCreature(t, e, 0, brewingSrc)
			e.emit(events.Event{Kind: events.CounterChange, Obj: brewing,
				Counter: "INGREDIENT", Amount: tc.ing})
			elfID := find("Fixture Elf", 0)
			addMana(t, e, 0, "GG")
			castObj(t, e, elfID)
			if got := e.G.Obj(elfID).Counter("P1P1"); got != tc.want {
				t.Fatalf("Elf entered with %d P1P1, want %d (the ingredient counters)", got, tc.want)
			}
			if len(activeMovedReplacements(e)) != 0 {
				t.Fatalf("the ETBCreat effect survived its own entry: %+v", activeMovedReplacements(e))
			}
			assertNoUnimplementedNote(t, e)
			replayCheck(t, e, cfg)
		})
	}
}

// TestTaiiWakeenActivationAddsBoundX is the stretch leg: the AB$ Effect
// activation binds {X} paid (Count$xPaid) as the SetChosenNumber$ number,
// and the registered DamageDone replacement's ReplaceCount$DamageAmount/Plus.Y
// body -- the Plus operand behind SVar Y:Count$ChosenNumber, which
// replCountOp's numeric-only parser used to drop -- deals amount PLUS X. A
// {X}=2 activation turns a 2-damage bolt into 4.
func TestTaiiWakeenActivationAddsBoundX(t *testing.T) {
	t.Parallel()
	e, cfg, _ := etbConfig(t, seedTossSeat0(239), []string{taiiSrc, wgBoltSrc}, nil)
	taiiID := putCreature(t, e, 0, taiiSrc)
	e.priorityRound()
	// Taii's {X}{T} cost needs an untapped, unsick source: drive to seat 0's
	// turn-3 main phase (turn 2 is the opponent's).
	driveToStep(t, e, 3, 0, state.StepMain1)
	addMana(t, e, 0, "RRR")
	opt, ok := findAbilityOption(e, taiiID, 0)
	if !ok {
		t.Fatal("Taii's {X}{T} effect ability not offered")
	}
	submitChoices(t, e, opt.Index)
	chooseX(t, e, 2)
	passUntilStackEmpty(t, e, 20)
	// The binding froze at creation: the registered replacement carries it.
	live := false
	for i := range e.continuous {
		if ce := &e.continuous[i]; ce.ReplacementEvent == "DamageDone" && ce.ChosenNumber == 2 {
			live = true
		}
	}
	if !live {
		t.Fatal("no DamageDone replacement registered with ChosenNumber 2")
	}
	boltID := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Fixture Bolt" {
			boltID = id
		}
	}
	if boltID == 0 {
		t.Fatal("Fixture Bolt not in hand")
	}
	addMana(t, e, 0, "RRR")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == boltID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for the bolt: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	submitTargetTo(t, e, 1)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[1].Life; got != 16 {
		t.Fatalf("opponent life = %d, want 16 (2 damage plus the bound X=2)", got)
	}
	replayCheck(t, e, cfg)
}

// TestMysticReflectionShapeStaysLoud is Done-3: a non-PutCounter Event$
// Moved body (the Clone-bodied enter-as-copy replacement) is NOT admitted by
// the widened gate and keeps its loud "continuous replacement unimplemented"
// Note -- the gate widened, it did not silence.
func TestMysticReflectionShapeStaysLoud(t *testing.T) {
	t.Parallel()
	e, cfg, _ := etbConfig(t, seedTossSeat0(241), []string{mysticShapeSrc, wgBearSrc, wgWolfSrc}, nil)
	putCreature(t, e, 0, wgBearSrc)
	mysticID := findCardObj(t, e, 0, "Mystic Reflection Shape", state.ZHand)
	addMana(t, e, 0, "UUG")
	castObj(t, e, mysticID)
	// effEffect emits the unimplemented Note at REGISTRATION time -- when the
	// SP$ Effect resolves, before any creature has entered -- so the Note is
	// asserted only after the entry step below.
	putCreature(t, e, 0, wgWolfSrc) // a creature enters; the Clone body must stay loud
	if !hasNote(e, "continuous replacement unimplemented (ReplaceETB)") {
		t.Fatal("the non-PutCounter Moved body no longer emits its loud Note")
	}
	replayCheck(t, e, cfg)
}

// voidShapeSrc is void's exact SP$ ChooseNumber chain (its corpus carrier's
// SVar chain, minus the RevealDiscard sub whose targeting is noise here):
// SVar:X:Count$ChosenNumber feeding a ValidCards$ cmcEQX DestroyAll. Void's
// chosen number lives on state.Object.ChosenNumber (effects/choose.go's
// Choose event), NEVER on Ctx -- the Choose-event population the
// Count$ChosenNumber head answers from the source object when unbound.
const voidShapeSrc = "Name:Void Shape\nManaCost:3 B R\nTypes:Sorcery\n" +
	"A:SP$ ChooseNumber | SubAbility$ DBVoidDestroyAll | SpellDescription$ Choose a number. Destroy all artifacts and creatures with mana value equal to that number.\n" +
	"SVar:DBVoidDestroyAll:DB$ DestroyAll | ValidCards$ Artifact.cmcEQX,Creature.cmcEQX\n" +
	"SVar:X:Count$ChosenNumber\n" +
	"Oracle:x\n"

const wgConstructSrc = "Name:Fixture Construct\nManaCost:0\nTypes:Artifact Creature Construct\nPT:0/2\nOracle:x\n"
const wgBaubleSrc = "Name:Fixture Bauble\nManaCost:0\nTypes:Artifact\nOracle:x\n"

// TestVoidChooseNumberReadsTheLoggedChoice pins the OTHER side of the
// Count$ChosenNumber verdict: on a non-effect shape (void's SP$ ChooseNumber
// chain) the chosen number lives on the SOURCE object (effects/choose.go's
// logged Choose fold), never on Ctx. Before fuzz-cov3 the unbound head stayed
// UNRESOLVED, so void's cmcEQX never matched and Void destroyed nothing
// whatever number was chosen; the head now falls back to the source's own
// recorded answer, so the comparison reads the number the player actually
// chose -- never Ctx's meaningless zero (the first wildgrowth draft's bug,
// which killed every MV-0 permanent regardless of the answer). Choosing 1
// spares the MV-0 board; choosing 0 destroys it, exactly as the oracle says.
func TestVoidChooseNumberReadsTheLoggedChoice(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		answer   int
		survives bool
	}{{1, true}, {0, false}} {
		e, cfg, find := etbConfig(t, seedTossSeat0(251), []string{voidShapeSrc, wgConstructSrc, wgBaubleSrc}, nil)
		construct := putCreature(t, e, 0, wgConstructSrc)
		bauble := putCreature(t, e, 0, wgBaubleSrc)
		voidID := find("Void Shape", 0)
		addMana(t, e, 0, "BBRRRR")
		if o := e.G.Obj(voidID); o == nil || o.Zone != state.ZHand {
			t.Fatalf("precondition: Void Shape must be in hand: %+v", o)
		}
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision pending to cast Void Shape")
		}
		cast := -1
		for _, opt := range d.Options {
			if opt.Kind == "cast" && opt.Obj == voidID {
				cast = opt.Index
			}
		}
		if cast < 0 {
			t.Fatalf("no cast option for Void Shape: %+v", d.Options)
		}
		submitChoices(t, e, cast)
		for i := 0; i < 20; i++ {
			d = e.Pending()
			if d == nil {
				t.Fatal("no decision pending before the number ask")
			}
			if d.Kind == decision.KChoose && d.ResumeKind == "choosenumber" {
				break
			}
			if d.Kind != decision.KPriority {
				t.Fatalf("unexpected decision before the number ask: %+v", d)
			}
			passPriorityOnce(t, e)
		}
		if d.Kind != decision.KChoose || d.ResumeKind != "choosenumber" {
			t.Fatalf("Void Shape did not pose its number ask: %+v", d)
		}
		pick := -1
		for _, opt := range d.Options {
			if opt.Kind == "number" && int(opt.Amount) == tc.answer {
				pick = opt.Index
			}
		}
		if pick < 0 {
			t.Fatalf("number %d is not offered: %+v", tc.answer, d.Options)
		}
		submitChoices(t, e, pick)
		passUntilStackEmpty(t, e, 20)
		for _, obj := range []struct {
			name string
			id   state.ObjID
		}{{"MV-0 artifact creature", construct}, {"MV-0 artifact", bauble}} {
			o := e.G.Obj(obj.id)
			alive := o != nil && o.Zone == state.ZBattlefield
			if alive != tc.survives {
				t.Fatalf("chose %d: %s on battlefield = %v, want %v", tc.answer, obj.name, alive, tc.survives)
			}
		}
		replayCheck(t, e, cfg)
	}
}
