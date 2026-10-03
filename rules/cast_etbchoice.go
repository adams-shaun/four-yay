package rules

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func etbChoiceKind(api string) string {
	switch api {
	case "NameCard":
		return "name"
	case "ChooseType":
		return "type"
	case "ChooseNumber":
		return "number"
	case "ChooseColor":
		return "color"
	case "ChooseEvenOdd":
		return "evenodd"
	case "Clone":
		return "copy"
	}
	return ""
}

// etbPayLifeBound reports whether r's ReplaceWith$ body is the exact
// "as CARDNAME enters, pay any amount of life" shape -- a `Cost$
// Mandatory PayLife<X>` body whose face SVar:X is Count$xPaid (the announced
// value the body stores) -- and, if so, the largest X the payer may announce:
// the payer's life total, further capped by the body's `XMax$ <SVar>` when it
// names one that resolves (Nameless Race's Limit: the white permanents plus
// white cards in opponents' graveyards). A body that is not the exact shape
// (a fixed PayLife cost, another API, a missing Count$xPaid binding) returns
// false and keeps the ordinary replacement path.
func (e *Engine) etbPayLifeBound(o *state.Object, with *cards.SA) (int, bool) {
	if with == nil || o.Face() == nil {
		return 0, false
	}
	body, present := o.Face().SVars["X"]
	if !present || !strings.EqualFold(strings.TrimSpace(body), "Count$xPaid") {
		return 0, false
	}
	c := ParseCost(with.ParamStr(cards.PKCost))
	if len(c.LifeX) == 0 {
		return 0, false
	}
	bound := int(e.G.Players[o.Controller].Life)
	if bound < 0 {
		bound = 0
	}
	if raw := strings.TrimSpace(with.Params["XMax"]); raw != "" {
		ctx := effects.NewCtxPtr(o.ID, o.Controller, effects.CtxInit{SVars: o.Face().SVars})
		if cap, ok := effects.NumResolved(e, ctx, with, "XMax", 0); ok {
			if cap < 0 {
				cap = 0
			}
			if int(cap) < bound {
				bound = int(cap)
			}
		}
	}
	return bound, true
}

// etbPayLifeOptions builds the ascending 0..bound option list a
// "pay any amount of life" entry offers; option 0 is the legal pay-nothing
// announcement (Oracle: "pay any amount" includes zero). The Kind is the
// shared "number" kind so the answer records through the same
// events.Choose fold a ChooseNumber uses, and resumeETBEntry reads the
// announced X off the option's Amount.
func etbPayLifeOptions(you state.PlayerID, card state.ObjID, bound int) []decision.Option {
	out := make([]decision.Option, 0, bound+1)
	for i := 0; i <= bound; i++ {
		label := strconv.Itoa(i) + " life"
		if i == 1 {
			label = "1 life"
		}
		out = append(out, decision.Option{Index: len(out), Kind: "paylife", Label: label, Amount: i, Obj: card, Player: you})
	}
	return out
}

// entryETBChoice returns the ordinal-th choice that must be made for ev's
// battlefield entry. It is deliberately derived from the same prospective
// MoveZone event the replacement matcher will later consume: an ActiveZones or
// ValidCard gate therefore cannot make the engine ask about a replacement that
// will not apply. The ordinal lets several choices on one permanent suspend
// and resume without adding transient state to the event log.
func (e *Engine) entryETBChoice(ev events.Event, ordinal int) (etbChoice, bool) {
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Face() == nil || ev.To != state.ZBattlefield {
		return etbChoice{}, false
	}
	you := o.Controller
	seen := 0
	if o.Face().HasKeyword("Riot") {
		if seen == ordinal {
			return etbChoice{kind: "riot", options: []decision.Option{
				{Index: 0, Kind: "riot", Label: "Enter with a +1/+1 counter", Obj: o.ID, Player: you},
				{Index: 1, Kind: "riot", Label: "Gain haste", Obj: o.ID, Player: you},
			}}, true
		}
		seen++
	}
	if o.Face().HasKeyword("Unleash") {
		if seen == ordinal {
			return etbChoice{kind: "unleash", options: unleashOptions(o.ID, you)}, true
		}
		seen++
	}
	for i := range o.Face().Repls {
		r := &o.Face().Repls[i]
		if r.With == nil || !e.replacementMatches(*r, o.ID, ev) {
			continue
		}
		if r.ParamStr(cards.PKKeyword) != "ETBReplacement" {
			// A non-keyword R:Event$ Moved replacement whose ReplaceWith$ body
			// carries `Cost$ Mandatory PayLife<X>` (Minion of the Wastes,
			// Phyrexian Processor, Nameless Race: "as CARDNAME enters, pay any
			// amount of life"). The payer announces X here, at the entry
			// boundary, where a suspend-and-resume is possible -- the
			// replacement body itself runs off the Move fold with no ask
			// channel (the approximation this closes). Only the exact
			// Count$xPaid life-announcement shape is offered; anything else
			// falls through to the ordinary replacement path unchanged.
			bound, ok := e.etbPayLifeBound(o, r.With)
			if !ok {
				continue
			}
			if seen == ordinal {
				return etbChoice{kind: "paylife", options: etbPayLifeOptions(you, o.ID, bound)}, true
			}
			seen++
			continue
		}
		kind := etbChoiceKind(r.With.API)
		if kind == "" {
			continue
		}
		// The ETB Clone slice is deliberately narrow: offering a copy while
		// dropping an exception rider is worse than retaining today's loud
		// unimplemented-API fallback. A body outside the whitelist is not a
		// choice at all, so it is skipped before the ordinal is counted.
		if kind == "copy" && !etbCloneWhitelist(r.With, o.Face().SVars) {
			continue
		}
		if seen == ordinal {
			// The fifth filter slot means different things per kind: Choices$
			// is the copy-template selector the clone slice reads, while
			// ValidDescription$ is Forge prompt text for the name kinds, not
			// a second filter (effects.NameChoices reads it only as a safety
			// fallback when ValidCards$ is absent).
			selector := r.With.ParamStr(cards.PKValidDescription)
			if kind == "copy" {
				selector = r.With.ParamStr(cards.PKChoices)
			}
			var opts []decision.Option
			if kind == "type" {
				// The type ask is category-aware (task ct1): a Type$ Basic Land
				// or Card or Planeswalker ranges over that category's real list,
				// exactly the list the mid-resolution ChooseType ask builds
				// (effects/type_choices.go), so the two asks cannot disagree.
				opts = e.typeChoiceOptions(you, o.ID, r.With.Params)
			} else {
				opts = e.etbOptions(you, o.ID, kind,
					r.With.ParamStr(cards.PKValidCards), selector,
					r.With.ParamStr(cards.PKType), r.With.Params["Exclude"], r.With.ParamStr(cards.PKChooseFromList))
			}
			if kind == "name" && len(opts) == 0 {
				// No name passes the filter: the legacy (no-universe) builder
				// only sees public objects, so "choose a nonbasic land card
				// name" with none in view (Alpine Moon, cardfuzz batch1 line
				// 14) built a Min 1 ask with zero options that no answer
				// could satisfy. Mirror effNameCard's own empty-list rule so
				// the two NameCard paths agree: without a corpus universe (or
				// with no ChooseFromList$) it names the deterministic legacy
				// stand-in; a universe-backed ChooseFromList$ with nothing
				// eligible names nothing, so there is no choice to pose and
				// the entry proceeds (the body's effNameCard then returns
				// without naming, as it does mid-resolution).
				if len(e.G.NameUniverse) > 0 && strings.TrimSpace(r.With.ParamStr(cards.PKChooseFromList)) != "" {
					continue
				}
				opts = []decision.Option{{Index: 0, Kind: "name",
					Label: effects.LegacyNameFallback(e.G, you)}}
			}
			if kind == "copy" {
				// ":Optional" on the keyword line is the "you MAY have it
				// enter as a copy" half; an empty template list also needs
				// the decline, or the ask would have zero options (the
				// totality rule in etbOptions' doc).
				optional := strings.Contains(strings.ToLower(r.Params["KeywordLine"]), ":optional")
				if optional || len(opts) == 0 {
					opts = append(opts, decision.Option{Index: len(opts), Kind: "clone",
						Label: "Enter as itself", Player: you})
				}
			}
			return etbChoice{kind: kind, options: opts}, true
		}
		seen++
	}
	return etbChoice{}, false
}

// etbColourLabels pairs the WUBRG letter the Choose event records with the
// option label the client shows, in fixed WUBRG order -- the same order every
// colour choice in this build offers (askManaColor, triggeredManaColourChoice,
// commanderIdentityColours). etbOptions and resumeETBEntry both read it, so
// the option offered and the letter recorded always agree.
var etbColourLabels = []struct{ letter, name string }{
	{"W", "White"}, {"U", "Blue"}, {"B", "Black"}, {"R", "Red"}, {"G", "Green"},
}

// etbColourLetter maps an option label (or already-a-letter) back to the
// WUBRG letter the event records; "" when the label is neither (the entry
// continuation only sees options etbOptions built, so the guard is defensive).
func etbColourLetter(name string) string {
	for _, cl := range etbColourLabels {
		if strings.EqualFold(name, cl.name) || strings.EqualFold(name, cl.letter) {
			return cl.letter
		}
	}
	return ""
}

// etbOptions builds the option list for one "as this enters" choice. It is a
// total list-pick -- every entryETBChoice caller is guaranteed at least one
// legal option (a name is anything on the board/hand/yard, a type falls back
// to "Human", a number is always 0..12) -- so no etb decision can ever be
// handed out with zero options, and nothing asks an empty choice (R-9's
// totality rule; see the Options here and the entry decision's Min/Max 1).
//
// Option list order is deterministic: names and types are sorted strings
// (never from a map), numbers are ascending.
// choices carries the per-kind filter text: Choices$ (the copy-template
// selector) for the clone slice, ValidDescription$ prompt text for the name
// kinds. Callers fill it per kind; see collectETBChoices.
func (e *Engine) etbOptions(you state.PlayerID, card state.ObjID, kind, validCards, choices, typeCategory, exclude string, chooseFromList ...string) []decision.Option {
	switch kind {
	case "color":
		// Exclude$ tokens (comma-separated, e.g. "black" on Black Dragon
		// Gate) remove the matching WUBRG label. Fail OPEN: a token
		// etbColourLetter cannot resolve is ignored, never emptied into an
		// ask with zero options (the totality rule in this doc comment).
		excluded := map[string]bool{}
		for tok := range strings.SplitSeq(exclude, ",") {
			if letter := etbColourLetter(strings.TrimSpace(tok)); letter != "" {
				excluded[letter] = true
			}
		}
		out := make([]decision.Option, 0, len(etbColourLabels))
		for _, cl := range etbColourLabels {
			if excluded[cl.letter] {
				continue
			}
			out = append(out, decision.Option{Index: len(out), Kind: "color", Label: cl.name})
		}
		if len(out) == 0 {
			// Totality guard: an exclusion naming every colour must never
			// empty the ask (corpus carriers exclude exactly one; this is
			// defensive against a future carrier).
			out = make([]decision.Option, 0, len(etbColourLabels))
			for _, cl := range etbColourLabels {
				out = append(out, decision.Option{Index: len(out), Kind: "color", Label: cl.name})
			}
		}
		return out
	case "copy":
		spec := strings.TrimSpace(choices)
		if spec == "" {
			spec = strings.TrimSpace(validCards)
		}
		if spec == "" {
			spec = "Creature.Other"
		}
		if !strings.Contains(spec, ".") && !strings.HasPrefix(spec, "Card") {
			spec = "Card." + spec
		}
		out := []decision.Option{}
		for _, p := range e.G.AliveFrom(0) {
			for _, id := range e.G.Zone(state.ZBattlefield, p) {
				o := e.G.Obj(id)
				if o != nil && o.Face() != nil && effects.MatchesSpecFrom(e.G, spec, id, you, card) {
					out = append(out, decision.Option{Index: len(out), Kind: "clone", Obj: id, Label: o.Face().Name})
				}
			}
		}
		return out
	case "name":
		// A no-universe Config is a pre-feature match on replay. Its visible
		// object builder, including the Card.nonLand default and its full
		// MatchesSpecFrom semantics, is retained byte-for-byte below; changing
		// it would invalidate persisted ETB NameCard logs.
		if len(e.G.NameUniverse) == 0 {
			return e.legacyETBNameOptions(you, card, validCards)
		}
		// NameCard ranges over the compiled card-name universe, not public
		// objects currently visible to the chooser: Pithing Needle names any
		// card (a land included) and Revoker/Cabal Therapy name a nonland,
		// both through the SA's own ValidCards$ filter. An omitted
		// ValidCards$ is intentionally unrestricted. effects.NameChoices is
		// the ONE builder the mid-resolution NameCard ask shares, so the two
		// paths offer the same names. (choices carries ValidDescription$
		// prompt text here; see collectETBChoices.)
		list := ""
		if len(chooseFromList) > 0 {
			list = chooseFromList[0]
		}
		names := effects.NameChoicesFromList(e.G, validCards, choices, list)
		out := effects.NameOptions(names, you)
		if out == nil {
			out = []decision.Option{}
		}
		return out
	case "type":
		// The shared, category-aware enumeration; a caller that reaches here
		// with a non-creature category (a body that did not go through
		// entryETBChoice's category dispatch) still gets the real list, never a
		// creature-type list. This arm carries no ValidTypes$/InvalidTypes$
		// (the positional slots above are ValidCards$/Exclude$, different
		// params); the ETB dispatch passes the whole parameter map to
		// typeChoiceOptions instead.
		return e.typeChoiceOptions(you, card, map[string]string{"Type": typeCategory})
	case "evenodd":
		return []decision.Option{{Index: 0, Kind: "evenodd", Label: "Odd"}, {Index: 1, Kind: "evenodd", Label: "Even"}}
	default: // "number"
		// The shared 0..N list (task cli-20260923T060000Z-choose-number:
		// effects/number_choices.go is the ONE home), so the as-enters ask
		// and the mid-resolution ChooseNumber ask cannot disagree.
		return effects.NumberChoices()
	}
}

// legacyETBNameOptions is the exact pre-name-universe ETB builder. It stays
// separate from the corpus path because a sidecar without NameUniverse is an
// old log: its DecisionAsk options, including an empty ValidCards$ defaulting
// to Card.nonLand, must replay byte-for-byte.
func (e *Engine) legacyETBNameOptions(you state.PlayerID, card state.ObjID, validCards string) []decision.Option {
	if validCards == "" {
		validCards = "Card.nonLand"
	}
	seen := map[string]bool{}
	names := []string{}
	add := func(z state.Zone, players []state.PlayerID) {
		for _, p := range players {
			for _, id := range e.G.Zone(z, p) {
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil {
					continue
				}
				if !effects.MatchesSpecFrom(e.G, validCards, id, you, card) {
					continue
				}
				if seen[o.Face().Name] {
					continue
				}
				seen[o.Face().Name] = true
				names = append(names, o.Face().Name)
			}
		}
	}
	add(state.ZHand, []state.PlayerID{you})
	add(state.ZBattlefield, e.G.AliveFrom(0))
	add(state.ZGraveyard, e.G.AliveFrom(0))
	sort.Strings(names)
	out := make([]decision.Option, 0, len(names))
	for _, n := range names {
		out = append(out, decision.Option{Index: len(out), Kind: "name", Label: n})
	}
	return out
}

// isCreatureFace is a local creature test (effects.hasType is unexported);
// reads the printed Types, which is all any creature-subtype enumeration
// needs.
func isCreatureFace(f *cards.Face) bool {
	for _, t := range f.Types {
		if t == "Creature" {
			return true
		}
	}
	return false
}

// creatureTypeOptions enumerates the creature-type option list the cast-time
// "as this enters" ask (etbOptions' "type" arm) and the mid-resolution
// ChooseType ask (Engine.TypeChoices, task ct1) BOTH offer, so the two asks
// and the no-ask fallback can never disagree about what a creature-type
// choice ranges over. CR 205.3m: "choose a creature type" ranges over EVERY
// creature type, not only the ones in the game, so the list is the whole
// effects.CreatureTypeWordList vocabulary. The distinct creature subtypes of
// every object you OWN (all zones, object order) lead it, sorted
// alphabetically -- "Human" when you own none -- and the rest of the
// vocabulary follows, sorted. The leading block keeps the deterministic
// first option (the no-ask default) exactly what it was when the list was
// owner-scoped; the tail is what lets a player name a type nobody has
// (Banner of Kinship with no creature of the type, a type only an opponent
// has). It used to be owner-scoped only, and a lone owned type was
// auto-picked without asking.
func (e *Engine) creatureTypeOptions(you state.PlayerID) []decision.Option {
	seen := map[string]bool{}
	types := []string{}
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != you {
			continue
		}
		f := o.Face()
		if f == nil || !isCreatureFace(f) {
			continue
		}
		for _, t := range f.Types {
			if !effects.CreatureTypeWords(t) || seen[t] {
				continue
			}
			seen[t] = true
			types = append(types, t)
		}
	}
	if len(types) == 0 {
		types = []string{"Human"}
		seen["Human"] = true
	}
	sort.Strings(types)
	all := effects.CreatureTypeWordList()
	out := make([]decision.Option, 0, len(all)+1)
	for _, t := range types {
		out = append(out, decision.Option{Index: len(out), Kind: "type", Label: t})
	}
	for _, t := range all {
		if !seen[t] {
			out = append(out, decision.Option{Index: len(out), Kind: "type", Label: t})
		}
	}
	return out
}

// typeChoiceOptions builds the option list for one ChooseType Type$ category
// (task ct1), the ONE builder both the as-enters ask (entryETBChoice's "type"
// dispatch) and etbOptions' "type" arm use. A creature (or absent) category
// keeps the owner-scoped creatureTypeOptions; the context-scoped Shared
// category reads the entering object's own exiled-with set; every other
// enumerable category reads effects.TypeChoiceLabels' static list. A category
// that yields no list (an unresolvable context, or one this build still
// cannot name) falls back to the creature list so the ask is never emptied --
// the totality rule every as-enters ask lives by.
func (e *Engine) typeChoiceOptions(you state.PlayerID, source state.ObjID, params map[string]string) []decision.Option {
	cat := strings.TrimSpace(params["Type"])
	if cat == "" || strings.EqualFold(cat, "Creature") {
		return e.creatureTypeOptions(you)
	}
	var labels []string
	if strings.EqualFold(cat, "Shared") {
		labels = effects.SharedTypeLabels(e.G, source)
	} else {
		labels = effects.TypeChoiceLabels(cat, params["ValidTypes"], params["InvalidTypes"])
	}
	if len(labels) == 0 {
		return e.creatureTypeOptions(you)
	}
	out := make([]decision.Option, 0, len(labels))
	for _, label := range labels {
		out = append(out, decision.Option{Index: len(out), Kind: "type", Label: label})
	}
	return out
}

// TypeChoices implements effects.Host.TypeChoices (task ct1): the option list
// a mid-resolution ChooseType ask offers its chooser. Only the creature
// category reaches this Host method now -- the creature list is owner-scoped
// and lives here, while effects/type_choices.go builds the non-creature
// categories (and Shared/ CreatureInTargetedDeck from the resolving effect's
// own context) directly. An absent or "Creature" category returns the shared
// creatureTypeOptions; any other category yields nil, which the asking effect
// no longer reaches (it answers those categories itself).
func (e *Engine) TypeChoices(chooser state.PlayerID, category string) []decision.Option {
	if category != "" && !strings.EqualFold(category, "Creature") {
		return nil
	}
	return e.creatureTypeOptions(chooser)
}

// etbChoicePrompt names the kind of an "as this enters" choice for a client
// prompt; a cosmetic suffix on the shared "Choose" heading.
func etbChoicePrompt(kind string) string {
	switch kind {
	case "name":
		return " a card name"
	case "type":
		return " a creature type"
	case "evenodd":
		return " odd or even"
	case "color":
		return " a color"
	case "riot":
		return " how this creature enters (counter or haste)"
	case "unleash":
		return " how this creature enters (with a +1/+1 counter or without)"
	case "copy":
		return " a creature to copy"
	case "paylife":
		return " how much life to pay"
	}
	return " a number"
}

// etbCloneWhitelist reports whether a DB$ Clone ETB body's rider set is
// entirely inside the supported scope: Choices$ (the copy-template selector),
// AddTypes$ and AddKeywords$ (the CR 707.9e copy modifiers) and
// SpellDescription$. This is a POSITIVE whitelist over the parsed parameter
// keys -- the param census's case-whitelist range shape -- never a blacklist:
// an explicit key list cannot keep up with the corpus. The round-1 blacklist
// missed IntoPlayTapped$ (Vesuva), ChoiceTitle$ (Mirrorhall Mimic),
// Embalm$-provenance riders (Vizier of Many Faces), AddColors$, RemoveCost$,
// PumpKeywords$/PumpDuration$ and the AI-hint params, each of which offered a
// copy that silently dropped the exception. A body carrying any other
// parameter keeps today's loud unimplemented-API fallback (the etbclone1
// scope boundary); rules/etb_clone_whitelist_census_test.go pins the
// classified population bidirectionally.
func etbCloneWhitelist(sa *cards.SA, svars map[string]string) bool {
	for k := range sa.Params {
		switch k {
		case "Choices", "AddKeywords", "AddTypes", "SpellDescription", "AddStaticAbilities", "IntoPlayTapped":
			// supported: the copy-template selector, the CR 707.9e
			// copy modifiers, and (staticgoad1) the granted Goad$ static
			// effClone registers -- value-checked below.
		default:
			return false
		}
	}
	// A supported KEY is not a supported VALUE. Two value shapes inside the
	// key whitelist are withheld too, because admitting them offered a route
	// that silently did the wrong thing:
	//
	//  - A Choices$ selector carrying a predicate whose right-hand side is an
	//    SVar rather than a literal (Mockingbird's "Creature.Other+cmcLEY",
	//    Y = Count$CastTotalManaSpent). Both the option build (etbOptions)
	//    and the replacement-time revalidation (effects' cloneETBTemplateLegal)
	//    match through MatchesSpecFrom, which has no resolver, so every such
	//    predicate answers "recognised shape, never matches": the election
	//    would offer nothing but the decline at every paid X. Supporting it
	//    needs the choice deferred past payment with the cast's mana total
	//    bound as the RHS resolver -- not this task.
	//  - An AddKeywords$ member whose head is not a single word. Forge's
	//    conditional modifier grammar rides that space ("IfNew Vanishing:3",
	//    Flesh Duplicate: vanishing 3 only if the copied creature has no
	//    vanishing), and effClone installs the raw member as a layer-6
	//    AddKeywords grant, so cards.KeywordHead would read the head as
	//    "IfNew Vanishing" -- no conditional test, no vanishing, no entry
	//    time counters, silently. This is deliberately conservative: it also
	//    withholds a body whose modifier is a legitimate multi-word keyword
	//    ("First Strike"), a shape no ETB Clone carrier has today.
	if effects.SpecNeedsResolver(strings.TrimSpace(sa.ParamStr(cards.PKChoices))) {
		return false
	}
	for _, kw := range cards.SplitKeywordList(sa.ParamStr(cards.PKAddKeywords)) {
		if strings.ContainsAny(cards.KeywordHead(kw), " \t") {
			return false
		}
	}
	// A named static is installed on the cloned face by CloneStatic, so
	// every static reader sees it through its normal printed-S: path. An
	// unresolvable member still fails closed before posing the ETB election.
	for _, name := range strings.FieldsFunc(sa.ParamStr(cards.PKAddStaticAbilities), func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		if !effects.CloneStaticGrantReadable(svars, name) {
			return false
		}
	}
	if raw, ok := sa.Param(cards.PKIntoPlayTapped); ok && !strings.EqualFold(raw, "True") {
		return false
	}
	return true
}
