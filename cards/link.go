package cards

// maxSVarDepth caps sub-ability nesting. The deepest real chain in the corpus
// is well under this; the cap exists so a cyclic SVar reference in upstream
// data cannot hang the compiler.
const maxSVarDepth = 32

// Link resolves SVar-named sub-abilities into a tree. Call it once, after
// Parse, before the card is used.
func (c *Card) Link() []Diag {
	var diags []Diag
	for _, f := range c.Faces {
		diags = append(diags, f.link(c.Path)...)
	}
	return diags
}

func (f *Face) link(path string) []Diag {
	f.expandKeywords()
	var diags []Diag

	var resolve func(name string, depth int) *SA
	resolve = func(name string, depth int) *SA {
		if name == "" || depth > maxSVarDepth {
			return nil
		}
		body, ok := f.SVars[name]
		if !ok {
			diags = append(diags, Diag{path, "unresolved SVar ref: " + name})
			return nil
		}
		sa, d := parseSA(path, body)
		diags = append(diags, d...)
		if sa != nil {
			sa.Sub = resolve(sa.Params["SubAbility"], depth+1)
		}
		return sa
	}
	walk := func(sa *SA) {
		for d := 0; sa != nil && d <= maxSVarDepth; d++ {
			if sa.Sub == nil {
				sa.Sub = resolve(sa.Params["SubAbility"], d+1)
			}
			sa = sa.Sub
		}
	}

	for _, a := range f.Abilities {
		walk(a)
	}
	for i := range f.Triggers {
		f.Triggers[i].Effect = resolve(f.Triggers[i].Params["Execute"], 0)
		walk(f.Triggers[i].Effect)
	}
	for i := range f.Repls {
		f.Repls[i].With = resolve(f.Repls[i].Params["ReplaceWith"], 0)
		walk(f.Repls[i].With)
	}
	return diags
}

// builtinSVars are the SVar bodies the RULES layer references by name from
// delayed-trigger registrations (dash's end-step return, warp's end-step
// exile, encore's end-step sacrifice) whose SOURCE object has no SVar table
// to hold them: a Face is immutable card-script data, and the registering
// source may be a token or copy with no script SVars at all. resolveSVar
// falls back to this table when the face's own table lacks the name, so a
// replayed DelayedPush resolves the identical ability a live game did.
var builtinSVars = map[string]string{
	// Evoke's mandatory ETB trigger. It is minted as a real triggered ability
	// by KeywordTriggerPush, so players receive priority and may counter it.
	"__kwEvokeSacrifice": "DB$ Sacrifice | Defined$ Self",
	// Madness's real triggered ability is handled by rules at resolution: its
	// owner may cast the exiled source card for the madness cost, otherwise it
	// goes to the graveyard. A distinct API marker lets the ordinary stack
	// object stay respondable without pretending this is an effects primitive.
	"__kwMadnessCast": "DB$ MadnessCast",
	// Dash (CR 702): "returned from the battlefield to its owner's hand at
	// the beginning of the next end step". The registered source is the
	// dashed permanent itself, so Defined$ Self is it.
	"__kwDashReturn": "DB$ ChangeZone | Defined$ Self | Origin$ Battlefield | Destination$ Hand",
	// Warp: "exile this creature at the beginning of the next end step".
	"__kwWarpExile": "DB$ ChangeZone | Defined$ Self | Origin$ Battlefield | Destination$ Exile",
	// Unearth (CR 702.84a): "exile it at the beginning of the next end
	// step". A distinct name from __kwWarpExile because rules reads the
	// registration back at the leave-replacement boundary
	// (rules/unearth.go's unearthReplacementApplies) to decide whether a
	// permanent that would leave the battlefield is exiled instead; a
	// warped creature's own departure must not be captured by that read.
	// The registered source is the unearthed permanent itself, so
	// Defined$ Self is it.
	"__kwUnearthExile": "DB$ ChangeZone | Defined$ Self | Origin$ Battlefield | Destination$ Exile",
	// Encore tokens: "Sacrifice them at the beginning of the next end step".
	// Each token registers its own delayed trigger, so Self is that token.
	"__kwEncoreSacrifice":      "DB$ Sacrifice | Defined$ Self",
	"__kwEncoreSacrificeGroup": "DB$ Sacrifice | Defined$ DelayTriggerRememberedLKI",
	// AtEOT$ Destroy (the end-of-turn rider's destroy arm, read by the shared
	// effects.scheduleAtEOT helper on Animate/Pump/PumpAll/Token/ChangeZone
	// bodies): the registered source is the affected permanent itself.
	"__kwAtEOTDestroy": "DB$ Destroy | Defined$ Self",
	// Earthbend (effects/earthbend.go): the animated land's "when it dies or
	// is exiled, return it to the battlefield tapped" promise. The
	// registration's Source is the land itself, so Defined$ Self is it. The
	// name deliberately carries none of the tracked prefixes above
	// (__kwDash/__kwWarp/__kwAtEOT), so a re-entering land is NOT
	// incarnation-tracked: the one-shot registration is consumed at its
	// first fire, and the returned land is a plain tapped land whose
	// counters were removed by CR 122.2. Origin$ is comma-split by
	// effects.ParseZones at resolution, so the single body serves both the
	// Graveyard and Exile registrations.
	"__kwEarthbendReturn": "DB$ ChangeZone | Defined$ Self | Origin$ Graveyard,Exile | Destination$ Battlefield | Tapped$ True",
	// K:MayFlashSac (CR 702.8): "if you cast it any time a sorcery couldn't
	// have been cast, the controller of the permanent it becomes sacrifices
	// it at the beginning of the next cleanup step". The registration's
	// Source is the permanent the spell became, so Defined$ Self is it.
	"__kwMayFlashSacrifice": "DB$ Sacrifice | Defined$ Self",
	// Rebound (CR 702.95a): "At the beginning of your next upkeep, you may
	// cast this card from exile without paying its mana cost." The
	// registration's Source is the exiled card itself, so the Play's
	// population is Card.Self; ValidZone$ Exile pins the offer to the exile
	// zone, so a card that left exile (and even one that later returned) is
	// never cast from anywhere else by a stale registration. Optional$ True
	// makes the ask declinable -- a decline leaves the card exiled, which is
	// exactly CR 702.95's result.
	"__kwReboundCast": "DB$ Play | Valid$ Card.Self | ValidZone$ Exile | WithoutManaCost$ True | Optional$ True",
	// Blitz (CR 702.152c-d): the cast-for-blitz permanent is sacrificed at
	// the beginning of the next end step. The registration's Source is the
	// permanent itself, so Defined$ Self is it.
	"__kwBlitzSacrifice": "DB$ Sacrifice | Defined$ Self",
	// Blitz (CR 702.152c): the cast-for-blitz permanent has "When this
	// creature dies, draw a card." rules/altcast.go's blitzEnter registers a
	// ContinuousEffect whose AddTrigger$ names this SVar, so the body is
	// resolved against the permanent's own face (or this builtin fallback).
	"__kwBlitzDraw": "DB$ Draw | NumCards$ 1",
}

// ResolveSVar compiles the ability an SVar name refers to, recursively
// resolving its own SubAbility$ chain the same way Link does for a face's
// printed abilities. Some Forge parameters point at a sub-ability by SVar
// name rather than through the auto-linked "SubAbility$" (Charm's Choices$,
// Repeat's RepeatSubAbility$), so effects primitives that need to actually
// run one of those need a way to compile it on demand, after Parse/Link, from
// the resolving face's own SVar table. A missing name, or a body that fails
// to parse, yields nil rather than a partial result -- callers already treat
// a nil *SA as "nothing to run", the same degrade-to-nothing convention Num
// and EvalCount use for an expression this build does not model.
func ResolveSVar(svars map[string]string, name string) *SA {
	return resolveSVar(svars, name, 0)
}

func resolveSVar(svars map[string]string, name string, depth int) *SA {
	if name == "" || depth > maxSVarDepth {
		return nil
	}
	body, ok := svars[name]
	if !ok {
		body, ok = builtinSVars[name]
	}
	if !ok {
		return nil
	}
	sa, _ := parseSA("", body)
	if sa != nil {
		sa.Sub = resolveSVar(svars, sa.Params["SubAbility"], depth+1)
	}
	return sa
}
