package rules

// HasXManaCost$ True on cast/activation trigger modes (Mode$ SpellCast and
// Mode$ AbilityCast/SpellAbilityCast): "whenever you cast a permanent spell
// with a mana cost that contains {X}" / "if that spell's mana cost or that
// ability's activation cost contains {X}" (Unbound Flourishing et al.). The
// gate reads the PRINTED cost -- the spell's face ManaCost or the ability's
// printed Cost$ param -- so a trigger with the param fires only on casts and
// activations whose printed shape carries an {X} mana symbol, whatever value
// was announced (an X announced as 0 still "contains {X}").
//
// Fixtures are inline per the licensing rule (never a .cards/ .txt); the real
// corpus carriers' T:/SVar: lines are copied verbatim off .cards/cardsfolder
// (script text lives only in this gitignored corpus; a fixture copies it into
// a test, never into a tracked .txt).
//
// DEVIATION from the original brief, measured: the brief's symptom claimed
// Unbound Flourishing's SpellCast trigger "fires on EVERY permanent spell you
// cast". It did not fire AT ALL in this build: spellCastMatches read
// ValidCard$ through the zone-unaware MatchesObjectCtx, whose base
// "Permanent" was `o.Zone == ZBattlefield` (effects/filter.go matchesBase) --
// a spell on the stack never matched, so the trigger was dead before and
// after the gate. That zone-blindness is now FIXED (rules/trigger_match.go's
// spellCastPermanentSpec rewrites the leading `Permanent` token to
// `PermanentCard`, whose base reads a permanent spell on the stack, CR
// 109.2), and TestUnboundFlourishingFiresOnPermanentSpellCasts below pins
// the live behaviour; the SpellCast gate itself is also proven on Brass
// Infiniscope's carrier line, whose ValidSA$ Spell DOES match every stack
// spell (the over-firing the brief described is real there, and on UF's
// SpellAbilityCast half).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const unboundFlourishingSrc = "Name:Unbound Flourishing\nManaCost:2 G\nTypes:Enchantment\n" +
	"T:Mode$ SpellCast | ValidCard$ Permanent | ValidActivatingPlayer$ You | Execute$ TrigDouble | TriggerZones$ Battlefield | HasXManaCost$ True | TriggerDescription$ Whenever you cast a permanent spell with a mana cost that contains {X}, double the value of X.\n" +
	"SVar:TrigDouble:DB$ ChangeX | Defined$ TriggeredSpellAbility | Value$ TriggeredSpellAbility>Count$xPaid/Twice\n" +
	"T:Mode$ SpellAbilityCast | ValidSA$ Instant,Sorcery,Activated | ValidActivatingPlayer$ You | Execute$ TrigCopySpell | HasXManaCost$ True | TriggerZones$ Battlefield | TriggerDescription$ Whenever you cast an instant or sorcery spell or activate an ability, if that spell's mana cost or that ability's activation cost contains {X}, copy that spell or ability. You may choose new targets for the copy.\n" +
	"SVar:TrigCopySpell:DB$ CopySpellAbility | Defined$ TriggeredSpellAbility | MayChooseTarget$ True | AILogic$ Always\n" +
	"Oracle:Whenever you cast a permanent spell with a mana cost that contains {X}, double the value of X.\\nWhenever you cast an instant or sorcery spell or activate an ability, if that spell's mana cost or that ability's activation cost contains {X}, copy that spell or ability. You may choose new targets for the copy.\n"

// brassInfiniscopeTrigger is the real corpus carrier .cards/cardsfolder/b/brass_infiniscope.txt's
// Effect-granted CastTrigger line and its SVar chain, VERBATIM. In the corpus
// the trigger rides a DB$ Effect's Triggers$ (which this build leaves a Note
// -- the Effect registration covers only CantTarget/CantRegenerate), so the
// fixture attaches the T: line directly to the artifact to isolate the gate
// itself; the trigger line and bodies are unchanged.
const brassInfiniscopeSrc = "Name:Brass Infiniscope\nManaCost:4\nTypes:Artifact\n" +
	"T:Mode$ SpellCast | ValidActivatingPlayer$ You | ValidSA$ Spell | OneOff$ True | Execute$ TrigDraw | HasXManaCost$ True | TriggerDescription$ When you next cast a spell with {X} in its mana cost this turn, you draw a card and gain half X life, rounded down.\n" +
	"SVar:TrigDraw:DB$ Draw | SubAbility$ DBGainLife\n" +
	"SVar:DBGainLife:DB$ GainLife | LifeAmount$ Y\n" +
	"SVar:X:Count$xPaid\n" +
	"SVar:Y:SVar$X/HalfDown\n" +
	"Oracle:{T}: Add {C}{C}. When you next cast a spell with {X} in its mana cost this turn, you draw a card and gain half X life, rounded down.\n"

// An X-cost permanent spell with no other moving part: the X ask is the only
// announcement the cast needs, and the creature resolves with no counters.
const xCostHydraSrc = "Name:Test X Hydra\nManaCost:X G\nTypes:Creature Beast\nPT:0/0\n" +
	"SVar:X:Count$xPaid\n" +
	"Oracle:x\n"

const nonXBeastSrc = "Name:Plain Beast\nManaCost:1 G\nTypes:Creature Beast\nPT:1/1\n" +
	"Oracle:x\n"

// xBoltSrc is an instant WITH {X} in its printed cost, so the only gate that
// can keep UF's SpellCast trigger silent on it is ValidCard$ Permanent -- a
// permanent spell (CR 109.2) an instant is not.
const xBoltSrc = "Name:Test X Bolt\nManaCost:X R\nTypes:Instant\n" +
	"SVar:X:Count$xPaid\n" +
	"Oracle:x\n"

// The X-cost activated ability the SpellAbilityCast half needs: a printed
// Cost$ X T, no target ask, one draw.
const xSifterSrc = "Name:X Sifter\nTypes:Artifact\n" +
	"A:AB$ Draw | Cost$ X T | NumCards$ 1 | SpellDescription$ Draw a card.\n" +
	"Oracle:Draw a card.\n"

const plainSifterSrc = "Name:Plain Sifter\nTypes:Artifact\n" +
	"A:AB$ Draw | Cost$ T | NumCards$ 1 | SpellDescription$ Draw a card.\n" +
	"Oracle:Draw a card.\n"

// glavaSrc is the real corpus carrier .cards/cardsfolder/g/glava_five_advents_mage.txt
// (T:/SVar: lines copied verbatim). Its ValidSA$ names both spell kinds
// (Spell.Permanent) and the activated kind (Activated.!ManaAbility); the
// spell half is DEAD in this build (Mode$ SpellAbilityCast only observes
// AbilityPush -- see the report's Issues), so the carrier is exercised on its
// activation half, where the HasXManaCost$ gate is the whole difference. The
// line's OptionalDecider$ You makes the trigger optional with the choice at
// resolution (rules/trigger_queue.go's optionalDecider), so the drain helper
// answers that ask; OptionalDecider$ and ResolvedLimit$ reads beyond that are
// NOT this ticket's scope.
const glavaSrc = "Name:Glava, Five-Advents Mage\nManaCost:5 G G\nTypes:Legendary Creature Human Wizard\nPT:5/5\n" +
	"T:Mode$ SpellAbilityCast | ValidSA$ Spell.Permanent,Activated.!ManaAbility | ValidActivatingPlayer$ You | HasXManaCost$ True | TriggerZones$ Battlefield | Execute$ TrigSetX | OptionalDecider$ You | ResolvedLimit$ 1 | TriggerDescription$ Whenever you cast a permanent spell with {X} in its mana cost or activate an ability with {X} in its activation cost that isn't a mana ability, you may have the value of X become 5. Do this only once each turn.\n" +
	"SVar:TrigSetX:DB$ ChangeX | Defined$ TriggeredSpellAbility | Value$ 5\n" +
	"DeckHas:Ability$Counters\n" +
	"Oracle:Whenever you cast a permanent spell with {X} in its mana cost or activate an ability with {X} in its activation cost that isn't a mana ability, you may have the value of X become 5. Do this only once each turn.\n"

// gatePairSrc is the per-trigger-param no-regression fixture: two
// SpellAbilityCast triggers on one card, one with HasXManaCost$ True and one
// without. The gate is per-trigger-param: the paramless trigger must keep
// firing on EVERY activation, X or not.
const gatePairSrc = "Name:Gate Pair\nManaCost:1 G\nTypes:Enchantment\n" +
	"T:Mode$ SpellAbilityCast | ValidSA$ Activated | ValidActivatingPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigGate | HasXManaCost$ True | TriggerDescription$ X-only.\n" +
	"SVar:TrigGate:DB$ GainLife | Defined$ You | LifeAmount$ 1\n" +
	"T:Mode$ SpellAbilityCast | ValidSA$ Activated | ValidActivatingPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigAny | TriggerDescription$ Always.\n" +
	"SVar:TrigAny:DB$ GainLife | Defined$ You | LifeAmount$ 1\n" +
	"Oracle:x\n"

// pushCount counts TriggerPush events naming id as the trigger source. Events
// are emitted at PLACEMENT (putTriggersOnStack), after any trigger_order ask
// has been answered -- count only after the drain helpers below.
func pushCount(e *Engine, id state.ObjID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == id {
			n++
		}
	}
	return n
}

// drainStack passes priority until the stack is empty, answering the two
// trigger-drain decisions passUntilStackEmpty does not know: a trigger_order
// ask (two same-controller triggers, CR 603.3b -- option 0 puts that one on
// the stack first) and a trigger_optional ask (an OptionalDecider$ trigger's
// resolution-time "apply the effect?" -- option 0 = yes; the bodies under
// test here are a gain-life either way, and since changex1 the ChangeX
// bodies rewrite the paid X, all recorded unconditionally, and the CR 603.5
// placement is unconditional regardless of the answer).
func drainTriggerAsks(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining the stack (stack depth %d)", len(e.G.Stack))
		}
		switch d.Kind {
		case decision.KPriority:
			for _, o := range d.Options {
				if o.Kind == "pass" {
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
						t.Fatalf("submit pass: %v", err)
					}
					goto next
				}
			}
			t.Fatalf("priority decision with no pass option: %+v", d.Options)
		case decision.KTriggerOrder: // the answer is a full permutation
			idx := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				idx = append(idx, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: idx}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		case decision.KChoose:
			// Non-mana resolution choices retain the helper's deterministic
			// first-option answer.
			if len(d.Options) == 0 || d.Options[0].Kind != "mana" {
				if len(d.Options) == 0 {
					t.Fatalf("choose decision has no options: %+v", d)
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
					t.Fatalf("submit choose: %v", err)
				}
				goto next
			}
			// A triggered Combo mana effect allocates one distinct option per
			// produced unit. Take the first colour for every unit rather than
			// submitting the old single-choice answer.
			idx := make([]int, 0, d.Min)
			for _, o := range d.Options {
				if o.Kind != "mana" {
					t.Fatalf("mixed mana choice options: %+v", d.Options)
				}
				if len(idx) == d.Min {
					break
				}
				idx = append(idx, o.Index)
			}
			if len(idx) != d.Min {
				t.Fatalf("mana allocation has %d options, need %d: %+v", len(idx), d.Min, d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: idx}); err != nil {
				t.Fatalf("submit mana allocation: %v", err)
			}
		default:
			// trigger_optional and any other single-choice resolution ask:
			// take the first option.
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
				t.Fatalf("submit %v: %v", d.Kind, err)
			}
		}
	next:
	}
	if !e.G.Over && len(e.G.Stack) > 0 {
		t.Fatalf("stack never emptied (depth %d after %d passes)", len(e.G.Stack), limit)
	}
}

// TestHasXManaCostSpellCastGateOnBrassInfiniscope is the SpellCast leaf:
// Brass Infiniscope's real corpus trigger line (ValidSA$ Spell -- it matched
// EVERY spell cast before the gate, the brief's over-firing symptom) fires
// exactly once on an X-cost cast and not at all on a non-X cast.
func TestHasXManaCostSpellCastGateOnBrassInfiniscope(t *testing.T) {
	t.Parallel()
	e, cfg, _ := etbConfig(t, 131, []string{brassInfiniscopeSrc, xCostHydraSrc, nonXBeastSrc}, nil)
	brass := moveSeeded(t, e, 0, brassInfiniscopeSrc, state.ZBattlefield)
	if pushCount(e, brass) != 0 {
		t.Fatalf("trigger pushed before any cast")
	}
	// {X}{G} at X=2: the gate must let the trigger through.
	addMana(t, e, 0, "GGG")
	castFirst(t, e, "cast")
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("X decision %+v", d)
	}
	submitChoices(t, e, 2)
	drainTriggerAsks(t, e, 30)
	if got := pushCount(e, brass); got != 1 {
		t.Fatalf("Brass TriggerPush count after X-cost cast = %d, want 1", got)
	}
	// Non-X cast: the gate must silence the trigger entirely.
	addMana(t, e, 0, "GG")
	castFirst(t, e, "cast")
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "x" {
		t.Fatalf("non-X cast asked for an X value: %+v", d)
	}
	drainTriggerAsks(t, e, 30)
	if got := pushCount(e, brass); got != 1 {
		t.Fatalf("Brass TriggerPush count after non-X cast = %d, want still 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestUnboundFlourishingSpellAbilityCastGateFiresOnlyOnXAbilities is the
// SpellAbilityCast leaf on the headline card: activating an X-cost ability
// pushes UF's trigger exactly once (the printed Cost$ still carries the X at
// AbilityPush time, whatever value was announced -- X=0 here); activating a
// non-X ability pushes nothing more.
func TestUnboundFlourishingSpellAbilityCastGateFiresOnlyOnXAbilities(t *testing.T) {
	t.Parallel()
	e, cfg, _ := etbConfig(t, 131, []string{unboundFlourishingSrc, xSifterSrc, plainSifterSrc}, nil)
	uf := moveSeeded(t, e, 0, unboundFlourishingSrc, state.ZBattlefield)
	xs := moveSeeded(t, e, 0, xSifterSrc, state.ZBattlefield)
	moveSeeded(t, e, 0, plainSifterSrc, state.ZBattlefield)
	if pushCount(e, uf) != 0 {
		t.Fatalf("trigger pushed before any activation")
	}
	addMana(t, e, 0, "CC")
	castFirst(t, e, "ability")
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("X decision %+v", d)
	}
	submitChoices(t, e, 0) // announce X=0: the PRINTED cost still contains {X}
	drainTriggerAsks(t, e, 30)
	if got := pushCount(e, uf); got != 1 {
		t.Fatalf("UF SpellAbilityCast TriggerPush count after X-cost activation = %d, want 1", got)
	}
	if o := e.G.Obj(xs); o.Zone != state.ZBattlefield {
		t.Fatalf("X Sifter left the battlefield: %s", o.Zone)
	}
	// Non-X activation: nothing more.
	castFirst(t, e, "ability")
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "x" {
		t.Fatalf("non-X activation asked for an X value: %+v", d)
	}
	drainTriggerAsks(t, e, 30)
	if got := pushCount(e, uf); got != 1 {
		t.Fatalf("UF TriggerPush count after non-X activation = %d, want still 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestGlavaFiveAdventsSpellAbilityCastGateOnRealCorpusCarrier is the second
// real corpus carrier, its T:/SVar: lines copied verbatim, exercised on the
// activation half of its Mode$ SpellAbilityCast trigger.
func TestGlavaFiveAdventsSpellAbilityCastGateOnRealCorpusCarrier(t *testing.T) {
	t.Parallel()
	e, cfg, _ := etbConfig(t, 131, []string{glavaSrc, xSifterSrc, plainSifterSrc}, nil)
	glava := moveSeeded(t, e, 0, glavaSrc, state.ZBattlefield)
	moveSeeded(t, e, 0, xSifterSrc, state.ZBattlefield)
	moveSeeded(t, e, 0, plainSifterSrc, state.ZBattlefield)
	addMana(t, e, 0, "CC")
	castFirst(t, e, "ability")
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("X decision %+v", d)
	}
	submitChoices(t, e, 1)
	drainTriggerAsks(t, e, 30)
	if got := pushCount(e, glava); got != 1 {
		t.Fatalf("Glava TriggerPush count after X-cost activation = %d, want 1", got)
	}
	// Non-X activation: the gate must silence the trigger.
	castFirst(t, e, "ability")
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "x" {
		t.Fatalf("non-X activation asked for an X value: %+v", d)
	}
	drainTriggerAsks(t, e, 30)
	if got := pushCount(e, glava); got != 1 {
		t.Fatalf("Glava TriggerPush count after non-X activation = %d, want still 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestHasXManaCostGateIsPerTrigger is the no-regression leaf: two
// SpellAbilityCast triggers on one card, one carrying HasXManaCost$ True and
// one not. The gated trigger fires only on the X-cost activation; the
// paramless one fires on both.
func TestHasXManaCostGateIsPerTrigger(t *testing.T) {
	t.Parallel()
	e, cfg, _ := etbConfig(t, 131, []string{gatePairSrc, xSifterSrc, plainSifterSrc}, nil)
	gp := moveSeeded(t, e, 0, gatePairSrc, state.ZBattlefield)
	moveSeeded(t, e, 0, xSifterSrc, state.ZBattlefield)
	moveSeeded(t, e, 0, plainSifterSrc, state.ZBattlefield)
	addMana(t, e, 0, "CC")
	castFirst(t, e, "ability")
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("X decision %+v", d)
	}
	submitChoices(t, e, 1)
	drainTriggerAsks(t, e, 30)
	if got := pushCount(e, gp); got != 2 {
		t.Fatalf("Gate Pair TriggerPush count after X-cost activation = %d, want 2 (both triggers)", got)
	}
	castFirst(t, e, "ability")
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "x" {
		t.Fatalf("non-X activation asked for an X value: %+v", d)
	}
	drainTriggerAsks(t, e, 30)
	if got := pushCount(e, gp); got != 3 {
		t.Fatalf("Gate Pair TriggerPush count after non-X activation = %d, want 3 (only the paramless trigger)", got)
	}
	replayCheck(t, e, cfg)
}

// spellCastPushCount counts TriggerPush events naming id as the trigger
// source at trigger index 0 -- the card's FIRST T: line. On Unbound
// Flourishing that is the Mode$ SpellCast trigger (the second T: line, the
// SpellAbilityCast trigger, pushes at index 1), so the two halves of the
// card can be asserted separately.
func spellCastPushCount(e *Engine, id state.ObjID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == id && ev.Amount == 0 {
			n++
		}
	}
	return n
}

// TestUnboundFlourishingFiresOnPermanentSpellCasts is the flipped pin (was
// TestUnboundFlourishingSpellCastCorpusLineStaysInert, which pinned the
// zone-blind ValidCard$ read keeping the trigger dead). With the
// spellCastPermanentSpec rewrite, casting an X-cost CREATURE spell fires UF's
// SpellCast trigger exactly once; casting an X-cost INSTANT spell does not
// fire it -- the printed {X} satisfies HasXManaCost$, so the only clause
// standing between the trigger and the cast is ValidCard$ Permanent, and an
// instant is not a permanent spell (CR 109.2). The SpellAbilityCast half's
// behaviour is asserted unchanged by the tests above.
func TestUnboundFlourishingFiresOnPermanentSpellCasts(t *testing.T) {
	t.Parallel()
	e, cfg, _ := etbConfig(t, 131, []string{unboundFlourishingSrc, xCostHydraSrc, xBoltSrc}, nil)
	uf := moveSeeded(t, e, 0, unboundFlourishingSrc, state.ZBattlefield)
	addMana(t, e, 0, "GGG")
	castFirst(t, e, "cast")
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("X decision %+v", d)
	}
	submitChoices(t, e, 2)
	drainTriggerAsks(t, e, 30)
	if got := spellCastPushCount(e, uf); got != 1 {
		t.Fatalf("UF SpellCast TriggerPush count after X-cost creature cast = %d, want 1", got)
	}
	// X-cost instant: not a permanent spell -- the ValidCard$ gate must keep
	// the SpellCast trigger silent (the SpellAbilityCast trigger, index 1,
	// may still fire; that is the other half's business).
	addMana(t, e, 0, "RRR")
	castFirst(t, e, "cast")
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("instant X decision %+v", d)
	}
	submitChoices(t, e, 2)
	drainTriggerAsks(t, e, 30)
	if got := spellCastPushCount(e, uf); got != 1 {
		t.Fatalf("UF SpellCast TriggerPush count after X-cost instant cast = %d, want still 1 (an instant is not a permanent spell)", got)
	}
	replayCheck(t, e, cfg)
}
