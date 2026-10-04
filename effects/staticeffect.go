package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// StaticEffect$ <name> is the ChangeZone move rider that applies a continuous
// type/keyword/colour change to the moved card ("return it to the battlefield
// ... It's a Spirit Detective"): the value names an SVar on the resolution's
// table whose body is a Mode$ Continuous static line, and the move registers
// that static as a live continuous effect on each card it moved. The layer
// machinery the registration feeds is the same walk the S: static scanner
// feeds (rules/layers.go): layer 4 types/strips, layer 5 colours, layer 6
// keywords/ability grants/removals, layer 7b base P/T.
//
// Scoping is the registration's own, not the body's Affected$ evaluated
// against a remembered set: Forge's Animate bodies scope with Affected$
// <Base>.IsRemembered -- the cards the move remembered -- and the move path
// already KNOWS that set exactly (the ids it moved), so each moved card gets
// its own registration with Source = the card and the predicate rewritten to
// <Base>.Self (the same one-shot shape registerAnimateEffects uses, keeping
// the base's restriction). A body whose Affected$ names anything else is
// registered verbatim (Source = the moved card, You = the resolving
// controller): a spec this build cannot evaluate fails closed in the ordinary
// filter grammar and applies to nobody.
//
// Lifetime is the ordinary source-leaves rule (CR 611.2c): with no Duration$
// the grant is active while the moved permanent stays on the battlefield and
// ends when it leaves. A Duration$ the body does carry is honoured where this
// build can read it (Permanent, UntilEOT/end-of-turn, the next-turn spellings
// AddContinuous turns into a real turn boundary) and named in the unread Note
// where it cannot.
//
// Replay: the registration is part of the resolving SA's execution (it emits
// only ClockTick events through AddContinuous, the same channel effEffect
// uses), so a replay re-executing the trigger re-registers the identical
// effects at identical clock stamps.

// staticGrant is one parsed StaticEffect$ body: the layers it asks for and
// the parameters this build does not read.
type staticGrant struct {
	types               []string
	allCreatureTypes    bool
	removeCreatureTypes bool
	removeCardTypes     bool
	addColors           []string
	setColors           []string // SetColor$ overwrites (layer 5)
	addKeywords         []string
	abilities           []string // AddAbility$/AddAbilities$ SVar names
	removeAbilities     bool
	powerExpr, toughExp string
	hasPower, hasTough  bool
	// addPowerExpr/addToughExpr are the layer-7c ADDITIVE P/T parameters
	// (AddPower$/AddToughness$), distinct from the layer-7b SET parameters
	// above. They ride the continuous effect's expression fields so the layer
	// walk evaluates them (SVar- and Count$-capable) against the grant's own
	// SVar table, exactly as the printed-static scanner's layer walk does.
	addPowerExpr, addToughExpr         string
	hasAddPower, hasAddTough           bool
	addPowerAffected, addToughAffected bool
	duration                           string
	untilEOT                           bool
	permanent                          bool
	affectedZone                       string
	unread                             []string
}

// staticEffectTypeList parses an additive TYPE parameter: the comma-separated
// grammar whose elements may carry the " & " conjunction (rules' statList).
// A "ChosenType" element stays verbatim here; the caller resolves it against
// the moved card's own recorded choice exactly as the static scanner's
// resolveChosenTypes does.
func staticEffectTypeList(raw string) []string {
	var out []string
	for v := range strings.SplitSeq(raw, ",") {
		for part := range strings.SplitSeq(strings.TrimSpace(v), " & ") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// parseStaticEffectGrant parses one Mode$ Continuous body into the layers it
// grants, together with the body's Affected$ spec. Colour values
// colourLetters cannot fully parse fail closed (the Animate Colors$
// direction): the layer is skipped and the parameter named in unread rather
// than an empty parse overwriting the card's colours.
// rememberedAsSelf selects how an `Affected$ <base>.IsRemembered` spec is
// resolved. The StaticEffect$ route (true) already knows the exact moved set,
// so it rewrites the predicate to `<base>.Self` and registers Source = the
// moved card. An Effect-delivered grant (false) keeps the spec verbatim and
// registers Source = the effect's source, because the remembered cards are
// NOT the source (energybending's `Affected$ Permanent.IsRemembered`); the
// layer walk binds the registered Remembered set through matchesWithChars.
func parseStaticEffectGrant(params map[string]string, rememberedAsSelf bool) (staticGrant, string, bool) {
	var g staticGrant
	if !strings.EqualFold(strings.TrimSpace(params["Mode"]), "Continuous") {
		return g, "", false
	}
	if raw := strings.TrimSpace(params["AddTypes"]); raw != "" {
		g.types = append(g.types, staticEffectTypeList(raw)...)
	}
	if raw := strings.TrimSpace(params["AddType"]); raw != "" {
		g.types = append(g.types, staticEffectTypeList(raw)...)
	}
	g.allCreatureTypes = strings.EqualFold(strings.TrimSpace(params["AddAllCreatureTypes"]), "True")
	g.removeCreatureTypes = strings.EqualFold(strings.TrimSpace(params["RemoveCreatureTypes"]), "True")
	g.removeCardTypes = strings.EqualFold(strings.TrimSpace(params["RemoveCardTypes"]), "True")
	if raw, isAdd := params["AddColor"]; isAdd || params["AddColors"] != "" {
		if !isAdd {
			raw = params["AddColors"]
		}
		if cols, ok := colorLetters(strings.TrimSpace(raw)); ok {
			g.addColors = cols
		} else {
			g.unread = append(g.unread, "AddColor$ "+strings.TrimSpace(raw))
		}
	}
	if raw, isSet := params["SetColor"]; isSet || params["SetColors"] != "" {
		if !isSet {
			raw = params["SetColors"]
		}
		if cols, ok := colorLetters(strings.TrimSpace(raw)); ok {
			g.setColors = cols
		} else {
			g.unread = append(g.unread, "SetColor$ "+strings.TrimSpace(raw))
		}
	}
	g.addKeywords = cards.SplitKeywordList(params["AddKeyword"])
	for _, key := range []string{"AddAbility", "AddAbilities"} {
		for nm := range strings.SplitSeq(params[key], ",") {
			if nm = strings.TrimSpace(nm); nm != "" {
				g.abilities = append(g.abilities, nm)
			}
		}
	}
	g.removeAbilities = strings.EqualFold(strings.TrimSpace(params["RemoveAllAbilities"]), "True")
	if _, has := params["SetPower"]; has {
		g.powerExpr, g.hasPower = strings.TrimSpace(params["SetPower"]), true
	}
	if _, has := params["SetToughness"]; has {
		g.toughExp, g.hasTough = strings.TrimSpace(params["SetToughness"]), true
	}
	if _, has := params["AddPower"]; has {
		g.addPowerExpr, g.hasAddPower = strings.TrimSpace(params["AddPower"]), true
		g.addPowerAffected = AffectedXStaticAmount(g.addPowerExpr)
	}
	if _, has := params["AddToughness"]; has {
		g.addToughExpr, g.hasAddTough = strings.TrimSpace(params["AddToughness"]), true
		g.addToughAffected = AffectedXStaticAmount(g.addToughExpr)
	}
	g.duration = strings.TrimSpace(params["Duration"])
	g.affectedZone = strings.TrimSpace(params["AffectedZone"])
	switch {
	case g.duration == "":
	case strings.EqualFold(g.duration, "Permanent"):
		g.permanent = true
	case strings.EqualFold(g.duration, "UntilEOT"), strings.EqualFold(g.duration, "EndOfTurn"):
		g.untilEOT = true
	case IsNextTurnDuration(g.duration):
		// a real turn boundary: AddContinuous resolves it at registration
	default:
		g.unread = append(g.unread, "Duration$ "+g.duration)
		g.duration = ""
	}
	for _, key := range []struct{ name, val string }{
		{"AddStaticAbility$", params["AddStaticAbility"]},
		{"AddTrigger$", params["AddTrigger"]},
		{"AddHiddenKeyword$", params["AddHiddenKeyword"]},
		{"AddReplacementEffect$", params["AddReplacementEffect"]},
		{"SetName$", params["SetName"]},
		{"RemoveSubTypes$", params["RemoveSubTypes"]},
	} {
		if strings.TrimSpace(key.val) != "" {
			g.unread = append(g.unread, key.name)
		}
	}
	affects := strings.TrimSpace(params["Affected"])
	// Forge's Affected$ <Base>.IsRemembered means "the <Base> cards this move
	// moved" -- the set the caller already holds exactly -- so the
	// registration pins each moved card (Source = the card). The rewrite is
	// SUFFIX-based, not a single exact spelling: measured over the 55
	// StaticEffect$ carrier files the named bodies split 54
	// "Card.IsRemembered" + 1 "Creature.IsRemembered" (Lim-Dûl, the
	// Necromancer), and the corpus writes the predicate over every base word
	// (492 Card, 6 Creature, 2 Instant, 2 Permanent, 1 Equipment, 1 Land).
	// The base is kept and only the predicate is swapped for Self ("Card.Self"
	// / "Creature.Self"), which every matchesBase+matching-predicate path
	// already evaluates, so a "Creature.IsRemembered" body still restricts
	// the grant to the moved card when that card is a creature -- dropping
	// the base would over-apply the grant. A body with an empty Affected$ is
	// the Self default; any other spec rides verbatim (Source = the moved
	// card) and fails closed in the ordinary filter grammar if unreadable.
	if affects == "" {
		affects = "Card.Self"
	} else if rememberedAsSelf {
		if base, ok := strings.CutSuffix(strings.ToLower(affects), ".isremembered"); ok && base != "" && !strings.HasSuffix(base, "!") {
			affects = affects[:len(base)] + ".Self"
		}
	}
	return g, affects, true
}

// effectStaticGrantReadable rejects a partial Effect-delivered static. The
// StaticEffect$ move rider retains its historical Note-and-subset contract;
// an Effect with an unread condition or additional grant must not silently
// turn that condition into an unconditional layer modification.
func effectStaticGrantReadable(params map[string]string, g staticGrant) bool {
	if len(g.unread) != 0 {
		return false
	}
	for key := range params {
		if !effectStaticGrantReadableKeys.Has(key) {
			return false
		}
	}
	return true
}

// applyStaticEffect registers the StaticEffect$ rider of a ChangeZone move
// that landed on the battlefield. Called per moved card (or once with the
// whole moved batch -- the registration is per card either way), after the
// move's own riders (entry counters, control change, tapped entry, attach)
// have been emitted, so the grant applies to the card exactly as it entered.
func applyStaticEffect(h Host, c *Ctx, sa *cards.SA, to state.Zone, moved []state.ObjID) {
	if to != state.ZBattlefield || len(moved) == 0 {
		return
	}
	name := strings.TrimSpace(sa.ParamStr(cards.PKStaticEffect))
	if name == "" {
		return
	}
	// StaticEffectCheckSVar$ / StaticEffectSVarCompare$ (Dance of the Manse:
	// the animate only when the cast's X is 6 or more): the named value
	// resolves through the ordinary Num grammar against the resolution's
	// SVar table and must satisfy the comparison. An unresolvable value
	// degrades to 0 (Num's convention) and a failed comparison is a resolved
	// condition being false -- both skip the registration silently; a
	// comparison op this parser does not know fails closed the same way.
	if gate := strings.TrimSpace(sa.ParamStr(cards.PKStaticEffectCheckSVar)); gate != "" {
		op := strings.TrimSpace(sa.ParamStr(cards.PKStaticEffectSVarCompare))
		if op == "" {
			op = "GT0"
		}
		value := Num(h, c, sa, "StaticEffectCheckSVar", 0)
		lop := "GT"
		th := 0
		if len(op) >= 2 {
			lop = strings.ToUpper(op[:2])
			if n, err := strconv.Atoi(op[2:]); err == nil {
				th = n
			} else {
				return // an unparseable threshold cannot be satisfied
			}
		} else {
			return
		}
		pass := false
		switch CmpOpOf(lop) {
		case CmpGE:
			pass = value >= int32(th)
		case CmpGT:
			pass = value > int32(th)
		case CmpEQ:
			pass = value == int32(th)
		case CmpLE:
			pass = value <= int32(th)
		case CmpLT:
			pass = value < int32(th)
		}
		if !pass {
			return
		}
	}
	mode, params := parseStaticLine(c.SVars, name)
	if mode == "" && len(params) == 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "StaticEffect$ " + name + " names no SVar body; not registered"})
		return
	}
	g, affects, ok := parseStaticEffectGrant(params, true)
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "StaticEffect$ " + name + " body is not a Mode$ Continuous static; not registered"})
		return
	}
	// The unread remainder is named ONCE per application, never per card.
	if len(g.unread) > 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "StaticEffect$ " + name + " unread: " + strings.Join(g.unread, "/")})
	}
	for _, id := range moved {
		registerStaticEffectGrant(h, c, id, affects, g, staticGrantLifetime{})
	}
}

// staticGrantLifetime carries the registration riders an Effect-delivered
// grant shares across every layer it touches. registerStaticEffectGrant
// stamps them on EACH effect it registers, so a multi-layer body's layer-6
// grant expires and forgets on exactly the same schedule as its layer-4 one
// (a rider left on only one layer would outlive the effect). The
// StaticEffect$ ChangeZone route passes the zero value: its lifetime is the
// body's own Duration$, which the parser already read.
type staticGrantLifetime struct {
	Name          string
	Remembered    []state.ObjID
	ForgetOnMoved string
	ExileOnMoved  string
	ForgetCounter string
	ImprintOnHost bool
	ForgetOnCast  string
	ChosenNumber  int32
	// FromEffect marks the grant registrations as created by an api:Effect
	// (state.ContinuousEffect.FromEffect): the source-scoped one-shot
	// self-exile ender keys on it. The printed StaticEffect$ move rider
	// leaves it false, so only the Effect route is ended by the idiom.
	FromEffect bool
}

// stampGrantLifetime applies lt to one layer's registration. Zero-value
// fields are left alone so the StaticEffect$ route keeps exactly the fields
// registerStaticEffectGrant sets itself.
func stampGrantLifetime(ce *state.ContinuousEffect, lt staticGrantLifetime) {
	if lt.Name != "" {
		ce.Name = lt.Name
	}
	if lt.Remembered != nil {
		ce.Remembered = lt.Remembered
	}
	if lt.ForgetOnMoved != "" {
		ce.ForgetOnMoved = lt.ForgetOnMoved
	}
	if lt.ExileOnMoved != "" {
		ce.ExileOnMoved = lt.ExileOnMoved
	}
	if lt.ForgetCounter != "" {
		ce.ForgetCounter = lt.ForgetCounter
	}
	if lt.ImprintOnHost {
		ce.ImprintOnHost = true
	}
	if lt.ForgetOnCast != "" {
		ce.ForgetOnCast = lt.ForgetOnCast
	}
	if lt.ChosenNumber != 0 {
		ce.ChosenNumber = lt.ChosenNumber
	}
	if lt.FromEffect {
		ce.FromEffect = true
	}
}

// registerStaticEffectGrant is the one per-card registration path: the same
// layer split registerAnimateEffects uses, with the static body's expression
// parameters (SetPower$/SetToughness$ SVar-capable) left for the layer walk
// to resolve against the grant's own SVar table. It reports whether it
// registered anything: a body whose only parameters are unread (an
// AddHiddenKeyword$-only line) registers nothing, so a caller can fall back
// to its own honest Note rather than claiming a grant went live.
func registerStaticEffectGrant(h Host, c *Ctx, id state.ObjID, affects string, g staticGrant, lt staticGrantLifetime) bool {
	registered := false
	add := func(ce state.ContinuousEffect) {
		stampGrantLifetime(&ce, lt)
		h.AddContinuous(ce)
		registered = true
	}
	// A "ChosenType" element resolves against the moved card's own recorded
	// "as this enters" choice (the static scanner's resolveChosenTypes
	// direction); a card with no recorded choice fails closed to no grant
	// rather than leaking the literal word onto the object.
	types := make([]string, 0, len(g.types))
	chosenMissing := false
	if o := h.Game().Obj(id); o != nil {
		for _, t := range g.types {
			if t != "ChosenType" {
				types = append(types, t)
				continue
			}
			if o.ChosenType == "" {
				chosenMissing = true
				continue
			}
			types = append(types, o.ChosenType)
		}
	}
	if len(types) > 0 || g.removeCardTypes || g.removeCreatureTypes || g.allCreatureTypes {
		add(state.ContinuousEffect{
			Source: id, Affects: affects, Controller: c.Controller,
			Layer:               state.LType,
			AddTypes:            types,
			AddAllCreatureTypes: g.allCreatureTypes,
			RemoveCreatureTypes: g.removeCreatureTypes,
			RemoveCardTypes:     g.removeCardTypes,
			AffectedZone:        g.affectedZone,
			Duration:            g.duration, Permanent: g.permanent, UntilEOT: g.untilEOT,
			SVars: c.SVars,
		})
	}
	if chosenMissing {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "StaticEffect$ ChosenType names a card with no recorded choice"})
	}
	if len(g.setColors) > 0 {
		// SetColor$ is the layer-5 overwrite (the scanner's direction): the
		// moved card's colours are exactly the named set.
		add(state.ContinuousEffect{
			Source: id, Affects: affects, Controller: c.Controller,
			Layer:           state.LColor,
			AddColors:       g.setColors,
			OverwriteColors: true,
			AffectedZone:    g.affectedZone,
			Duration:        g.duration, Permanent: g.permanent, UntilEOT: g.untilEOT,
		})
	}
	if len(g.addColors) > 0 {
		add(state.ContinuousEffect{
			Source: id, Affects: affects, Controller: c.Controller,
			Layer:        state.LColor,
			AddColors:    g.addColors,
			AffectedZone: g.affectedZone,
			Duration:     g.duration, Permanent: g.permanent, UntilEOT: g.untilEOT,
		})
	}
	// CR 613.1f: the ability removal, the keyword grants and the ability
	// grants of ONE static body are a single simultaneous modification, so
	// they register as ONE LAbilities effect. The layer walk's in-effect
	// order (clear on RemoveAbilities, then append AddKeywords) is then the
	// card text's own, and the walk's removal-before-grant tie-break applies.
	// Splitting them into separate AddContinuous calls was a real defect:
	// each call stamps its own ClockTick, so a body carrying BOTH
	// RemoveAllAbilities$ True AND AddKeyword$ (bronzehide_lion,
	// hellcat_undying_vigilante, harold_and_bob_first_numens -- 3 of the 55
	// carriers) had the keyword wiped by its own removal (the removal ran at
	// a LATER timestamp). This mirrors copypermanent.go's single-effect shape.
	if g.removeAbilities || len(g.addKeywords) > 0 || len(g.abilities) > 0 {
		add(state.ContinuousEffect{
			Source: id, Affects: affects, Controller: c.Controller,
			Layer:           state.LAbilities,
			RemoveAbilities: g.removeAbilities,
			AddKeywords:     g.addKeywords,
			AddAbilities:    g.abilities,
			AffectedZone:    g.affectedZone,
			Duration:        g.duration, Permanent: g.permanent, UntilEOT: g.untilEOT,
			SVars: c.SVars,
		})
	}
	if g.hasPower || g.hasTough {
		add(state.ContinuousEffect{
			Source: id, Affects: affects, Controller: c.Controller,
			Layer: state.LPT, Sub: state.SubSet,
			SetPowerExpr:        g.powerExpr,
			SetToughnessExpr:    g.toughExp,
			SetPowerPresent:     g.hasPower,
			SetToughnessPresent: g.hasTough,
			StaticSet:           true,
			HasSet:              true,
			AffectedZone:        g.affectedZone,
			Duration:            g.duration, Permanent: g.permanent, UntilEOT: g.untilEOT,
			SVars: c.SVars,
		})
	}
	// The layer-7c ADDITIVE half (AddPower$/AddToughness$, the +X/+X a lord or
	// emblem grants). Registering it on the SAME builder is what keeps the two
	// delivery routes from disagreeing: the printed-static scanner's layer walk
	// stores the raw expression in AddPowerExpr/AddToughnessExpr and evaluates
	// it per derivation, so the Effect route does the same rather than
	// collapsing an SVar/Count$ value to a numeric zero. A body carrying both a
	// Set and an Add parameter registers both (the scanner emits both), and the
	// layer order (7b before 7c) applies the add after the set.
	if g.hasAddPower || g.hasAddTough {
		add(state.ContinuousEffect{
			Source: id, Affects: affects, Controller: c.Controller,
			Layer: state.LPT, Sub: state.SubModify,
			AddPowerExpr:         g.addPowerExpr,
			AddToughnessExpr:     g.addToughExpr,
			AddPowerAffected:     g.addPowerAffected,
			AddToughnessAffected: g.addToughAffected,
			AffectedZone:         g.affectedZone,
			Duration:             g.duration, Permanent: g.permanent, UntilEOT: g.untilEOT,
			SVars: c.SVars,
		})
	}
	return registered
}

// AffectedXStaticAmount is the shared per-affected-object convention for
// printed statics and Effect-delivered grants. Num strips exactly one leading
// sign before resolving an SVar name; other SVar names and inline count
// expressions remain anchored on the grantor on both routes.
func AffectedXStaticAmount(expr string) bool {
	expr = strings.TrimSpace(expr)
	if len(expr) > 1 && (expr[0] == '+' || expr[0] == '-') {
		expr = expr[1:]
	}
	return expr == "AffectedX"
}

var effectStaticGrantReadableKeys = state.NewNameSet("Mode", "Affected", "AffectedZone", "Description", "AddTypes", "AddType", "AddAllCreatureTypes", "RemoveCreatureTypes", "RemoveCardTypes", "AddColor", "AddColors", "SetColor", "SetColors", "AddKeyword", "AddAbility", "AddAbilities", "RemoveAllAbilities", "SetPower", "SetToughness", "AddPower", "AddToughness")
