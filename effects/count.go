package effects

import (
	"math"
	"math/bits"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// Num resolves a numeric parameter. A literal is used directly (a leading
// sign included); anything else is treated as an SVar name whose body is a
// Count$ expression, after stripping a leading sign that carries Forge's
// stat-direction convention rather than naming the reference. An expression
// this build does not model evaluates to zero rather than to the default, so
// the failure mode is "the card did nothing" rather than "the card did
// something arbitrary".
func Num(h Host, c *Ctx, sa *cards.SA, key string, def int32) int32 {
	if n, ok := NumResolved(h, c, sa, key, def); ok {
		return n
	}
	if _, present := sa.Params[key]; present {
		return 0 // present but unresolvable degrades to zero, not to def
	}
	return def
}

// NumForObject is NumResolved for an amount that depends on WHICH object it
// is applied to. Forge's bare "Double" P/T amount (NumAtt$ Double, NumDef$
// Double) is the whole reason: it means "add this object's current
// characteristic back to itself", so a scalar resolver that reads the first
// Defined object (or any one object) is wrong the moment the effect affects
// more than one creature -- the corpus's dominant carriers are
// `Defined$ Valid Creature.YouCtrl` (Double Trouble, Unnatural Growth,
// Zopandrel, Roar of Endless Song, ...), where one hoisted amount would be
// applied to every affected creature. Resolving per object, at resolution
// time, through Host's LAYER-DERIVED value (CR 613, CR 608.2h) keeps a
// creature that itself grew this turn doubling its grown power, and keeps two
// differently-sized creatures each doubling their own.
//
// This is the ONE home for the "Double" token: the token and the key->
// characteristic mapping live once, in statIsPowerKey, rather than re-spelled
// per primitive. Pump and PumpAll -- the corpus's only carriers -- resolve
// their amounts per affected object through here. Any other Num consumer that
// later needs a per-object amount names its P/T key in statIsPowerKey and
// calls NumForObject instead of Num; until it does, its scalar read of
// "Double" degrades to zero via Num below (fail-closed), never to another
// object's stat. Every non-Double value falls through to Num, so NumForObject
// is a drop-in superset: a literal, an SVar reference or a Count$ body behaves
// identically whether or not the caller has an object to hand, including Num's
// degrade-to-zero for an unmodelled body.
func NumForObject(h Host, c *Ctx, sa *cards.SA, key string, def int32, obj state.ObjID) int32 {
	if raw := strings.TrimSpace(sa.Params[key]); raw == "Double" && obj != 0 {
		if p, ok := statIsPowerKey(key); ok {
			if !p {
				return h.Toughness(obj)
			}
			return h.Power(obj)
		}
	}
	return Num(h, c, sa, key, def)
}

// statIsPowerKey maps a P/T amount parameter to the characteristic it names:
// ok reports whether the key is a modelled P/T amount at all, and the bool is
// true for power and false for toughness. It is the single key table the
// "Double" resolution reads, so a new P/T primitive that resolves through
// NumForObject is covered by naming its parameter here, not by re-spelling
// the token at the call site. The keys are the real P/T amount parameters the
// Num family is handed: Pump/PumpAll's NumAtt/NumDef (the only wired callers),
// Animate's Power/Toughness, SetPower$/SetToughness$-style base-sets, and the
// token / face-down families.
func statIsPowerKey(key string) (power, ok bool) {
	switch key {
	case "NumAtt", "Power", "SetPower", "TokenPower", "FaceDownPower":
		return true, true
	case "NumDef", "Toughness", "SetToughness", "TokenToughness", "FaceDownToughness":
		return false, true
	}
	return false, false
}

// NumResolved is Num plus a resolvability verdict: it answers whether the
// parameter RESOLVED under the same grammar Num reads -- a signed literal, an
// SVar name present in the context's table, a recognised inline expression
// prefix (Count$/Sacrificed$/Remembered$/TriggerCount$/ReplaceCount$), a bare
// count body the head dispatch resolves (the PlayerCount<group>$<property>
// family -- Forge writes these as SVar bodies WITHOUT the Count$ prefix, so a
// direct parameter value is the same bare body, Tolarian Contempt's
// TargetMax$ PlayerCountOpponents$Amount), or the bare X. Num itself degrades an unresolvable value to zero ("the card did
// nothing"); NumResolved exists for a caller that must not confuse that
// degrade-to-zero with a LEGITIMATE zero -- rules' replacement matcher gates
// a DB$ ReplaceDamage prevention body on its Amount$ and fails closed on a
// value this build cannot price, so an unmodelled ShieldAmount frame cannot
// silently erase the damage it was supposed to partially prevent.
func NumResolved(h Host, c *Ctx, sa *cards.SA, key string, def int32) (int32, bool) {
	if c == nil {
		c = &Ctx{}
	}
	raw, ok := sa.Params[key]
	if !ok {
		return def, false
	}
	raw = strings.TrimSpace(raw)
	if n, err := strconv.Atoi(raw); err == nil {
		return int32(n), true // a signed literal ("+2"/"-2") lands here: Atoi eats the sign
	}
	// Forge writes a stat direction as a sign on the value ("NumAtt$ +X" --
	// Goblin Piledriver), so a signed non-literal is not a reference NAMED
	// with the sign but an ordinary reference carrying a direction. Strip a
	// leading sign here and resolve the bare body through the fallbacks
	// below, applying the sign to whatever they return. A lone sign with
	// nothing after it is not a value and keeps the degrade-to-zero path.
	sign := int32(1)
	if len(raw) > 1 && (raw[0] == '+' || raw[0] == '-') {
		if raw[0] == '-' {
			sign = -1
		}
		raw = raw[1:]
	}
	if v, ok := runtimeSVar(c, raw); ok {
		// A runtime write (api:StoreSVar) shadows the printed body of the same
		// name -- checked BEFORE the table, or LifePaidOnETB:Number$0 would
		// win over the stored value.
		return sign * v, true
	}
	if c.SVars != nil {
		if body, ok := c.SVars[raw]; ok {
			return sign * EvalCount(h, c, body), true
		}
	}
	// A DB$ RollDice publication of this same resolution (effects/dice.go):
	// a sub's numeric parameter naming the roll -- Boomflinger's NumDmg$
	// Result, Grave Endeavor's LifeAmount$ Y, Neverwinter Hydra's
	// CounterNum$ Result -- reads the published value through the bare name,
	// exactly as the SVar$ indirection resolves the same name in an SVar
	// body. Checked after the card's own SVar table so a real SVar of the
	// same name keeps winning.
	if v, ok := runtimePublished(c, raw); ok {
		return sign * v, true
	}
	// A bare SVar name with an arithmetic suffix (Expression$ Aid/Plus.1)
	// uses the same runtime -> printed -> publication precedence as the
	// explicit SVar$ spelling below. Keep this at the parameter boundary so
	// evalCountBody's bare-head semantics remain unchanged.
	if name, op, hasOp := strings.Cut(raw, "/"); hasOp && modelledCountOp(c, op) {
		name = strings.TrimSpace(name)
		if v, ok := runtimeSVar(c, name); ok {
			return sign * applyCountOpOperand(h, c, v, op, 0), true
		}
		if body, ok := c.SVars[name]; ok {
			v, evaluated := evalCountExprOK(h, c, body, 1)
			if evaluated {
				return sign * applyCountOpOperand(h, c, v, op, 0), true
			}
		}
		if v, ok := runtimePublished(c, name); ok {
			return sign * applyCountOpOperand(h, c, v, op, 0), true
		}
	}
	// An inline Count$ expression (Storm's own Amount$ Count$ThisTurnCast/
	// Minus1, Task 17) is a body in its own right, not an SVar name -- a
	// param value of "Count$..." evaluates directly rather than being
	// mistaken for an SVar lookup (which would fail and degrade the count to
	// zero, silencing the whole SpellCopy/amount the expression was meant to
	// size). The SVar-indirection form above stays authoritative for names.
	if strings.HasPrefix(raw, "Count$") {
		return sign * EvalCount(h, c, raw), true
	}
	if strings.HasPrefix(raw, "Sacrificed$") {
		return sign * EvalCount(h, c, raw), true
	}
	if strings.HasPrefix(raw, "Remembered$") {
		// The doc above already listed this prefix; the read makes it real --
		// the corpus writes Remembered$Amount as a DIRECT parameter value on
		// the ImmediateTrigger family (TriggerAmount$ Remembered$Amount,
		// Forum Filibuster / Dain Ironfoot / Ratonhnhaké:ton; the /Op suffix
		// rides the same body, Diregraf Horde's /DivideEvenlyDown.2), not
		// behind an SVar name.
		return sign * EvalCount(h, c, raw), true
	}
	if strings.HasPrefix(raw, "TriggerCount$") || strings.HasPrefix(raw, "TriggerCountMax$") || strings.HasPrefix(raw, "ReplaceCount$") {
		return sign * EvalCount(h, c, raw), true
	}
	// A <Ref>>Count$... indirection (Unbound Flourishing's Value$
	// TriggeredSpellAbility>Count$xPaid/Twice) is a count expression in its
	// own right, not an SVar name -- the SVar lookup above would otherwise
	// miss it and degrade the whole parameter to zero. The verdict rides
	// through from evalCountExprOK, so an unknown ref (the CastSA
	// adamant-gate family's shape) stays NOT evaluated rather than a silent
	// zero.
	if _, rest, found := strings.Cut(raw, ">"); found && strings.HasPrefix(strings.TrimSpace(rest), "Count$") {
		n, ok := evalCountExprOK(h, c, raw, 0)
		return sign * n, ok
	}
	// Forge's bare P/T amount token "Double" (NumAtt$/NumDef$, the Mightform
	// Harmonizer / Wolverine / Zopandrel family) is NOT a scalar: it means the
	// affected object's OWN current characteristic added back to itself, so it
	// cannot be resolved here without naming that object. A caller that
	// genuinely has one object reads it through NumForObject; a scalar caller
	// falls through to the degrade-to-zero path below, exactly as any other
	// unmodelled body does -- never another object's stat. See NumForObject
	// for why the resolution is per-object and at resolution time.
	if raw == "X" {
		return sign * c.X, true
	}
	// A bare inline count body with no SVar name and no Count$ prefix is a
	// body in its own right: Forge writes the PlayerCount<group>$<property>
	// family as SVar bodies WITHOUT the Count$ prefix (Vampire Lacerator's
	// SVar:OpponentSmallest:PlayerCountOpponents$LowestLifeTotal), so a direct
	// parameter value of the same shape -- Tolarian Contempt's
	// TargetMax$ PlayerCountOpponents$Amount -- must resolve the way the
	// SVar-mediated form (Havoc Eater's SVar:X:PlayerCountOpponents$Amount
	// behind TargetMax$ X) resolves through the lookup above. Otherwise the
	// pfpe1 per-player bound collapses to the default 1 on one spelling and
	// not the other. A token that names no modelled head keeps the
	// degrade-to-zero path -- evalCountExprOK's verdict is exactly
	// "the head matched nothing".
	if n, ok := evalCountExprOK(h, c, raw, 0); ok {
		return sign * n, true
	}
	return 0, false
}

// NumResolvedStrict is NumResolved with the expression verdict honoured:
// where NumResolved reports a named SVar or inline Count$ expression as
// resolved whatever its body evaluates to (an effect amount's
// degrade-to-zero contract), this reports it resolved only when EvalCountOK
// understood the expression. A caller whose unresolved default differs from
// zero reads this form so an unmodelled body falls back to its default instead
// of a fake 0.
func NumResolvedStrict(h Host, c *Ctx, sa *cards.SA, key string, def int32) (int32, bool) {
	n, ok := NumResolved(h, c, sa, key, def)
	if !ok || c == nil {
		return n, ok
	}
	raw := strings.TrimSpace(sa.Params[key])
	if len(raw) > 1 && (raw[0] == '+' || raw[0] == '-') {
		raw = raw[1:]
	}
	if _, runtime := runtimeSVar(c, raw); runtime {
		return n, ok
	}
	if c.SVars != nil {
		if body, named := c.SVars[raw]; named {
			if _, evaluated := EvalCountOK(h, c, body); !evaluated {
				return def, false
			}
			return n, ok
		}
	}
	// NumResolved intentionally treats an inline Count$ body as resolved,
	// even when EvalCount's unknown-head fallback produced zero. Strict
	// callers (notably bounded number choices) must distinguish that fake
	// zero from a modelled count that legitimately returned zero.
	if strings.HasPrefix(raw, "Count$") {
		if _, evaluated := EvalCountOK(h, c, raw); !evaluated {
			return def, false
		}
	}
	return n, ok
}

// EvalCount evaluates a "Count$..." expression. The grammar in the corpus is a
// head, an optional space-separated argument, and an optional "/Op" suffix.
func EvalCount(h Host, c *Ctx, expr string) int32 {
	n, _ := evalCountExprOK(h, c, expr, 0)
	return n
}

// EvalCountOK is EvalCount plus a resolvability verdict: ok is false exactly
// when the expression's head matched NOTHING the evaluator models (the
// dispatch's fallthrough), so a caller can tell a legitimate zero (a modelled
// head that counted zero things) from "this body was never understood". The
// SVar-condition gates (effects.CheckSVarHolds) are the reason it exists: a
// gate over an unmodelled count head must fail OPEN (run anyway, the
// documented conditionMet convention) rather than enforce a meaningless zero,
// and only the dispatch itself knows which heads are modelled -- deriving the
// verdict here, at the dispatch, keeps it rot-proof: a head added to
// evalCountBody's switch automatically becomes evaluated, one deleted
// automatically stops being so.
func EvalCountOK(h Host, c *Ctx, expr string) (int32, bool) {
	return evalCountExprOK(h, c, expr, 0)
}

// maxCountDepth bounds the SVar recursion the Compare head introduces: a
// compared value or a branch may name another SVar, whose body may itself be
// a Count$Compare naming further SVars. The corpus chains are two deep
// (Nissa's Pilgrimage: X -> Y; The Biblioplex: X -> Y -> Z), so 8 is generous
// headroom against a self-referential or accidental-cycle SVar table, which
// would otherwise be the only unbounded recursion in this evaluator.
const maxCountDepth = 8

// evalCountExpr is EvalCount's body plus a recursion depth for the Compare
// head's SVar-name resolution; the public entry point always starts at 0.
func evalCountExpr(h Host, c *Ctx, expr string, depth int) int32 {
	n, _ := evalCountExprOK(h, c, expr, depth)
	return n
}

// evalCountExprOK is EvalCountOK's body plus the recursion depth; see the
// EvalCountOK doc for the verdict's meaning.
func evalCountExprOK(h Host, c *Ctx, expr string, depth int) (int32, bool) {
	if h == nil || c == nil {
		return 0, false
	}
	if depth > maxCountDepth {
		return 0, false
	}
	expr = strings.TrimSpace(expr)
	// PlayerCountRemembered$Valid carries its /Op on the bare Valid filter
	// argument in Forge SVars (Pox, Pox Plague and Fraying Omnipotence). Peel
	// that suffix before the head dispatch: evalCountBody otherwise passes it
	// to the zone matcher as part of the filter and gets an evaluated zero,
	// preventing the generic bare-expression operator fallback from running.
	// Keep this scoped to the one head and to modeled operators so player-head
	// and unknown-operator behavior remains unchanged.
	if strings.HasPrefix(expr, "PlayerCountRemembered$Valid ") {
		if body, op, hasOp := strings.Cut(expr, "/"); hasOp && modelledCountOp(c, op) {
			if n, ok := evalCountBody(h, c, strings.TrimSpace(body), depth); ok {
				return applyCountOpOperand(h, c, n, strings.TrimSpace(op), depth), true
			}
		}
	}
	// A Remembered$... expression answers a question about the objects this
	// resolving spell/ability has remembered so far -- Forge's host remembered
	// list, which never contains the event object the trigger fired on. It is
	// cut BEFORE evalRefProperty so its "Amount" head answers the
	// CAPTURE-EXCLUDED remembered set (Ctx.Remembered minus Ctx.Captured, via
	// the one-home helper rememberedExcludingCapture) rather than refTargets'
	// Remembered read (rememberedWithSource, which unions the source's
	// persistent list). A firing trigger's ctx is seeded with Remembered ==
	// Captured == its event capture, so a raw len(Ctx.Remembered) would count
	// the capture as something the resolution itself remembered -- a phase
	// trigger body with an empty remembered set would read 1.
	// The one head this build models directly is Amount -- the number of
	// remembered objects, which is Swift Silence's "Draw a card for each
	// spell countered this way" (SVar:X:Remembered$Amount after effCounter's
	// RememberCountered$ True appended every countered spell); every other
	// property delegates to the shared <Ref>$<Property> family inside
	// evalRememberedOK. The /Op suffix is applied the same way Count$ applies
	// it. An unmodelled head degrades to zero.
	if body, ok := strings.CutPrefix(expr, "Remembered$"); ok {
		return evalRememberedOK(h, c, strings.TrimSpace(body))
	}
	// A <Ref>$<Property> body answers a numeric question about the objects a
	// target reference names: Targeted$CardPower (Vein Drinker's "deals
	// damage equal to its power", Kiku's Shadow), ParentTargeted$CardPower,
	// TriggeredCard$CardPower, and their Toughness/ManaCost/CardCounters/
	// Valid siblings -- heads that used to evaluate to zero and made exactly
	// the damage amounts they sized collapse. A ref or property outside the
	// modelled family returns false and falls through to the heads below
	// (evalRemembered still owns Remembered$Amount), so every shape that was
	// zero before stays zero.
	if n, ok := evalRefProperty(h, c, expr); ok {
		return n, true
	}
	// A TargetedPlayer$/ThisTargetedPlayer$ body answers a numeric question
	// about the PLAYERS a target reference names (task tgtplayer1): the
	// object-only evalRefProperty loop skips every IsPlayer target and its
	// property switch is object-only, so these heads need their own arm --
	// placed beside that call exactly like the Remembered$/Sacrificed$ arms
	// above. The player list is the generic pre-ask's answered set
	// (PickedTargets) when non-nil, else the resolution-level Ctx.Targets --
	// the same precedence effects/context.go's Defined$ Targeted dispatch
	// takes, so a count body can never name a different player than the
	// body's own Defined$ would act on. Several player targets sum. An
	// unmodelled property or ref returns false and falls through to the
	// heads below, so every shape that was zero before stays zero.
	if n, ok := evalPlayerRefProperty(h, c, expr); ok {
		return n, true
	}
	// A <Ref>">Count$..."[/Op] indirection (Unbound Flourishing's Value$
	// TriggeredSpellAbility>Count$xPaid/Twice): the ref names the objects and
	// the right side is a Count$ expression evaluated against the FIRST
	// resolved object, bound as that object's own resolution context (Source,
	// Controller, X seeded from the object, SVars from its face) -- so
	// Count$xPaid answers the {X} the CAST paid, not the triggering
	// permanent's own. The ref switch is the same one evalRefProperty
	// dispatches through (refTargets), so the two cannot disagree; an unknown
	// ref fails closed to not-evaluated, exactly as evalRefProperty's default
	// does. The /Op suffix rides the ordinary Count$ read of the right side.
	if ref, right, found := strings.Cut(expr, ">"); found {
		if strings.HasPrefix(strings.TrimSpace(right), "Count$") {
			ts, ok := refTargets(h, c, strings.TrimSpace(ref))
			if !ok {
				return 0, false
			}
			g := h.Game()
			for _, t := range ts {
				if t.IsPlayer {
					continue
				}
				o := g.Obj(t.Obj)
				if o == nil {
					continue
				}
				sub := &Ctx{Source: o.ID, Controller: o.Controller, X: o.X}
				if f := o.Face(); f != nil {
					sub.SVars = f.SVars
				}
				return evalCountExprOK(h, sub, strings.TrimSpace(right), depth+1)
			}
			return 0, false
		}
	}
	// A Sacrificed$... expression answers "the sacrificed object's" head (CR
	// 608.2g last-known-information): power, toughness, mana value, or the
	// number of objects sacrificed. It reads the LKI snapshot captured at the
	// instant of the sacrifice (Ctx.Sacrificed), never the live object -- a
	// graveyard object has no layer-derived P/T and Move has reset its
	// counters. The /Op suffix (e.g. Sacrificed$Amount/Plus.1) is applied the
	// same way Count$ applies it.
	if body, ok := strings.CutPrefix(expr, "Sacrificed$"); ok {
		return evalSacrificedOK(c, strings.TrimSpace(body))
	}
	// A Remembered$... expression block was hoisted above evalRefProperty;
	// see its comment there for the ordering contract.
	// A TriggerCount$... expression answers a question about the event that
	// fired the trigger currently resolving -- "how much damage did that event
	// deal" (TriggerCount$DamageAmount), "how much life did it gain/lose"
	// (TriggerCount$LifeAmount), the generic event magnitude
	// (TriggerCount$Amount), or the die result a RolledDie trigger fired on
	// (TriggerCount$Result). The answer comes from the triggering event's own
	// amount, captured by rules into Ctx.TriggerAmount (or, for Result,
	// Ctx.TriggerResult) when the trigger fired
	// and carried to resolution through the per-stack-instance
	// triggerContexts map -- never from the live board, and never re-inferred
	// at resolution. A head this build does not model (ScryNum) degrades to
	// zero, exactly as it did before TriggerCount$ was recognised at all.
	if body, ok := strings.CutPrefix(expr, "TriggerCountMax$"); ok {
		return evalTriggerCountOK(c, strings.TrimSpace(body), true)
	}
	if body, ok := strings.CutPrefix(expr, "TriggerCount$"); ok {
		return evalTriggerCountOK(c, strings.TrimSpace(body), false)
	}
	// A SVar$<name>[/Op] indirection resolves another SVar on the same face
	// and applies the suffix (Herald of War-adjacent shapes:
	// SVar:Z:SVar$Y/Times.2 chains two reductions' amounts). It also resolves
	// the RollDice publications (effects/dice.go's ResultSVar$ names -> the
	// die result/total/difference, plus the chosen/other and
	// MaxRolls/EvenResults counts): a roll's value lives in the resolution's
	// publication record, not in a static SVar table, so it is consulted
	// only when the name is not an SVar of this face. An unknown name
	// degrades to zero -- the conservative no-op every unmodelled head
	// applies. The /Op suffix is applied exactly as applyCountOp does.
	if rest, ok := strings.CutPrefix(expr, "SVar$"); ok {
		name, op, hasOp := strings.Cut(rest, "/")
		n, ok3 := int32(0), false
		if v, ok2 := runtimeSVar(c, strings.TrimSpace(name)); ok2 {
			// A runtime write (api:StoreSVar) shadows the printed body of the
			// same name -- checked first, or LifePaidOnETB:Number$0 would win.
			n, ok3 = v, true
		} else if body, ok2 := c.SVars[strings.TrimSpace(name)]; ok2 {
			n, ok3 = evalCountExprOK(h, c, body, depth+1)
		} else if v, ok2 := runtimePublished(c, strings.TrimSpace(name)); ok2 {
			n, ok3 = v, true
		}
		if hasOp {
			// The op goes through applyCountOpOperand, not the numeric-only
			// applyCountOp: Forge names SVar operands here too (Alrund's
			// SVar$X/Plus.Y chains two count heads -- 86 corpus files carry
			// the shape), and the numeric-only read silently DROPPED such an
			// operand (the unknown-op fallthrough keeps the base value). A
			// non-numeric, non-SVar operand still falls through to
			// applyCountOp unchanged.
			n = applyCountOpOperand(h, c, n, op, depth)
		}
		return n, ok3
	}
	// ReplaceCount$ reads the event currently being replaced. Damage
	// replacement bodies use both the bare DamageAmount form (Vigor, Purity,
	// Hostility) and arithmetic suffixes (Fiery Emancipation, Angel of
	// Suffering). Rules carries the amount in Ctx so every supported body API,
	// not only ReplaceEffect itself, sees the same in-flight value.
	if body, ok := strings.CutPrefix(expr, "ReplaceCount$"); ok {
		field, op, hasOp := strings.Cut(strings.TrimSpace(body), "/")
		// "Number" is Forge's DrawCards-replacement spelling of the same
		// in-flight amount (Quantum Riddler's NumCards$
		// ReplaceCount$Number/Plus.1 body; 8 corpus files carry the field).
		// "CounterNum" is the AddCounter class's spelling (Hardened Scales'
		// X:ReplaceCount$CounterNum/Plus.1, Branching Evolution's /Twice): the
		// number of counters the held CounterChange would place.
		if field != "DamageAmount" && field != "Amount" && field != "Number" && field != "CounterNum" {
			return 0, false
		}
		n := c.ReplacementAmount
		if hasOp {
			// Resolve named operands consistently with the SVar$ head above.
			// Runtime-published roll values are resolved on the live
			// replacement path by resolveCountOperand's bare-word arm.
			n = applyCountOpOperand(h, c, n, op, depth)
		}
		return n, true
	}
	body, ok := strings.CutPrefix(expr, "Count$")
	if !ok {
		if n, err := strconv.Atoi(expr); err == nil {
			return int32(n), true
		}
		// Forge's literal "Number$<int>" SVar body -- the "this is just the
		// number" spelling (Vraska, Betrayal's Sting's
		// SVar:Difference:Number$9/Minus.X, Kokusho's Number$7/Minus.Y,
		// Krang's Number$4/Minus.X, and the bookkeeping Number$0 bodies)
		// evaluates the literal, with the shared /Op suffix resolved through
		// the same operand grammar the Count$ branch applies (so
		// Number$9/Minus.X reads 9 minus the X SVar, the differential the
		// [-9] ultimates print). A non-integer body fails closed to
		// (0, false) -- the verdict every Number$ body had before this arm
		// existed, so no new shape silently changes direction.
		if rest, ok2 := strings.CutPrefix(expr, "Number$"); ok2 {
			lit, op, hasOp := strings.Cut(strings.TrimSpace(rest), "/")
			n, err := strconv.Atoi(strings.TrimSpace(lit))
			if err != nil {
				return 0, false
			}
			if hasOp {
				v, ok := applyCountOpOperandOK(h, c, int32(n), op, depth)
				return v, ok
			}
			return int32(n), true
		}
		// Forge's PlayerCount SVar bodies omit the Count$ prefix
		// (SVar:OpponentSmallest:PlayerCountOpponents$LowestLifeTotal --
		// Vampire Lacerator's upkeep gate): run the head dispatch on the raw
		// body before giving up. An unrecognised bare word still falls
		// through, not evaluated.
		if n, ok2 := evalCountBody(h, c, strings.TrimSpace(expr), depth); ok2 {
			return n, true
		}
		// The same bare body with the shared /Op suffix (Avacyn's Judgment's
		// SVar:MaxTgts:PlayerCountPlayers$Amount/Plus.MaxPermanents): the
		// Count$ branch below cuts the suffix before the head dispatch, and
		// a prefix-less body must too, or the whole bound reads as an
		// unmodelled zero -- a TargetMax$ that silently forbids targeting.
		// Tried only after the whole body missed, so a head whose argument
		// legitimately carries a slash keeps its reading, and only for an
		// operator this evaluator models (a literal arithmetic op, or a
		// Plus/Minus/Times operand naming one of this face's SVars) -- an
		// unmodelled operator stays unresolved rather than silently
		// returning the bare head's value.
		if head, op, hasOp := strings.Cut(strings.TrimSpace(expr), "/"); hasOp && modelledCountOp(c, op) {
			if n, ok2 := evalCountBody(h, c, strings.TrimSpace(head), depth); ok2 {
				return applyCountOpOperand(h, c, n, op, depth), true
			}
		}
		// A bare SVar-name body (Spark Fiend's StoreSVar Expression$ Result)
		// resolves a DB$ RollDice publication of this same resolution -- the
		// same name the SVar$ indirection resolves above, in the one shape a
		// corpus body carries a bare runtime name. Anything else is
		// unrecognised: zero, and NOT evaluated.
		if v, ok := runtimePublished(c, strings.TrimSpace(expr)); ok {
			return v, true
		}
		return 0, false
	}
	body, op, hasOp := strings.Cut(body, "/")
	if hasOp && strings.TrimSpace(body) == "Convoked$Amount" && !validConvokedCountOp(op) {
		return 0, false
	}
	n, ok2 := evalCountBody(h, c, strings.TrimSpace(body), depth)
	if hasOp {
		if clamped, isLimit := countDistinctLimitMax(strings.TrimSpace(body), op, n); isLimit {
			n = clamped
		} else {
			n = applyCountOpOperand(h, c, n, op, depth)
		}
	}
	return n, ok2
}

// applyCountOpOperand applies a Count$ arithmetic suffix. Besides numeric
// operands such as Plus.1, Forge uses SVar names (for example
// Plus.DragonControlled). Resolve those names in the current face's SVar
// table before applying the existing saturating arithmetic.
func applyCountOpOperand(h Host, c *Ctx, n int32, op string, depth int) int32 {
	v, ok := applyCountOpOperandOK(h, c, n, op, depth)
	if !ok {
		return applyCountOp(n, op)
	}
	return v
}

// applyCountOpOperandOK also reports whether op belongs to the SVar-operand
// subset this helper implements. Count$ keeps its historical no-op result for
// other suffixes through applyCountOpOperand, while Number$ literals use the
// verdict to fail closed instead of treating an unknown suffix as their base
// literal (for example Mathemagics' unsupported Number$2/Pow.X).
func applyCountOpOperandOK(h Host, c *Ctx, n int32, op string, depth int) (int32, bool) {
	for _, prefix := range []string{"Plus.", "Minus.", "Times."} {
		operand, ok := strings.CutPrefix(op, prefix)
		if !ok {
			continue
		}
		operand = strings.TrimSpace(operand)
		if _, err := strconv.Atoi(operand); err == nil {
			return applyCountOp(n, op), true
		}
		if strings.HasPrefix(operand, "Count$") {
			value, resolved := evalCountExprOK(h, c, operand, depth+1)
			if resolved {
				return applyCountOp(n, prefix+strconv.FormatInt(int64(value), 10)), true
			}
		}
		if c != nil && c.SVars != nil {
			if body, exists := c.SVars[operand]; exists {
				value, _ := evalCountExprOK(h, c, body, depth+1)
				return applyCountOp(n, prefix+strconv.FormatInt(int64(value), 10)), true
			}
		}
		return applyCountOp(n, op), true
	}
	return 0, false
}

// countDistinctLimitMax answers whether op is a LimitMax.<n> clamp on a
// Count$Valid/ValidZone body whose property is one of the bounded
// distinct-set reads -- Colors's one corpus op suffix (happily_ever_after's
// Permanent.YouCtrl$Colors/LimitMax.5) and CreatureType's two (Valiant
// Changeling's /LimitMax.5, Saavik's /LimitMax.10). It is called from
// evalCountExprOK's generic /Op site, which cuts the suffix off the whole
// body BEFORE the head dispatch, so the countZone branch never sees it;
// keeping the clamp scoped to the bounded distinct-set properties (the
// summed properties carry no op in the corpus and keep the plain
// applyCountOp read, where an unknown op name is ignored and the base value
// stands) means an unclamped spelling cannot silently lose its cap.
func countDistinctLimitMax(body, op string, n int32) (int32, bool) {
	head, arg, _ := strings.Cut(body, " ")
	if _, ok := countZone(head); !ok && head != "ValidSelf" {
		return n, false
	}
	_, prop, hasProp := strings.Cut(strings.TrimSpace(arg), "$")
	if !hasProp {
		return n, false
	}
	switch strings.TrimSpace(prop) {
	case "Colors", "CreatureType":
	default:
		return n, false
	}
	lim, ok := strings.CutPrefix(op, "LimitMax.")
	if !ok {
		return n, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(lim))
	if err != nil || v < 0 {
		return n, false
	}
	if n > int32(v) {
		return int32(v), true
	}
	return n, true
}

// countMostCardName implements Forge's Count$MostCardName <spec>: the
// greatest number of objects matching <spec> that share one face name, which
// is the oracle's "N or more <things> with the same name as one another"
// (Mechanized Production, Endless Atlas, Chrome Replicator, Sceptre of
// Eternal Glory -- all four corpus carriers name a battlefield spec). The
// scan is over every alive seat's battlefield, matched through the same
// zone-aware filter the Count$Valid family uses (matchesZoneSpecCtx), so
// `Artifact.YouCtrl` / `Land.YouCtrl` / `Permanent.nonLand+!token+YouCtrl`
// read exactly as they do under Count$Valid. The per-name counts live in a
// local map read only by key plus a running maximum, so no map iteration
// order ever reaches an event or a view. A spec this build cannot match
// (an unknown predicate fails closed inside the matcher) counts zero of every
// name and so returns 0, the conservative no-op every unmodelled filter
// takes. An empty spec is NOT evaluated -- the caller's fail-open convention
// for an unreadable head applies instead of a meaningless zero.
//
// The name read is the printed face's Name (o.Face().Name), matching the
// sibling distinct-name read at the zone-count site (the
// token$DifferentCardNames set) rather than the layer-3 rename table; the
// corpus shape is four same-named printed permanents, so the two agree. The
// zone is the battlefield only, which is what Count$MostCardName's Valid
// semantics mean in Forge and what all four corpus specs name.
func countMostCardName(h Host, c *Ctx, spec string) (int32, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, false
	}
	g := h.Game()
	specCtx := c.SpecContext(c.Controller)
	counts := make(map[string]int32)
	var best int32
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if !matchesZoneSpecCtx(g, spec, id, specCtx, state.ZBattlefield) {
				continue
			}
			o := g.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			name := o.Face().Name
			counts[name]++
			if counts[name] > best {
				best = counts[name]
			}
		}
	}
	return best, true
}

// evalTriggerCount resolves a "TriggerCount$<Head>[/Op]" body against the
// triggering event's magnitude (Ctx.TriggerAmount). The heads this build
// models -- DamageAmount (damage the event dealt), LifeAmount (life it
// gained/lost) and Amount (the generic event magnitude) -- all answer the
// same number, because each is the single amount the causing event carried;
// the distinction between them is only in which trigger mode populates it
// (and, for the corpus, that the LifeGained trigger mode is not yet
// registered, so a LifeAmount head is unreachable today). Result is the
// RolledDie head: the die result the trigger fired on (Ctx.TriggerResult,
// captured at fire time -- Mr. House's BranchConditionSVar$ reads it after
// the RollDice resolution that produced it has finished). The /Op suffix is
// applied exactly as applyCountOp does for Count$ and Sacrificed$. An
// unmodelled head (ScryNum) degrades to zero. max selects the
// TriggerCountMax$ prefix's reading: the same heads, but Result answers the
// highest result in the roll batch (Ctx.TriggerResultMax -- Farideh's "if any
// of those results was 10 or higher") rather than the batch's reported result.
//
// ScryBottom is the completed Scry's own magnitude: events.Scry.Amount, the
// count the player actually chose to put on the bottom (rules captures it
// into TriggerAmount, the same fire-time binding every other head reads).
// The Temporal Anchor's `SVar:X:TriggerCount$ScryBottom` sizes its bottom
// exile through it. ScryNum -- the number LOOKED at -- remains unmodelled:
// this build raises no scry marker carrying that number.
//
// Result is an EVALUATED head (verdict true) on every trigger, not only a
// roll trigger: before RolledDie was registered it reported (0, false), so a
// CheckSVar$ gate over it failed open; now a non-roll trigger reads 0 and the
// gate is enforced. Measured: all 8 corpus files carrying TriggerCount$Result
// (`/usr/bin/grep -rlE 'TriggerCount\$Result' .cards/cardsfolder`) sit on
// Mode$ RolledDie/RolledDieOnce triggers, where Ctx.TriggerResult is set, so
// no corpus gate changes direction.
func evalTriggerCountOK(c *Ctx, body string, max bool) (int32, bool) {
	body, op, hasOp := strings.Cut(body, "/")
	var n int32
	switch strings.TrimSpace(body) {
	case "DamageAmount", "LifeAmount", "Amount", "ScryBottom":
		// ScryBottom is the events.Scry marker's Amount (the number put on
		// the bottom), captured into TriggerAmount by rules for Mode$ Scry.
		n = c.TriggerAmount
	case "Result":
		if max {
			n = c.TriggerResultMax
		} else {
			n = c.TriggerResult
		}
	default:
		// ScryNum (the number LOOKED at) is a head whose triggering number
		// this build does not carry, so it stays zero -- the same
		// conservative no-op as before the prefix was recognised. NOT
		// evaluated: a gate over it fails open.
		return 0, false
	}
	if hasOp {
		n = applyCountOp(n, op)
	}
	return n, true
}

// evalSacrificed resolves a "Sacrificed$<Property>[/Op]" body against the
// LKI snapshots this resolving spell/ability captured when it sacrificed
// each object (Ctx.Sacrificed). Property is the corpus head after the
// Sacrificed$ prefix: CardPower, CardToughness, CardManaCost, or Amount (the
// count of objects sacrificed). For a numeric property over more than one
// sacrificed object the values are summed -- corpus Sac costs are almost
// always exactly one candidate, so summing agrees with the single-value
// answer and is the least surprising reading of "the sacrificed creature's
// power" when a shape somehow names several. An empty Sacrificed list
// (nothing captured) degrades to zero rather than panicking, preserving the
// "the card did nothing" totality convention of every other head here. The
// /Op suffix (Plus/Minus/Times./Twice/HalfDown/HalfUp/Negative) is applied
// after the base value, exactly as applyCountOp does for Count$.
func evalSacrificedOK(c *Ctx, body string) (int32, bool) {
	body, op, hasOp := strings.Cut(body, "/")
	var n int32
	switch strings.TrimSpace(body) {
	case "CardPower":
		n = sacrificedNumeric(c, func(s state.SacrificedInfo) int32 { return s.Power })
	case "CardToughness":
		n = sacrificedNumeric(c, func(s state.SacrificedInfo) int32 { return s.Toughness })
	case "CardManaCost":
		n = sacrificedNumeric(c, func(s state.SacrificedInfo) int32 { return s.ManaValue })
	case "Amount":
		n = int32(len(c.Sacrificed))
	default:
		// An out-of-scope head (Valid, CardTypes, ChromaSource, CardNumColors,
		// CardCounters) degrades to zero, exactly as before the fix -- the
		// conservative same-as-before no-op the brief scopes out. NOT
		// evaluated: a gate over one of these fails open.
		return 0, false
	}
	if hasOp {
		n = applyCountOp(n, op)
	}
	return n, true
}

// sacrificedNumeric folds a numeric property across every object this
// spell/ability sacrificed, summing (see evalSacrificed's doc for why sum and
// not first).
func sacrificedNumeric(c *Ctx, f func(state.SacrificedInfo) int32) int32 {
	var n int32
	for _, s := range c.Sacrificed {
		n += f(s)
	}
	return n
}

func evalRememberedOK(h Host, c *Ctx, body string) (int32, bool) {
	body, op, hasOp := strings.Cut(body, "/")
	switch strings.TrimSpace(body) {
	case "Amount":
		n := int32(len(rememberedExcludingCapture(h, c)))
		if hasOp {
			n = applyCountOp(n, op)
		}
		return n, true
	}
	// A Remembered$CardPower / CardToughness / CardManaCost / CardCounters.
	// / Valid body is the shared <Ref>$<Property> family (evalRefProperty);
	// evalCountExpr routes it here first only because the Remembered$
	// prefix cut wins. An unmodelled property still degrades to zero, the
	// same conservative no-op evalSacrificed's default takes, and the
	// property verdict rides through: an unmodelled one is NOT evaluated.
	if n, ok := evalRefProperty(h, c, "Remembered$"+body); ok {
		if hasOp {
			n = applyCountOp(n, op)
		}
		return n, true
	}
	return 0, false
}

// rememberedExcludingCapture is the ctx's Remembered list minus its
// fire-time event capture (Ctx.Captured) -- Forge's host remembered list,
// which never contains the event object the trigger fired on. The exclusion
// lives in ONE helper every plain-Remembered reader goes through:
// effImmediateTrigger (which builds each "when you do" instance's ctx from
// it, so its exclusion and the TriggerRemembered count head's cannot drift),
// refTargets' TriggerRemembered case (Loamcrafter Faun's SVar:X:
// TriggerRemembered$Amount), evalRememberedOK's Amount head,
// evalCountExprOK's RememberedNumber head, rememberedWithSource (the plain
// Remembered$ group every Valid/condition reader resolves through),
// rememberedLKIGroup (the RememberedLKI ref group) and evalRefProperty's
// Remembered$<Property> heads.
// A no-capture ctx (captured empty) returns the list unchanged; the helper
// is idempotent -- the instance ctx effImmediateTrigger builds has Captured
// and Remembered disjoint, so applying it a second time there answers the
// same set.
func rememberedExcludingCapture(h Host, c *Ctx) []state.Target {
	if len(c.Captured) == 0 {
		return c.Remembered
	}
	// Remove only the seeded occurrence(s), not every equal target. A body
	// can explicitly remember the captured object again (e.g. RememberSacrificed
	// on a death trigger); that later occurrence is real memory even though its
	// identity equals the capture.
	remaining := make(map[state.Target]int, len(c.Captured))
	for _, t := range c.Captured {
		remaining[t]++
	}
	var out []state.Target
	for _, t := range c.Remembered {
		if remaining[t] > 0 {
			remaining[t]--
			continue
		}
		out = append(out, t)
	}
	return out
}

// rememberedLKIGroup resolves the RememberedLKI ref group: the SAME
// remembered set the plain-Remembered resolver (rememberedWithSource)
// answers -- Forge's host remembered list is the source card's persistent
// event-backed list UNIONED with the resolution's own ctx memory -- with ONE
// deviation: an object that IS the resolving source is kept when the
// resolution explicitly remembered it. rememberedWithSource drops
// c.Source unconditionally, which was this build's stand-in for keeping the
// fire-time capture out of a plain Remembered read; for RememberedLKI the
// capture-excluding helper above already removes exactly the seeded capture
// occurrences, so the blanket exclusion only ever deleted a REAL memory of
// the source -- Cosima's DBReturn (Defined$ Self RememberLKI$ True: the
// exiled god returns to the battlefield and its own X reads
// RememberedLKI$CardCounters.VOYAGE off itself) and Riders of the Mark
// (RememberChanged$ True on a Defined$ Self move, its toughness read
// RememberedLKI$CardToughness) both went to zero under it. The union is
// deduped by id (the same object may sit in both halves -- an explicit
// RememberChanged$/RememberLKI$ append after a capture seed, or a rider that
// eventRemembered into the persistent list while the ctx list already carried
// it), and the helper is idempotent: a ctx whose capture is already excluded
// answers the same set again.
func rememberedLKIGroup(h Host, c *Ctx) []state.Target {
	out := make([]state.Target, 0, len(c.Remembered))
	seen := make(map[state.ObjID]bool, len(c.Remembered))
	if o := h.Game().Obj(c.Source); o != nil {
		for _, t := range o.Remembered {
			if !t.IsPlayer && !seen[t.Obj] {
				seen[t.Obj] = true
				out = append(out, t)
			}
		}
	}
	for _, t := range rememberedExcludingCapture(h, c) {
		if t.IsPlayer {
			out = append(out, t)
			continue
		}
		if !seen[t.Obj] {
			seen[t.Obj] = true
			out = append(out, t)
		}
	}
	return out
}

// refTargets resolves one ref name of the <Ref>$<Property> family into the
// targets it names. Shared by evalRefProperty and the <Ref>>Count$...>
// indirection branch in evalCountExprOK, so the two cannot disagree about
// which refs exist. An unknown ref returns false -- the caller fails closed,
// exactly as evalRefProperty's default always did.
func refTargets(h Host, c *Ctx, ref string) ([]state.Target, bool) {
	if inner, ok := strings.CutPrefix(ref, "Spawner>"); ok {
		// Forge's adjustTriggerContext (AbilityUtils): "Spawner>" re-anchors
		// the rest of the chain on the resolving ability's TRIGGER's spawning
		// ability. This build's stand-in for that context is the firing
		// trigger's own event capture (Ctx.Captured), which a chained
		// ImmediateTrigger's Execute context no longer carries as Remembered
		// (effImmediateTrigger hands its instances the capture-excluded parent
		// set). Halana, Kessig Ranger's immediate-trigger chain is the corpus
		// user (X:Spawner>TriggeredCard$CardPower sizes its DamageSource$
		// Spawner>... hit); an inner ref this grammar does not know still
		// fails closed here exactly as it did before the arm existed.
		sc := *c
		sc.Remembered = copyTargets(c.Captured)
		// The substitution consumes the capture into Remembered, so a nested
		// ref that excludes Ctx.Captured (TriggerRemembered) must not be
		// handed the old capture again and subtract it away. No corpus line
		// writes Spawner>TriggerRemembered; this keeps the composition correct
		// structurally (the count head's one-home helper).
		sc.Captured = nil
		return refTargets(h, &sc, strings.TrimSpace(inner))
	}
	switch ref {
	case "Targeted", "ParentTarget", "ParentTargeted", "ThisTargetedCard":
		return c.Targets, true
	case "AllTargeted":
		// AllTargeted (task alltargeted1) is Forge's UNION of every targeting
		// SA's targets down the root ability's sub-ability chain. The cast
		// flow pre-asks the whole chain's targets before payment (CR 601.2c)
		// and threads the union through Ctx.AllTargets at the cost-evaluation
		// sites that read it (ownReduceCost's reprice, the CollectEvidence
		// amount resolution); a caller that did not bind one -- a resolution-
		// time read, where no corpus line carries the ref -- keeps the
		// faithful-as-available Ctx.Targets binding, which for a chain with
		// no sub targets IS the union.
		if c.AllTargets != nil {
			return c.AllTargets, true
		}
		return c.Targets, true
	case "TriggeredCard", "TriggeredCardLKICopy", "TriggeredNewCard",
		"TriggeredNewCardLKICopy",
		"TriggeredAttacker", "TriggeredAttackerLKICopy",
		"TriggeredTargetLKICopy", "DelayTriggerRemembered",
		"DelayTriggerRememberedLKI":
		return c.Remembered, true
	case "RememberedLKI":
		// The LKI spelling of the Remembered$ group names the SAME objects
		// (Forge's remembered list, which never contains the event object the
		// trigger fired on); only the characteristic read differs, through
		// Ctx.LKI's pre-move snapshot. Route it through the one RememberedLKI
		// resolver so the two cannot disagree about WHICH objects are
		// remembered -- a raw Ctx.Remembered read summed a triggered
		// Destroy/ChangeZone's fire-time capture (the trigger's own source)
		// into a chained RememberedLKI$CardToughness (Noxious Gearhulk,
		// Rotfeaster Maggot), inflating the read by the source's own
		// characteristic, while the plain resolver's blanket source exclusion
		// dropped an object that IS the source and was genuinely remembered
		// (Cosima's self-return RememberLKI$ read, Riders of the Mark's
		// RememberChanged$ one). The capture-occurrence exclusion is precise;
		// the source itself is kept.
		return rememberedLKIGroup(h, c), true
	case "TriggerRemembered":
		// TriggerRemembered (task triggerremembered1) is Forge's name for the
		// trigger's own RememberObjects$ capture -- the set the resolving
		// body introspects ("return up to that many"). It is NOT the whole
		// ctx list: rules seeds a firing trigger's ctx with Remembered =
		// Captured = the trigger's own event capture, and effImmediateTrigger
		// hands each "when you do" instance the capture-EXCLUDED parent set.
		// Bind the exclusion here, at the one place the ref resolves, so a
		// read that happens at either level -- the ImmediateTrigger's own ctx
		// (Loamcrafter Faun's TriggerAmount$/a direct Execute read) or an
		// instance ctx -- answers the chain's remembered set and never the
		// fire-time capture. A plain Ctx.Remembered read (what the sibling
		// ticket first landed) overcounts by that capture in the outer ctx;
		// the helper is idempotent with effImmediateTrigger's own exclusion
		// (the instance capture is disjoint from its remembered set), so the
		// instance read is unchanged. The members the corpus writes (Amount/
		// CardPower/CardToughness/CardManaCost/CardManaCostLKI/
		// CardCounters.*) read through the one property switch below.
		return rememberedExcludingCapture(h, c), true
	case "TriggeredExploited":
		// The exploited creature (CR 702.58c's "that creature"): the Exploit
		// marker's triggerReferents case binds ev.IDs[0] to TriggerCard at
		// fire time, so Henry Wu's TriggeredExploited$CardPower and Profaner
		// of the Dead's TriggeredExploited$CardToughness read exactly the
		// sacrificed creature. evalRefProperty then reads its LKI P/T from
		// Ctx.LKIPower/LKIToughness, which rules' attachExploitedLKI sets from
		// the as-sacrificed snapshot effects/exploit.go publishes (CR 608.2g):
		// the bare graveyard card would carry only its printed face, losing a
		// +1/+1 counter or a pump the creature had when it was sacrificed. The
		// role-absent fallback keeps the old Remembered read for a hand-built
		// context (the TriggeredBlocker precedent).
		if c.TriggerCard != 0 {
			return []state.Target{{Obj: c.TriggerCard}}, true
		}
		return c.Remembered, true
	case "TriggeredBlocker", "TriggeredBlockerLKICopy":
		// The pair's BLOCKER (trig:Blocks): prefer the fire-time TriggerBlocker
		// role when the Blocks capture set it (Remembered names the attacker
		// there); the role-absent fallback keeps the old Remembered read --
		// the AttackerBlockedByCreature queue entries and hand-built contexts,
		// whose Remembered IS the blocker. This mirrors the shared case in
		// effects/context.go's knownDefinedTargets so the two resolvers cannot
		// disagree about one spelling.
		if c.TriggerBlocker != 0 {
			return []state.Target{{Obj: c.TriggerBlocker}}, true
		}
		return c.Remembered, true
	case "TriggeredSpellAbility":
		// The activation arm (abcopy1): the fire-time TriggerAbility role is
		// the exact referent (Remembered names the source permanent); the
		// spell-cast arm and hand-built contexts keep the Remembered entry.
		if c.TriggerAbility != 0 {
			return []state.Target{{Obj: c.TriggerAbility}}, true
		}
		return c.Remembered, true
	case "CastSA":
		// The cast spell ability (Graven Archfiend's ETB gate
		// "CastSA>Count$OptionalGenericCostPaid.1.0"): the cast spell's own
		// object. For the corpus shape -- an ETB trigger of the permanent the
		// cast spell became -- the ctx source IS that object (the
		// stack->battlefield move preserves the id, and the pay-time CastInfo
		// folded the paid provenance onto it), so binding the ctx source is
		// exactly the binding the indirection needs; a copy of the spell is a
		// distinct object and reads its own (unpaid) provenance.
		//
		// CastSA names THIS source's own cast. When the trigger context names
		// a DIFFERENT cast spell (a SpellCast trigger firing on another card's
		// cast), that referent is TriggeredSpellAbility, not CastSA -- this
		// source was not the card being cast, so the ref is unbound. Every
		// corpus CastSA carrier is self-referential (SpellCast ValidCard$
		// Card.Self, a self ChangesZone ETB, or a bare CheckSVar$/replacement
		// ctx with no trigger referent), so no real shape regresses; the
		// alternative reading silently bound an unrelated cast spell's X.
		if c.TriggerCard != 0 && c.TriggerCard != c.Source {
			return nil, false
		}
		if c.Source != 0 {
			return []state.Target{{Obj: c.Source}}, true
		}
		return nil, false
	case "Remembered":
		// Forge's plain Remembered$ form reads the executing ability's shared
		// host-card remembered list: the ctx walk's set UNIONED with the
		// source's persistent event-backed list (rememberedWithSource).
		return rememberedWithSource(h, c), true
	case "Imprinted":
		// The imprint reference the <Ref>$<Property> family reads RAW: the
		// RepeatEach subject first (the same precedence definedSpec's Imprinted
		// case takes -- the loop's CURRENT subject, Master of the Wild Hunt's
		// X:Imprinted$CardPower), else the source's imprint associations raw
		// (Forge's getImprintedCards has no zone gate; the damage-source and
		// count consumers are not definedSpec's CR 607.2a exile-gated Defined$
		// readers -- rawImprintTargets carries the rationale). An empty pile is
		// a legitimate zero, not an unresolvable body.
		if c.RepeatSubject.Obj != 0 && !c.RepeatSubject.IsPlayer {
			return []state.Target{{Obj: c.RepeatSubject.Obj}}, true
		}
		return rawImprintTargets(h.Game(), c), true
	case "ChosenCard":
		// The chosen-card read resolutionChosenCards already serves definedSpec's
		// ChosenCard case -- the same shared read here keeps a count body from
		// disagreeing with a Defined$ ChosenCard (Crush Underfoot's
		// X:ChosenCard$CardPower sizes its DamageSource$ ChosenCard hit).
		return resolutionChosenCards(h.Game(), c), true
	case "ExiledWith":
		// The same defined-targets resolver case effects/context.go's
		// knownDefinedTargets carries, so a count body over the ref (the
		// CheckSVar gate SVar:X:ExiledWith$Amount of Colfenor's Urn,
		// Veteran Survivor and River Song's Diary) evaluates against the
		// same set the body's own Defined$ ExiledWith resolves. Without it
		// the ref fails closed and the gate is never evaluated, so those
		// triggers stay silent even when the exile count meets the gate.
		var out []state.Target
		g := h.Game()
		for _, id := range g.Zone(state.ZExile, c.Controller) {
			if o := g.Obj(id); o != nil && o.ExiledWith == c.Source {
				out = append(out, state.Target{Obj: id})
			}
		}
		return out, true
	case "Exiled", "Revealed":
		// Forge's cast-cost PAID lists (AbilityUtils.getPaidCards ->
		// SpellAbility.getPaidList): the cards this cast's own cost removed --
		// CostExile's row is keyed "Exiled" (HashLKIListKey),
		// CostReveal.doPayment's is "Revealed". The cards ride Ctx.Exiled /
		// Ctx.Revealed, bound from the engine's per-stack-object capture at
		// payment, so a body such as Draconic Intervention's
		// `SVar:X:Exiled$CardManaCost` reads the card the ExileFromGrave cost
		// actually exiled rather than the source or a remembered set. An
		// absent binding is a legitimate EMPTY list (ok=true, zero), never a
		// fallback -- exactly the ExiledWith association's empty case above.
		// This is deliberately NOT Object.ExiledWith: an ExileFromGrave cost
		// emits a plain MoveZone with no ExiledWith marker, so aliasing the
		// association would read zero for the real carriers. paidCostTargets
		// is the shared home with definedSpec's own case.
		return paidCostTargets(c, ref), true
	case "ExiledCards":
		// Forge's `ExiledCards` count referent (Corpseweft's
		// `SVar:Y:ExiledCards$Amount/Twice` -- the only corpus carrier at this
		// pin): the cards THIS cast or activation exiled as a cost, i.e. the
		// SAME paid list the `Exiled` spelling immediately above reads. It is
		// claimed here explicitly because the default fallback below cannot
		// model it -- definedSpec carries no `ExiledCards` selector -- so the
		// body would fail closed and Corpseweft's Zombie Horror would be minted
		// at the dynamic side's zero and swept by state-based actions. It is
		// deliberately NOT Object.ExiledCards (a ChangeZone zone association on
		// the exiling object) nor Ctx.Remembered (the memory/captured trigger
		// objects `TokenRemembered$ ExiledCards` reads): neither holds the paid
		// list an ExileFromGrave cost fills. Paired with evalRefProperty's
		// `Amount` property, this sizes the token; the corpus's `/Twice` op
		// rides the ordinary applyCountOp suffix.
		return paidCostTargets(c, "Exiled"), true
	case "Equipped", "Enchanted", "AttachedTo":
		// The object the source is attached to (Glamdring's "where X is
		// equipped creature's power", Equipped$CardPower). Claimed here rather
		// than through the defined-targets fallback below, which refuses an
		// empty set: an unattached Equipment names NO creature, and "its
		// power" is then a legitimate zero, not an unreadable body.
		g := h.Game()
		if o := g.Obj(c.Source); o != nil && o.AttachedTo != 0 && g.Obj(o.AttachedTo) != nil {
			return []state.Target{{Obj: o.AttachedTo}}, true
		}
		return nil, true
	case "TargetedObjects", "TargetedObjectsDistinct":
		// Forge's TargetedObjects referent (AbilityUtils.calcX's
		// `calcX[0].startsWith("TargetedObjects")` arm): the UNION of every
		// targeting SA's chosen targets down the root ability's sub-ability
		// chain. Forge's Amount includes players; object properties below skip
		// player entries. The union
		// is the same set Ctx.AllTargets carries when the cast pre-ask bound
		// it (the AllTargeted ref's own source), else the resolution's own
		// Ctx.Targets -- which for a chain with no sub targets IS the union.
		// Builders' Bane's "Destroy X target artifacts", Fireball's and
		// Firestorm's TargetMin$/Max$ and Choking Vines' X are the corpus
		// users. "Distinct" (TargetedObjectsDistinct, Officious
		// Interrogation's IncreaseCost$) de-duplicates by full target identity
		// in first-seen order -- Forge's `new ArrayList<>(new HashSet<>(objects))`.
		var out []state.Target
		seen := map[state.Target]bool{}
		for _, t := range refTargetUnion(c) {
			if t.Obj == 0 && !t.IsPlayer {
				continue
			}
			if ref == "TargetedObjectsDistinct" {
				if seen[t] {
					continue
				}
				seen[t] = true
			}
			out = append(out, t)
		}
		return out, true
	case "SpellTargeted":
		// Forge's SpellTargeted names the targeted SPELL -- the target that is
		// a spell on the stack (AbilityUtils.calcX's
		// `calcX[0].equals("SpellTargeted")` arm, which reads the FIRST of
		// `getDefinedSpellAbilities(card, "SpellTargeted", sa)` and counts its
		// host card). This build's target list already holds that spell's
		// object id (the cast pre-ask stored the target), so bind the first
		// OBJECT target; a chain with no object target yields the empty set
		// (ok=true, a legitimate zero). Reject Imperfection's, Press the
		// Enemy's, Gale's Redirection's and Sound the Trumpets'
		// SpellTargeted$CardManaCostLKI are the four corpus users. The mana
		// value is a printed face characteristic, so it survives the
		// counter/exile that follows the read and the LKI spelling reads the
		// same face value the plain spelling does (the shared CardManaCost/
		// CardManaCostLKI property arm below).
		//
		// The spell identity is the RESOLUTION-START snapshot (Ctx.TargetSpellLKI,
		// captured by effects.Resolve before any effect can move a target), not
		// the live zone: a Counter or ChangeZone earlier in the same chain has
		// already moved the spell off the stack when the later sub-ability reads
		// this ref (Reject Imperfection's DBProliferate gate, Gale's
		// Redirection's DBRoll modifier, Press the Enemy's DBMayPlay filter), so
		// a live ZStack test would lose exactly the spell the ref names. A
		// hand-built Ctx with no snapshot falls back to the live zone, which is
		// correct for a directly-evaluated body and never admits a battlefield
		// permanent (its object was never on the stack at entry).
		for _, t := range refTargetUnion(c) {
			if t.IsPlayer || t.Obj == 0 {
				continue
			}
			if c.TargetSpellLKI[t.Obj] {
				return []state.Target{t}, true
			}
			if o := h.Game().Obj(t.Obj); o == nil || o.Zone != state.ZStack {
				continue
			}
			return []state.Target{t}, true
		}
		return nil, true
	default:
		// A ref this build's explicit cases do not name is delegated to the
		// SAME defined-targets resolver a body's own Defined$ spelling uses
		// (effects/context.go knownDefinedTargets), so the count vocabulary can
		// no longer lag the defined-targets vocabulary and the next sibling
		// spelling is covered without a new hand-written case. The explicit
		// cases ABOVE keep the deliberate count-only distinctions (the RAW
		// Imprinted associations, ChosenCard, the ExiledWith scan) ahead of this
		// fallback.
		//
		// The delegation claims the ref only when the resolved set carries at
		// least one OBJECT: a player-valued ref (TargetedPlayer, TriggeredTarget,
		// Player.IsRemembered, ...) belongs to the player arm
		// (evalPlayerRefProperty), and claiming it here with zero objects would
		// short-circuit that arm and read every player property as 0. An empty
		// or player-only resolution therefore still fails closed, exactly as
		// before the fallback existed -- an unknown ref too, since
		// knownDefinedTargets refuses a selector it does not model.
		if ts, ok := knownDefinedTargets(h, c, ref); ok {
			for _, t := range ts {
				if !t.IsPlayer && t.Obj != 0 {
					return ts, true
				}
			}
		}
		return nil, false
	}
}

// refTargetUnion is the target set Forge's chain-union refs read: every
// targeting SA's chosen targets down the root ability's sub-ability chain.
// Ctx.AllTargets carries that union when the cast pre-ask bound it (the
// AllTargeted ref's own binding); a resolution that did not bind one -- where
// no corpus line carries a chain union -- keeps the resolution's own
// Ctx.Targets, which for a chain with no sub targets IS the union. Shared by
// the AllTargeted case and the TargetedObjects/SpellTargeted refs so the
// three cannot disagree about what "the targets" means.
func refTargetUnion(c *Ctx) []state.Target {
	if c.AllTargets != nil {
		return c.AllTargets
	}
	return c.Targets
}

// castManaSpentTotals is one cast's recorded CR 601.2h / 106.12 spend
// breakdown: the unfiltered total, the snow-unit part, and the per-producer
// typed parts in state.TypedManaTags order. The plain Count$CastTotalManaSpent
// head reads it live off the source Object; the TriggeredCard$CastTotalManaSpent
// ref-head reads the fire-time snapshot a trigger context carries, so the two
// forms can never disagree about what one argument selects.
type castManaSpentTotals struct {
	total, snow int32
	typed       [4]int32
}

// manaSpentTotalsOf reads a cast object's recorded spend breakdown. The typed
// parts come from Object.TypedManaSpentByTag so adding a tag to
// state.TypedManaTags extends the head automatically.
func manaSpentTotalsOf(o *state.Object) castManaSpentTotals {
	t := castManaSpentTotals{total: o.ManaSpent, snow: o.ManaSnowSpent}
	for i := range state.TypedManaTags {
		t.typed[i] = o.TypedManaSpentByTag(i)
	}
	return t
}

// byTag selects the spend a Count$CastTotalManaSpent <Type> argument names:
// the bare total for an empty arg, the snow part for "Snow", a modelled typed
// tag for its word, and the fail-closed 0 for anything the pool cannot tag.
func (t castManaSpentTotals) byTag(arg string) int32 {
	switch arg {
	case "":
		return t.total
	case "Snow":
		return t.snow
	default:
		for i, tagWord := range state.TypedManaTags {
			if arg == tagWord {
				return t.typed[i]
			}
		}
		return 0
	}
}

// evalRefProperty resolves one "<Ref>$<Property>[...][/Op]" count body over
// the objects a target reference names. Refs: Targeted/ParentTarget/
// ThisTargetedCard name the resolving ability's chosen targets, AllTargeted
// the whole root/sub-ability chain union (Ctx.AllTargets when the cost site
// bound it, Ctx.Targets otherwise; see refTargets);
// TriggeredCard (and its LKI spellings) and TriggeredAttacker name the
// objects the firing trigger remembered; Remembered is the plain form. A
// property this build does not model (or a body with no $ at all -- every
// other head in this evaluator) returns false, and the caller degrades to
// zero exactly as before this evaluator existed.
//
// The per-object answers mirror evalCountBody's own source-anchored heads:
// CardPower/CardToughness read the face plus marked P1P1 counters
// (battlefield layer output for a battlefield object; a graveyard object's
// face), CardManaCost the face's converted cost, CardCounters.<KIND> one
// counter kind, Valid the count of referenced objects matching a card spec
// (unknown predicates fail closed inside the matcher, so an unreadable
// filter counts zero, never everything). Several references sum -- Forge's
// Count$ reads the same way -- and the /Op suffix applies through
// applyCountOp like every other head.
func evalRefProperty(h Host, c *Ctx, expr string) (int32, bool) {
	ref, prop, found := strings.Cut(expr, "$")
	if !found || h == nil {
		return 0, false
	}
	prop, op, hasOp := strings.Cut(prop, "/")
	prop = strings.TrimSpace(prop)
	var ts []state.Target
	if ref == "TriggerObjectsCards" {
		ts = c.Captured
	} else {
		var ok bool
		ts, ok = refTargets(h, c, ref)
		if !ok {
			return 0, false
		}
	}
	if (ref == "TargetedObjects" || ref == "TargetedObjectsDistinct") && prop == "Amount" {
		n := int32(len(ts))
		if hasOp {
			n = applyCountOp(n, op)
		}
		return n, true
	}
	g := h.Game()
	var n int32
	triggerObjectTypes := map[string]bool{}
	// The Different* distinct-set property family over a reference's objects
	// (task diffcount1): `Remembered$DifferentCardManaCost` (Azor's Gateway,
	// Sanctum of the Sun settling X, Atemsis All-Seeing). The set is read
	// through len, so no map ordering ever reaches an event or a view.
	diffKind := differentPropertyKindOf(prop)
	var seenDiffValues map[int32]bool
	var seenDiffNames map[string]bool
	if diffKind != diffNone {
		if diffKind == diffName {
			seenDiffNames = make(map[string]bool)
		} else {
			seenDiffValues = make(map[int32]bool)
		}
	}
	for _, t := range ts {
		if t.IsPlayer {
			continue
		}
		o := g.Obj(t.Obj)
		lki := c.LKI != nil && c.LKI.ID == t.Obj
		if lki {
			// A zone-change trigger must read the causing object's snapshot,
			// not the same id after Move has cleared its counters and removed
			// battlefield layers. triggerLKI carries this value through the
			// TriggerPush wrapper to both initial and resumed resolution.
			o = c.LKI
		} else if ref == "Remembered" {
			for _, rememberedLKI := range c.ChangeZoneLKI {
				if rememberedLKI.Obj == t.Obj && rememberedLKI.Snapshot.Card != nil {
					snapshot := rememberedLKI.Snapshot
					o = &snapshot
					lki = true
					break
				}
			}
		}
		if o == nil {
			continue
		}
		f := o.Face()
		switch {
		case (ref == "TriggerObjectsCards" || ref == "TriggerRemembered") && prop == "CardTypes":
			if f != nil {
				for _, typ := range f.Types {
					if cardTypeWords[typ] {
						triggerObjectTypes[typ] = true
					}
				}
			}
		case ref == "TriggerObjectsCards" && prop == "GreatestCardManaCost":
			if f != nil && f.Cmc() > n {
				n = f.Cmc()
			}
		case prop == "CardPower":
			if f != nil {
				if lki && c.LKIPTValid {
					n += c.LKIPower
				} else {
					n += refPower(h, o, lki)
				}
			}
		case prop == "CardToughness":
			if f != nil {
				if lki && c.LKIPTValid {
					n += c.LKIToughness
				} else {
					n += refToughness(h, o, lki)
				}
			}
		case prop == "CardManaCost" || prop == "CardManaCostLKI":
			// CardManaCostLKI (56 raw corpus lines -- 51
			// TriggeredSpellAbility$CardManaCostLKI, Sunbird's Invocation's
			// PeekAmount X among them) is Forge's LKI spelling of the same
			// property: the mana value the object HAD when the triggering
			// event happened. A face's mana value never changes and the lki
			// swap above already binds the zone-change snapshot when one is
			// carried, so the LKI spelling reads the same number the plain
			// spelling does -- one shared case, so the two cannot disagree.
			if f != nil {
				n += f.Cmc()
			}
		case prop == "CardNumColors":
			// The referenced object's colour count (task rv2b-countheads):
			// Mana Drain's drain (Lurking Spinecrawler's
			// TriggeredCard$CardNumColors, Moonveil Regent's and Mana
			// Cannons' Targeted$CardNumColors), read off the object the ref
			// names rather than the resolving source the plain
			// Count$CardNumColors head reads. h.ObjectColors is the SAME read
			// that head uses -- live layer-5 colours on a battlefield
			// permanent, the printed face's colours elsewhere (and on the lki
			// snapshot above, whose Face points at the unchanged card) -- so
			// the two spellings cannot disagree about one object.
			n += int32(len(h.ObjectColors(o)))
		case strings.HasPrefix(prop, "CardCounters."):
			// ALL is the sum over every kind (the same wildcard the plain
			// Count$CardCounters.ALL head reads -- Kinsbaile Borderguard's
			// TriggeredCard$CardCounters.ALL), never a literal kind lookup.
			// CR 608.2b/h: an object target that has left the battlefield is
			// read with the counters it had there, so Dismantle's
			// `X:Targeted$CardCounters.ALL` still sizes the placement after
			// the chained Destroy cleared the live counters. The trigger
			// snapshot (lki) has already substituted its own object above and
			// stays authoritative.
			if !lki && ref == "Targeted" {
				if cs, ok := targetCountersLKI(c, t.Obj, o); ok {
					oc := *o
					oc.Counters = cs
					o = &oc
				}
			}
			if strings.EqualFold(strings.TrimPrefix(prop, "CardCounters."), "ALL") {
				n += sumCounters(o.Counters)
			} else {
				n += o.Counter(strings.TrimPrefix(prop, "CardCounters."))
			}
		case prop == "Amount":
			// The count of referenced objects themselves (SVar:X:ExiledWith$Amount,
			// the same "how many" the Remembered$Amount head answers for the
			// Remembered ref). Players in the list do not count.
			n++
		case prop == "Valid" || strings.HasPrefix(prop, "Valid "):
			spec := strings.TrimSpace(strings.TrimPrefix(prop, "Valid"))
			if (lki && MatchesObjectCtx(g, spec, o, c.SpecContext(c.Controller))) ||
				(!lki && MatchesSpecCtx(g, spec, t.Obj, c.SpecContext(c.Controller))) {
				n++
			}
		case prop == "Converge":
			// CR 107.4f-family converge, the TRIGGER-relative spelling: the
			// distinct-colour spend count of the cast the firing trigger is
			// about (Magmablood Archaic's SVar:Y:TriggeredCard$Converge), not
			// the resolving ability's own cast the plain Count$Converge head
			// at evalCountBody's "Converge" case reads off c.Source. Same
			// provenance discipline as that head and as TriggerPaidX: the
			// value was stamped on the cast spell by payCast's trailing
			// FlagConverged CastInfo BEFORE the deferred SpellCast trigger
			// re-walk fired, so a replay derives the same number; when the
			// read object IS the triggering card the fire-time snapshot
			// TriggerConverge wins over the live field, because a spell
			// countered between trigger push and resolution has had its
			// stack->graveyard move clear ConvergeColours while the colours
			// were spent regardless (CR 601.2h: the payment is not undone).
			// A copy of the spell was never cast and reads 0.
			if c.TriggerCard != 0 && t.Obj == c.TriggerCard {
				n += c.TriggerConverge
			} else {
				n += o.ConvergeColours
			}
		case prop == "CastTotalManaSpent" || strings.HasPrefix(prop, "CastTotalManaSpent "):
			// CR 601.2h / 106.12's payment, the TRIGGER-relative spelling: the
			// TOTAL mana actually spent to cast the spell the firing trigger is
			// about (Muse Seeker's "unless five or more mana was spent to cast
			// that spell", Aetherflux Conduit's energy gain), not the resolving
			// ability's own cast the plain Count$CastTotalManaSpent head at
			// evalCountBody reads off c.Source. The argument selects the same
			// subset that head's <Type> filter does (the bare total, the snow
			// part, or a modelled producer tag -- state.TypedManaTags), through
			// the one shared castManaSpentTotals.byTag so the two forms cannot
			// disagree. Same provenance discipline as Converge and TriggerPaidX:
			// the spend was stamped on the cast spell by payCast BEFORE the
			// deferred SpellCast trigger re-walk fired, so a replay derives the
			// same number; when the read object IS the triggering card the
			// fire-time snapshot wins over the live fields, because a spell that
			// has left the stack (countered, or resolved onto the battlefield)
			// before the trigger resolves has had its ManaSpent/ManaSnowSpent/
			// typed captures zeroed by the stack->zone move while the mana was
			// spent regardless (CR 601.2h: the payment is not undone). A copy
			// of the spell was never cast and reads 0.
			arg := strings.TrimSpace(strings.TrimPrefix(prop, "CastTotalManaSpent"))
			if c.TriggerCard != 0 && t.Obj == c.TriggerCard {
				n += castManaSpentTotals{total: c.TriggerManaSpent, snow: c.TriggerManaSnowSpent, typed: c.TriggerManaTyped}.byTag(arg)
			} else {
				n += manaSpentTotalsOf(o).byTag(arg)
			}
		default:
			if diffKind != diffNone {
				switch {
				case seenDiffNames != nil:
					if f := o.Face(); f != nil {
						seenDiffNames[f.Name] = true
					}
				case diffKind == diffPower:
					// The derived power, with the zone-change snapshot when one
					// is carried -- the CardPower case's exact read.
					if lki && c.LKIPTValid {
						seenDiffValues[c.LKIPower] = true
					} else {
						seenDiffValues[refPower(h, o, lki)] = true
					}
				case diffKind == diffToughness:
					if lki && c.LKIPTValid {
						seenDiffValues[c.LKIToughness] = true
					} else {
						seenDiffValues[refToughness(h, o, lki)] = true
					}
				default:
					if v, ok := differentPropertyValue(h, o, diffKind); ok {
						seenDiffValues[v] = true
					}
				}
				continue
			}
			return 0, false
		}
	}
	if (ref == "TriggerObjectsCards" || ref == "TriggerRemembered") && prop == "CardTypes" {
		n = int32(len(triggerObjectTypes))
	}
	if diffKind != diffNone {
		if seenDiffNames != nil {
			n = int32(len(seenDiffNames))
		} else {
			n = int32(len(seenDiffValues))
		}
	}
	if hasOp {
		n = applyCountOp(n, op)
	}
	return n, true
}

// evalPlayerRefProperty resolves one "TargetedPlayer$<Property>[...][/Op]"
// (and the siblings "ThisTargetedPlayer$..." and "TargetedController$...")
// count body over the PLAYERS a target reference names -- the
// <Ref>$<Property> family's player-valued half, which evalRefProperty's
// object loop structurally cannot serve (it `continue`s every IsPlayer target
// and its property switch is object-only). The player list is the generic
// pre-ask's answered set (Ctx.PickedTargets) when non-nil, else the
// resolution's own Ctx.Targets, filtered to IsPlayer entries --
// effects/context.go's Defined$ Targeted precedence exactly, so the head
// answers the player the resolving body acts on. The count of players can be
// several; Forge's own Count sums over the referenced players the same way
// evalRefProperty sums over referenced objects.
//
// TargetedController$<Property> is the same family read through the CONtroller
// of the target list: its players are controllersOf(Ctx.Targets/PickedTargets)
// -- the exact resolution effects/context.go's Defined$ TargetedController
// case already uses -- so it shares this arm's whole property switch.
//
// A ref this arm does not special-case (TriggeredTarget, TriggeredPlayer,
// TriggeredDefendingPlayer, ...) is resolved through effects/context.go's
// shared definedSpec -- the SAME resolver the body's own Defined$ spelling
// goes through -- so the ref list is not a hand-maintained duplicate that
// can miss the next sibling. The LifeTotal head is wired for those refs
// (task rv2b-countheads: Quietus Spike's and Ebonblade Reaper's
// TriggeredTarget$LifeTotal/HalfUp); every OTHER property on such a ref
// still fails closed, so this arm's wider ref set cannot silently widen the
// player count semantics. An unknown ref or an unmodelled property returns
// false and the caller degrades to zero.
//
// Properties (the heads the 81-file corpus population is dominated by and
// that are exactly definable today): LifeTotal (the player's current
// life), CardsInHand/CardsInLibrary/CardsInGraveyard (zone sizes),
// CreaturesInPlay (battlefield creatures the player controls), the Valid
// head and its countZone family (Valid/ValidHand/ValidGraveyard/
// ValidLibrary/ValidExile/ValidStack over a card spec; the referenced
// player is the filter's You, so `TargetedPlayer$ValidGraveyard
// Instant.YouOwn,Sorcery.YouOwn` counts the TARGET player's own
// instants/sorceries in THEIR graveyard -- The Mouth of Sauron's head),
// LifeLostThisTurn (the shared Host predicate), DamageThisTurn (the
// Host's per-player damage-taken fold, implemented engine-side beside
// LifeLostThisTurn) and Counters.Poison. The /Op suffix applies through
// applyCountOp like every other head. A property this build does not
// model (StartingLife, DomainPlayer, CardsDrawn, ...) or a ref
// outside the two names, the Defined$-resolved names above, the
// vote-carrier ref
// TriggeredPlayersOpponentVotedDiff (trig:Vote) and the DamageAll batch
// ref TriggeredPlayersTargets (trig:DamageAll; both refs' only property
// is Amount) returns false, and the caller degrades to zero
// exactly as evalRefProperty's default always did. CardsDiscardedThisTurn
// is the one optional head the brief allowed in: the shared Host predicate
// already existed.
func evalPlayerRefProperty(h Host, c *Ctx, expr string) (int32, bool) {
	ref, prop, found := strings.Cut(expr, "$")
	if !found || h == nil {
		return 0, false
	}
	prop, op, hasOp := strings.Cut(prop, "/")
	prop = strings.TrimSpace(prop)
	var ts []state.Target
	switch ref {
	case "TargetedPlayer", "ThisTargetedPlayer":
		ts = c.Targets
		if c.PickedTargets != nil {
			ts = c.PickedTargets
		}
	case "TargetedController":
		// The target list read through its controllers: the same
		// PickedTargets-else-Targets precedence as the TargetedPlayer arm,
		// converted with the shared controllersOf helper the Defined$
		// TargetedController case in effects/context.go also uses, so a
		// count head and a Defined$ spelling of the ref cannot disagree.
		// Lullmage's Domination's SVar:CheckTgt reads
		// `TargetedController$CardsInGraveyard` through the ReduceCost
		// static's Count$Compare (agent-20260928T043626Z-b7e271c1).
		src := c.Targets
		if c.PickedTargets != nil {
			src = c.PickedTargets
		}
		ts = controllersOf(h.Game(), src)
	case "TriggeredPlayersOpponentVotedDiff":
		// The canonical vote-finished carrier's diff set (trig:Vote): the
		// fire-time referent capture is the ONLY binding, so a count read
		// outside a Vote resolution fails closed to the empty list -- the
		// same convention the vote's own Defined$ spellings take. Amount is
		// the count of those opponents (Erestor's SVar:X, the scry size),
		// added ONLY for this ref: TargetedPlayer$Amount stays unmodelled,
		// its doc-listed degrade unchanged.
		for _, p := range c.TriggeredOpponentsVotedDiff {
			ts = append(ts, state.Target{Player: p, IsPlayer: true})
		}
	default:
		// A ref this arm does not special-case, but the ENGINE's shared
		// Defined$ resolver already names (TriggeredTarget, TriggeredPlayer,
		// TriggeredDefendingPlayer, TriggeredCardController, ...): resolve it
		// through definedSpec -- the SAME resolver the body's own Defined$
		// spelling goes through -- and keep its player entries, so the ref
		// list is not a hand-maintained duplicate that can miss the next
		// sibling. This ticket (rv2b-countheads) wires the LifeTotal head for
		// those refs (Quietus Spike's / Ebonblade Reaper's
		// TriggeredTarget$LifeTotal/HalfUp). The property is confined to the
		// one head the ticket names so the change cannot widen the family's
		// other player count semantics (CardsInHand, Valid, Counters.Poison,
		// ...) for refs that used to fail closed; an unknown ref or property
		// still fails closed to (0, false).
		if prop != "LifeTotal" {
			return 0, false
		}
		pts, ok := definedSpec(h, c, ref)
		if !ok {
			return 0, false
		}
		for _, t := range pts {
			if t.IsPlayer {
				ts = append(ts, t)
			}
		}
	case "TriggeredCapturedPlayers":
		// The firing trigger's fire-time PLAYER capture (Ctx.Captured) read
		// on purpose. The plain Remembered heads (Remembered$Amount,
		// Count$RememberedNumber) exclude that capture -- Forge's host
		// remembered list never holds the event referent -- so a body whose
		// count IS the referent set must name it through this Triggered*-
		// family ref instead. Its one user is the synthesized Melee pump
		// (cards.MeleePumpCount): rules captures one player ref per distinct
		// opponent attacked in the declaration (rules/melee.go
		// meleeRemembered) and the stack wrapper's logged Remembered comes
		// back as Captured at resolution, so replay and stack copies read the
		// same count. Amount is the only property.
		for _, t := range c.Captured {
			if t.IsPlayer {
				ts = append(ts, t)
			}
		}
	case "TriggeredPlayersTargets":
		// The batch's matching TARGET PLAYERS (trig:DamageAll): Malcolm
		// Keen-Eyed Navigator's and Hordewing Skaab's SVar:X reads the count
		// of opponents the damage batch dealt damage to ("create a Treasure
		// token for each opponent dealt damage" / "draw cards equal to the
		// number of opponents dealt damage this way"). The capture is the
		// fire-time batch target set, filtered to its player entries in
		// first-seen order; Amount is the count of those players.
		for _, t := range c.TriggerDamageTargets {
			if t.IsPlayer {
				ts = append(ts, state.Target{Player: t.Player, IsPlayer: true})
			}
		}
	}
	// TriggeredPlayersOpponentVotedDiff is the canonical vote-finished
	// carrier's diff set (trig:Vote); its ONLY documented property is Amount
	// (Erestor's SVar:X, the scry size). Confine the head to it here, so the
	// ref cannot silently inherit LifeTotal/CardsInHand/Valid... sums that
	// belong to TargetedPlayer/ThisTargetedPlayer -- the contract the
	// evalPlayerRefProperty doc states. TriggeredPlayersTargets (the
	// DamageAll batch ref) takes the same confinement: its only modelled
	// property is Amount, so a future property on it fails closed to zero
	// instead of silently reading the batch players' current zone sizes.
	if ref == "TriggeredPlayersOpponentVotedDiff" && prop != "Amount" {
		return 0, false
	}
	if (ref == "TriggeredPlayersTargets" || ref == "TriggeredCapturedPlayers") && prop != "Amount" {
		return 0, false
	}
	g := h.Game()
	var n int32
	for _, t := range ts {
		if !t.IsPlayer {
			continue
		}
		p := t.Player
		switch {
		case prop == "LifeTotal":
			n += g.Players[p].Life
		case prop == "CardsInHand":
			n += int32(len(g.Zone(state.ZHand, p)))
		case prop == "CardsInLibrary":
			n += int32(len(g.Zone(state.ZLibrary, p)))
		case prop == "CardsInGraveyard":
			n += int32(len(g.Zone(state.ZGraveyard, p)))
		case prop == "LifeLostThisTurn":
			n += h.LifeLostThisTurn(p)
		case prop == "DamageThisTurn":
			n += h.DamageTakenThisTurn(p)
		case prop == "CardsDiscardedThisTurn":
			n += h.CardsDiscardedThisTurn(p)
		case prop == "TotalCommanderCastFromCommandZone" || prop == "CommanderCastFromCommandZone":
			n += h.CommanderCastsFromCommandZone(p)
		case prop == "Counters.Poison":
			for _, pc := range g.Players[p].Counters {
				if pc.Kind == "POISON" {
					n += pc.N
				}
			}
		case prop == "CreaturesInPlay":
			// Battlefield creatures the referenced player controls, through
			// the same spec matcher the Valid family uses.
			for _, id := range g.Zone(state.ZBattlefield, p) {
				if matchesZoneSpecCtx(g, "Creature", id, c.SpecContext(p), state.ZBattlefield) {
					n++
				}
			}
		case prop == "Amount" && (ref == "TriggeredPlayersOpponentVotedDiff" || ref == "TriggeredPlayersTargets" ||
			ref == "TriggeredCapturedPlayers"):
			n++
		default:
			// The Valid head and its countZone family: "Valid <spec>",
			// "ValidGraveyard <spec>", ... -- the exact template of the
			// Count$Valid<zone> head, scoped to the referenced player's zone
			// and matched with the referenced player as the filter's You.
			head, spec, _ := strings.Cut(prop, " ")
			spec = strings.TrimSpace(spec)
			zone, ok := countZone(head)
			if !ok {
				return 0, false
			}
			if spec == "" {
				// A head with no filter counts the zone itself (corpus:
				// every TargetedPlayer$Valid... occurrence carries a spec;
				// the bare form stays the honest reading rather than a
				// fail-closed zero).
				n += int32(len(g.Zone(zone, p)))
				continue
			}
			for _, id := range g.Zone(zone, p) {
				if matchesZoneSpecCtx(g, spec, id, c.SpecContext(p), zone) {
					n++
				}
			}
		}
	}
	if hasOp {
		n = applyCountOp(n, op)
	}
	return n, true
}

// refPower/refToughness use rules' derived characteristics while a referenced
// object is a battlefield permanent. A referred-to object that already left
// keeps the LKI-compatible printed-face-plus-P/T-counters fallback: no live
// layer applies in a graveyard, and asking Host for it would read a different
// state. The counter sum covers every P/T counter kind (CR 613.7d), not just
// the +1/+1 / -1/-1 pair.
// inProgressDerivedPTHost is implemented by rules.Engine so a Count$ read made
// during layer 7 can consume the current walk's value instead of recursively
// asking the host to derive the same object again. It is optional to preserve
// the small effects.Host contract and its test doubles.
type inProgressDerivedPTHost interface {
	InProgressDerivedPT(id state.ObjID) (power, toughness int32, ok bool)
}

func refPower(h Host, o *state.Object, snapshot bool) int32 {
	if !snapshot && o.Zone == state.ZBattlefield {
		if provider, ok := h.(inProgressDerivedPTHost); ok {
			if power, _, found := provider.InProgressDerivedPT(o.ID); found {
				return power
			}
		}
		return h.Power(o.ID)
	}
	dp, _ := o.CounterPTTotals()
	return int32(o.Face().Power()) + dp
}

func refToughness(h Host, o *state.Object, snapshot bool) int32 {
	if !snapshot && o.Zone == state.ZBattlefield {
		if provider, ok := h.(inProgressDerivedPTHost); ok {
			if _, toughness, found := provider.InProgressDerivedPT(o.ID); found {
				return toughness
			}
		}
		return h.Toughness(o.ID)
	}
	_, dt := o.CounterPTTotals()
	return int32(o.Face().Toughness()) + dt
}

// EvalCountOnObject evaluates a Count$ expression with the count's source
// anchor moved to ONE specific object: a shallow Ctx copy keeps the resolving
// ability's SVar table, controller and remembered set, but `Source` -- what
// the source-anchored heads (CardPower, CardToughness, CardManaCost,
// CardNumColors) read -- becomes obj. This is what a
// `CounterNumPerDefined$` parameter needs: the count is evaluated per
// AFFECTED object (Canopy Gargantuan's "equal to that creature's toughness"),
// not once for the resolving source. An expression whose head the evaluator
// does not model degrades to zero, exactly as EvalCount does.
func EvalCountOnObject(h Host, c *Ctx, expr string, obj state.ObjID) int32 {
	if c == nil {
		c = &Ctx{}
	}
	cc := *c
	cc.Source = obj
	return EvalCount(h, &cc, expr)
}

// evalCountBody is the Count$ head dispatch; ok is false only at the
// fallthrough (the head matched nothing), never inside a modelled branch -- a
// modelled head that legitimately counts zero still counts as evaluated.
func evalCountBody(h Host, c *Ctx, body string, depth int) (int32, bool) {
	g := h.Game()
	// ThisTurnCast_<spec> keeps the WHOLE body as the spec, before the
	// generic head/space split below: a Forge count spec can carry a space
	// (Rain of Riches' "Card.YouCtrl+CastSa Spell.ManaFromTreasure" — the
	// card-level CastSa property token), and the split would truncate the
	// spec at the space and drop the property. The /Op suffix was already
	// cut by the caller. Space-free specs take the identical path they took
	// through the switch arm (the same reads, the same returns), so every
	// existing carrier is byte-identical.
	if rest, ok := strings.CutPrefix(body, "ThisTurnCast_"); ok {
		if stripped, selfExcl := stripBareCastSaSource(rest); selfExcl {
			return int32(h.SpellsCastThisTurnMatchingExcluding(c.Controller, stripped, c.Source)), true
		}
		// The ARGUMENTED forms (task castprov2) peel the token and reuse the
		// same Excluding read:
		//
		//   - !CastSaSource/<op> (thunder_salvo's /Plus.2): this form never
		//     reaches this arm — evalCountExprOK's GENERIC /Op peel cuts the
		//     body at the first "/" before the head parse, leaving the bare
		//     !CastSaSource for the bare arm above and handing the op to the
		//     ordinary applyCountOp — which is exactly the oracle's reading
		//     (the exclusion count, then Plus.2). Pinned by
		//     TestThunderSalvoXIsTwoPlusOtherSpellsCast.
		//   - !CastSaSource$<Property> (call_forth_the_tempest's
		//     $CardManaCost): the matching casts' objects, the property
		//     AGGREGATED over them instead of counting 1 each (the zone-count
		//     heads' `$Property` precedent). An unknown property fails closed
		//     to (0, false), the unresolvable verdict.
		if stripped, prop, ok2 := stripCastSaSourceAggregate(rest); ok2 {
			return aggregateCastProperty(h, h.EachSpellCastThisTurnMatching(c.Controller, stripped, c.Source), prop)
		}
		// The COUNT-level `$<Property>` suffix (rootha_mastering_the_moment's
		// `Instant.YouCtrl,Sorcery.YouCtrl$GreatestCardManaCost` and
		// april_oneil_hacktivist's `Card.YouCtrl$CardTypes`): the matching
		// casts' objects, the property folded over them instead of counting
		// them one each. This is the SAME `$Property` precedent as the zone
		// head (Count$Valid <spec>$GreatestCardManaCost), but on the
		// cast-count branch. The peel runs AFTER the two CastSaSource arms so
		// it can never steal the predicate-token `$` of an argumented
		// `!CastSaSource$<Property>` (call_forth_the_tempest), and it fires
		// ONLY when the segment after the last `$` is in the admitted
		// vocabulary -- an unadmitted suffix keeps the byte-identical
		// whole-token read below.
		if stripped, prop, ok3 := stripCastSourceAggregate(rest); ok3 {
			return aggregateCastSourceProperty(h, h.EachSpellCastThisTurnMatching(c.Controller, stripped, c.Source), prop)
		}
		return int32(h.SpellsCastThisTurnMatching(c.Controller, rest)), true
	}
	// ThisTurnActivated_<spec> (Professor Hojo's and Tezzeret, Betrayer of
	// Flesh's "the first activated ability ... each turn" gates, the Equip/
	// Cycling/Exhaust families' counts): the activated abilities activated
	// this turn matching an `Activated.<props>` spec, the in-flight
	// activation being priced (Ctx.AffectedAbility) included -- Forge records
	// an activation before its cost is adjusted, which is why those gates
	// read LE1. The spec keeps its spaces (IsTargeting Valid <spec>), so the
	// arm runs before the head split, exactly as ThisTurnCast_ does. The log
	// fold lives in rules; a Host without it leaves the head unmodelled.
	if rest, ok := strings.CutPrefix(body, "ThisTurnActivated_"); ok {
		// The Ctx's fields are passed by value, never the *Ctx itself: a Ctx
		// handed to an interface method escapes, and the hot layer walk
		// builds one per object (TestEvalCountValidZoneScanIsAllocationFree).
		provider, okP := h.(interface {
			AbilitiesActivatedThisTurnMatching(you state.PlayerID, affected state.ObjID, ab *cards.SA, targets []state.Target, spec string) (int32, bool)
		})
		if !okP {
			return 0, false
		}
		return provider.AbilitiesActivatedThisTurnMatching(c.Controller, c.AffectedObj, c.AffectedAbility, c.Targets, strings.TrimSpace(rest))
	}
	head, arg, _ := strings.Cut(body, " ")
	arg = strings.TrimSpace(arg)
	if head == "ValidSelf" {
		return evalCountValidSelf(h, c, arg)
	}
	if head == "TotalDamageReceivedThisTurn" && arg == "" {
		self := c.TriggerCard
		if self == 0 {
			self = c.Source
		}
		if self == 0 {
			return 0, true
		}
		o := h.Game().Obj(self)
		if c.LKI != nil && c.LKI.ID == self {
			o = c.LKI
		}
		if o == nil {
			return 0, true
		}
		return o.DamageReceivedThisTurn, true
	}
	if arg == "" {
		// ONLY OptionalGenericCostPaid's space-less dotted <paid>.<unpaid>
		// argument is split here. Every other dotted head (CardCounters.CHARGE,
		// Kicked.4.0, Foretold.1.0, ...) is parsed WHOLE by its own downstream
		// CutPrefix arm, so a generic split would truncate the head to its
		// first segment and bypass that arm -- Count$CardCounters.CHARGE would
		// reach the bare-CardCounters fallthrough as an unresolved zero.
		if rest, ok := strings.CutPrefix(head, "OptionalGenericCostPaid."); ok {
			head, arg = "OptionalGenericCostPaid", strings.TrimSpace(rest)
		}
	}

	// Convoked$Amount (CR 702.66): the number of creatures that convoked the
	// resolving spell's cast -- Forge's own `SVar:X:Convoked$Amount` head.
	// The count reads the SAME source-object provenance the Defined$ Convoked
	// selector reads (effects/context.go's definedSpec): the pay-time
	// CastInfo's FlagConvoked IDs, folded onto Object.Convoked by
	// events.Apply and preserved across the stack->battlefield move, so the
	// ETB half of Ancient Imperiosaur and Knight-Errant of Eos reads the same
	// set the spell on the stack did. A cast with no convoke, an absent
	// source and a copy all read a legitimate zero (the modelled-head
	// convention every other cast-provenance count takes, NOT the
	// unresolvable verdict the fallthrough gives). A creature that left play
	// after convoking still counts -- CR 702.66 counts the creatures that
	// CONVOKED, and Object.Convoked holds their ids.
	//
	// This is a `<Head>$<Property>` body, so it carries its OWN optional /Op
	// exactly like PlayerCountHasLost$Amount/Times.10: a
	// `Count$Convoked$Amount/Twice` gets the suffix peeled upstream by
	// evalCountExprOK and applied generically, while the corpus's bare
	// `SVar:X:Convoked$Amount/Twice` (Ancient Imperiosaur's two
	// counters-per-creature) reaches here with the suffix intact and must
	// strip it before the exact-name compare. Splitting it here -- not in a
	// second Twice arm -- keeps ONE composition path for the op.
	if rest, ok := strings.CutPrefix(head, "Convoked$"); ok {
		name, op, hasOp := strings.Cut(rest, "/")
		if strings.TrimSpace(name) != "Amount" || hasOp && !validConvokedCountOp(op) {
			return 0, false
		}
		n := int32(0)
		if o := g.Obj(c.Source); o != nil {
			n = int32(len(o.Convoked))
		}
		if hasOp {
			n = applyCountOp(n, op)
		}
		return n, true
	}

	// TargetedByTarget$Valid <spec> (Forge; 2 corpus carriers -- Not of This
	// World's SVar:CheckTgt:TargetedByTarget$Valid Card.powerGE7+YouCtrl and
	// Bane's Contingency's ...IsCommander+YouCtrl+inZoneBattlefield): the
	// number of objects, among the targets of the spell/ability(ies) that the
	// resolving source targets, that match <spec> -- the NESTED read one
	// level past spellIsTargetingMatches' own .Targets walk. The resolving
	// source's targets are Ctx.Targets (registry.go), the same binding the
	// Targeted ref reads; the cost-modifier static path binds the cast's
	// chosen or potential targets onto it (rules' modAmountX), so Not of This
	// World's Amount$ Compare gate sees Giant Growth's chosen target both at
	// the offer gate and at the CR 601.2c reprice. Object targets of each
	// targeted spell are matched through MatchesSpecCtx with the resolving
	// context's SpecContext (You/Source/Remembered bound as everywhere else);
	// player targets cannot match a Card... spec and are skipped. An absent
	// or empty Ctx.Targets is a legitimate zero -- a MODELLED head, not the
	// unresolvable verdict -- so the Compare gate fails closed at 0 (no
	// reduction) instead of erroring. A property other than Valid (the only
	// form either carrier uses) and an empty spec fail closed per the
	// unmodelled-property convention.
	if rest, ok := strings.CutPrefix(head, "TargetedByTarget$"); ok {
		if strings.TrimSpace(rest) != "Valid" || arg == "" {
			return 0, false
		}
		n := int32(0)
		sc := c.SpecContext(c.Controller)
		for _, t := range c.Targets {
			if t.IsPlayer || t.Obj == 0 {
				continue
			}
			inner := g.Obj(t.Obj)
			if inner == nil {
				continue
			}
			for _, it := range inner.Targets {
				if it.IsPlayer || it.Obj == 0 {
					continue
				}
				isc := sc
				// The zone-count fold's derived-PT bind (zoneCountFold.visit):
				// a battlefield inner target's numeric filter must read rules'
				// layer-derived characteristics (Syr Elenora's power-equals-hand
				// size), not the printed face -- skip the bind entirely unless
				// the spec reads a P/T field, the same dependency guard.
				if io := g.Obj(it.Obj); io != nil && io.Zone == state.ZBattlefield && SpecReadsPT(arg) {
					if provider, ok := h.(interface {
						FilterDerivedPT(state.ObjID) (power, toughness, basePower, baseToughness int32, ok bool)
					}); ok {
						if power, toughness, basePower, baseToughness, found := provider.FilterDerivedPT(it.Obj); found {
							isc.DerivedPower, isc.DerivedToughness, isc.HasDerivedPT = power, toughness, true
							isc.BasePower, isc.BaseToughness, isc.HasBasePT = basePower, baseToughness, true
						}
					}
				}
				if MatchesSpecCtx(g, arg, it.Obj, isc) {
					n++
				}
			}
		}
		return n, true
	}

	// The fuzz-cov3 heads (effects/count_cov3.go): heads that previously
	// matched nothing below, so consulting them first changes no verdict
	// another arm gave.
	if n, ok := evalCov3Head(h, c, head, arg, depth); ok {
		return n, true
	}
	if n, ok := evalCov3PlayerHead(h, c, head, arg, depth); ok {
		return n, true
	}

	switch head {
	case "YouDescendedThisTurn":
		// The number of times the resolving controller descended this turn
		// (CR 700.11): The Mycotyrant's TokenAmount$ X and Molten Collapse's
		// CharmNum$ Count$Compare Y GE1.2.1. Reads the same fx20 provenance
		// ledger as the Player.descended predicate (descendedThisTurn), so
		// the head and the predicate cannot drift. An out-of-range seat and
		// an empty ledger are both a MODELLED zero -- never the unresolvable
		// verdict, which would make an SVar gate fail closed.
		if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
			return 0, true
		}
		return descendedThisTurn(g, c.Controller), true
	case "Compare":
		return evalCompare(h, c, arg, depth), true
	case "MostCardName":
		// Forge's Count$MostCardName <spec> (task api-winsgame; 4 corpus
		// carriers -- Mechanized Production, Endless Atlas, Chrome
		// Replicator, Sceptre of Eternal Glory): the GREATEST number of
		// battlefield objects matching <spec> that share one face name.
		// Mechanized Production's "eight or more artifacts with the same
		// name as one another" is exactly this read over
		// Artifact.YouCtrl. An empty spec is unresolvable (fail closed),
		// the same verdict every other argument-taking head gives a
		// missing argument.
		return countMostCardName(h, c, arg)
	case "ResolvedThisTurn":
		// Forge's Count$ResolvedThisTurn: how many times the resolving ability
		// has resolved this turn, the current resolution included ("if this is
		// the FOURTH time ... transform"). rules binds the per-ability tally
		// onto Ctx; an unbound Ctx reads a legitimate zero, the same
		// modelled-head zero every other count gives (NOT the unresolvable
		// verdict), so the SVar gate fails closed at 0 rather than failing
		// open and running its sub on the first resolution (the Sephiroth
		// transform defect this head's absence caused).
		return c.ResolvedThisTurn, true
	case "CardNumColors":
		if o := g.Obj(c.Source); o != nil {
			return int32(len(h.ObjectColors(o))), true
		}
		return 0, true
	case "ValidSelf":
		// Forge's Count$ValidSelf <Card$property> reads a property of the
		// source object ITSELF rather than of a scanned zone set (Diligent
		// Zookeeper's `SVar:AffectedX:Count$ValidSelf
		// Card$CreatureType/LimitMax.10`, whose AffectedX
		// AddPower$/AddToughness$ the layer-7c modify walk anchors on each
		// affected creature). c.Source is that object: for an AffectedX
		// static rules.staticAmountOn binds the recipient's id as the
		// anchor, and the /LimitMax.<n> cap arrives as the Count$ /Op
		// suffix (clamped by countDistinctLimitMax above, which admits this
		// head for the bounded CreatureType property). The corpus's other
		// ValidSelf argument shapes -- Card.!IsPrepared (the prepared
		// mechanic), Card.IsSuspected, and the unprefixed
		// Creature.greatestPowerControlledByCardController -- are NOT this
		// distinct-creature-type read, so they keep failing closed to the
		// unresolvable verdict (0, false) rather than silently reading the
		// wrong property as zero. Counted over the object's effective layer-4
		// types (falling back to the printed face when no derived entry exists),
		// with the SAME subtype vocabulary the sibling Count$Valid
		// <spec>$CreatureType distinct-set read uses (creatureSubtypeWords).
		// AffectedX P/T reads run in layer 7, after layer 4 establishes these
		// characteristics. The seen set is read only through len, so no map
		// ordering reaches an event or view.
		if strings.TrimSpace(arg) != "Card$CreatureType" {
			return 0, false
		}
		o := g.Obj(c.Source)
		if o == nil || o.Face() == nil {
			return 0, true
		}
		types := o.Face().Types
		for _, derived := range c.EffectiveTypes {
			if derived.ID == o.ID {
				types = derived.Types
				break
			}
		}
		seen := make(map[string]bool)
		for _, typ := range types {
			if creatureSubtypeWords[typ] {
				seen[typ] = true
			}
		}
		return int32(len(seen)), true
	case "CardNumAttacksThisTurn":
		// Forge's Count$CardNumAttacksThisTurn: how many times THIS object has
		// attacked this turn (Moraug, Fury of Akoum's "+1/+0 for each time it
		// has attacked this turn"). state.Object.AttacksThisTurn is the
		// event-folded per-object tally -- events.Apply increments it on each
		// DeclareAttackers and TurnChange resets it -- so the read is
		// deterministic and replay-stable. c.Source is the object the count
		// anchors on: for the AffectedX static that is the recipient creature
		// (rules.staticAmountOn binds the affected id), and for an ordinary
		// SVar body it is the resolving source. A missing source reads a
		// legitimate zero -- the modelled-head convention, NOT the
		// unresolvable verdict.
		if o := g.Obj(c.Source); o != nil {
			return o.AttacksThisTurn, true
		}
		return 0, true
	case "xPaid":
		// CR 107.3i: the {X} paid for the resolving spell or ability. On a
		// TRIGGER of a permanent that was cast for {X} the ability object's
		// own X is zero (a trigger was never paid an X), so the paid value
		// is read off the source permanent, which CastInfo carried out of
		// the cast onto the battlefield object (Meathook Massacre II's
		// SVar:X:Count$xPaid driving "each player sacrifices X creatures").
		if c.X != 0 {
			return c.X, true
		}
		if o := g.Obj(c.Source); o != nil {
			return o.X, true
		}
		return 0, true
	case "ReplicatePaid":
		// CR 702.55a: the number of replicate payments the resolving spell's
		// cast made, carried by the pay-time CastInfo's FlagReplicated Amount
		// (rules/cast.go's replicateAsk and payCast). Read off the SOURCE --
		// the cast spell, the same provenance read xPaid makes -- so a replay
		// derives the same count; a copy of the spell was never cast and
		// reads 0.
		if o := g.Obj(c.Source); o != nil {
			return o.ReplicateTimes, true
		}
		return 0, true
	case "SquadPaid":
		// CR 702.66: the number of squad payments the resolving spell's cast
		// made ("you may pay [cost] any number of times"), carried by the
		// pay-time CastInfo's FlagSquadPaid Amount (rules/cast.go's squadAsk
		// and payCast). The same provenance read ReplicatePaid makes: read off
		// the SOURCE -- the cast spell on the stack, and in the keyword
		// expansion's ETB trigger the permanent the spell became (the
		// stack->battlefield move preserves the field) -- so a replay derives
		// the same count; a copy of the spell was never cast and reads 0.
		if o := g.Obj(c.Source); o != nil {
			return o.SquadPaid, true
		}
		return 0, true
	case "OptionalGenericCostPaid":
		// OptionalCost's paid/unpaid branches are a boolean cast provenance.
		// The CastSA indirection has already bound c.Source to the cast object.
		// Each branch token is a numeric literal in the common case
		// (Count$OptionalGenericCostPaid.4.2), but Forge also writes another
		// SVar's value as the branch (Dragon's Fire's
		// `SVar:Y:Count$OptionalGenericCostPaid.X.3`, where the paid branch is
		// SVar X = Revealed$CardPower): a non-numeric token resolves as an
		// SVar$ indirection through the SAME runtime -> printed -> publication
		// precedence the SVar$ head uses, so the paired X/Y sizes from the
		// chosen card rather than collapsing to an unresolved zero.
		parts := strings.Split(strings.TrimSpace(arg), ".")
		if len(parts) < 2 {
			return 0, false
		}
		branch := func(tok string) (int32, bool) {
			tok = strings.TrimSpace(tok)
			if n, err := strconv.ParseInt(tok, 10, 32); err == nil {
				return int32(n), true
			}
			return evalCountExprOK(h, c, "SVar$"+tok, depth+1)
		}
		paid, ok1 := branch(parts[0])
		unpaid, ok2 := branch(parts[1])
		if !ok1 || !ok2 {
			return 0, false
		}
		if o := g.Obj(c.Source); o != nil && o.OptionalCostPaid {
			return paid, true
		}
		return unpaid, true
	case "OffspringPaid":
		// CR 702.175a: whether the resolving spell's cast paid the optional
		// Offspring additional cost ("You may pay an additional [cost] as you
		// cast this spell. If you do, when this creature enters, create a 1/1
		// token copy of it."), carried by the pay-time CastInfo's
		// FlagOffspringPaid (rules/cast.go's payCast). The same provenance
		// read SquadPaid makes: read off the SOURCE -- the cast spell on the
		// stack, and in the keyword expansion's ETB trigger the permanent the
		// spell became (the stack->battlefield move preserves the field) -- so
		// a replay derives the same value; a copy of the spell was never cast
		// and reads 0 (so a minted 1/1 copy mints no further copies).
		if o := g.Obj(c.Source); o != nil {
			if o.OffspringPaid {
				return 1, true
			}
		}
		return 0, true
	case "TimesKicked":
		// CR 702.43: the number of times the resolving spell's multikicker
		// cost was paid as it was cast, carried by the pay-time CastInfo's
		// FlagMultikicked Amount (rules/cast.go's multikickAsk and payCast).
		// The same provenance read ReplicatePaid makes: read off the SOURCE
		// (the cast spell on the stack; an ETB reader sees the PERMANENT it
		// became -- the stack->battlefield move preserves the field -- so a
		// replay derives the same count). A pending cast's count is seeded
		// into ctx.TimesKicked by targetBoundCtx when the spell's own
		// announcement ask reads a TimesKicked bound BEFORE payment has
		// stamped the object (Comet Storm's TargetMin/Max$ TargetsNum); a
		// COPY of the spell was never kicked and reads 0.
		if c.TimesKicked != 0 {
			return c.TimesKicked, true
		}
		if o := g.Obj(c.Source); o != nil {
			return o.TimesKicked, true
		}
		return 0, true
	case "TimesMutated":
		// CR 702.140f: how many times the SOURCE permanent has mutated, folded
		// by events.Apply's Mutate case onto state.Object.TimesMutated and reset
		// when the pile leaves the battlefield. The "this creature" readers
		// (Vadrok, Apex of Thunder's "where X is the number of times this
		// creature has mutated") resolve against the mutated permanent, which
		// is c.Source at resolution.
		if o := g.Obj(c.Source); o != nil {
			return o.TimesMutated, true
		}
		return 0, true
	case "Conspired":
		// CR 702.78a: 1 when the resolving spell's Conspire tap was actually
		// paid as it was cast, else 0. Carried by the pay-time CastInfo's
		// FlagConspired (rules/cast.go's conspireAsk/payCast). Same provenance
		// read ReplicatePaid makes -- the cast spell, the SOURCE -- so a
		// replay derives the same answer; a COPY of the spell was never cast
		// and reads 0. The keyword expansion's copy trigger uses this as its
		// Amount, so a declined Conspire (false) emits nothing.
		if o := g.Obj(c.Source); o != nil && o.Conspired {
			return 1, true
		}
		return 0, true
	case "Converge":
		// CR 107.4f-family converge: the number of DISTINCT colours (WUBRG)
		// of mana actually spent to cast the resolving spell, carried by the
		// pay-time CastInfo's FlagConverged Amount (rules/cast.go's
		// payManaCastSpent capture and payCast's trailing CastInfo). Same
		// provenance read ReplicatePaid makes -- the cast spell, and in the
		// K:etbCounter ETB replacement the same object after the
		// stack->battlefield move preserves it -- so a replay derives the
		// same count; a copy of the spell was never cast and reads 0.
		if o := g.Obj(c.Source); o != nil {
			return o.ConvergeColours, true
		}
		return 0, true
	case "CastTotalManaSpent":
		// CR 601.2h's payment: the TOTAL mana actually spent to cast the
		// resolving spell. The bare form (arg == "") is the spent delta's pips
		// summed over every slot, carried by the pay-time CastInfo's
		// FlagManaSpent Amount (rules/cast.go's payCast capture -- the
		// converge/replicate/multikick pattern; faceWantsCastSpend is the
		// heads-safety gate). Same provenance read Converge makes -- the cast
		// spell, and in the K:etbCounter ETB replacement the same object after
		// the stack->battlefield move preserves it -- so a replay derives the
		// same number; a copy of the spell was never cast and a cheated-in
		// permanent reads 0. The ref-property reader of OTHER casts
		// (TriggeredCard$CastTotalManaSpent, evalRefProperty) reads the
		// fire-time snapshot a trigger context carries, which payCast stamps
		// through the same gate's reader-out arm (triggeredCastSpendReaderOut)
		// when a battlefield permanent reads it.
		//
		// The FILTERED form `Count$CastTotalManaSpent <Type>` (tasks
		// castfilter1/castfilter2) counts only the mana spent whose SOURCE was
		// a permanent of <Type>. That per-unit producer provenance is carried
		// by the pool's parallel tallies and captured at payCast: <Type> ==
		// "Snow" resolves from the snow tally the pool has always carried (CR
		// 107.4h, Object.ManaSnowSpent), and <Type> == "Treasure"/"Cave"/
		// "Desert" (task castfilter2 — Marut, Bat Colony, Cataclysmic
		// Prospecting) resolves from Player.TypedMana's tagged units
		// (Object.ManaTreasureSpent / ManaCaveSpent / ManaDesertSpent). An
		// unknown <Type> — a producer type no tagging models — fails closed
		// to 0, which is strictly closer to the truth than the unfiltered
		// total the head used to return. Every resolved form is a real
		// per-unit count, not an approximation.
		if o := g.Obj(c.Source); o != nil {
			// The typed tags are the SAME table the producer-side tagging
			// reads (state.TypedManaTags), so a modelled type counts and a
			// type the pool cannot tag stays the fail-closed 0.
			return manaSpentTotalsOf(o).byTag(arg), true
		}
		return 0, true
	case "ChosenNumber":
		// The Effect's SetChosenNumber$ binding (state.ContinuousEffect.ChosenNumber,
		// threaded into Ctx by rules' replCtx for effect-created replacement
		// bodies, task wildgrowth1: torgal_a_fine_hound / communal_brewing /
		// wildgrowth_archaic's "enters with an additional +1/+1 counter for
		// each ..." body). Bound ONCE when the Effect was created, against the
		// trigger's own context, so the body reads the frozen number wherever
		// the entry lands. The VERDICT is the bound flag (Ctx.ChosenNumberBound,
		// set only by rules' seedEffectReplCtx on effect-created matches): an
		// unbound context is UNRESOLVED, so every EvalCountOK consumer keeps
		// its pre-wildgrowth fail direction for the Choose-event population
		// whose ChosenNumber lives on state.Object.ChosenNumber and never
		// reaches here -- CheckSVarHolds fails open, a numeric filter RHS
		// (void's cmcEQX through resolveNumericRHS) never matches -- instead
		// of enforcing a meaningless zero. A bound zero is a real binding and
		// evaluates (torgal with no Dogs/Wolves on the board).
		if c.ChosenNumberBound {
			return c.ChosenNumber, true
		}
		// The Choose-event population (effects/choose.go's ChooseNumber,
		// the as-enters number choice): the answer lives on the SOURCE
		// object's ChosenNumber, folded from the logged choice. Aether
		// Spike's "counter unless its controller pays X, where X is the
		// chosen number", Galvanic Discharge, Die Young -- 52 supported
		// carriers whose amount read an unresolved zero (fuzz-cov3). A
		// source that never chose reads the field's zero, which is also
		// Forge's reading of an unset chosen number.
		if o := g.Obj(c.Source); o != nil {
			return o.ChosenNumber, true
		}
		return 0, false
	case "ChosenSize":
		// Forge's Count$ChosenSize (CardUtil.getChosenCards().size()): the
		// number of CARDS the current resolution's ChooseCard chain has
		// chosen -- the same set Defined$ ChosenCard resolves (effects/
		// context.go's definedSpec case), read with the same precedence so a
		// count and a defined fetch can never disagree: the resolution's
		// bound Ctx.Chosen when it is live, else the source object's
		// event-backed Chosen list (the Choose "chosen" fold), which is what
		// a re-entry after a suspended ask reads. Player entries (a
		// ChoosePlayer's half) are not cards and do not count. A legitimate
		// zero (Feather, Radiant Arbiter's MinAmount$ 0 ask answered with
		// nothing) is exactly that -- the /Op suffix (/Times.2, the
		// UnlessCost$ CopyCost pricing) folds the zero like any other.
		// resolutionChosenCards is the shared chosen-card read (context.go's
		// Defined$ ChosenCard case, copy.go's DefinedTarget$ ChosenCard).
		chosen := resolutionChosenCards(g, c)
		n := int32(0)
		for _, t := range chosen {
			if !t.IsPlayer {
				n++
			}
		}
		return n, true
	case "YourStartingLife":
		return h.StartingLife(), true
	case "YourLifeTotal":
		if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
			return 0, true
		}
		return g.Players[c.Controller].Life, true
	case "PlayerCountPlayers":
		return int32(g.AliveCount()), true
	case "PlayerCountOpponents":
		return int32(g.AliveCount() - 1), true
	case "ThisTurnCast":
		// Task 17 (Storm): spells cast this turn by anyone, read off the
		// log via h.CastThisTurn() so a replay derives the same count. The
		// classic idiom is Count$ThisTurnCast/Minus1 (storm copies the spell
		// once per spell cast before it, i.e. everyone's casts minus itself).
		return int32(h.CastThisTurn()), true
	case "TotalCommanderCastFromCommandZone", "CommanderCastFromCommandZone":
		// Both Forge spellings read the resolving controller's own
		// command-zone commander casts over the whole game — log-derived
		// through the Host like CastThisTurn, so replay derives the same
		// number and the same provenance read the CR 903.8 commander tax.
		return h.CommanderCastsFromCommandZone(c.Controller), true
	case "RememberedNumber":
		if c.ExchangeNumberBound {
			return c.ExchangeNumber, true
		}
		// Forge's Count$RememberedNumber is the executing ability's remembered
		// count -- the same list evalRememberedOK's Amount head reads, so it
		// applies the same capture exclusion: Forge's host remembered list is
		// never seeded with the event object the trigger fired on (a body reads
		// that through the separate Triggered* family). Use the one-home helper
		// rememberedExcludingCapture, exactly as the Amount head does, so a
		// firing trigger's ctx -- seeded Remembered == Captured == its event
		// capture -- does not overcount by that capture. A caller that already
		// passed a capture-excluded ctx (effImmediateTrigger's TriggerAmount$
		// read) is unchanged: the helper is idempotent there (its instance
		// capture is disjoint from its remembered set). Five corpus
		// ImmediateTrigger lines and 38 files elsewhere carry it.
		//
		// A DB$ FlipCoin RememberNumber$ publication takes precedence: Forge's
		// FlipCoinEffect writes the flip's rememberedNumber (Yusri's "If you
		// won five flips this way" gates Count$RememberedNumber), and the flip
		// resolves in the SAME chain the reader runs, so the remembered number
		// is the flip count, not the remembered-object count.
		if c.FlipMemory != nil && c.FlipMemory.RememberNumberKind != "" {
			return c.FlipMemory.RememberNumber, true
		}
		// A RememberCounteredCMC$ binding takes the same precedence: the
		// remembered number is the countered spell's mana VALUE (a counter
		// rider sizes off it), not a count of remembered entries.
		if c.RememberedCMCBound {
			return c.RememberedCMC, true
		}
		return int32(len(rememberedExcludingCapture(h, c))), true
	case "RememberedSize":
		// Forge's RememberedSize is the HOST CARD's remembered list -- the
		// persistent list riders (RememberDiscarded$/RememberCountered$/
		// RememberChosen$/RememberControlled$/RememberSacrificed$) add to and
		// Cleanup's ClearRemembered$ clears. In this engine that list is the
		// SOURCE object's event-backed Remembered; the ctx-level list also
		// carries a trigger's captured event object, which is NOT part of
		// Forge's host list (the same exclusion iterationBase applies). A
		// resolution with no source object falls back to the ctx list.
		// RememberRevealed$ is one of the riders that fills this list: effReveal
		// writes BOTH halves (the rememberMilled discipline), so the source
		// read serves it too -- a ctx-first preference here would double-count
		// on every trigger resolution (Mind Maggots: ctx = the trigger's event
		// capture + its own RememberDiscarded$ entries).
		if o := g.Obj(c.Source); o != nil {
			return int32(len(o.Remembered)), true
		}
		return int32(len(c.Remembered)), true
	case "LifeOppsLostThisTurn":
		// The total life the controller's OPPONENTS have lost this turn
		// (Rakdos, Lord of Riots). Each opponent's loss comes from the Host's
		// log-derived LifeLostThisTurn, so the count is replay-derivable.
		if c.Controller < 0 {
			return 0, true
		}
		var n int32
		for _, p := range g.AliveFrom(0) {
			if p != c.Controller {
				n += h.LifeLostThisTurn(p)
			}
		}
		return n, true
	case "DamageOppsTakenThisTurn":
		// The total damage the controller's OPPONENTS were dealt this turn
		// (kw:Bloodthirst, CR 702.54, is the reader). Each opponent's take
		// comes from the Host's log-derived DamageTakenThisTurn (player-targeted
		// Damage events only, the same fold the TargetedPlayer$DamageThisTurn
		// head reads), so the count is replay-derivable. The sum answers BOTH
		// Bloodthirst shapes: a fixed N's condition ("an opponent was dealt
		// damage this turn") is the sum compared GT0 -- damage amounts are
		// positive, so a positive sum is exactly "at least one opponent was
		// dealt damage" -- and Bloodthirst X's amount ("enters with X +1/+1
		// counters, where X is the damage dealt to your opponents this turn",
		// Petrified Wood-Kin) is the sum itself.
		if c.Controller < 0 {
			return 0, true
		}
		var n int32
		for _, p := range g.AliveFrom(0) {
			if p != c.Controller {
				n += h.DamageTakenThisTurn(p)
			}
		}
		return n, true
	case "LifeYouLostThisTurn":
		// The total life the controller LOST this turn -- Luminarch
		// Ascension's and Boarded Window's end-step CheckSVar$ gate ("if you
		// didn't lose life this turn"). The same log-derived Host fold
		// LifeOppsLostThisTurn sums over the opponents, read for the
		// controller alone, so a replay derives the same count. Unmodelled,
		// the gate failed closed and Luminarch Ascension never once gained a
		// quest counter (cardfuzz coverage audit).
		if c.Controller < 0 {
			return 0, true
		}
		return h.LifeLostThisTurn(c.Controller), true
	case "Party":
		// CR 700.8: the controller's party -- one each of Cleric, Rogue,
		// Warrior and Wizard among the creatures they control, a creature
		// filling at most one role (partySize). 39 raw corpus carriers
		// (Archpriest of Iona, Squad Commander, Nimble Trapfinder's gates and
		// every "for each creature in your party" amount).
		if c.Controller < 0 {
			return 0, true
		}
		return partySize(g, c), true
	case "LifeYouGainedThisTurn":
		// The total life the controller GAINED this turn — the CheckSVar$ gate
		// behind the "At the beginning of each end step, if you gained 4 or
		// more life this turn" family (Angelic Accord, Resplendent Angel,
		// Valkyrie Harbinger; 86 raw corpus Count$ lines). Folded from the log
		// through the Host's LifeGainedThisTurn like LifeOppsLostThisTurn, so
		// a replay derives the same count.
		if c.Controller < 0 {
			return 0, true
		}
		return h.LifeGainedThisTurn(c.Controller), true
	case "YouDrewThisTurn":
		// The number of cards the controller DREW this turn — Elenda and
		// Azor's `SVar:Y:Count$YouDrewThisTurn` feeding `TokenAmount$ Y`
		// ("create a number of 1/1 black Vampire Knight creature tokens with
		// lifelink equal to the number of cards you've drawn this turn") and
		// the 29-carrier raw corpus family behind it. The same log fold the
		// PlayerCount$CardsDrawn property reads (Host.CardsDrawnThisTurn,
		// rules' bridge for the Smuggler's Share family), so the head and
		// the property can never drift apart; derived from the event log —
		// every events.Draw since the last TurnChange, the opening deal
		// naturally invisible behind turn one's own TurnChange — so a replay
		// derives the same count.
		if c.Controller < 0 {
			return 0, true
		}
		return h.CardsDrawnThisTurn(c.Controller), true
	case "CountersAddedThisTurn":
		// Count$CountersAddedThisTurn <KIND> <Player> <ObjectSpec>.
		// Keep malformed or unsupported shapes unresolvable: CheckSVar
		// distinguishes that from an evaluated zero.
		parts := strings.Fields(arg)
		if len(parts) == 3 && c.Controller >= 0 && countersAddedThisTurnArgsKnown(parts[0], parts[1], parts[2]) {
			// The measured grammar needs only You and Source: Card.Self and
			// Card.EffectSource resolve from Source, while the other forms are
			// object/player predicates. Do not pass c.SpecContext here: handing
			// its resolution slices through the Host interface makes c escape,
			// allocating on the Derived hot path.
			sc := SpecContext{You: c.Controller, Source: c.Source,
				EffectiveNames: c.EffectiveNames, DerivedTypes: c.EffectiveTypes}
			return h.CountersAddedThisTurn(parts[0], parts[1], parts[2], sc), true
		}
	case "CountersRemovedThisTurn":
		// Count$CountersRemovedThisTurn <KIND> <Player> — the number of counters
		// of KIND the named players have PAID or LOST this turn (Creative
		// Energy's cost engine: Blaster Hulk's `Amount$ Count$CountersRemovedThisTurn
		// ENERGY You` cast discount and Izzet Generatorium's `CheckSVar$ … |
		// SVarCompare$ GE4` paid-or-lost-four activation gate — 2 of the 3 corpus
		// carriers; the third, Churning Reservoir, counts OBJECT-counter removals
		// through an object spec plus a /Plus.X op, which this build does not
		// resolve: it falls through to the (0,false) tail below). A payment and a
		// loss both leave the player's pool through the ONE event shape a grant
		// uses — a negative-Amount PlayerCounterChange (rules/mana.go's PayEnergy
		// settle) — so the fold over that event since the last TurnChange, through
		// the Host like LifeLostThisTurn, is replay-derivable. KIND matches
		// case-insensitively (the same read YourCounters takes); the Player spec
		// resolves over the living seats through MatchesPlayerSpec (You/Opponent/
		// Player/Any and their qualifiers — a qualifier MatchesPlayerSpec does not
		// know fails closed to an empty set, the documented filter convention); a
		// spec whose BASE is not a player-spec base (an object spec) leaves the
		// head unresolvable — (0,false), never a fake evaluated zero.
		kind, spec, _ := strings.Cut(arg, " ")
		spec = strings.TrimSpace(spec)
		if kind != "" && spec != "" && playerSpecBaseKnown(spec) && c.Controller >= 0 {
			var n int32
			for _, p := range g.AliveFrom(0) {
				if MatchesPlayerSpec(g, spec, p, c.Controller) {
					n += h.CountersRemovedThisTurn(p, kind)
				}
			}
			return n, true
		}
	case "YourTurns":
		// How many of the game's turns have begun with the controller as the
		// active player, current turn included (Serra Avenger's "your first,
		// second, or third turns of the game"). Log-derived through the Host
		// like LifeOppsLostThisTurn, so a replay derives the same number.
		return h.TurnsTaken(c.Controller), true
	case "CardPower":
		if o := g.Obj(c.Source); o != nil && o.Face() != nil {
			return refPower(h, o, false), true
		}
		return 0, true
	case "CardBasePower":
		if o := g.Obj(c.Source); o != nil && o.Face() != nil {
			return h.BasePower(c.Source), true
		}
		return 0, true
	case "CardToughness":
		if o := g.Obj(c.Source); o != nil && o.Face() != nil {
			return refToughness(h, o, false), true
		}
		return 0, true
	case "AttackersDeclared":
		// Count$AttackersDeclared: the attackers declared THIS turn — the Raid
		// family's "attacked this turn" read (Bloodsoaked Champion's
		// CheckSVar$ RaidTest activation gate plus 10 ConditionCheckSVar$
		// bodies). Folded from the event log through the Host (rules'
		// Engine.AttackersThisTurn) so a replay derives the identical number,
		// the same discipline CastThisTurn takes.
		return int32(h.AttackersThisTurn()), true
	case "ColorsColorIdentity":
		// Count$ColorsColorIdentity: the number of colours in the resolving
		// controller's commanders' colour identity (War Room's
		// "SVar:X:Count$ColorsColorIdentity" driving "{3}, {T}, Pay life equal
		// to the number of colors in your commanders' color identity: Draw a
		// card", the corpus's only carrier). Read through the Host's
		// CommanderIdentityColourCount like the other log/state-derived heads
		// (LifeLostThisTurn, TurnsTaken), so a replay derives the identical
		// count. An empty identity (no commander, or a colourless one) is a
		// real, resolvable 0 — the gate that withholds the ability outside the
		// Commander format is ActivationGameTypes$, not this count.
		if c.Controller < 0 {
			return 0, true
		}
		return int32(h.CommanderIdentityColourCount(c.Controller)), true
	}

	// Count$YourCounters<KIND> — the resolving controller's own player
	// counters of KIND (Forge's Count$YourCounters* family, measured 31
	// corpus files at the current pin: YourCountersExperience 15 lines,
	// YourCountersEnergy 15, YourCountersRAD 1). The suffix upper-cased is
	// Forge's counter-kind name; the stored kinds are the CounterType$ text
	// the granting script wrote ("ENERGY" from Razorfield Ripper,
	// "Experience" from Otharri, "RAD" from Radaway), so the read matches
	// case-insensitively and sums any same-kind entries — deterministic
	// either way, since the slice is insertion order and a kind is written
	// one way per card. The read is a plain read of Player.Counters, which
	// events.PlayerCounterChange folds, so it is event-backed and
	// replay-derivable; no new event or provenance is needed. Razorfield
	// Ripper's SVar:X:Count$YourCountersEnergy drives its attack pump,
	// Localized Destruction's and Aether Refinery's DB$ ChooseNumber
	// Max$ Count$YourCountersEnergy bounds their may-pay-{E} ask. A
	// controller out of range (no resolution in flight) reads 0.
	if rest, ok := strings.CutPrefix(head, "YourCounters"); ok {
		if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
			return 0, true
		}
		n := int32(0)
		for _, pc := range g.Players[c.Controller].Counters {
			if strings.EqualFold(pc.Kind, rest) {
				n += pc.N
			}
		}
		return n, true
	}

	// PlayerCount<Players|Opponents|RegisteredOpponents>$<Property> — per-
	// group extreme properties. "Players" spans every living player,
	// "Opponents" every living player but the resolving controller, the same
	// groups the bare PlayerCountPlayers/PlayerCountOpponents heads count
	// (RegisteredOpponents is Forge's game-start opponent set, read here as
	// the same living-opponent group). Two property families are resolvable:
	// the life-total extremes (Vampire Lacerator's ConditionCheckSVar$
	// OpponentSmallest: PlayerCountOpponents$LowestLifeTotal, GE11 — "you
	// lose 1 life unless an opponent has 10 or less life") and the
	// count/counted-quantity extremes playerCountExtreme answers below. Any
	// other property is NOT resolvable: (0, false) — the same verdict
	// Count$Valid's UnknownPredicates takes — so a gate over one fails per
	// its caller's documented direction rather than enforcing a fake zero.
	// An empty group also fails unresolvable for a life extreme (lifeExtreme
	// reports no extreme), for the same reason: a threshold compared against
	// an absent extreme is not readable either.
	//
	// The `Amount` property IS resolvable on the living groups (and the
	// Registered spellings, which read the same living sets): Forge's
	// property `Amount` counts 1 per group member, so
	// PlayerCountOpponents$Amount is the opponent count — the dominant
	// "one each" bound the corpus names SVar:OneEach (99 raw Opponents +
	// 52 raw Players lines at the pfpe1 gate) and the per-player target
	// bound the TargetsForEachPlayer$ shape needs (Havoc Eater's
	// TargetMax$ X with SVar:X:PlayerCountOpponents$Amount).
	if rest, ok := strings.CutPrefix(head, "PlayerCountPlayers$"); ok {
		if strings.HasPrefix(rest, "Counters.") {
			kind := strings.TrimPrefix(rest, "Counters.")
			var total int32
			for _, p := range g.AliveFrom(0) {
				if kind == "ALL" {
					for _, counter := range g.Players[p].Counters {
						total += counter.N
					}
				} else {
					total += g.Players[p].Counter(strings.ToUpper(kind))
				}
			}
			return total, true
		}
		if n, ok2 := playerGroupCount(g.AliveFrom(0), rest); ok2 {
			return n, true
		}
		if n, ok2 := lifeExtreme(g, g.AliveFrom(0), rest); ok2 {
			return n, true
		}
		if n, ok2 := hasPropertyLostLifeCount(h, g.AliveFrom(0), rest); ok2 {
			return n, true
		}
		if n, ok2 := hasPropertyStateBacked(h, g, c, g.AliveFrom(0), rest, arg); ok2 {
			return n, true
		}
		if n, ok2 := playerCountCondition(h, g, c, g.AliveFrom(0), rest, arg); ok2 {
			return n, true
		}
		return playerCountExtreme(h, g, c, g.AliveFrom(0), rest, arg)
	}
	if rest, ok := strings.CutPrefix(head, "PlayerCountRegisteredOpponents$"); ok {
		if n, ok2 := playerGroupCount(opponentGroup(g, c), rest); ok2 {
			return n, true
		}
		if n, ok2 := playerCountCondition(h, g, c, opponentGroup(g, c), rest, arg); ok2 {
			return n, true
		}
		// Forge's REGISTERED opponents — the opponents registered at game
		// start (Bloodchief Ascension's "if an opponent lost 2 or more life
		// this turn" gate). No registered-membership list survives a replay
		// here, so the group reads as the same living-opponent set
		// PlayerCountOpponents$ counts; the property dispatch below is shared
		// with PlayerCountDefinedRegistered.Other$ (same group), so the
		// HasPropertywasDealtCombatDamageThisTurnBy carriers on this group
		// (Blitzball's legendary creature, Estinien Varlineau's
		// Card.Self,Dragon) resolve through the same code.
		if n, ok2 := hasPropertyStateBacked(h, g, c, opponentGroup(g, c), rest, arg); ok2 {
			return n, true
		}
		return playerCountDefinedRegistered(h, g, c, opponentGroup(g, c), rest, arg)
	}
	if head == "OppGreatestLifeTotal" {
		return lifeExtreme(g, opponentGroup(g, c), "HighestLifeTotal")
	}
	if rest, ok := strings.CutPrefix(head, "PlayerCountOpponents$"); ok {
		if n, ok2 := playerGroupCount(opponentGroup(g, c), rest); ok2 {
			return n, true
		}
		if n, ok2 := lifeExtreme(g, opponentGroup(g, c), rest); ok2 {
			return n, true
		}
		if n, ok2 := hasPropertyLostLifeCount(h, opponentGroup(g, c), rest); ok2 {
			return n, true
		}
		if rest == "HasPropertycontrolsCreature.powerGE4" && arg == "" {
			// Yojimbo's chapter IV counts opponents, not creatures. Read the
			// battlefield and derived power (including continuous effects).
			seen := make(map[state.PlayerID]bool)
			for i := range g.Objs {
				o := &g.Objs[i]
				if o.Zone == state.ZBattlefield && o.Controller != c.Controller &&
					h.IsCreature(o.ID) && h.Power(o.ID) >= 4 {
					seen[o.Controller] = true
				}
			}
			var n int32
			for _, p := range opponentGroup(g, c) {
				if seen[p] {
					n++
				}
			}
			return n, true
		}
		if n, ok2 := hasPropertyStateBacked(h, g, c, opponentGroup(g, c), rest, arg); ok2 {
			return n, true
		}
		if n, ok2 := playerCountCondition(h, g, c, opponentGroup(g, c), rest, arg); ok2 {
			return n, true
		}
		return playerCountExtreme(h, g, c, opponentGroup(g, c), rest, arg)
	}
	// PlayerCountHasLost$<Property> — Forge's group of players who have LOST
	// the game (CR 104.2-3; a concession counts). `Amount` counts them, so
	// Hot Pursuit's `CheckSVar$ PlayerCountHasLost$Amount | SVarCompare$
	// GE2` is "if two or more players have lost the game". Read off the live
	// seat set (state.Player.Lost), which events.Apply's PlayerLost fold sets
	// and Clone copies, so a replay derives the same count. Any other
	// property fails closed: the head has no other corpus reader (measured:
	// only hot_pursuit and rampant_frogantua carry it).
	if rest, ok := strings.CutPrefix(head, "PlayerCountHasLost$"); ok {
		// The /Op count suffix applies like every other property head's
		// (Rampant Frogantua's `PlayerCountHasLost$Amount/Times.10` — its
		// +10/+10-per-lost-player SVar). Split before the name check so the
		// suffix does not make the exact-name compare miss.
		name, op, hasOp := strings.Cut(rest, "/")
		if strings.TrimSpace(name) == "Amount" {
			var n int32
			for i := range g.Players {
				if g.Players[i].Lost {
					n++
				}
			}
			if hasOp {
				n = applyCountOp(n, op)
			}
			return n, true
		}
		return 0, false
	}
	// PlayerCountDefinedPlayer.PlayerUID_RelativePlayerUID$<Property> —
	// Forge's per-player property group whose "defined player" is the player
	// the property is being evaluated FOR (the relative-player read). The
	// only corpus property is StartingLife, with the /Op suffix
	// (Anya, Merciless Angel's SVar:Z and Game Over's SVar:Y both spell
	// `...StartingLife/HalfDown` — "half THEIR starting life total"). The
	// engine carries no PER-seat starting total, so the read is the one
	// game-wide opening total effects.Host.StartingLife reports; every corpus
	// carrier is a Constructed/Commander game where all seats open equal, so
	// the game-wide value IS each player's starting life. A caller that
	// evaluates this head with Controller = the member (playerCountCondition's
	// per-member SVar resolution, and the ordinary static/effect reads for a
	// self-targeting carrier) therefore gets the right member's threshold.
	if rest, ok := strings.CutPrefix(head, "PlayerCountDefinedPlayer.PlayerUID_RelativePlayerUID$"); ok {
		return relativePlayerProperty(h, rest)
	}
	// PlayerCountDefinedRegistered$<Property> — the group of registered
	// players. The two spellings are matched EXACTLY (never the wider
	// PlayerCountDefined prefix: the corpus's other PlayerCountDefined*
	// groups — DefinedRememberedOwner, DefinedNonTriggeredTarget,
	// DefinedActivePlayer, … — need referent machinery this head does not
	// build and stay fail-closed at the fallthrough below):
	//
	//   - PlayerCountDefinedRegistered$: every living player, the controller
	//     INCLUDED (Knight of the Ebon Legion and Y'shtola say "if a PLAYER
	//     lost 4 or more life this turn", not "an opponent"). No
	//     registered-membership list survives a replay, so the group reads as
	//     the same living set PlayerCountPlayers$ counts — the reading the
	//     RegisteredOpponents$ arm above already documents.
	//   - PlayerCountDefinedRegistered.Other$: living players minus the
	//     resolving controller (Ludevic, Necro-Alchemist's "a player other
	//     than you lost life this turn").
	if rest, ok := strings.CutPrefix(head, "PlayerCountDefinedRegistered.Other$"); ok {
		if n, ok2 := playerGroupCount(opponentGroup(g, c), rest); ok2 {
			return n, true
		}
		if n, ok2 := playerCountCondition(h, g, c, opponentGroup(g, c), rest, arg); ok2 {
			return n, true
		}
		return playerCountDefinedRegistered(h, g, c, opponentGroup(g, c), rest, arg)
	}
	if rest, ok := strings.CutPrefix(head, "PlayerCountDefinedRegistered$"); ok {
		if n, ok2 := playerGroupCount(g.AliveFrom(0), rest); ok2 {
			return n, true
		}
		if n, ok2 := playerCountCondition(h, g, c, g.AliveFrom(0), rest, arg); ok2 {
			return n, true
		}
		return playerCountDefinedRegistered(h, g, c, g.AliveFrom(0), rest, arg)
	}

	// PlayerCountPropertyYou$<Property> — the supported members of Forge's
	// PlayerCountProperty<group>$<Property> family. HasPropertyActive reads 1
	// when the resolving controller is active, else 0 — Starting Town's
	// ETB gate reads SVar:Y:PlayerCountPropertyYou$HasPropertyActive and
	// feeds Count$Compare Y GE1.Z.4, so X is YourTurns on your turn and 4
	// off it, tapped only when X > 3. The per-turn properties below read the
	// existing replay-derived host tallies or the event-backed LandsPlayed
	// state. Unsupported properties and all other group spellings retain the
	// fail-closed unresolvable verdict.
	if rest, ok := strings.CutPrefix(head, "PlayerCountPropertyYou$"); ok {
		switch strings.TrimSpace(rest) {
		case "HasPropertyActive":
			if c.Controller == g.Active {
				return 1, true
			}
			return 0, true
		case "CardsDiscardedThisTurn":
			// The log-derived discard count (trigcost2): how many cards the
			// RESOLVING controller discarded this turn — every
			// events.IsDiscard move naming p since the last TurnChange, the
			// cost form included (Ambergris Citadel Agent's
			// "SVar:X:PlayerCountPropertyYou$CardsDiscardedThisTurn" behind a
			// Cost$ Discard<1/Hand> Draw<2/You> body reads the paid discard).
			// Derived from the event log like LifeLostThisTurn, so a replay
			// derives the same number. The OTHER group spellings of the same
			// property (PlayerCountPlayers$/Opponents$/TargetedPlayer$) keep
			// the fail-closed verdict below — no group machinery here prices
			// them, and a fake zero is worse.
			return h.CardsDiscardedThisTurn(c.Controller), true
		case "LifeLostThisTurn":
			return h.LifeLostThisTurn(c.Controller), true
		case "LandsPlayed":
			if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
				return 0, false
			}
			return g.Players[c.Controller].LandsPlayed, true
		case "RingTemptedYou":
			// The resolving controller's own "the Ring has tempted you" count
			// (CR 701.54a, folded by events.Apply's RingTemptsYou case): what
			// Frodo, Adventurous Hobbit / Frodo, Sauron's Bane's
			// ConditionCheckSVar$ NumRingTempted reads (GE2 / GE4 level-ability
			// gates). The raw count is never capped, so a gate compares, and
			// a zero means "not yet tempted" — a real read, never a fake one.
			return g.Players[c.Controller].RingTempted, true
		}
		return 0, false
	}

	// ThisTurnCast_<spec> is handled ABOVE the head/space split — a spec
	// can carry a space; see the comment at the top of this function.

	// StartingPlayer.<yes>.<no> is Forge's two-branch opening designation
	// count. Desert Cenote's StartingPlayer.0.1 feeds LT1, so only the
	// starting player gets its tapped-entry replacement; the other corpus
	// cards use different numeric branches. Parse the grammar rather than a
	// card-specific literal so every branch pair follows the current, replayed
	// designation (including an opening effect that changes it).
	if branches, ok := strings.CutPrefix(head, "StartingPlayer."); ok {
		yes, no := splitDot(branches)
		if g.IsStartingPlayer(c.Controller) {
			return yes, true
		}
		return no, true
	}

	// DifferentCounterKinds_<spec> counts distinct real counter kinds over
	// matching battlefield objects. These are the three corpus selectors;
	// other spellings are unreadable, not an evaluated zero.
	if spec, ok := strings.CutPrefix(head, "DifferentCounterKinds_"); ok {
		if arg != "" || (spec != "Card.Self" && spec != "Creature.YouCtrl" && spec != "Permanent.YouCtrl") {
			return 0, false
		}
		kinds := make(map[string]bool)
		for i := range g.Objs {
			o := &g.Objs[i]
			if o.Zone != state.ZBattlefield || !matchesZoneSpecCtx(g, spec, o.ID, c.SpecContext(c.Controller), state.ZBattlefield) {
				continue
			}
			for _, counter := range o.Counters {
				if counter.N > 0 && !state.InternalCounterMarker(counter.Kind) {
					kinds[counter.Kind] = true
				}
			}
		}
		return int32(len(kinds)), true
	}

	// CardCounters.<KIND> counts a counter kind on the source; ALL is the
	// sum over every kind (Forge's CardCounters.ALL wildcard -- Denry Klin's
	// intervening-if gate, Kyler's and Warden of the Inner Sky's X), which a
	// literal Counter("ALL") lookup can never answer because no object ever
	// carries a counter KIND named ALL.
	if kind, ok := strings.CutPrefix(head, "CardCounters."); ok {
		if o := g.Obj(c.Source); o != nil {
			if strings.EqualFold(kind, "ALL") {
				return sumCounters(o.Counters), true
			}
			return o.Counter(kind), true
		}
		return 0, true
	}
	// Kicked.<yes>.<no> is <yes> when the source was kicked, else <no>.
	// A pending cast's announcement ask reads it BEFORE payment stamps the
	// stack object, so Ctx.PendingKicked (rules' targetBoundCtx binding) is
	// ORed with the object's FlagKicked; at resolution no pending cast exists
	// and the object read is authoritative.
	if rest, ok := strings.CutPrefix(head, "Kicked."); ok {
		yes, no := splitDot(rest)
		if c.PendingKicked {
			return yes, true
		}
		if o := g.Obj(c.Source); o != nil && o.CastFlags&state.FlagKicked != 0 {
			return yes, true
		}
		return no, true
	}
	// PromisedGift.<yes>.<no> is <yes> when the source's cast promised an
	// opponent a gift (CR 702.168), else <no> -- Forge's
	// Count$PromisedGift.2.1 family (Wear Down's destroy-two, Long River's
	// Pull's X/Y, Valley Rally's first strike). The read is Object.PromisedGift,
	// the SAME one home the PromisedGift filter predicate reads (folded by
	// events.GiftPromise), so the matcher and the count can never disagree. A
	// missing source, a card never cast, and a stack copy (whose fresh object
	// carries no promise) all read the <no> branch, the modelled-head
	// convention. A malformed body with a missing branch fails closed.
	if rest, ok := strings.CutPrefix(head, "PromisedGift."); ok {
		yes, no, found := strings.Cut(rest, ".")
		if !found || strings.TrimSpace(yes) == "" || strings.TrimSpace(no) == "" {
			return 0, false
		}
		promised := false
		if c.PromisedGiftOverride != nil {
			promised = *c.PromisedGiftOverride
		} else if o := g.Obj(c.Source); o != nil {
			promised = o.CastFlags&state.FlagPromisedGift != 0
		}
		tok := no
		if promised {
			tok = yes
		}
		return evalCountOperand(h, c, tok, depth), true
	}
	// Foretold.<ifTrue>.<ifFalse> is <ifTrue> when the resolving source was
	// cast foretold (CR 702.126a -- the pay-time FlagForetold provenance,
	// the same read Kicked makes), else <ifFalse>. The operands resolve
	// through the same operand machinery evalCompare's branches use (a
	// literal, or an SVar name resolved recursively -- Starnheim Unleashed's
	// Count$Foretold.X.1 reads the announced X through the face's SVar
	// table), with the same depth discipline. A carrier missing a branch is
	// a corpus bug: fail closed (0, false) rather than answer a half body.
	if rest, ok := strings.CutPrefix(head, "Foretold."); ok {
		yes, no, found := strings.Cut(rest, ".")
		if !found || strings.TrimSpace(yes) == "" || strings.TrimSpace(no) == "" {
			return 0, false
		}
		foretold := false
		if o := g.Obj(c.Source); o != nil {
			foretold = o.CastFlags&state.FlagForetold != 0
		}
		if foretold {
			return evalCountOperand(h, c, yes, depth), true
		}
		return evalCountOperand(h, c, no, depth), true
	}
	// NotedNumber is the number a trigger's Execute$ body last noted onto
	// the source card (DB$ Pump | NoteNumber$ <expr> -- Lupine Harbingers'
	// exile trigger noting Count$YourTurns). Read off the card the ETB
	// replacement resolves over (c.Source), the same object events.NotedNumber
	// wrote; a card with no note reads 0.
	if head == "NotedNumber" {
		if o := g.Obj(c.Source); o != nil {
			return o.NotedNumber, true
		}
		return 0, true
	}
	// UrzaLands.<assembled>.<not assembled> is <assembled> when the controller
	// controls at least one of each Urza land subtype on the battlefield
	// (Urza's Mine, Urza's Tower, Urza's Power-Plant), else <not assembled>.
	// The subtypes are matched on the Types line, where Power-Plant is
	// hyphenated -- the card Name's "Urza's Power Plant" is a different
	// string and matching it would be exactly the defect this head fixes.
	if rest, ok := strings.CutPrefix(head, "UrzaLands."); ok {
		assembled, notAssembled := splitDot(rest)
		if controlsAllUrzaLands(g, c.Controller) {
			return assembled, true
		}
		return notAssembled, true
	}

	// Count$ThisTurnEntered_<Dest>[_from_<Origin>]_<Valid> counts the cards
	// ADDED to zone <Dest> this turn (optionally only those that came from
	// <Origin>) matching <Valid> -- Forge's CardUtil.getThisTurnEntered over
	// the per-zone getCardsAddedThisTurn lists. The list is state.Entered,
	// appended once per move by events.Move and cleared at TurnChange, so a
	// replay folds the identical count. A card listed twice (it entered the
	// zone twice) counts twice, exactly like Forge's per-add list; a card
	// that has since moved on is still listed and its validity is evaluated
	// against the object wherever it now lives -- the same live-card read
	// Forge's getValidCards applies to the zone list. Gravelighter's "draw a
	// card if a creature died this turn" is the corpus carrier.
	if rest, ok := strings.CutPrefix(head, "ThisTurnEntered"); ok && strings.HasPrefix(rest, "_") {
		if arg != "" {
			rest += " " + arg
		}
		return evalThisTurnEntered(g, c, rest[1:])
	}

	// Count$<Predicate>.<yes>.<no> — Forge's yes/no branch heads: the value
	// is the first number when the predicate holds, the second when it does
	// not (Count$Morbid.1.0 ×33 and Count$Monarch.1.0 ×10 are the corpus's
	// dominant spellings). wasCastFromGraveyard is modelled below — the
	// resolving source's graveyard-origin cast bits (the Increasing cycle's
	// Count$wasCastFromGraveyard.10.5, 11 corpus lines); the remaining
	// exotic predicates — Delirium, Void, Adamant_<n>.<colour> —
	// stay unmodelled and degrade to zero (Blessing is read below off the
	// CR 702.131 latch). Morbid is
	// CR 702.53's "a creature died this turn": a creature entered a graveyard
	// FROM THE BATTLEFIELD this turn, folded off the same state.Entered list
	// ThisTurnEntered_ reads (a battlefield→graveyard MoveZone is exactly a
	// death, sacrifice included), so a replay derives the identical answer.
	// Monarch is the resolving controller's current designation (the same
	// state g.IsMonarch answers for a CheckDefinedPlayer$ .isMonarch spec).
	if dot := strings.IndexByte(head, '.'); dot > 0 {
		if holds, known := cov3BranchHolds(h, c, head[:dot], depth); known {
			// The fuzz-cov3 branch predicates (Delirium, Metalcraft,
			// Hellbent, FatefulHour, Landfall, Void): branch tokens through
			// countBranchOperand, the Threshold sibling's read.
			yesTok, noTok, _ := strings.Cut(head[dot+1:], ".")
			return countBranchOperand(h, c, holds, yesTok, noTok, depth), true
		}
		switch head[:dot] {
		case "wasCastFromGraveyard":
			// The resolving source was CAST FROM A GRAVEYARD (CR 601.2b's
			// alternative-cost provenance): any graveyard-origin cast bit —
			// FlagFlashback, FlagHarmonize or FlagEscaped — holds it. This is
			// the same bit test the Card.wasCastFromGraveyard filter predicate
			// and its compiled twin share (effects/filter.go,
			// effects/compiled_predicate.go); a nil/missing source reads
			// false, and so does a stack copy (IsCopy — a copy was never cast,
			// even though StackCopy preserves the original's flags). The
			// branch tokens resolve through resolveCountOperand, not splitDot:
			// the_final_days' YES branch is the SVar X
			// (Count$wasCastFromGraveyard.X.2, X = Count$ValidGraveyard
			// Creature.YouCtrl), the Compare head's evalCountOperand recursion
			// precedent; an unresolvable token degrades to 0, never wedges.
			yesTok, noTok, _ := strings.Cut(head[dot+1:], ".")
			holds := state.ObjectWasCastFromGraveyard(g.Obj(c.Source))
			if holds {
				y, ok := resolveCountOperand(h, c, yesTok, depth)
				if !ok {
					y = 0
				}
				return y, true
			}
			n, ok := resolveCountOperand(h, c, noTok, depth)
			if !ok {
				n = 0
			}
			return n, true
		case "wasCastFromYourHandByYou":
			// The resolving source was cast from ITS OWN CONTROLLER's hand by
			// that controller (the Myojin cycle's etbCounter CheckSVar$ gate:
			// "enters with a divinity counter on it if you cast it from your
			// hand", 12 corpus carriers). An ordinary hand-origin cast carries
			// no CastFlags bit — the flags mark alternative costs and origins
			// only — so the provenance is the object's latest PutOnStack
			// (Host.WasCastFromHandByYou's log scan, replay-derivable like
			// CastThisTurn); a copy was never cast, and a card never put on
			// the stack (cheated into play) reads false, the same guards the
			// wasCastFromGraveyard case takes. The branch tokens resolve
			// through resolveCountOperand, the same machinery.
			yesTok, noTok, _ := strings.Cut(head[dot+1:], ".")
			holds := false
			if o := g.Obj(c.Source); o != nil && !o.IsCopy {
				if h != nil {
					holds = h.WasCastFromHandByYou(c.Source, o.Controller)
				}
			}
			if holds {
				y, ok := resolveCountOperand(h, c, yesTok, depth)
				if !ok {
					y = 0
				}
				return y, true
			}
			n2, ok := resolveCountOperand(h, c, noTok, depth)
			if !ok {
				n2 = 0
			}
			return n2, true
		case "wasCastFromYourHand":
			// The BARE (no "ByYou") hand-provenance branch head (task
			// castprov3, see_the_truth's SVar:X:Count$wasCastFromYourHand.1.3 —
			// "put one of those cards into your hand ... If this spell was cast
			// from anywhere other than your hand, put each of those cards into
			// your hand instead"): the resolving source's latest cast came from
			// a hand — ANY caster's hand, the player scoping the ByYou twin
			// carries being absent here. The same guards the ByYou case takes:
			// the provenance is the object's latest PutOnStack
			// (Host.WasCastFromHand's log scan, replay-derivable), a copy was
			// never cast, a card never put on the stack (cheated into play)
			// reads false. Branch tokens through resolveCountOperand, the same
			// machinery.
			yesTok, noTok, _ := strings.Cut(head[dot+1:], ".")
			holds := false
			if o := g.Obj(c.Source); o != nil && !o.IsCopy {
				holds = h.WasCastFromHand(c.Source)
			}
			if holds {
				y, ok := resolveCountOperand(h, c, yesTok, depth)
				if !ok {
					y = 0
				}
				return y, true
			}
			n3, ok := resolveCountOperand(h, c, noTok, depth)
			if !ok {
				n3 = 0
			}
			return n3, true
		case "wasCastFromExile":
			// The resolving source was CAST FROM EXILE (task wascastfrom; the
			// delayed_blast_fireball `Count$wasCastFromExile.5.2`,
			// lifestreams_blessing `.2.0` and the ultimate_magic `.1.0`
			// carriers): the CR 601.2b provenance of an exile-origin cast —
			// foretell, warp, may-play — which carries no CastFlags bit (the
			// flags mark alternative costs and origins only), so the read is
			// the object's latest PutOnStack (Host.WasCastFromExile's log
			// scan, replay-derivable), the same discipline the hand branch
			// heads take: a copy was never cast, and a card never put on the
			// stack (cheated into play) reads false. Branch tokens resolve
			// through resolveCountOperand, the same machinery.
			yesTok, noTok, _ := strings.Cut(head[dot+1:], ".")
			holds := false
			if o := g.Obj(c.Source); o != nil && !o.IsCopy {
				if h != nil {
					holds = h.WasCastFromExile(c.Source)
				}
			}
			if holds {
				y, ok := resolveCountOperand(h, c, yesTok, depth)
				if !ok {
					y = 0
				}
				return y, true
			}
			nE, ok := resolveCountOperand(h, c, noTok, depth)
			if !ok {
				nE = 0
			}
			return nE, true
		case "IfCastInOwnMainPhase", "InOwnMainPhase":
			// CR "if you cast this spell during your main phase": the
			// yes/no branch head Forge's AbilityUtils reads as
			// Count$IfCastInOwnMainPhase.<numMain>.<numNotMain> (7 corpus
			// carriers: Return to Dust's TargetMax$ X, Might of Old
			// Krosa's NumAtt$/NumDef$, Haunting Hymn's and Careful
			// Consideration's NumCards$, Sulfurous Blast's and Summary
			// Judgment's NumDmg$). The reading is LIVE, matching
			// Forge's game.getPhaseHandler(): the current step must be a
			// main phase and the active player the resolving controller
			// -- NOT a stamp of the cast's phase, which would diverge
			// from Forge (a spell cast in a main phase but resolved in
			// another reads the resolution phase). The two spellings
			// differ only in the third conjunct: IfCastInOwnMainPhase
			// additionally requires the source to have been CAST (Forge
			// c.wasCast(), the Host.WasCast read -- the pending CR 601.2c
			// announcement ask counts, since Forge sets castFrom before
			// setupTargets), while bare InOwnMainPhase (Dose of Dawnglow's
			// Count$InOwnMainPhase.0.1 blight gate) skips that conjunct.
			// Branch tokens resolve through resolveCountOperand, the
			// sibling cases' machinery; a missing/invalid token degrades
			// to 0, never wedges.
			yesTok, noTok, _ := strings.Cut(head[dot+1:], ".")
			holds := g.Step.IsMain() && g.Active == c.Controller
			if holds && head[:dot] == "IfCastInOwnMainPhase" {
				// A copy was never cast (the sibling provenance cases' IsCopy
				// guard); an absent source reads false too. The rules-side
				// WasCast applies the same IsCopy guard, but the count head is
				// reachable with a synthetic Host, so the guard lives here.
				holds = false
				if o := g.Obj(c.Source); o != nil && !o.IsCopy {
					holds = h != nil && h.WasCast(c.Source)
				}
			}
			if holds {
				y, ok := resolveCountOperand(h, c, yesTok, depth)
				if !ok {
					y = 0
				}
				return y, true
			}
			n, ok := resolveCountOperand(h, c, noTok, depth)
			if !ok {
				n = 0
			}
			return n, true
		case "Morbid", "Monarch":
			y, n := splitDot(head[dot+1:])
			holds := false
			if head[:dot] == "Monarch" {
				holds = g.IsMonarch(c.Controller)
			} else {
				for _, en := range g.Entered {
					if en.To != state.ZGraveyard || en.From != state.ZBattlefield {
						continue
					}
					if o := g.Obj(en.Obj); o != nil && hasType(o, "Creature") {
						holds = true
						break
					}
				}
			}
			if holds {
				return y, true
			}
			return n, true
		case "Revolt":
			// CR 702.38's branch head (the corpus's two carriers: Lifecraft
			// Cavalry's SVar:Revolt:Count$Revolt.1.0 etbCounter gate and
			// Fatal Push's Count$Revolt.4.2 destroy bound): <yes> when a
			// permanent the resolving CONTROLLER controlled left the
			// battlefield this turn, else <no> -- the same Host predicate
			// the bare Condition$ Revolt gate and the rules-side Revolt$
			// clauses share, so the spellings cannot drift apart. Literal
			// branches, the Morbid/Monarch precedent.
			y, n := splitDot(head[dot+1:])
			if h.RevoltHolds(c.Controller) {
				return y, true
			}
			return n, true
		case "Blessing":
			// CR 702.131's city's-blessing branch head (10 corpus carriers:
			// Golden Demise's SVar:X:Count$Blessing.1.0 pump fork, Kumena's
			// Awakening's TrigDraw, Expel from Orazca, Anduril/Pride of
			// Conquerors' .2.1, Secrets of the Golden City's .3.2 and the
			// .0.1 "unless you have it" forks). <yes> when the resolving
			// CONTROLLER holds the one-way state.Player.Blessing latch that
			// events.Apply's BlessingChange fold writes (rules/ascend.go
			// grants it), else <no> -- the SAME bit the bare Condition$
			// Blessing gate (effects/conditions.go) and the Activation$
			// Blessing offer gate (rules/legal.go) read, so the three
			// spellings cannot drift apart. Literal-branch read via
			// splitDot, the Revolt/Morbid precedent; an out-of-range
			// controller denies, the fail-closed direction its siblings take.
			y, n := splitDot(head[dot+1:])
			if int(c.Controller) < len(g.Players) && g.Players[c.Controller].Blessing {
				return y, true
			}
			return n, true
		case "Threshold":
			// CR 702.24's Threshold branch head (7 corpus carriers: Cabal
			// Ritual's Count$Threshold.5.3 mana ritual, Thermal Blast and
			// Swirling Sandstorm's .5.x/.5.0 damage, Far Wanderings' .3.1
			// search, Grizzly Fate's .4.2 tokens, Shower of Coals' and
			// Patriarch's Desire's .4.2): <yes> when the resolving CONTROLLER
			// has Threshold active -- seven or more cards in their graveyard
			// (CR 702.24a's latch-free active-while read, the same seven the
			// Threshold keyword statics share) -- else <no>. The graveyard is
			// a plain zone read (event-backed), so a replay derives the same
			// branch. Branch tokens resolve through resolveCountOperand (the
			// wasCastFromGraveyard precedent -- a literal, an SVar name or an
			// inline expression; an unresolvable token degrades to 0, never
			// wedges), and an out-of-range controller denies, the same
			// fail-closed direction the Blessing sibling takes.
			yesTok, noTok, _ := strings.Cut(head[dot+1:], ".")
			inGrave := false
			if int(c.Controller) >= 0 && int(c.Controller) < len(g.Players) {
				inGrave = len(g.Zone(state.ZGraveyard, c.Controller)) >= 7
			}
			return countBranchOperand(h, c, inGrave, yesTok, noTok, depth), true
		case "Devotion":
			// CR 700.5's devotion head (49 corpus carriers: Aspect of Hydra's
			// Count$Devotion.Green pump, Gray Merchant of Asphodel's .Black
			// drain, Nykthos' four .Chosen lines behind a ChooseColor): the
			// number of mana symbols of ONE colour among the mana costs of the
			// permanents the resolving CONTROLLER controls (devotionCount
			// below). <Colour> is a Forge colour word or WUBRG letter
			// (colourLetter); the corpus prints only the five words plus the
			// Chosen spelling, which reads the source object's own recorded
			// colour choice (events.Choose's "color" fold -- effChooseColor's
			// deterministic "W" fallback included) so the count and the ask
			// that binds it can never disagree; an unchosen or unreadable
			// colour fails closed to the unresolvable verdict, never to a
			// fake zero.
			col := colourLetter(head[dot+1:])
			if col == 0 {
				if !strings.EqualFold(head[dot+1:], "Chosen") {
					return 0, false
				}
				src := g.Obj(c.Source)
				if src == nil {
					return 0, false
				}
				col = colourLetter(src.ChosenColor)
				if col == 0 {
					return 0, false
				}
			}
			return devotionCount(g, c.Controller, col), true
		case "DevotionDual":
			// CR 700.5's two-colour devotion head (14 corpus carriers: Mogis'
			// Count$DevotionDual.Black.Red drain and the DevotionDual spellings
			// the temples/god cycle print): the SUM of the devotion to both
			// named colours -- each hybrid symbol counts once toward EACH of
			// its colours, so the sum is the oracle's "devotion to <A> and
			// <B>". An unreadable colour fails closed to the unresolvable
			// verdict, the Devotion sibling's read.
			aTok, bTok, found := strings.Cut(head[dot+1:], ".")
			ca, cb := colourLetter(aTok), colourLetter(bTok)
			if !found || ca == 0 || cb == 0 {
				return 0, false
			}
			return devotionCount(g, c.Controller, ca) + devotionCount(g, c.Controller, cb), true
		}
	}

	// Valid / ValidZone forms count objects in a zone matching a filter (the
	// ValidAll all-zones head is the one exception -- see the branch itself).
	// A `$<Property>` suffix sums that numeric property over the matches
	// instead of counting them -- Mosswort Bridge's gate
	// `Count$Valid Creature.YouCtrl$CardPower` ("creatures you control have
	// total power 10 or greater") is the corpus shape (62 raw lines over 61
	// files: CardPower 42, CardManaCost 13, CardToughness 5). CardTypes and
	// Colors are DISTINCT-set counts over the same matches, not sums:
	// Colors counts the distinct colours among the matched permanents
	// ("the number of colors among permanents you control", Shimmercreep's
	// Vivid et al., 31 raw corpus lines, bounded by five), read through
	// ColorMaskOf so an explicit Colors: line and Devoid's colourless
	// treatment agree with the colour predicates; its single corpus op
	// suffix /LimitMax.<n> (happily_ever_after) clamps the result. An
	// unrecognised property keeps the whole token as the spec -- the
	// pre-existing fail-closed behaviour, since such a token never matched
	// anyway. The Different* distinct family IS read since diffcount1
	// (differentPropertyKindOf's case above): distinct powers/names/mana
	// values among the matches, the Augur of Autumn Coven gate's shape.
	// CreatureType and CardTypesPermanent (task diffcount2) are the same
	// distinct-set shape over a narrower vocabulary. The four extreme
	// reductions (GreatestCardPower 64 files,
	// GreatestCardManaCost 62, GreatestCardToughness 12, LeastCardPower 1;
	// 136 files total) are read: the max (or min, for Least) of the
	// property over the matches, with zero matches yielding 0 rather than a
	// sentinel. The bounded distinct-set properties' /LimitMax.<n> op
	// suffix (Colors one, CreatureType two) is honoured at
	// evalCountExprOK's generic /Op site (countDistinctLimitMax).
	isAll := head == "ValidAll"
	multi := multiCountZones(head)
	if zone, ok := countZone(head); ok || isAll || multi != nil {
		spec, prop, hasProp := strings.Cut(arg, "$")
		if !hasProp {
			spec, prop = arg, ""
		} else {
			prop = strings.TrimSpace(prop)
			switch {
			case prop == "CardPower" || prop == "CardToughness" || prop == "CardManaCost" ||
				prop == "CardTypes" || prop == "CardTypesPermanent" || prop == "Colors" ||
				prop == "CreatureType" || strings.HasPrefix(prop, "CardCounters."):
			case isExtremeProperty(prop):
			case differentPropertyKindOf(prop) != diffNone:
			default:
				// Not a recognised property (DifferentNames,
				// Different*, ...): keep the old whole-token spec read.
				spec, prop = arg, ""
			}
		}
		// CardTypes is Tarmogoyf's distinct-card-type form, not a filter:
		// count each real card type (CR 205.1) represented among the
		// selected cards once. The map is read only through len, so its
		// iteration order never reaches an event or a view. The two
		// siblings (task diffcount2) are the same distinct-set shape over a
		// narrower vocabulary: CreatureType counts distinct creature
		// subtypes (Valiant Changeling's per-type reduction),
		// CardTypesPermanent the six CR 205.2 permanent types (Korvold,
		// Gleeful Glutton's combat-damage trigger; Matzalantli's transform
		// gate, whose oracle names the six).
		var seenCardTypes map[string]bool
		if prop == "CardTypes" || prop == "CardTypesPermanent" {
			seenCardTypes = make(map[string]bool)
		}
		var seenCreatureTypes map[string]bool
		if prop == "CreatureType" {
			seenCreatureTypes = make(map[string]bool)
		}
		// The bare wasCastFromYourHand qualifier (task castprov3, Approach of
		// the Second Sun's Count$ValidStack Card.wasCastFromYourHand+Self):
		// not a filter predicate — split out per CANDIDATE object through the
		// Host's log read before the ordinary match (the ByYou family never
		// needed this here because it had no Valid* carrier; a ByYou spec
		// still routes to its own helper's absence and fails closed as
		// before, unchanged).
		hasBareHand := !strings.Contains(spec, "wasCastFromYourHandByYou") && strings.Contains(spec, "wasCastFromYourHand")
		// token$DifferentCardNames (Sandsteppe War Riders' "bolster X, where X
		// is the number of differently named artifact tokens you control";
		// also Gimbal Gremlin Prodigy, Audience with Trostani, Neriv Crackling
		// Vanguard -- 4 raw corpus lines): a SET-level qualifier the
		// per-object filter cannot express -- the count is the number of
		// DISTINCT face names among the matching tokens, not the number of
		// tokens. Stripped here and rewritten to the plain `token` predicate
		// for the per-object match; the distinctness is a seen-names set at
		// this count site (the CardTypes/Colors distinct-count precedent).
		// The filter's own read (matchPositive's token$DifferentCardNames
		// case) is the per-object half -- "is a token" -- so a non-count read
		// of the qualifier admits every matching token and narrows nothing.
		var seenTokenNames map[string]bool
		if strings.Contains(spec, "token$DifferentCardNames") {
			spec = strings.ReplaceAll(spec, "token$DifferentCardNames", "token")
			seenTokenNames = make(map[string]bool)
		}
		// An extreme property (Greatest*/Least*) folds a max/min over the
		// matches instead of a sum, so it needs its own accumulator plus a
		// seen flag -- zero matches must read 0, never an int-min/max
		// sentinel.
		extreme := isExtremeProperty(prop)
		// The fold scans candidates IN PLACE -- no materialised candidate
		// slice: Count$Valid is on the hottest condition path
		// (effects.CheckSVarHolds intervening-ifs, static gates, SVarCompare)
		// and the single-zone scan must stay allocation-free (the alloc-gate
		// budget and TestEvalCountValidZoneScanIsAllocationFree hold the
		// line). ValidAll (6 corpus Count$ValidAll carriers: Cactus Preserve
		// and Tangleweave Armor's greatest-commander-mana-value, Kefka's
		// imprinted card, Mangara/Tomik's attacking-LKI count, You Will Know
		// True Suffering's commander mana value) extends the scan to EVERY
		// card zone -- countAllZones per seat plus the ONE stack pass --
		// because a commander sits in the command zone and an imprinted card
		// in exile; a battlefield-only scan can never see them. Each
		// candidate is matched against ITS OWN zone (the way Forge evaluates
		// a ValidAll spec against the card's actual zone), so a battlefield
		// candidate keeps the whole-spec MatchesObjectCtx read and a
		// command-zone or exile candidate the per-alternative in-zone read.
		// (ValidAll also occurs outside the count head -- `Defined$ ValidAll`
		// 3 files, `ImprintCards$ ValidAll` 2; 11 carrier files total -- and
		// those two paths are still unhandled: a `Defined$ ValidAll` spec
		// fails closed in effects/context.go, an `ImprintCards$ ValidAll`
		// imprint remembers nothing. Recorded in the report.)
		// specCtx is a LOCAL, never a struct field: storing the
		// SpecContext(...)-built value in the fold struct made escape
		// analysis summarise evalCountBody's *Ctx param as leaking (the
		// struct escapes through the pointer receiver), which heap-
		// allocated EVERY caller-built Ctx on the hot layer-walk path
		// (rules/layers.go's cdaSetPT Ctx) -- exactly the allocation class
		// the alloc-gate budget and rules' Derived pin hold the line on.
		// Built once here and passed to visit as a parameter instead.
		specCtx := c.SpecContext(c.Controller)
		f := zoneCountFold{h: h, g: g, spec: spec,
			prop: prop, extreme: extreme, isLeast: isLeastProperty(prop),
			hasBareHand: hasBareHand, readsPT: SpecReadsPT(spec),
			seenTokenNames: seenTokenNames, seenCardTypes: seenCardTypes,
			seenCreatureTypes: seenCreatureTypes}
		// The Different* distinct-set property family (task diffcount1):
		// DifferentCardManaCost / DifferentCardPower / DifferentCardNames /
		// DifferentColorPair count the DISTINCT values among the matching
		// cards, not the cards themselves. Numeric values fold into a set
		// keyed by the value; names into a string set. Both are read only
		// through len, so no map ordering ever reaches an event or a view.
		if dk := differentPropertyKindOf(prop); dk != diffNone {
			f.diffKind = dk
			if dk == diffName {
				f.seenDiffNames = make(map[string]bool)
			} else {
				f.seenDiffValues = make(map[int32]bool)
			}
		}
		if multi != nil {
			// A comma-joined zone list (Count$ValidGraveyard,Exile,
			// ValidBattlefield,Graveyard, ValidBattlefield,Command --
			// fuzz-cov3): the single-zone scan over each named zone in turn,
			// the shared stack once. Each candidate is matched against its
			// own zone, the ValidAll convention.
			for _, z := range multi {
				if z == state.ZStack {
					for _, id := range g.Stack {
						f.visit(id, z, specCtx)
					}
					continue
				}
				for _, p := range g.AliveFrom(0) {
					for _, id := range g.Zone(z, p) {
						f.visit(id, z, specCtx)
					}
				}
			}
		} else if isAll {
			for _, p := range g.AliveFrom(0) {
				for _, z := range countAllZones {
					for _, id := range g.Zone(z, p) {
						f.visit(id, z, specCtx)
					}
				}
			}
			for _, id := range g.Stack {
				f.visit(id, state.ZStack, specCtx)
			}
		} else {
			// The stack is ONE shared list (state.Game.Zone returns g.Stack
			// for every seat), so a single-zone stack scan must run exactly
			// once: without this guard an N-seat table counts every stack
			// object N times -- Mindbreak Trap's MaxTgts bound and Display
			// of Power's copy count both read on the caster's own spell(s).
			// Scanned under the first alive seat, the same convention
			// rules/statics.go and rules/trigger_match.go use for the
			// shared stack. The ValidAll branch above is exempt: its stack
			// pass sits outside the seat loop already.
			for si, p := range g.AliveFrom(0) {
				if zone == state.ZStack && si > 0 {
					continue
				}
				for _, id := range g.Zone(zone, p) {
					f.visit(id, zone, specCtx)
				}
			}
		}
		if extreme {
			// A matched set with no members has no extreme: 0, per the
			// seen guard, never an int-min/max sentinel.
			if !f.seen {
				return 0, true
			}
			return f.best, true
		}
		if prop == "CardTypes" || prop == "CardTypesPermanent" {
			return int32(len(seenCardTypes)), true
		}
		if prop == "CreatureType" {
			return int32(len(seenCreatureTypes)), true
		}
		if seenTokenNames != nil {
			return int32(len(seenTokenNames)), true
		}
		if prop == "Colors" {
			return int32(bits.OnesCount8(uint8(f.colorsSeen))), true
		}
		if f.diffKind != diffNone {
			if f.seenDiffNames != nil {
				return int32(len(f.seenDiffNames)), true
			}
			return int32(len(f.seenDiffValues)), true
		}
		return f.n, true
	}
	return 0, false
}

// countersAddedThisTurnArgsKnown accepts the complete measured grammar for
// Count$CountersAddedThisTurn. Unlike the ordinary filter parser, this count
// head cannot safely treat an unknown field as a filter that matches nothing:
// CheckSVar distinguishes that evaluated zero from an unresolvable Count$.
// Keep this narrow until a corpus carrier establishes another spelling.
//
// evalCountValidSelf models Forge's `Count$ValidSelf <spec>` head: the count
// of the "Self" card when it matches <spec>, 0 otherwise. Self is the card the
// SVar is evaluated for -- the triggering event's card under
// `CheckOnTriggeredCard$` (a dies trigger's dying creature), else the
// resolving source (a `CheckSVar$` gate). It is a RECOGNISED head even when
// <spec> is outside this build's filter grammar: an unreadable argument then
// fails closed with an evaluated zero instead of falling through to the
// unknown-head verdict, so a `GE1` gate reads a real false rather than an
// unresolvable body a caller might fail open. A missing Self binding is the
// same evaluated zero.
//
// The match runs over the shared filter matcher, so every predicate the
// engine already reads -- including `greatestPower...` and its
// `ControlledBy CardController` referent -- answers here without a second
// implementation. CR 603.10: a card that left the battlefield is judged as it
// last existed there, so the trigger's LKI snapshot is preferred when it
// names Self (a dying creature's controller is reset to its owner by the
// move, and its counters with it).
func evalCountValidSelf(h Host, c *Ctx, arg string) (int32, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return 0, true
	}
	g := h.Game()
	self := c.TriggerCard
	if self == 0 {
		self = c.Source
	}
	if self == 0 {
		return 0, true
	}
	o := g.Obj(self)
	if c.LKI != nil && c.LKI.ID == self {
		o = c.LKI
	}
	if o == nil {
		return 0, true
	}
	if MatchesObjectCtx(g, arg, o, c.SpecContext(c.Controller)) {
		return 1, true
	}
	return 0, true
}

func countersAddedThisTurnArgsKnown(kind, actor, object string) bool {
	if !strings.EqualFold(kind, "Any") && !strings.EqualFold(kind, "P1P1") && !strings.EqualFold(kind, "LORE") {
		return false
	}
	if actor != "You" && actor != "Player" {
		return false
	}
	switch object {
	case "Creature", "Creature.YouCtrl", "Permanent.YouCtrl", "Card.Self", "Card.EffectSource":
		return true
	}
	return false
}

// playerSpecBaseKnown reports whether spec's base word (the text before the
// first qualifier separator) is one of the player-spec bases MatchesPlayerSpec
// resolves (You/Opponent/Other/Player/Any, matched case-insensitively). It is
// what keeps a head argument that names an OBJECT spec (Churning Reservoir's
// `Card.YouCtrl+inRealZoneBattlefield/Plus.X`) unresolvable rather than
// laundering it through an empty-player-set read as a legitimate evaluated
// zero — the fail-closed verdict (0,false), so the caller picks its own
// direction for the gate it serves.
func playerSpecBaseKnown(spec string) bool {
	base := spec
	if i := strings.IndexAny(base, ".+,"); i >= 0 {
		base = base[:i]
	}
	for _, known := range []string{"You", "Opponent", "Other", "Player", "Any"} {
		if strings.EqualFold(strings.TrimSpace(base), known) {
			return true
		}
	}
	return false
}

// evalThisTurnEntered parses a ThisTurnEntered_<Dest>[_from_<Origin>]_<Valid>
// tail -- the split Forge's own parser applies (workingCopy[0] = the head, so
// parts[0] here is <Dest>; at most five underscore parts, and a <Valid> tail
// of more than one token is rejoined). An unknown destination zone or an
// empty valid fails closed to zero rather than counting everything.
func evalThisTurnEntered(g *state.Game, c *Ctx, rest string) (int32, bool) {
	return evalThisTurnEnteredAs(g, c, c.Controller, rest)
}

// evalThisTurnEnteredAs is evalThisTurnEntered with the counted member's own
// perspective: the spec's You* qualifiers bind to `you`, not the resolving
// controller. The PlayerCount condition's per-member properties
// (Smuggler's Share's ThisTurnEntered_Battlefield_Land.YouCtrl, meaning "lands
// under THAT opponent's control") need exactly this — the counted member IS
// the filter's You.
func evalThisTurnEnteredAs(g *state.Game, c *Ctx, you state.PlayerID, rest string) (int32, bool) {
	// The optional trailing `$<Property>` sum form (Genesis of the Daleks'
	// `Count$ThisTurnEntered_Graveyard_from_Battlefield_Dalek$CardPower` --
	// "each of your opponents loses life equal to the total power of Daleks
	// that died this turn"): a recognised sum property is split off the
	// <Valid> tail and each matching entry contributes its value through the
	// shared objectProperty reader, the same per-object read the
	// `Count$Valid <spec>$CardPower` aggregate uses. An unrecognised property
	// keeps the WHOLE token as <Valid> -- the fail-closed read the
	// Count$Valid family also takes for a property it does not model, so a
	// `token$DifferentCardNames`-style qualifier can never be mistaken for a
	// sum. Only the count path splits here; parseThisTurnEnteredSpec stays
	// the one grammar PlayerCount validation shares, unchanged.
	prop := ""
	if spec, tail, ok := strings.Cut(rest, "$"); ok && modeledProperty(tail) {
		rest, prop = spec, strings.TrimSpace(tail)
	}
	dest, origin, valid, parsed := parseThisTurnEnteredSpec(rest)
	if !parsed {
		return 0, false
	}
	return countEnteredAs(g, c, you, dest, origin, valid, prop)
}

// parseThisTurnEnteredSpec splits a ThisTurnEntered_<Dest>[_from_<Origin>]_<Valid>
// tail into its parts (Forge's own parser applies the same underscore split:
// parts[0] is <Dest>; at most five underscore parts; a <Valid> tail of more
// than one token is rejoined). ok is false when the shape is not a well-formed
// spec at all -- too few or too many parts, an unknown destination or origin
// zone word, or an EMPTY <Valid>. It is the ONE grammar both
// evalThisTurnEnteredAs (which counts with it) and playerPropertyModelled
// (which validates a PlayerCount$Condition property before ranging a possibly
// empty group) share, so a shape one accepts cannot be rejected by the other
// -- the drift hazard the previous prefix-only check carried, where
// `ThisTurnEntered_` and `ThisTurnEntered_Nonsense` passed the pre-check and
// an empty group laundered them into a legitimate-looking (0, true).
func parseThisTurnEnteredSpec(rest string) (dest state.Zone, origin *state.Zone, valid string, ok bool) {
	parts := strings.Split(strings.TrimSpace(rest), "_")
	if len(parts) < 2 || len(parts) > 5 {
		return 0, nil, "", false
	}
	d, known := zoneWords[parts[0]]
	if !known {
		return 0, nil, "", false
	}
	if len(parts) >= 3 && parts[1] == "from" {
		if len(parts) < 4 {
			return 0, nil, "", false
		}
		o, known := zoneWords[parts[2]]
		if !known {
			return 0, nil, "", false
		}
		v := strings.Join(parts[3:], "_")
		if strings.TrimSpace(v) == "" {
			return 0, nil, "", false
		}
		return d, &o, v, true
	}
	v := strings.Join(parts[1:], "_")
	if strings.TrimSpace(v) == "" {
		return 0, nil, "", false
	}
	return d, nil, v, true
}

// countEntered folds the per-add entry list over one destination zone (and
// optionally one origin zone): the entries whose object matches valid from
// the resolving controller's perspective are counted, or -- when prop names a
// modelled sum property -- their values are summed through objectProperty
// (the Genesis of the Daleks total-power form).
func countEnteredAs(g *state.Game, c *Ctx, you state.PlayerID, dest state.Zone, origin *state.Zone, valid, prop string) (int32, bool) {
	if valid == "" {
		return 0, false
	}
	// The two CheckOnTriggeredCard carriers spell the event player's filter as
	// `<base>.ControlledBy CardController`; peel that referent before the
	// ordinary object matcher and constrain each historical entry by the
	// entering object's controller. Unknown/unbound referents fail closed.
	controlledByCardController := false
	if base, ref, ok := strings.Cut(valid, ".ControlledBy "); ok && ref == "CardController" {
		valid = base
		controlledByCardController = true
	}
	var players []state.PlayerID
	if controlledByCardController {
		var ok bool
		players, ok = controlReferentPlayers(g, c.SpecContext(you), "ControlledBy", "CardController")
		if !ok {
			return 0, true
		}
	}
	var n int32
	for _, e := range g.Entered {
		if e.To != dest {
			continue
		}
		if origin != nil && e.From != *origin {
			continue
		}
		// Evaluate the spec in the entry's DESTINATION zone: an object that
		// entered a non-battlefield zone has already left the battlefield,
		// so the ordinary matcher's `Permanent` base (o.Zone ==
		// ZBattlefield) would reject every such entry. matchesZoneSpecCtx
		// reads a non-battlefield `Permanent` base as a permanent CARD
		// (Forge's Card.isPermanent()), which is what Gravestorm's
		// Count$ThisTurnEntered_Graveyard_from_Battlefield_Permanent needs.
		if !matchesZoneSpecCtx(g, valid, e.Obj, c.SpecContext(you), e.To) {
			continue
		}
		if controlledByCardController {
			o := g.Obj(e.Obj)
			controlled := false
			if o != nil {
				for _, p := range players {
					if o.Controller == p {
						controlled = true
						break
					}
				}
			}
			if !controlled {
				continue
			}
		}
		// The plain count form, and the $<Property> sum form's per-entry
		// contribution (CardPower's printed face plus its +1/+1 counters, the
		// objectProperty read the Count$Valid aggregate shares). A property
		// the split did not recognise never reaches here -- it stayed in
		// <Valid> and failed the match above.
		if prop == "" {
			n++
			continue
		}
		n += objectProperty(g, e.Obj, prop)
	}
	return n, true
}

// objectProperty reads one Count$Valid-spec "$Property" aggregate term over
// a single object: its face value plus the summed P/T counter deltas for
// power/toughness, the mana value for CardManaCost. An unknown property reads
// 0 — the same conservative no-op every unmodelled head here takes.
func objectProperty(g *state.Game, id state.ObjID, prop string) int32 {
	o := g.Obj(id)
	if o == nil || o.Face() == nil {
		return 0
	}
	switch strings.TrimSpace(prop) {
	case "CardPower":
		dp, _ := o.CounterPTTotals()
		return int32(o.Face().Power()) + dp
	case "CardToughness":
		_, dt := o.CounterPTTotals()
		return int32(o.Face().Toughness()) + dt
	case "CardManaCost":
		return o.Face().ManaValue()
	}
	return 0
}

// modeledProperty reports whether a Count$Valid-spec "$Property" aggregate
// term is one objectProperty can evaluate; an unknown property read 0 — the
// same conservative no-op every unmodelled head here takes — and a gate over
// one must fail open rather than enforce that zero.
func modeledProperty(prop string) bool {
	switch strings.TrimSpace(prop) {
	case "CardPower", "CardToughness", "CardManaCost":
		return true
	}
	return false
}

// lifeExtreme answers PlayerCount...$LowestLifeTotal / $HighestLifeTotal:
// the lowest/highest CURRENT life total among the given players. Anything
// else — another property, or an empty group (no extreme exists) — reports
// (0, false): the caller degrades to unresolvable, so a gate over the shape
// fails open rather than enforcing a fake zero.
func lifeExtreme(g *state.Game, players []state.PlayerID, prop string) (int32, bool) {
	prop = strings.TrimSpace(prop)
	if prop != "LowestLifeTotal" && prop != "HighestLifeTotal" {
		return 0, false
	}
	best := int32(0)
	seen := false
	for _, p := range players {
		if int(p) < 0 || int(p) >= len(g.Players) {
			continue
		}
		life := g.Players[p].Life
		if !seen || (prop == "LowestLifeTotal" && life < best) || (prop == "HighestLifeTotal" && life > best) {
			best, seen = life, true
		}
	}
	if !seen {
		return 0, false
	}
	return best, true
}

// opponentGroup returns the living players other than the resolving
// controller — the group PlayerCountOpponents$ and (by the reading
// documented at its dispatch site) PlayerCountRegisteredOpponents$ both
// count over.
// playerGroupCount answers Forge's count property `Amount` on a
// PlayerCount<group>$ head: 1 per member, so the value is the group's size --
// the "one each" bound (SVar:OneEach:PlayerCountOpponents$Amount) and the
// per-player target maximum the TargetsForEachPlayer$ shape reads. Any other
// property fails closed to the ordinary dispatch.
func playerGroupCount(players []state.PlayerID, rest string) (int32, bool) {
	if strings.TrimSpace(rest) != "Amount" {
		return 0, false
	}
	return int32(len(players)), true
}

func opponentGroup(g *state.Game, c *Ctx) []state.PlayerID {
	var opps []state.PlayerID
	for _, p := range g.AliveFrom(0) {
		if p != c.Controller {
			opps = append(opps, p)
		}
	}
	return opps
}

// playerCountDefinedRegistered answers the PlayerCountDefinedRegistered[.Other]$
// properties. The two life-extreme properties route through the shared
// playerCountExtreme (whose LifeLostThisTurn arm is the Host's log-derived
// read — Knight of the Ebon Legion's and Y'shtola's
// HighestLifeLostThisTurn gates); HasPropertyLostLifeThisTurn counts the
// group members who lost any life this turn; and
// HasPropertywasDealtCombatDamageThisTurnBy <spec>[ <op><n>] counts the
// group members who were dealt combat damage this turn by a source matching
// the Forge spec (Lost Monarch of Ifnir's Zombie, Estinien Varlineau's
// Card.Self,Dragon, Blitzball's Creature.Legendary). Anything else reports
// (0, false) — unresolvable, so a gate over it fails per its caller's
// documented direction rather than enforcing a fake zero.
func playerCountDefinedRegistered(h Host, g *state.Game, c *Ctx, group []state.PlayerID, prop, arg string) (int32, bool) {
	prop = strings.TrimSpace(prop)
	// A `HasProperty…` head may carry Forge's /Op count suffix
	// (belbe_corrupted_observer's `PlayerCountOpponents$HasPropertyLostLife
	// ThisTurn/Twice`). Split it here so the property switch below reads the
	// bare name; the suffix applies inside hasPropertyLostLifeCount.
	base, _, _ := strings.Cut(prop, "/")
	base = strings.TrimSpace(base)
	// Deliberately NO lifeExtreme call here: the brief names exactly three
	// resolvable properties on this group, and the life-TOTAL extremes
	// (HighestLifeTotal/LowestLifeTotal) are not among them — they stay
	// (0, false) on DefinedRegistered[.Other]$ even though the sibling
	// Players$/Opponents$ arms resolve them. No corpus carrier reads a life
	// total extreme through this head; if one ever does, widening is a
	// one-line change with its own pin.
	switch base {
	case "HighestLifeLostThisTurn", "LowestLifeLostThisTurn":
		// The life-lost extremes — Knight of the Ebon Legion's and
		// Y'shtola's gates. playerCountExtreme's LifeLostThisTurn arm is the
		// Host's log-derived read.
		return playerCountExtreme(h, g, c, group, prop, arg)
	case "HasPropertyLostLifeThisTurn":
		// "a player [other than you] lost life this turn" — the shared read
		// every group's HasPropertyLostLifeThisTurn carrier uses. Calling the
		// helper (rather than inlining the count) is what keeps the property
		// from resolving on one group and failing closed on its sibling.
		return hasPropertyLostLifeCount(h, group, prop)
	case "HasPropertywasDealtCombatDamageThisTurnBy":
		spec, op, threshold, ok := splitPropertyThreshold(arg)
		if !ok {
			return 0, false
		}
		hits := h.CombatDamageToPlayersThisTurn()
		sc := c.SpecContext(c.Controller)
		var n int32
		for _, p := range group {
			var got int32
			for _, hit := range hits {
				if hit.Player != p {
					continue
				}
				// Zone is set to the battlefield: Forge's bare `Permanent`
				// base reads o.Zone == ZBattlefield (effects/filter.go's
				// matchesBase), and the captured source WAS a permanent on
				// the battlefield when it dealt the damage.
				o := &state.Object{ID: hit.Source, Card: hit.Card, FaceIdx: hit.FaceIdx, Controller: hit.Controller, Zone: state.ZBattlefield}
				if MatchesObjectCtx(g, spec, o, sc) {
					got++
				}
			}
			if countOpHolds(op, threshold, got) {
				n++
			}
		}
		return n, true
	}
	// Any other property is NOT resolvable: (0, false), the documented
	// fail-closed verdict. The HighestValid/LowestValid zone-count extremes
	// of playerCountExtreme are deliberately not offered on this group (the
	// brief names exactly the three properties above, and no corpus carrier
	// reaches a zone-count extreme here); only the two LifeLostThisTurn
	// extremes route into playerCountExtreme, above.
	return 0, false
}

// hasPropertyStateBacked answers the narrowly supported player-state
// HasProperty heads on the three ordinary living-player groups. Zone counts
// are evaluated from each member's perspective, so You/YouOwn selectors refer
// to that member rather than the resolving controller.
func hasPropertyStateBacked(h Host, g *state.Game, c *Ctx, group []state.PlayerID, prop, arg string) (int32, bool) {
	base, op, hasOp := strings.Cut(strings.TrimSpace(prop), "/")
	base = strings.TrimSpace(base)
	if strings.TrimSpace(arg) != "" {
		return 0, false
	}

	var qualifies func(state.PlayerID) bool
	switch {
	case base == "HasPropertyIsCorrupted":
		qualifies = func(p state.PlayerID) bool { return playerIsCorrupted(g, p) }
	case base == "HasPropertyisMonarch":
		qualifies = func(p state.PlayerID) bool { return g.IsMonarch(p) }
	case base == "HasPropertywasDealtDamageThisTurn":
		qualifies = func(p state.PlayerID) bool { return h.DamageTakenThisTurn(p) > 0 }
	case base == "HasPropertywasDealtCombatDamageThisTurn":
		hits := h.CombatDamageToPlayersThisTurn()
		qualifies = func(p state.PlayerID) bool {
			for _, hit := range hits {
				if hit.Player == p {
					return true
				}
			}
			return false
		}
	default:
		zone := state.Zone(0)
		prefix := ""
		switch {
		case strings.HasPrefix(base, "HasPropertyHasCardsInHand_"):
			zone, prefix = state.ZHand, "HasPropertyHasCardsInHand_"
		case strings.HasPrefix(base, "HasPropertyHasCardsInGraveyard_"):
			zone, prefix = state.ZGraveyard, "HasPropertyHasCardsInGraveyard_"
		default:
			return 0, false
		}
		tail := strings.TrimPrefix(base, prefix)
		i := strings.LastIndexByte(tail, '_')
		if i <= 0 || i == len(tail)-1 {
			return 0, false
		}
		spec, cmp := tail[:i], tail[i+1:]
		parsedOp, threshold, ok := parseCountCompare(cmp)
		if !ok || (parsedOp != "GE" && parsedOp != "GT" && parsedOp != "LE") {
			return 0, false
		}
		// An unread card spec fails CLOSED before any member is evaluated:
		// the zone matcher reports only a match boolean, so an unrecognized
		// predicate (Card.NoSuchPredicate) or base (NoSuchBase) would match
		// nothing, read as a fabricated count of zero and return (0, true) --
		// a condition gate would then enforce a zero that no rule stated.
		// unreadZoneSpec validates through the same classifiers the matcher
		// itself walks (UnknownPredicates, the census's shared base/predicate
		// vocabulary), so a spec the matcher would silently zero out here
		// reads unresolvable instead.
		if unreadZoneSpec(spec) {
			return 0, false
		}
		qualifies = func(p state.PlayerID) bool {
			var cards int32
			for _, id := range g.Zone(zone, p) {
				if matchesZoneSpecCtx(g, spec, id, c.SpecContext(p), zone) {
					cards++
				}
			}
			return countOpHolds(parsedOp, threshold, cards)
		}
	}
	n := hasPropertyCount(group, qualifies)
	if hasOp {
		n = applyCountOp(n, op)
	}
	return n, true
}

// unreadZoneSpec reports whether a zone-count HasProperty's card spec is
// unread — malformed, an unknown base word, or carrying a predicate no part
// of the filter recognises. It is the object-free validation mirror of
// matchesZoneSpecCtx: that matcher answers only a match boolean, so the sole
// classifier shared with it is the UnknownPredicates census plus the base
// vocabulary matchesBase dispatches on (its special bases, the CR 205.1
// card-type words, and the creature-subtype words hasTypeCtx reads).
// Anything outside that vocabulary fails closed here, so the count head can
// never turn an unread spec into a fabricated zero.
func unreadZoneSpec(spec string) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return true
	}
	// Comma alternatives are validated per alternative — the base of the
	// WHOLE spec is only the first alternative's (Mysterious Stranger's
	// `Instant,Sorcery` is two valid bases, not one unknown one).
	for alt := range filterAlternatives(spec) {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		base, _, _ := strings.Cut(alt, ".")
		base = strings.TrimSpace(base)
		if neg := strings.TrimPrefix(base, "non"); neg != base {
			base = strings.TrimSpace(neg)
		}
		if base == "" {
			return true
		}
		switch base {
		case "Any", "Card", "Permanent", "PermanentCard", "Spell", "SpellAbility", "CARDNAME", "Affinity":
			// matchesBase's own special bases (and the CARDNAME base
			// matchesZoneSpecCtx binds to the resolving source).
		default:
			if !cardTypeWords[base] && !CreatureTypeWords(base) {
				return true
			}
		}
	}
	return len(UnknownPredicates(spec)) != 0
}

// hasPropertyLostLifeCount answers PlayerCount*$HasPropertyLostLifeThisTurn
// over a group, honouring Forge's /Op suffix. It is the shared read the
// Players$, Opponents$, RegisteredOpponents$ and DefinedRegistered[.Other]$
// arms all use, so the property can never resolve on one group and fail
// closed on its sibling (the class the fix closes).
func hasPropertyLostLifeCount(h Host, group []state.PlayerID, prop string) (int32, bool) {
	base, op, hasOp := strings.Cut(prop, "/")
	if strings.TrimSpace(base) != "HasPropertyLostLifeThisTurn" {
		return 0, false
	}
	n := hasPropertyCount(group, func(p state.PlayerID) bool { return h.LifeLostThisTurn(p) > 0 })
	if hasOp {
		n = applyCountOp(n, op)
	}
	return n, true
}

// hasPropertyCount counts the group members satisfying pred, in the group's
// own deterministic order.
func hasPropertyCount(group []state.PlayerID, pred func(state.PlayerID) bool) int32 {
	var n int32
	for _, p := range group {
		if pred(p) {
			n++
		}
	}
	return n
}

// splitPropertyThreshold parses a HasProperty<property> argument of the form
// `<spec>[ <op><n>]` — the LAST space-separated token, when it matches a
// Forge comparison op (GE/GT/LE/LT/EQ) followed by digits, is the threshold;
// the remainder is the source spec. A missing threshold means "any hit"
// (>= 1), which is what an empty argument and a spec-only argument both
// mean. An empty spec fails closed (ok false) — a property with nothing to
// match must not match everything.
func splitPropertyThreshold(arg string) (spec, op string, threshold int32, ok bool) {
	arg = strings.TrimSpace(arg)
	spec = arg
	op, threshold = "GE", 1
	if fields := strings.Fields(arg); len(fields) > 1 {
		last := fields[len(fields)-1]
		if o, n, good := parseCountCompare(last); good {
			op, threshold = o, n
			spec = strings.TrimSpace(strings.TrimSuffix(arg, last))
		}
	}
	if strings.TrimSpace(spec) == "" {
		return "", "", 0, false
	}
	return spec, op, threshold, true
}

// parseCountCompare parses a Forge comparison token like GE1, GT2, EQ0,
// LE3, LT4 into its operator and integer threshold.
func parseCountCompare(tok string) (op string, threshold int32, ok bool) {
	if len(tok) < 3 {
		return "", 0, false
	}
	op = strings.ToUpper(tok[:2])
	switch op {
	case "GE", "GT", "LE", "LT", "EQ":
	default:
		return "", 0, false
	}
	n, err := strconv.ParseInt(tok[2:], 10, 32)
	if err != nil {
		return "", 0, false
	}
	return op, int32(n), true
}

// countOpHolds applies a comparison against a per-player hit count, the
// same operator set parseCountCompare recognises.
func countOpHolds(op string, threshold, got int32) bool {
	switch op {
	case "GE":
		return got >= threshold
	case "GT":
		return got > threshold
	case "LE":
		return got <= threshold
	case "LT":
		return got < threshold
	case "EQ":
		return got == threshold
	}
	return false
}

// playerCountCondition answers Forge's PlayerCount<group>$Condition<OP><RHS>
// <property> family — the per-member threshold count. For each member of the
// group the named property is evaluated FROM THAT MEMBER'S OWN PERSPECTIVE
// (a YouCtrl qualifier in the property's spec names the member), and the
// member is counted when the property satisfies <OP> against <RHS>.
//
// The shared dispatch is what keeps this family from resolving on one group
// and failing closed on its sibling (the class the fix closes): every group
// arm that carries a property dispatch — Players$, Opponents$,
// RegisteredOpponents$ and both DefinedRegistered spellings — routes its
// `Condition...` head here before falling through to playerCountExtreme, so
// the property evaluators below are the ONE grammar for all of them.
//
// <RHS> is either a literal (ConditionGE2 CardsDrawn) or an SVar NAME
// resolved PER MEMBER (Anya, Merciless Angel's `ConditionLTZ LifeTotal`, whose
// Z is `PlayerCountDefinedPlayer.PlayerUID_RelativePlayerUID$StartingLife/
// HalfDown` = that member's half starting life; Game Over's `ConditionLEY
// LifeTotal` is the same shape). A property or RHS this build cannot evaluate
// reports (0, false) — UNRESOLVABLE, never a fake zero — so a gate over it
// degrades per its caller's documented direction (triggers and statics fail
// closed, Num reads zero) rather than enforcing a made-up count.
func playerCountCondition(h Host, g *state.Game, c *Ctx, group []state.PlayerID, rest, arg string) (int32, bool) {
	cond, ok := strings.CutPrefix(strings.TrimSpace(rest), "Condition")
	if !ok || len(cond) < 3 {
		return 0, false
	}
	op := strings.ToUpper(cond[:2])
	switch op {
	case "GE", "GT", "LE", "LT", "EQ":
	default:
		return 0, false
	}
	rhs := strings.TrimSpace(cond[2:])
	prop := strings.TrimSpace(arg)
	if rhs == "" || prop == "" {
		return 0, false
	}
	// The RHS is a literal when it parses as an integer; else it is an SVar
	// name resolved PER MEMBER (Anya's Z, Game Over's Y). A name with no body
	// anywhere is unreadable — (0, false), never a threshold of 0. The
	// property's GRAMMAR is validated ONCE, BEFORE the group is ranged, and
	// when the group is EMPTY the RHS body is resolved once too, against the
	// resolving context: an empty group would otherwise never reach the
	// per-member checks and the head would report a legitimate-looking
	// (0, true) for a property or RHS this build cannot evaluate — the leak an
	// `...LE0`-shaped gate evaluates as true over nothing. The per-member loop
	// still re-validates (a member can make a modelled property unevaluable,
	// e.g.
	// LifeTotal of a gone seat).
	lit, litErr := strconv.ParseInt(rhs, 10, 32)
	literalOK := litErr == nil
	var rhsBody string
	if !literalOK {
		body, found := "", false
		if c.SVars != nil {
			body, found = c.SVars[rhs]
		}
		if !found {
			if o := g.Obj(c.Source); o != nil && o.Face() != nil {
				body, found = o.Face().SVars[rhs]
			}
		}
		if !found {
			return 0, false
		}
		rhsBody = body
	}
	if !playerPropertyModelled(prop) {
		return 0, false
	}
	// An SVar RHS is resolved PER MEMBER, so an EMPTY group would never
	// evaluate its body at all and a body this build cannot evaluate would be
	// laundered into the empty-group zero alongside a readable one. Resolve it
	// ONCE against the resolving context when there is no member to resolve it
	// with: a body whose head matches nothing is UNRESOLVABLE (0, false), the
	// same verdict a live group's per-member read gives. (A body that resolves
	// against the sentinel is a modelled threshold, so the empty group keeps
	// its honest (0, true); a body that resolves for the sentinel but not for a
	// member on a live group is still caught by the loop's per-member check.)
	if !literalOK && len(group) == 0 {
		sub := *c
		if _, ok := evalCountExprOK(h, &sub, rhsBody, 0); !ok {
			return 0, false
		}
	}
	var n int32
	for _, m := range group {
		v, okv := playerMemberProperty(h, g, c, m, prop)
		if !okv {
			return 0, false
		}
		var threshold int32
		if literalOK {
			threshold = int32(lit)
		} else {
			// Evaluate the body with the MEMBER as the relative player: this
			// is what makes `...RelativePlayerUID$StartingLife/HalfDown`
			// answer the member's own threshold (and a per-member count body
			// its own perspective).
			sub := *c
			sub.Controller = m
			tv, tok := evalCountExprOK(h, &sub, rhsBody, 0)
			if !tok {
				return 0, false
			}
			threshold = tv
		}
		if countOpHolds(op, threshold, v) {
			n++
		}
	}
	return n, true
}

// playerMemberProperty evaluates one property of Forge's player-count
// condition family from the counted member's own perspective: their current
// life total, their per-turn draw / discard / cast census, the count of cards
// that entered a named zone this turn under their control (or owned by them),
// and — through relativePlayerProperty — the relative-player group. Each read
// is shared with the head of the same name elsewhere (the Host per-turn
// predicates, evalThisTurnEnteredAs), so the count family and the standalone
// heads can never drift apart. An unmodelled property reports (0, false) —
// unresolvable, the caller's documented direction, never a fake zero.
func playerMemberProperty(h Host, g *state.Game, c *Ctx, m state.PlayerID, prop string) (int32, bool) {
	switch strings.TrimSpace(prop) {
	case "LifeTotal":
		if int(m) < 0 || int(m) >= len(g.Players) {
			return 0, false
		}
		return g.Players[m].Life, true
	case "CardsDrawn":
		return h.CardsDrawnThisTurn(m), true
	case "CardsDiscardedThisTurn":
		return h.CardsDiscardedThisTurn(m), true
	case "SpellsCastThisTurn":
		return int32(h.SpellsCastThisTurnBy(m)), true
	}
	if rest, ok := strings.CutPrefix(strings.TrimSpace(prop), "ThisTurnEntered_"); ok {
		return evalThisTurnEnteredAs(g, c, m, rest)
	}
	return 0, false
}

// playerPropertyModelled reports whether prop names a property the condition
// family's evaluators model at all. playerCountCondition calls it BEFORE
// ranging the group so an empty group cannot launder an unmodelled property
// into a legitimate (0, true) — the per-member checks would never run, and an
// `...LE0`-shaped gate over an unmodelled property would evaluate TRUE over
// nothing. It must stay in lock-step with playerMemberProperty's switch: a
// property modelled there but missed here fails a non-empty group's count too
// (the fail-closed direction, still wrong), and the reverse re-opens the
// empty-group leak this guard closes. The ThisTurnEntered_ branch uses the
// SAME parseThisTurnEnteredSpec the evaluator does, so a structural shape one
// accepts cannot be rejected by the other (a bare `ThisTurnEntered_` prefix
// or an unknown zone word is NOT modelled, however well it prefixes).
func playerPropertyModelled(prop string) bool {
	switch strings.TrimSpace(prop) {
	case "LifeTotal", "CardsDrawn", "CardsDiscardedThisTurn", "SpellsCastThisTurn":
		return true
	}
	if rest, ok := strings.CutPrefix(strings.TrimSpace(prop), "ThisTurnEntered_"); ok {
		_, _, _, parsed := parseThisTurnEnteredSpec(rest)
		return parsed
	}
	return false
}

// relativePlayerProperty answers the
// PlayerCountDefinedPlayer.PlayerUID_RelativePlayerUID$<Property> family: the
// named property of the player the read is FOR (the resolving context's
// Controller — playerCountCondition sets it to the counted member). Only
// StartingLife is modelled (the /Op suffix — Anya's /HalfDown — applies
// through the shared applyCountOp). An unknown property fails closed to
// (0, false), the unresolvable verdict.
func relativePlayerProperty(h Host, prop string) (int32, bool) {
	name, op, hasOp := strings.Cut(strings.TrimSpace(prop), "/")
	var v int32
	switch name {
	case "StartingLife":
		v = h.StartingLife()
	default:
		return 0, false
	}
	if hasOp {
		v = applyCountOp(v, op)
	}
	return v, true
}

// playerCountExtreme answers the PlayerCount<group>$<Property> properties
// that are not a life total (lifeExtreme's pair is tried first at the
// dispatch site). Two families, the corpus's measured population over these
// heads:
//
//   - HighestValid/LowestValid <spec> and the zone-scoped spellings
//     (HighestValidGraveyard, LowestValidHand, ...): the highest/lowest,
//     over the group, of the count of objects the member has in the named
//     zone that match the spec, each member counted from their OWN
//     perspective — a YouCtrl qualifier in the spec names the counted
//     member, not the resolving controller (Land Tax's "if an opponent
//     controls more lands than you": SVar Y =
//     PlayerCountOpponents$HighestValid Land.YouCtrl, SVarCompare$ GTX;
//     Defense of the Heart's PlayerCountOpponents$HighestValid
//     Creature.YouCtrl, GE3). An empty zone or an all-zero group is a
//     readable extreme of 0, never unresolvable — a count always has one
//     answer.
//   - HighestLifeLostThisTurn/LowestLifeLostThisTurn: the extreme, over the
//     group, of the Host's log-derived LifeLostThisTurn (Bloodchief
//     Ascension's "if an opponent lost 2 or more life this turn" gate).
//
// An unknown property reports (0, false): unresolvable, so a gate over one
// fails per its caller's documented direction rather than enforcing a fake
// zero. Iterating the given group slice in order (never a map) keeps the
// extreme deterministic; ties match, like lifeExtreme's.
func playerCountExtreme(h Host, g *state.Game, c *Ctx, players []state.PlayerID, prop, spec string) (int32, bool) {
	prop = strings.TrimSpace(prop)
	highest := false
	switch {
	case strings.HasPrefix(prop, "Highest"):
		highest = true
		prop = prop[len("Highest"):]
	case strings.HasPrefix(prop, "Lowest"):
		highest = false
		prop = prop[len("Lowest"):]
	default:
		return 0, false
	}
	if strings.HasPrefix(prop, "Counters.") {
		kind := strings.TrimPrefix(prop, "Counters.")
		if kind != "Poison" {
			return 0, false
		}
		var best int32
		seen := false
		for _, p := range players {
			if int(p) < 0 || int(p) >= len(g.Players) {
				continue
			}
			v := g.Players[p].Counter("POISON")
			if !seen || (highest && v > best) || (!highest && v < best) {
				best, seen = v, true
			}
		}
		if !seen {
			return 0, false
		}
		_, op, hasOp := strings.Cut(prop, "/")
		if hasOp {
			best = applyCountOp(best, op)
		}
		return best, true
	}
	if _, ok := playerScalarProperty(h, g, 0, prop); ok && strings.TrimSpace(spec) == "" {
		// A per-player zone size or tally (Highest/LowestCardsInHand,
		// HighestCardsInGraveyard, HighestCardsDrawn -- fuzz-cov3).
		best, seen := int32(0), false
		for _, p := range players {
			v, ok := playerScalarProperty(h, g, p, prop)
			if !ok {
				continue
			}
			if !seen || (highest && v > best) || (!highest && v < best) {
				best, seen = v, true
			}
		}
		if !seen {
			return 0, true
		}
		return best, true
	}
	if prop == "LifeLostThisTurn" {
		if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
			return 0, false
		}
		best, seen := int32(0), false
		for _, p := range players {
			v := h.LifeLostThisTurn(p)
			if !seen || (highest && v > best) || (!highest && v < best) {
				best, seen = v, true
			}
		}
		if !seen {
			return 0, false
		}
		return best, true
	}
	zone, ok := countZone(prop)
	if !ok || strings.TrimSpace(spec) == "" {
		return 0, false
	}
	best, seen := int32(0), false
	for _, p := range players {
		if int(p) < 0 || int(p) >= len(g.Players) {
			continue
		}
		var n int32
		for _, id := range g.Zone(zone, p) {
			if matchesZoneSpecCtx(g, spec, id, c.SpecContext(p), zone) {
				n++
			}
		}
		if !seen || (highest && n > best) || (!highest && n < best) {
			best, seen = n, true
		}
	}
	if !seen {
		return 0, false
	}
	return best, true
}

// evalCompare resolves a "Compare <Name> <OP><threshold>.<ifTrue>.<ifFalse>"
// body -- the spell-mastery / lieutenant form Forge encodes as
// `SVar:X:Count$Compare Y GE2.3.2` (Nissa's Pilgrimage: 2 basic Forests, or 3
// with two or more instants/sorceries in the graveyard). <Name> resolves the
// compared value: an SVar body evaluated recursively (Y's
// `Count$ValidGraveyard Instant.YouOwn,Sorcery.YouOwn`), else the token itself
// as an inline expression. <OP> is one of GE/GT/EQ/LE/LT; the threshold and
// the branches are each an operand -- an integer literal, an SVar name, or an
// inline expression -- resolved through the same evalCountOperand as <Name>.
// That closes the corpus's SVar-named threshold singletons
// (`GEMePlus.3.2`, `LTZ.2.0`) alongside its SVar-named branch singletons
// (`GE4.X.4`, `LT5.X.Z`, `GE1.Y.Z`, ...). Per the evaluator's convention an
// operand whose inner head this build does not model degrades to zero (the
// same direction evalCountOperand always takes), so the comparison still runs
// rather than the whole head vanishing. The argument-less forms
// (`Count$Compare TronCheck`, `Count$Compare W`, `Count$Compare
// ReplacedCard$CardManaCost`) still fail closed to zero -- nothing to compare.
// Only a comparison head that is not one of the five named ops fails closed.
func evalCompare(h Host, c *Ctx, arg string, depth int) int32 {
	name, rest, _ := strings.Cut(arg, " ")
	rest = strings.TrimSpace(rest)
	if name == "" || rest == "" {
		// Argument-less shapes (`Count$Compare TronCheck`) have nothing to
		// compare; degrade to zero rather than guessing.
		return 0
	}
	if len(rest) < 2 {
		return 0
	}
	op, tail := rest[:2], rest[2:]
	thTok, branches, _ := strings.Cut(tail, ".")
	if thTok == "" {
		// A comparison operator must have a threshold. Do not treat a malformed
		// empty token as the otherwise valid numeric operand zero.
		return 0
	}
	// The threshold is an operand, not a literal-only field: Forge names an
	// SVar here whenever the bound is itself a computed value (Teachings of
	// the Archaics' GEMePlus -> SVar$Me/Plus.4, Anchor to Reality's LTZ ->
	// the sacrificed permanent's mana value). evalCountOperand handles the
	// integer-literal case first, so the plain shapes are unchanged.
	th := evalCountOperand(h, c, thTok, depth)
	ifTok, elseTok, _ := strings.Cut(branches, ".")
	value := evalCountOperand(h, c, name, depth)
	var hit bool
	switch op {
	case "GE":
		hit = value >= th
	case "GT":
		hit = value > th
	case "EQ":
		hit = value == th
	case "LE":
		hit = value <= th
	case "LT":
		hit = value < th
	default:
		// Not one of the five comparison heads.
		return 0
	}
	if hit {
		return evalCountOperand(h, c, ifTok, depth)
	}
	return evalCountOperand(h, c, elseTok, depth)
}

// evalCountOperand resolves one Compare operand: an integer literal directly,
// an SVar body through the ordinary expression evaluator, else the token
// itself as an inline expression (EvalCount degrades a bare unknown word to
// zero, the convention every other head follows).
func evalCountOperand(h Host, c *Ctx, tok string, depth int) int32 {
	n, _ := resolveCountOperand(h, c, tok, depth)
	return n
}

// resolveCountOperand is evalCountOperand with an evaluated verdict: an
// integer literal directly, an SVar body through the ordinary expression
// evaluator at depth+1 (the same recursion bound evalCountOperand always
// carried -- a self-referential Compare SVar must terminate), else the token
// itself as an inline expression. ok is false only when nothing resolved --
// the caller that binds a value once (effects' SetChosenNumber$ read) turns
// that into its fail-closed Note.
func resolveCountOperand(h Host, c *Ctx, tok string, depth int) (int32, bool) {
	if n, err := strconv.Atoi(tok); err == nil {
		return int32(n), true
	}
	if c.SVars != nil {
		if body, ok := c.SVars[tok]; ok {
			return evalCountExprOK(h, c, body, depth+1)
		}
	}
	return evalCountExprOK(h, c, tok, depth+1)
}

// splitDot splits an "a.b" pair into two integers, defaulting either side to
// zero if it does not parse -- the same forgiving-not-panicking convention
// applyCountOp already follows.
func splitDot(s string) (a, b int32) {
	x, y, _ := strings.Cut(s, ".")
	av, _ := strconv.Atoi(x)
	bv, _ := strconv.Atoi(y)
	return int32(av), int32(bv)
}

// countBranchOperand resolves one yes/no branch head's two branch tokens
// against a boolean the head's predicate computed: <yes> when it holds, else
// <no>. The token reader is resolveCountOperand (the wasCastFromGraveyard
// branch-head precedent -- a literal, an SVar name, or an inline expression;
// the recursion depth rides so a branch naming another SVar terminates), and
// an unresolvable token degrades to 0 rather than wedging -- the same
// direction the Compare head's operands take.
func countBranchOperand(h Host, c *Ctx, holds bool, yesTok, noTok string, depth int) int32 {
	if holds {
		return evalCountOperand(h, c, yesTok, depth)
	}
	return evalCountOperand(h, c, noTok, depth)
}

// colourLetter parses one Forge colour spelling -- a WUBRG letter or a full
// colour word, case-insensitive -- to its WUBRG letter, or 0 when the token
// names no colour. The callers that fail closed on 0 keep an unreadable
// colour unresolvable rather than counting a fake zero.
func colourLetter(s string) byte {
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) == 1 {
		switch s[0] {
		case 'W', 'U', 'B', 'R', 'G':
			return s[0]
		}
		return 0
	}
	switch s {
	case "WHITE":
		return 'W'
	case "BLUE":
		return 'U'
	case "BLACK":
		return 'B'
	case "RED":
		return 'R'
	case "GREEN":
		return 'G'
	}
	return 0
}

// devotionCount is CR 700.5's devotion to one colour: the number of mana
// symbols of that colour among the mana costs of the permanents the player
// controls. Every printed spelling the corpus carries counts -- plain pips
// ("2 B B"), two-colour hybrid ("GW"), monocolour hybrid ("2B"/"2/B"),
// Phyrexian ("BP", CR 107.4f: it is a symbol of its colour) and the
// compleated tricolour ("GWP"/"PRG") -- because each WUBRG letter inside a
// pip token counts once for its colour (manaCostColourSymbols below). A
// face-down battlefield permanent has no mana cost (CR 708.5's vanilla set)
// and contributes nothing; an out-of-range controller counts nothing.
func devotionCount(g *state.Game, controller state.PlayerID, col byte) int32 {
	if int(controller) < 0 || int(controller) >= len(g.Players) {
		return 0
	}
	var n int32
	for _, id := range g.Zone(state.ZBattlefield, controller) {
		o := g.Obj(id)
		if o == nil || (o.FaceDown && o.Zone == state.ZBattlefield) {
			continue
		}
		f := o.Face()
		if f == nil {
			continue
		}
		n += manaCostColourSymbols(f.ManaCost, col)
	}
	return n
}

// manaCostColourSymbols counts one printed ManaCost string's symbols of one
// colour. The corpus costs are space-separated pip tokens (measured: zero
// brace-form costs at the current pin); each WUBRG letter inside a token
// counts once for its colour, so hybrid/Phyrexian/compleated spellings read
// once per component colour. Generic digits, {X}, {S}, {C} and the P of a
// Phyrexian pip contribute to no colour.
func manaCostColourSymbols(cost string, col byte) int32 {
	if cost == "" || strings.EqualFold(cost, "no cost") {
		return 0
	}
	var n int32
	for sym := range strings.FieldsSeq(cost) {
		n += int32(strings.Count(sym, string(col)))
	}
	return n
}

// countZone maps a Count$ head to the zone it scopes over. ValidAll is NOT
// here: it scopes over every zone at once (countAllZones plus one stack
// pass, handled directly in the zone-count branch), and a single-zone
// mapping cannot express that.
// multiCountZones parses a multi-zone count head, Valid<Zone>,<Zone>[,...]
// (Count$ValidGraveyard,Battlefield Cave.YouCtrl), into its zones: nil for a
// single-zone head (countZone's) or any list naming an unknown or repeated
// zone word.
func multiCountZones(head string) []state.Zone {
	rest, ok := strings.CutPrefix(head, "Valid")
	if !ok || !strings.Contains(rest, ",") {
		return nil
	}
	var out []state.Zone
	for _, w := range strings.Split(rest, ",") {
		z, known := zoneWords[w]
		if !known {
			return nil
		}
		for _, have := range out {
			if have == z {
				return nil
			}
		}
		out = append(out, z)
	}
	return out
}

func countZone(head string) (state.Zone, bool) {
	switch head {
	case "Valid":
		return state.ZBattlefield, true
	case "ValidHand":
		return state.ZHand, true
	case "ValidGraveyard":
		return state.ZGraveyard, true
	case "ValidLibrary":
		return state.ZLibrary, true
	case "ValidExile":
		return state.ZExile, true
	case "ValidStack":
		return state.ZStack, true
	case "ValidCommand":
		return state.ZCommand, true
	}
	return 0, false
}

// zoneCountFold is the per-candidate accumulator the zone-count branch's
// scan drives. The visit body is shared between the single-zone scan and
// the ValidAll all-zones scan through a pointer-receiver method, so neither
// loop materialises a candidate slice and the common single-zone head keeps
// its allocation-free iteration (the maps are created only by the CardTypes
// and token$DifferentCardNames heads, which need a heap map anyway; the
// struct itself stays on the caller's stack because visit never leaks its
// receiver). The spec's SpecContext is NOT a field: it is built once by the
// caller and passed to visit as a parameter, so the *Ctx the caller built
// (the hot layer-walk Ctx) never escapes through this type.
type zoneCountFold struct {
	h           Host
	g           *state.Game
	spec        string
	prop        string
	extreme     bool
	isLeast     bool
	hasBareHand bool
	// readsPT is computed ONCE per fold from SpecReadsPT(spec): the fold
	// binds a battlefield candidate's layer-derived P/T into its SpecContext
	// only when the spec actually reads one of the four P/T comparison
	// fields. Without the gate every Count$Valid pays a full rules layer walk
	// per candidate, and a P/T CDA that itself counts permanents (Master of
	// Etherium) makes that walk recurse into the same count -- the
	// in-progress frame guard stops the cycle but not the factorial fan-out
	// (4 Masters 2.9 ms, 5 44 ms, 6 577 ms, 7 10.6 s for ONE Derived).
	readsPT        bool
	n, best        int32
	seen           bool
	seenTokenNames map[string]bool
	seenCardTypes  map[string]bool
	// seenCreatureTypes is the CreatureType spelling's distinct set (task
	// diffcount2): creature-subtype words among the matched faces, read
	// only through len.
	seenCreatureTypes map[string]bool
	colorsSeen        ColorMask
	// diffKind and the seen sets for the Different* distinct-set property
	// family (task diffcount1). Both maps are read only through len, so no
	// map ordering ever reaches an event or a view.
	diffKind       differentPropertyKind
	seenDiffValues map[int32]bool
	seenDiffNames  map[string]bool
}

// visit folds one candidate: the shared per-candidate body of the
// zone-count scan (the bare-hand provenance split, the match against the
// candidate's OWN zone, then the plain-count / extreme / sum /
// distinct-set accumulation).
func (f *zoneCountFold) visit(id state.ObjID, zone state.Zone, specCtx SpecContext) {
	if zone == state.ZBattlefield && id == specCtx.ExcludeFromBattlefieldCount && id != 0 {
		return
	}
	matchSpec := f.spec
	if f.hasBareHand {
		s, ok := castFromHandAnyAdmitsFilter(f.h, f.spec, id)
		if !ok {
			return
		}
		matchSpec = s
	}
	// Count$Valid is an effects-side scan, but battlefield numeric filters
	// still read rules' layer-derived characteristics. Bind the candidate's
	// values through a small optional value interface; effects remains below
	// rules and SpecContext carries no callable resolver. Skip the bind
	// entirely unless the spec reads a P/T comparison field (readsPT): a
	// spec that reads none cannot observe the values, and the bind runs a
	// full rules layer walk per candidate -- which a P/T CDA that counts
	// permanents turns into a factorial recursion (see readsPT's field
	// comment).
	if zone == state.ZBattlefield && f.readsPT {
		if provider, ok := f.h.(interface {
			FilterDerivedPT(state.ObjID) (power, toughness, basePower, baseToughness int32, ok bool)
		}); ok {
			if power, toughness, basePower, baseToughness, found := provider.FilterDerivedPT(id); found {
				specCtx.DerivedPower, specCtx.DerivedToughness, specCtx.HasDerivedPT = power, toughness, true
				specCtx.BasePower, specCtx.BaseToughness, specCtx.HasBasePT = basePower, baseToughness, true
			}
		}
	}
	if !matchesZoneSpecCtx(f.g, matchSpec, id, specCtx, zone) {
		return
	}
	if f.prop == "" {
		if f.seenTokenNames != nil {
			if o := f.g.Obj(id); o != nil && o.Face() != nil {
				f.seenTokenNames[o.Face().Name] = true
			}
			return
		}
		f.n++
		return
	}
	o := f.g.Obj(id)
	if o == nil || o.Face() == nil {
		return
	}
	if f.extreme {
		// The DERIVED, layer-aware characteristic (h.Power/h.Toughness),
		// not the printed face: Forge sizes "greatest power" from the
		// game's actual power, so a lord's bonus or a -1/-1 counter
		// counts. The sibling CardPower/... cases keep the printed-face
		// read; the divergence is recorded in the report.
		v := extremePropertyValue(f.h, o, f.prop)
		if !f.seen || (f.isLeast && v < f.best) || (!f.isLeast && v > f.best) {
			f.best, f.seen = v, true
		}
		return
	}
	// CardCounters.<KIND> sums one counter kind over the matched set (Kate
	// Stewart's time counters, Kyler's P1P1); CardCounters.ALL sums every
	// kind. state.Object.Counter is the ONE home for that marker, so this
	// read and the $<Ref>$CardCounters readers (evalRefProperty, the bare
	// source head) cannot disagree.
	if kind, ok := strings.CutPrefix(f.prop, "CardCounters."); ok {
		f.n += o.Counter(kind)
		return
	}
	switch f.prop {
	case "CardPower":
		dp, _ := o.CounterPTTotals()
		f.n += int32(o.Face().Power()) + dp
	case "CardToughness":
		_, dt := o.CounterPTTotals()
		f.n += int32(o.Face().Toughness()) + dt
	case "CardManaCost":
		f.n += o.Face().Cmc()
	case "CardTypes", "CardTypesPermanent":
		vocab := cardTypeWords
		if f.prop == "CardTypesPermanent" {
			vocab = permanentTypeWords
		}
		for _, typ := range o.Face().Types {
			if vocab[typ] {
				f.seenCardTypes[typ] = true
			}
		}
	case "CreatureType":
		for _, typ := range o.Face().Types {
			if creatureSubtypeWords[typ] {
				f.seenCreatureTypes[typ] = true
			}
		}
	case "Colors":
		f.colorsSeen |= ColorMaskOf(o)
	default:
		// The Different* distinct-set properties (task diffcount1): each
		// matching object contributes its value to the seen set. The nil-face
		// guard lives inside differentPropertyValue; visit's early return
		// already guarantees o.Face() != nil for the name read.
		if f.diffKind != diffNone {
			if f.seenDiffNames != nil {
				f.seenDiffNames[o.Face().Name] = true
			} else if v, ok := differentPropertyValue(f.h, o, f.diffKind); ok {
				f.seenDiffValues[v] = true
			}
		}
	}
}

// countAllZones is the ordered per-seat zone list a Count$ValidAll body
// scans -- every per-player card zone in enum order. ZStack is global and is
// appended once by the ValidAll branch itself, never here; ZCeased has no
// membership list and is never scanned. The order matters only for
// determinism -- a count and an extreme fold are order-insensitive -- but a
// fixed order keeps every evaluation byte-identical run to run.
var countAllZones = []state.Zone{
	state.ZLibrary, state.ZHand, state.ZBattlefield,
	state.ZGraveyard, state.ZExile, state.ZCommand,
}

// isExtremeProperty reports whether prop is one of the four extreme-reduction
// property suffixes (`Count$Valid <spec>$GreatestCardPower` and its siblings),
// as opposed to the summed (CardPower/CardManaCost) or distinct-set
// (CardTypes/Colors) properties. This is the ENTIRE extreme grammar: the
// corpus carries no other spelling and no argument-less form. A property the
// corpus writes but this build does not yet read -- the Different* distinct
// family -- is deliberately NOT admitted here and keeps the whole-token
// fail-closed read.
func isExtremeProperty(prop string) bool {
	switch prop {
	case "GreatestCardPower", "GreatestCardToughness", "GreatestCardManaCost", "LeastCardPower":
		return true
	}
	return false
}

// differentPropertyKind classifies a Different* distinct-set property -- the
// value family the count dedups over. diffNone means prop is not one of them.
type differentPropertyKind int

const (
	// diffNone is the zero value: prop is not a Different* property.
	diffNone differentPropertyKind = iota
	// diffManaCost: DifferentCardManaCost -- distinct printed mana values.
	diffManaCost
	// diffPower: DifferentCardPower -- distinct DERIVED powers (a lord's
	// bonus or a -1/-1 counter changes the value, matching the
	// GreatestCardPower read).
	diffPower
	// diffToughness: DifferentCardToughness -- distinct derived toughnesses.
	diffToughness
	// diffName: DifferentCardNames -- distinct face names.
	diffName
	// diffColorPair: DifferentColorPair -- distinct two-colour pairs among
	// permanents that are EXACTLY two colours (Niv-Mizzet, Guildpact).
	diffColorPair
)

// differentPropertyKindOf classifies the Different* distinct-set property
// family of a Count$Valid<zone> <spec>$<Property> body: the properties that
// count DISTINCT VALUES among the matching cards rather than the cards
// themselves. The three numeric spellings and the name spelling all existed in
// the corpus unread (whole-token fail-closed to zero) before task diffcount1;
// classifying them in ONE place keeps the matcher, the value fold and the
// verdict from drifting apart. A spelling outside this set returns diffNone
// and keeps the pre-existing behaviour.
func differentPropertyKindOf(prop string) differentPropertyKind {
	switch prop {
	case "DifferentCardManaCost":
		return diffManaCost
	case "DifferentCardPower":
		return diffPower
	case "DifferentCardToughness":
		return diffToughness
	case "DifferentCardNames":
		return diffName
	case "DifferentColorPair":
		return diffColorPair
	}
	return diffNone
}

// differentPropertyValue reads one matching object's contribution to a
// numeric Different* set. ok is false when the object contributes nothing
// (an exactly-two-colour property read against a card that is not exactly two
// colours), so the value is never a meaningless zero that would collide with
// a real zero-mana-value card. A diffName property has no numeric value and
// must be handled through the name map at the call site; it returns ok=false
// here so a caller that routed it wrongly adds nothing rather than a zero
// that would inflate the count.
func differentPropertyValue(h Host, o *state.Object, kind differentPropertyKind) (int32, bool) {
	switch kind {
	case diffManaCost:
		// Face() is nil for a Card==nil or out-of-range FaceIdx object
		// (state/object.go); a remembered/targeted shell contributes
		// nothing rather than panicking the match.
		if f := o.Face(); f != nil {
			return f.Cmc(), true
		}
		return 0, false
	case diffPower:
		// The derived, layer-aware power, matching extremePropertyValue's
		// GreatestCardPower read: a lord's bonus or a counter counts.
		return refPower(h, o, false), true
	case diffToughness:
		return refToughness(h, o, false), true
	case diffColorPair:
		mask := ColorMaskOf(o)
		// Only permanents that are EXACTLY two colours contribute a pair
		// (Niv-Mizzet, Guildpact's "exactly two colors"): a monocoloured or
		// colourless permanent has no pair to contribute.
		if bits.OnesCount8(uint8(mask)) != 2 {
			return 0, false
		}
		return int32(mask), true
	}
	return 0, false
}

// isLeastProperty reports whether an extreme property takes the MINIMUM over
// the matches (Least*) rather than the maximum (Greatest*).
func isLeastProperty(prop string) bool {
	return prop == "LeastCardPower"
}

// extremePropertyValue reads one object's contribution to an extreme
// property: the DERIVED, layer-aware power/toughness (h.Power/h.Toughness)
// for the power/toughness extremes, so a lord's bonus or a -1/-1 counter is
// seen the way Forge sizes "greatest power", and the printed mana value for
// GreatestCardManaCost (the same read the summed CardManaCost case uses).
// An unrecognised extreme reads 0 -- but isExtremeProperty admitted it, so a
// missing case here is a compile-time-visible oversight, not a silent one.
func extremePropertyValue(h Host, o *state.Object, prop string) int32 {
	switch prop {
	case "GreatestCardPower", "LeastCardPower":
		return h.Power(o.ID)
	case "GreatestCardToughness":
		return h.Toughness(o.ID)
	case "GreatestCardManaCost":
		return o.Face().ManaValue()
	}
	return 0
}

// controlsAllUrzaLands reports whether the player controls at least one
// permanent of each Urza land subtype on the battlefield -- the "is the Urza
// lands assembly complete?" predicate Count$UrzaLands encodes. Subtypes are
// matched on the Types line, where Power-Plant is hyphenated: the card Name
// "Urza's Power Plant" is a different string and matching it would be the
// exact defect the UrzaLands head exists to avoid. Iterating the dense
// object arena in order (never a map) keeps the count deterministic; it scans
// every battlefield permanent regardless of who owns it, so the controller
// test is purely o.Controller.
func controlsAllUrzaLands(g *state.Game, controller state.PlayerID) bool {
	var mine, tower, plant bool
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.Zone != state.ZBattlefield || o.Controller != controller || o.Face() == nil {
			continue
		}
		switch {
		case hasSubtype(o, "Urza's Mine"):
			mine = true
		case hasSubtype(o, "Urza's Tower"):
			tower = true
		case hasSubtype(o, "Urza's Power-Plant"):
			plant = true
		}
		if mine && tower && plant {
			return true
		}
	}
	return false
}

// hasSubtype reports whether an object's type line contains the given
// (possibly multi-word) subtype as consecutive tokens. Forge writes a
// subtype such as "Urza's Mine" inside the space-separated Types line, so
// the engine's fields-split representation breaks it into ["Land","Urza's",
// "Mine"]; a single-token EqualFold is therefore wrong for these and must be
// a consecutive-token match. An empty target never matches.
func hasSubtype(o *state.Object, sub string) bool {
	f := o.Face()
	if f == nil {
		return false
	}
	words := strings.Fields(sub)
	if len(words) == 0 {
		return false
	}
	for i := 0; i+len(words) <= len(f.Types); i++ {
		match := true
		for j, w := range words {
			if !strings.EqualFold(f.Types[i+j], w) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// validConvokedCountOp keeps this newly modelled head from treating a typo or
// unimplemented operator as a successful read of the base amount. Other
// heads retain their existing operator fallback; the two corpus carriers
// need only the bare value and /Twice.
// modelledCountOp reports whether op is an arithmetic suffix the Count$
// evaluator actually applies: a literal op validConvokedCountOp accepts, or a
// Plus./Minus./Times. operand naming one of c's SVars (applyCountOpOperandOK's
// SVar-operand arm, Avacyn's Judgment's /Plus.MaxPermanents).
func modelledCountOp(c *Ctx, op string) bool {
	op = strings.TrimSpace(op)
	if validConvokedCountOp(op) {
		return true
	}
	for _, prefix := range []string{"Plus.", "Minus.", "Times."} {
		if operand, ok := strings.CutPrefix(op, prefix); ok && c != nil && c.SVars != nil {
			_, named := c.SVars[strings.TrimSpace(operand)]
			return named
		}
	}
	return false
}

func validConvokedCountOp(op string) bool {
	switch op {
	case "Twice", "Thrice", "HalfDown", "HalfUp", "ThirdUp", "Negative":
		return true
	}
	for _, prefix := range []string{"Plus.", "Minus.", "NMinus.", "Times.", "Divide.", "DivideEvenly.", "DivideEvenlyUp.", "DivideEvenlyDown."} {
		if operand, ok := strings.CutPrefix(op, prefix); ok {
			n, err := strconv.Atoi(operand)
			return err == nil && (!strings.HasPrefix(prefix, "Divide") || n > 0)
		}
	}
	return false
}

// applyCountOp applies the /Op suffix of a Count$ expression. The arithmetic
// runs in int64 and the result clamps to [math.MinInt32, math.MaxInt32], so a
// huge count can neither overflow to a sign-flipped value nor panic. Task 20
// hygiene: the old raw-int32 version made MaxInt32 doubled, negated or nudged
// silently wrap.
func applyCountOp(n int32, op string) int32 {
	v := int64(n)
	switch {
	case strings.HasPrefix(op, "Plus"):
		if x, err := strconv.Atoi(strings.TrimPrefix(op[len("Plus"):], ".")); err == nil {
			v += int64(x)
		}
	case strings.HasPrefix(op, "Minus"):
		if x, err := strconv.Atoi(strings.TrimPrefix(op[len("Minus"):], ".")); err == nil {
			v -= int64(x)
		}
	case strings.HasPrefix(op, "NMinus"):
		// Forge's operand-first subtraction: /NMinus.X reads X minus the
		// base value -- Wheel of Torture's "X is 3 minus the number of cards
		// in their hand" (TriggeredPlayer$CardsInHand/NMinus.3) and Scourge
		// of the Skyclaves's "20 minus the highest life total among players"
		// (SVar:X:SVar$Y/NMinus.20) are the carriers. The result may go
		// negative -- that is the point (Scourge is -1/-1 at a 21-life
		// opponent, and CR 208.2 keeps the CDA in every zone). 15 corpus
		// files carry the op, every operand numeric; an SVar-named operand
		// stays with the unimplemented /Plus.Y family below (left alone).
		if x, err := strconv.Atoi(strings.TrimPrefix(op[len("NMinus"):], ".")); err == nil {
			v = int64(x) - v
		}
	case strings.HasPrefix(op, "Times."):
		if x, err := strconv.Atoi(op[len("Times."):]); err == nil {
			v *= int64(x)
		}
	case op == "Twice":
		v *= 2
	case op == "Thrice":
		// Stronghold Arena's Count$TimesKicked/Thrice: the script writes its
		// own arithmetic as the op suffix ("gain 3 life for each time it was
		// kicked" = 3 x the kicks). rules/replacement.go's replCountOp
		// already knows the word.
		v *= 3
	case op == "HalfDown":
		v /= 2
	case op == "HalfUp":
		v = (v + 1) / 2
	case op == "ThirdUp":
		v = (v + 2) / 3
	case op == "Negative":
		v = -v
	case strings.HasPrefix(op, "Divide"):
		// Forge's AmountOperators division family, one arm for every
		// rounding direction the corpus spells:
		//
		//   DivideEvenlyUp.N   -- ceil(n/N)
		//   DivideEvenlyDown.N -- floor(n/N) (the pre-existing arm; the
		//                         ImmediateTrigger "one instance per pair of
		//                         remembered tokens" shape, diregraf_horde
		//                         and faebloom_trick)
		//   DivideEvenly.N / Divide.N -- floor(n/N), Forge's default division
		//
		// Legate Lanius, Caesar's Ace is the DivideEvenlyUp carrier:
		// `SVar:X:Count$Valid Creature.RememberedPlayerCtrl/DivideEvenlyUp.10`
		// ("each opponent sacrifices a tenth of the creatures they control,
		// rounded up"). Before this arm Every DivideEvenlyUp spelling fell
		// through applyCountOp untouched, so the op returned the WHOLE
		// creature count and the Decimate trigger over-sacrificed -- the
		// wrong-value direction, not a no-op.
		//
		// A missing, non-numeric or non-positive divisor leaves the value
		// unchanged rather than dividing by zero, the pre-existing guard. A
		// non-numeric divisor (DivideEvenlyDown.NumOpps, .Y -- an SVar name)
		// is a DIFFERENT class: this op has no Ctx to resolve it against and
		// deliberately leaves the value alone, the same silent standing no-op
		// every SVar-named operand gets (see the open ticket for the
		// /Plus.Y / /Minus.X / /Times.Y family, 215 raw corpus lines).
		if x, err := strconv.Atoi(divisionOperand(op)); err == nil && x > 0 {
			v = divideCountOp(v, int64(x), strings.HasPrefix(op, "DivideEvenlyUp"))
		}
	}
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}

// divisionOperand returns the divisor text of a Divide-family op suffix: the
// part after the op name's trailing dot (DivideEvenlyUp.10 -> "10",
// DivideEvenlyDown.NumOpps -> "NumOpps"). A suffix with no dot (a bare
// "Divide") returns "", which Atoi rejects and the caller reads as "no
// divisor named", leaving the value unchanged.
func divisionOperand(op string) string {
	i := strings.LastIndexByte(op, '.')
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(op[i+1:])
}

// divideCountOp divides v by the positive divisor x under the division op's
// rounding direction. The arithmetic is int64 and the caller clamps to the
// int32 range, matching applyCountOp's hygiene. ceil is DivideEvenlyUp's
// direction ("rounded up"); everything else floors, which for the
// non-negative counts the corpus reaches is also Go's truncating integer
// division -- the explicit correction below only matters for a negative
// operand (-3/2 truncates to -1, floor is -2).
func divideCountOp(v, x int64, ceil bool) int64 {
	if ceil {
		// Ceiling division that is correct for either sign: for a positive
		// operand add x-1 before the truncating divide; for a negative one
		// truncation toward zero IS the ceiling.
		if v >= 0 {
			return (v + x - 1) / x
		}
		return v / x
	}
	q := v / x
	if v%x != 0 && v < 0 {
		q--
	}
	return q
}

// ApplyCountOp is applyCountOp's exported form, for callers outside effects
// (rules' cost-modifier amount read) that must apply the same /Op suffix the
// count grammar uses -- one shared arithmetic, so the two cannot drift.
func ApplyCountOp(n int32, op string) int32 {
	return applyCountOp(n, op)
}

// SetSVars binds a copy of the SVar table to a context. A nil input leaves
// c.SVars nil, preserving the defensive-copy convention established by
// copyTargets in context.go.
func SetSVars(c *Ctx, sv map[string]string) {
	if sv == nil {
		c.SVars = nil
		return
	}
	copied := make(map[string]string, len(sv))
	for k, v := range sv {
		copied[k] = v
	}
	c.SVars = copied
}

// stripCastSourceAggregate peels the COUNT-level trailing `$<Property>`
// suffix off a Count$ThisTurnCast_<spec> body (the third argument form,
// task: ThisTurnCast extreme suffix), returning the peeled spec and the
// property. It fires ONLY when the segment after the LAST `$` is in the
// admitted vocabulary -- the four extreme reductions (isExtremeProperty)
// plus CardTypes -- so every other suffix keeps the pre-existing whole-token
// fail-closed read byte-identically. The peel happens at the END of the
// whole body because Forge attaches the property to the count, not to the
// last comma alternative: Rootha's `Instant.YouCtrl,Sorcery.YouCtrl$X`
// means "the greatest X among instant AND sorcery spells", and peeling the
// suffix before the comma split leaves both alternatives real predicates.
func stripCastSourceAggregate(spec string) (rest, prop string, ok bool) {
	i := strings.LastIndexByte(spec, '$')
	if i < 0 {
		return "", "", false
	}
	p := spec[i+1:]
	if !isExtremeProperty(p) && p != "CardTypes" {
		return "", "", false
	}
	return spec[:i], p, true
}

// aggregateCastSourceProperty folds a property over the matching casts'
// objects for the COUNT-level trailing `$<Property>` suffix. CardTypes
// counts the DISTINCT card types among the matching casts' faces (April
// O'Neil's "draw a card for each card type among spells you've cast this
// turn"), the zone head's distinct-set read; the extreme reductions take the
// max (or min, for Least) via extremePropertyValue, with zero matches
// yielding 0 rather than a sentinel (the zone fix's seen-guard convention).
// Both folds are order-insensitive, so the match order never reaches an
// event or a view.
func aggregateCastSourceProperty(h Host, ids []state.ObjID, prop string) (int32, bool) {
	g := h.Game()
	if prop == "CardTypes" {
		seen := make(map[string]bool)
		for _, id := range ids {
			o := g.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			for _, typ := range o.Face().Types {
				if cardTypeWords[typ] {
					seen[typ] = true
				}
			}
		}
		return int32(len(seen)), true
	}
	least := isLeastProperty(prop)
	var best int32
	seenAny := false
	for _, id := range ids {
		o := g.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		v := extremePropertyValue(h, o, prop)
		if !seenAny || (least && v < best) || (!least && v > best) {
			best, seenAny = v, true
		}
	}
	return best, true
}

// aggregateCastProperty sums one numeric property over the matching casts'
// objects (the ARGUMENTED !CastSaSource$<Property> aggregate forms' shared
// read; task castprov2). The property vocabulary is the zone-count heads':
// CardManaCost sums the faces' converted costs, CardPower/CardToughness the
// engine's derived (layer-aware) characteristics; any other property is
// unresolvable (0, false) — the whole Count$ then degrades per its caller's
// documented direction. Measured population: CardManaCost x1
// (call_forth_the_tempest); the other two are supported for symmetry.
func aggregateCastProperty(h Host, ids []state.ObjID, prop string) (int32, bool) {
	g := h.Game()
	var n int32
	for _, id := range ids {
		switch prop {
		case "CardManaCost":
			if o := g.Obj(id); o != nil && o.Face() != nil {
				n += o.Face().Cmc()
			}
		case "CardPower":
			n += h.Power(id)
		case "CardToughness":
			n += h.Toughness(id)
		default:
			return 0, false
		}
	}
	return n, true
}

// sumCounters totals a counter slice's POSITIVE counts (a drained slot sits
// in the slice at N == 0 and adds nothing), in slice order -- the ALL
// wildcard's one shared read for both the Count$CardCounters.ALL head and
// the ref-property form. The engine's OWN status markers ("Shield"/"Deathtouched",
// state.InternalCounterMarker) are excluded, the same exclusion
// state.Object.Counter("ALL") applies -- the two ALL reads cannot disagree
// (the marker-exclusion fix, branch agent-20260920T071934Z-c2f52dab).
func sumCounters(cs []state.Counter) int32 {
	var n int32
	for i := range cs {
		if cs[i].N > 0 && !state.InternalCounterMarker(cs[i].Kind) {
			n += cs[i].N
		}
	}
	return n
}

// partyRoles are CR 700.8's four party roles.
var partyRoles = [4]string{"Cleric", "Rogue", "Warrior", "Wizard"}

// partySize is CR 700.8's party size for c's controller: the largest number
// of distinct roles among Cleric, Rogue, Warrior and Wizard that can be
// filled by DIFFERENT creatures they control (a Changeling or a multi-role
// creature fills only one). Each creature's role set is read through the
// ordinary spec matcher (derived types, Changeling), then a 16-state subset
// walk finds the best assignment -- deterministic, no map ranged.
func partySize(g *state.Game, c *Ctx) int32 {
	sc := c.SpecContext(c.Controller)
	var reach [16]bool
	reach[0] = true
	for _, id := range g.Zone(state.ZBattlefield, c.Controller) {
		if !MatchesSpecCtx(g, "Creature", id, sc) {
			continue
		}
		var roles uint8
		for i, r := range partyRoles {
			if MatchesSpecCtx(g, "Creature."+r, id, sc) {
				roles |= 1 << i
			}
		}
		if roles == 0 {
			continue
		}
		next := reach
		for set := 0; set < 16; set++ {
			if !reach[set] {
				continue
			}
			for i := 0; i < 4; i++ {
				if roles&(1<<i) != 0 && set&(1<<i) == 0 {
					next[set|1<<i] = true
				}
			}
		}
		reach = next
	}
	best := 0
	for set := 0; set < 16; set++ {
		if reach[set] {
			if n := bits.OnesCount8(uint8(set)); n > best {
				best = n
			}
		}
	}
	return int32(best)
}
