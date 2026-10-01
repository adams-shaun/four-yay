package effects

import (
	"math"
	"strconv"
	"strings"
)

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
