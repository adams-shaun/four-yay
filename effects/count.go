package effects

import (
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
	raw, present := sa.Params[key]
	return numText(h, c, ParamText{Text: raw, Present: present}, def)
}

// numText is Num over a compiled parameter (a typed parameter struct's
// ParamText) instead of a key read.
func numText(h Host, c *Ctx, p ParamText, def int32) int32 {
	if n, ok := numResolvedText(h, c, p, def); ok {
		return n
	}
	if p.Present {
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
	switch statIsPowerKeyCodes.Code(string(key)) {
	case statIsPowerKeyNumAtt:
		return true, true
	case statIsPowerKeyNumDef:
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
	raw, ok := sa.Params[key]
	return numResolvedText(h, c, ParamText{Text: raw, Present: ok}, def)
}

// numResolvedText is NumResolved over a compiled parameter.
func numResolvedText(h Host, c *Ctx, p ParamText, def int32) (int32, bool) {
	if c == nil {
		c = new(Ctx)
	}
	if !p.Present {
		return def, false
	}
	raw := strings.TrimSpace(p.Text)
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
	raw, ok := sa.Params[key]
	return numResolvedStrictText(h, c, ParamText{Text: raw, Present: ok}, def)
}

// numResolvedStrictText is NumResolvedStrict over a compiled parameter.
func numResolvedStrictText(h Host, c *Ctx, p ParamText, def int32) (int32, bool) {
	n, ok := numResolvedText(h, c, p, def)
	if !ok || c == nil {
		return n, ok
	}
	raw := strings.TrimSpace(p.Text)
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
	n, _ := EvalCountOK(h, c, expr)
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
//
// The count reads the board's derived characteristics by default (W1d): a
// context that carries no layer-3 rename or layer-4 type table -- one built
// outside effects.Resolve, such as a static's numeric-RHS SVar, a CDA, a cost
// or a target-offer count -- is bound to the host's always-published pair for
// the evaluation and restored afterwards, so the same Count$Valid answers the
// same number at offer time and at resolution.
func EvalCountOK(h Host, c *Ctx, expr string) (int32, bool) {
	if c == nil || (c.Layers.EffectiveNames != nil && c.Layers.DerivedTypes != nil) {
		return evalCountExprOK(h, c, expr, 0)
	}
	names, types := c.Layers.EffectiveNames, c.Layers.DerivedTypes
	board := layerTablesFor(h, nil)
	if names == nil {
		c.Layers.EffectiveNames = board.EffectiveNames
	}
	if types == nil {
		c.Layers.DerivedTypes = board.DerivedTypes
	}
	n, ok := evalCountExprOK(h, c, expr, 0)
	c.Layers.EffectiveNames, c.Layers.DerivedTypes = names, types
	return n, ok
}

// maxCountDepth bounds the SVar recursion the Compare head introduces: a
// compared value or a branch may name another SVar, whose body may itself be
// a Count$Compare naming further SVars. The corpus chains are two deep
// (Nissa's Pilgrimage: X -> Y; The Biblioplex: X -> Y -> Z), so 8 is generous
// headroom against a self-referential or accidental-cycle SVar table, which
// would otherwise be the only unbounded recursion in this evaluator.
const maxCountDepth = 8

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
	if body, ok := strings.CutPrefix(expr, "TriggerObjectsCurrentCastSpells$"); ok {
		return evalTriggerCurrentCastSpellsOK(h, c, body)
	}
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
				sub := NewCtxPtr(o.ID, o.Controller, CtxInit{X: o.X})
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
		n := c.Repl.Amount
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
		c = new(Ctx)
	}
	cc := *c
	cc.Source = obj
	return EvalCount(h, &cc, expr)
}

// evalCountBody is the Count$ head dispatch; ok is false only at the
// fallthrough (the head matched nothing), never inside a modelled branch -- a
// modelled head that legitimately counts zero still counts as evaluated.
//
// The arms are grouped into phase evaluators in this package, tried here in
// the original arm order: evalCountBodyProvenance (the whole-body
// ThisTurnCast_/ThisTurnActivated_ heads, which keep their spaces and so run
// before the generic head/argument split), then -- after the split and the
// space-less OptionalGenericCostPaid peel -- evalCountBodyCost,
// evalCountBodyPaid, evalCountBodyPlayer, evalCountBodyObjHeads,
// evalCountBodyDotted and evalCountBodyZone. matched distinguishes a claimed
// head (whose ok is the arm's own verdict, false for a recognised-but-
// unmodelled shape) from the fallthrough, so each phase hands the body to
// the next exactly as the sequential arms did.
func evalCountBody(h Host, c *Ctx, body string, depth int) (int32, bool) {
	g := h.Game()
	if v, ok, matched := evalCountBodyProvenance(h, c, body, depth); matched {
		return v, ok
	}
	head, arg, _ := strings.Cut(body, " ")
	arg = strings.TrimSpace(arg)
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
	if v, ok, matched := evalCountBodyCost(h, c, g, head, arg, depth); matched {
		return v, ok
	}
	if v, ok, matched := evalCountBodyPaid(h, c, g, head, arg, depth); matched {
		return v, ok
	}
	if v, ok, matched := evalCountBodyPlayer(h, c, g, head, arg, depth); matched {
		return v, ok
	}
	if v, ok, matched := evalCountBodyObjHeads(h, c, g, head, arg, depth); matched {
		return v, ok
	}
	if v, ok, matched := evalCountBodyDotted(h, c, g, head, arg, depth); matched {
		return v, ok
	}
	if v, ok, matched := evalCountBodyZone(h, c, g, head, arg, depth); matched {
		return v, ok
	}
	return 0, false
}

type statIsPowerKeyCode uint16

const (
	statIsPowerKeyNumAtt statIsPowerKeyCode = iota + 1
	statIsPowerKeyNumDef
)

var statIsPowerKeyCodes = state.NewStrCodes(
	state.StrEntry[statIsPowerKeyCode]{Key: "NumAtt", Val: statIsPowerKeyNumAtt},
	state.StrEntry[statIsPowerKeyCode]{Key: "Power", Val: statIsPowerKeyNumAtt},
	state.StrEntry[statIsPowerKeyCode]{Key: "SetPower", Val: statIsPowerKeyNumAtt},
	state.StrEntry[statIsPowerKeyCode]{Key: "TokenPower", Val: statIsPowerKeyNumAtt},
	state.StrEntry[statIsPowerKeyCode]{Key: "FaceDownPower", Val: statIsPowerKeyNumAtt},
	state.StrEntry[statIsPowerKeyCode]{Key: "NumDef", Val: statIsPowerKeyNumDef},
	state.StrEntry[statIsPowerKeyCode]{Key: "Toughness", Val: statIsPowerKeyNumDef},
	state.StrEntry[statIsPowerKeyCode]{Key: "SetToughness", Val: statIsPowerKeyNumDef},
	state.StrEntry[statIsPowerKeyCode]{Key: "TokenToughness", Val: statIsPowerKeyNumDef},
	state.StrEntry[statIsPowerKeyCode]{Key: "FaceDownToughness", Val: statIsPowerKeyNumDef},
)
