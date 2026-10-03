package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// ChooseType, ChooseNumber and ChooseColor record a choice on the source.
// With the choice already present -- for ChooseColor only the as-enters
// ENTRY-choice body, flagged Ctx.ETBColorRecorded by rules' replCtx (the
// entry ask machinery recorded it with a Choose event before the body runs,
// plan ruling R-6) -- these do nothing. Without one -- a script that
// uses them at RESOLUTION time -- ChooseType poses a real KChoose ask over
// its Type$ CATEGORY's option list (task ct1; effects/type_choices.go is the
// one home for the non-creature lists, and the suspension re-enters through
// rules' "choosetype" resume arm and Ctx.ChosenType) and ChooseColor poses
// a real KChoose ask over the WUBRG colour list (task
// cli-20260923T060000Z-choose-color; the suspension re-enters through
// rules' "choosecolor" resume arm and Ctx.ChosenColor), each falling back
// to its deterministic pick only when the host cannot ask, the option list
// is empty, or the SA carries a list shape this build cannot ask honestly.
// ChooseNumber likewise poses a real KChoose over its number list (task
// cli-20260923T060000Z-choose-number; the suspension re-enters through
// rules' "choosenumber" resume arm and Ctx.ChosenNumberPick/
// ChosenNumberAnswered), falling back to the deterministic 0 only when the
// host cannot ask or it is the as-enters ENTRY-choice body (the entry
// machinery already asked).
func init() {
	Register("ChooseType", effChooseType)
	Register("ChooseNumber", effChooseNumber)
	Register("ChooseColor", effChooseColor)
	Register("ChooseEvenOdd", effChooseEvenOdd)
}

// effChooseEvenOdd asks at resolution for the quality used by the
// cmcChosenEvenOdd filter. Its answer is recorded as the source's chosen type,
// keeping the answer replayable through the existing Choose event.
func effChooseEvenOdd(h Host, c *Ctx, sa *cards.SA) {
	if c.ETBEvenOddRecorded {
		c.ETBEvenOddRecorded = false
		return
	}
	if answer := c.ChosenType; answer == "odd" || answer == "even" {
		c.ChosenType = ""
		h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "type", Text: answer})
		return
	}
	d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		ResumeKind: "chooseevenodd", ResumeSA: sa, Prompt: "Choose odd or even", Source: c.Source,
		Options: []decision.Option{{Index: 0, Kind: "odd", Label: "odd"}, {Index: 1, Kind: "even", Label: "even"}}}
	if ans, ok := AskTape(h, d); ok {
		// The resolution kernel's answer in hand: the Choose event the
		// "chooseevenodd" resume arm's re-entry emits.
		if len(ans) > 0 && (ans[0].Label == "odd" || ans[0].Label == "even") {
			h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "type", Text: ans[0].Label})
			return
		}
	} else if Ask(h, d) == AskAsked {
		return
	}
	h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "type", Text: "odd"})
}

// effChooseColor records a colour choice. The ONE invocation that is a
// no-op is the as-enters ENTRY-choice body: rules' replCtx flags the
// K:ETBReplacement ChooseColor repl's body Ctx with ETBColorRecorded (the
// entry machinery -- applyETBChoiceReplacement -> resumeETBEntry -- already
// posed the entry ask and recorded the answer on the entering object before
// this body runs at the re-emitted move), and that invocation never asks.
// Any OTHER invocation treats a ChosenColor already on the source as STALE
// state -- an earlier choice's answer (a previous ChooseColor SA in the same
// resolution, or the entry choice an ability now re-asks) -- not this ask's
// own answer, and asks anyway. On the re-entry after its own mid-resolution
// ask was answered it emits the one Choose event the fallback emits, with
// the answered colour's WUBRG letter (Ctx.ChosenColor, consumed and cleared
// -- the fx42 scoping convention). On the first pass it poses a real KChoose
// over the chooseColorOptions list to the Defined$ player when two or more
// colours are offerable, so the chooser picks; with zero or one offerable
// colour the choice is forced (or empty) and the single legal answer equals
// the fallback's deterministic pick, so no ask is posed (the effChooseType
// strict-supersets convention). A host that cannot ask, an SA carrying a
// list shape this build cannot ask honestly (Random$, TwoColors$, OrColors$,
// UpTo$, ColorsFrom$ -- the latter four keep the loud Note the effChooseType
// unsupported-category convention carries), or an SA whose Exclude$/Choices$
// restriction resolves to no colour all fall through to the same fallback:
// the deterministic FIRST colour of the restricted option list, which for an
// unrestricted ask is the first-WUBRG "W" the old silent stand-in recorded --
// a degraded but restriction-respecting pick the ledger tracks (R-9).
// A SP$/AB$/DB$ ChooseColor mid-resolution ask (Wash Out, Nyx Lotus's
// devotion ability) therefore resolves through the chooser's pick instead of
// silently to W.
func effChooseColor(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	opts, askable, exotic := chooseColorOptions(sa)
	if c.ETBColorRecorded {
		// The as-enters ENTRY-choice body (ETBColorRecorded, set by rules'
		// replCtx for the K:ETBReplacement ChooseColor repl): the entry ask
		// already recorded the answer on the object, so this pass is the
		// historical no-op -- never a second ask. With no recorded answer
		// (an entry answer the fold could not name) it keeps the
		// deterministic fallback emit. The flag is consumed and cleared
		// (fx42): a nested ChooseColor deeper in the same chain poses its
		// own fresh ask.
		c.ETBColorRecorded = false
		if o := g.Obj(c.Source); o != nil && o.ChosenColor != "" {
			return
		}
		fallback := "W"
		if len(opts) > 0 {
			fallback = opts[0].Label
		}
		h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "color", Text: string(colourLetter(fallback))})
		return
	}
	if answered := c.ChosenColor; answered != "" {
		// The "choosecolor" resume arm's answer: emit the same Choose event
		// the fallback emits, with the answered colour's WUBRG letter, so
		// events.Apply records o.ChosenColor exactly the way every downstream
		// reader (Card.ChosenColor filters, devotion) already reads. A
		// malformed or off-list answer degrades to the deterministic pick
		// rather than inventing a colour the option list never named.
		c.ChosenColor = ""
		letter := colourLetter(answered)
		if !chooseColorOffers(opts, letter) {
			letter = 'W'
			if len(opts) > 0 {
				letter = colourLetter(opts[0].Label)
			}
		}
		h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "color", Text: string(letter)})
		return
	}
	if exotic != "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "ChooseColor " + exotic + " is not a shape this engine can ask; the choice falls back to the first colour of the restricted list"})
	}
	chooser := c.Controller
	if ts := Defined(h, c, sa); len(ts) > 0 && ts[0].IsPlayer {
		chooser = ts[0].Player
	}
	if askable && len(opts) > 1 {
		d := &decision.Decision{Player: chooser, Kind: decision.KChoose, Min: 1, Max: 1,
			ResumeKind: "choosecolor", ResumeSA: sa, Prompt: "Choose a color", Source: c.Source}
		d.Options = opts
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand: the same Choose event
			// the "choosecolor" resume arm's re-entry emits.
			letter := colourLetter(ans[0].Label)
			if !chooseColorOffers(opts, letter) {
				letter = colourLetter(opts[0].Label)
			}
			h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "color", Text: string(letter)})
			return
		}
		if Ask(h, d) == AskAsked {
			return
		}
	}
	fallback := "W"
	if len(opts) > 0 {
		fallback = opts[0].Label
	}
	h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "color", Text: string(colourLetter(fallback))})
}

// chooseColorLabels pairs the WUBRG letter the Choose event records with the
// full colour name the option Label carries, in fixed WUBRG order -- the
// same order and label shape the cast-time "as this enters" colour ask
// offers (rules' etbOptions "color" arm), so the two asks can never
// disagree about what a colour choice ranges over.
var chooseColorLabels = []struct {
	letter byte
	name   string
}{
	{'W', "White"}, {'U', "Blue"}, {'B', "Black"}, {'R', "Red"}, {'G', "Green"},
}

// TwoColors$/OrColors$ need two picks; UpTo$/ColorsFrom$ need an option
// list derived from game state. These are loud unaskable shapes (checked
// individually below so the parameter census sees each read). Random$ is
// the silent unaskable die-roll shape: asking would let the player pick.

// chooseColorOptions builds the option list a mid-resolution ChooseColor ask
// offers its chooser: the fixed WUBRG order of chooseColorLabels, with the
// SA's own colour restriction read where it carries one. Exclude$ removes
// colours (comma-separated names or letters; a token colourLetter cannot
// resolve is ignored -- the fail-open convention the cast-time arm's
// exclusion carries); Choices$ restricts the ask to the named colours. The
// totality guard keeps the list non-empty: a restriction that resolves to no
// colour reports the ask unaskable rather than offering a list that cannot
// answer the question. askable is false -- and exotic names the offending
// parameter -- when the SA carries a shape from chooseColorUnaskable, so the
// caller keeps the deterministic fallback instead of offering the wrong
// question.
func chooseColorOptions(sa *cards.SA) (opts []decision.Option, askable bool, exotic string) {
	if strings.TrimSpace(sa.Params["TwoColors"]) != "" {
		return nil, false, "TwoColors$"
	}
	if strings.TrimSpace(sa.Params["OrColors"]) != "" {
		return nil, false, "OrColors$"
	}
	if strings.TrimSpace(sa.ParamStr(cards.PKUpTo)) != "" {
		return nil, false, "UpTo$"
	}
	if strings.TrimSpace(sa.Params["ColorsFrom"]) != "" {
		return nil, false, "ColorsFrom$"
	}
	if strings.TrimSpace(sa.ParamStr(cards.PKRandom)) != "" {
		// The silent die-roll shape: unaskable, but no Note (see the
		// chooseColorUnaskable doc above).
		return nil, false, ""
	}
	excluded := map[byte]bool{}
	for _, l := range chooseColourTokens(sa.Params["Exclude"]) {
		excluded[l] = true
	}
	allowed := map[byte]bool{}
	if choices := chooseColourTokens(sa.ParamStr(cards.PKChoices)); len(choices) > 0 {
		for _, l := range choices {
			allowed[l] = true
		}
	}
	for _, cl := range chooseColorLabels {
		if excluded[cl.letter] || (len(allowed) > 0 && !allowed[cl.letter]) {
			continue
		}
		opts = append(opts, decision.Option{Index: len(opts), Kind: "color", Label: cl.name})
	}
	if len(opts) == 0 {
		return nil, false, ""
	}
	return opts, true, ""
}

// chooseColourTokens splits a comma-separated ChooseColor restriction value
// into the WUBRG letters it names (full names or letters, case-insensitive,
// the colourLetter vocabulary); a token naming no colour is dropped.
func chooseColourTokens(s string) []byte {
	var out []byte
	for tok := range strings.SplitSeq(s, ",") {
		if l := colourLetter(tok); l != 0 {
			out = append(out, l)
		}
	}
	return out
}

// chooseColorOffers reports whether the built option list still offers the
// WUBRG letter l.
func chooseColorOffers(opts []decision.Option, l byte) bool {
	if l == 0 {
		return false
	}
	for _, o := range opts {
		if colourLetter(o.Label) == l {
			return true
		}
	}
	return false
}

// effChooseNumber records a number choice. With the as-enters ENTRY-choice
// body flagged (Ctx.ETBNumberRecorded, set by rules' replCtx for the
// K:ETBReplacement ChooseNumber repl) it is the historical no-op: the entry
// machinery -- applyETBChoiceReplacement -> resumeETBEntry -- already posed
// the entry ask and recorded the answer on the entering object before this
// body runs at the re-emitted move, so no second ask is posed. Any OTHER
// invocation poses a real mid-resolution KChoose over the number list
// (NumberChoices, the one home) to the Defined$ player, so the chooser picks;
// on the re-entry after its own ask was answered it emits the one Choose
// event the fallback emits, with the answered number (Ctx.ChosenNumberPick
// plus the ChosenNumberAnswered marker, consumed and cleared -- the fx42
// scoping convention). The option list and prompt are chooseNumberAsk's
// (effects/number_choices.go, the one home): the card's own Max$ bound, Min$
// floor and ListTitle$ prompt, resolved against the current resolution
// context; a bound this context cannot honour keeps the loud fail-closed
// fallback (a Note naming the parameter, then the deterministic 0), a host
// that cannot ask falls through to the deterministic first legal value with
// no extra Note (R-9), the value the old stand-in always recorded.
//
// A number already on the source -- an EARLIER Choose event's answer, from a
// previous ChooseNumber SA or the entry choice an ability now re-asks -- is
// NOT this ask's own answer, so it never suppresses a fresh resolution-time
// ask (the sibling colour ask's stale-source-state rule). The entry body
// alone is exempted, through the flag rather than through the object's
// ChosenNumber: a recorded entry answer of 0 is indistinguishable from
// "never asked" on the object, but the flag is set precisely when the entry
// machinery recorded one.
func effChooseNumber(h Host, c *Ctx, sa *cards.SA) {
	if c.ETBNumberRecorded {
		// The as-enters ENTRY-choice body: resumeETBEntry already recorded
		// the answer on the object, so this pass emits nothing (the fx42
		// consume-and-clear).
		c.ETBNumberRecorded = false
		return
	}
	if c.ChosenNumberAnswered {
		// The "choosenumber" resume arm's answer: emit the same Choose event
		// the fallback emits, with the answered number.
		c.ChosenNumberAnswered = false
		n := c.ChosenNumberPick
		c.ChosenNumberPick = 0
		h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "number", Amount: n})
		return
	}
	// The multi-chooser SECRET election (api:ChooseNumber's
	// MatchedAbility$/UnmatchedAbility$ shape, Expert-Level Safe): a Defined$
	// selector naming SEVERAL players (TargetedAndYou) asks each one secretly,
	// then compares the picks and runs one of two SVar bodies. The three
	// parameters are read HERE, on the registered head, so the parameter
	// census attributes them to api:ChooseNumber.
	matchedAbility := strings.TrimSpace(sa.Params["MatchedAbility"])
	unmatchedAbility := strings.TrimSpace(sa.Params["UnmatchedAbility"])
	if matchedAbility != "" || unmatchedAbility != "" {
		effChooseNumberElection(h, c, sa, matchedAbility, unmatchedAbility,
			strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKSecretly)), "True"))
		return
	}
	chooser := c.Controller
	if ts := Defined(h, c, sa); len(ts) > 0 && ts[0].IsPlayer {
		chooser = ts[0].Player
	}
	opts, prompt, boundOK := chooseNumberAsk(h, c, sa)
	if !boundOK {
		// The card states a bound (Max$ or Min$) this context cannot honour:
		// an unresolvable expression, an inverted or negative bound, or one
		// past the list-pick ceiling. Fail closed loudly — a Note naming the
		// parameter, then the deterministic 0 the fallback always records
		// (chooseNumberAsk's doc) — rather than offering a list that could
		// violate the bound (the ChooseColor exotic-shape convention).
		// Name the bound the card actually states — a Min$-only failure must
		// not be logged as a Max$ problem (the note is the one record an
		// operator sees).
		text := "ChooseNumber"
		if mx := strings.TrimSpace(sa.ParamStr(cards.PKMax)); mx != "" {
			text += " Max$ " + mx
		}
		if mn := strings.TrimSpace(sa.ParamStr(cards.PKMin)); mn != "" {
			text += " Min$ " + mn
		}
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: text + " cannot be resolved in this context; the choice falls back to 0"})
		h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "number", Amount: 0})
		return
	}
	if len(opts) > 1 {
		d := &decision.Decision{Player: chooser, Kind: decision.KChoose, Min: 1, Max: 1,
			ResumeKind: "choosenumber", ResumeSA: sa, Prompt: prompt, Source: c.Source}
		d.Options = opts
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand: the Choose event the
			// "choosenumber" resume arm's re-entry emits (a malformed empty
			// answer keeps the arm's 0).
			n := int32(0)
			if len(ans) > 0 {
				n = int32(ans[0].Amount)
			}
			h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "number", Amount: n})
			return
		}
		if Ask(h, d) == AskAsked {
			return
		}
	}
	// The no-ask fallback: the deterministic first legal value of the list the
	// ask derived — for the historical fixed list and for every measured
	// corpus bound that is 0 (the value the old stand-in recorded, and the
	// R-9 no-host degradation); a Min$-floored list starts at its own floor.
	fallback := int32(0)
	if len(opts) > 0 {
		fallback = int32(opts[0].Amount)
	}
	h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "number", Amount: fallback})
}

// effChooseNumberElection implements api:ChooseNumber's multi-chooser SECRET
// election: a Defined$ selector naming several players each secretly pick a
// number from the ordinary bounded list (chooseNumberAsk), and the picks are
// then compared. All equal runs MatchedAbility$; otherwise UnmatchedAbility$.
// Expert-Level Safe ("You and target opponent each secretly choose 1, 2, or
// 3. Then those choices are revealed. If they match, ..." ) is the only corpus
// carrier and spells the pair as `Defined$ TargetedAndYou` + `Secretly$ True`
// + `MatchedAbility$ DBSacrifice` + `UnmatchedAbility$ DBFillSafe`.
//
// The chooser set is resolved through repeatPlayers -- the ONE home for the
// Defined$ player-selector grammar, which already understands TargetedAndYou
// (controller plus the resolution's targets), so this path adds no second
// Defined$ resolution. The choosers are asked in that deterministic APNAP
// order and the answers accumulated exactly as effPlayerVote rides its ballot:
// the answered pick is appended on each re-entry and the next chooser asked; a
// host that cannot ask (R-9) takes the deterministic first legal value so the
// election still completes. The accumulated numbers ride the decision's
// ResumeNumberPicks (the numeric sibling of ResumeChoices) and the asked
// chooser's index its ResumeTarget.
//
// SECRECY: no per-chooser event is emitted while the asks are posed, so a
// Secretly$ election exposes no individual pick before the reveal. On the last
// answer the choices are revealed (one Note naming every pick) and the matching
// SVar body is resolved through the same cards.ResolveSVar + Resolve chain the
// rest of the effects package uses for a named body. A missing or unparseable
// body is one loud Note and no branch, never a silent nothing.
func effChooseNumberElection(h Host, c *Ctx, sa *cards.SA, matched, unmatched string, secretly bool) {
	choosers, ok := repeatPlayers(h, c, strings.TrimSpace(sa.ParamStr(cards.PKDefined)))
	degraded := !ok || len(choosers) == 0
	if degraded {
		// The Defined$ selector is one this build cannot resolve to players;
		// fall back to the resolving controller alone rather than guessing a
		// second seat. A Note records the degrade.
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "ChooseNumber election could not resolve Defined$ " + strings.TrimSpace(sa.ParamStr(cards.PKDefined)) + "; asking the controller"})
		choosers = []state.PlayerID{c.Controller}
	}
	opts, prompt, boundOK := chooseNumberAsk(h, c, sa)
	if !boundOK || len(opts) == 0 {
		// A bound this context cannot honour (chooseNumberAsk's doc): keep the
		// loud fail-closed fallback, then run the UNMATCHED body -- a secret
		// election that could not be held did not match.
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "ChooseNumber election bound cannot be resolved in this context; the election did not match"})
		c.ChooseNumberPicks, c.ChooseNumberAnswer, c.ChooseNumberDone, c.ChooseNumberIndex = nil, 0, false, 0
		effChooseNumberElectionBranch(h, c, unmatched)
		return
	}
	picks := append([]int32(nil), c.ChooseNumberPicks...)
	i := c.ChooseNumberIndex
	if c.ChooseNumberDone {
		// The answer to chooser i's ask: its option's Amount (a legitimate
		// ZERO is carried by the separate Done marker). Consume and clear.
		picks = append(picks, c.ChooseNumberAnswer)
		c.ChooseNumberAnswer, c.ChooseNumberDone = 0, false
		i++
	}
	for ; i < len(choosers); i++ {
		d := &decision.Decision{Player: choosers[i], Kind: decision.KChoose, Min: 1, Max: 1,
			ResumeKind: "choosenumbermulti", ResumeSA: sa, ResumeTarget: i,
			ResumeNumberPicks: append([]int32(nil), picks...),
			Prompt:            prompt, Source: c.Source}
		d.Options = opts
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand: the pick the
			// "choosenumbermulti" arm carries (a malformed empty answer keeps
			// its 0), then the next chooser. The legacy re-entry re-runs the
			// election from its first line, so its Defined$ degrade Note is
			// emitted again before every answered chooser; so here.
			pick := int32(0)
			if len(ans) > 0 {
				pick = int32(ans[0].Amount)
			}
			picks = append(picks, pick)
			if degraded {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "ChooseNumber election could not resolve Defined$ " + strings.TrimSpace(sa.ParamStr(cards.PKDefined)) + "; asking the controller"})
			}
			continue
		}
		if Ask(h, d) == AskAsked {
			return
		}
		// R-9 no-ask host: take the deterministic first legal value for this
		// chooser and continue to the next.
		picks = append(picks, int32(opts[0].Amount))
	}
	c.ChooseNumberPicks, c.ChooseNumberAnswer, c.ChooseNumberDone, c.ChooseNumberIndex = nil, 0, false, 0
	// The reveal: after every chooser has answered, name the chosen numbers so
	// the secret picks become public at exactly the Oracle's "then those
	// choices are revealed" point (never before). The controller's pick is also
	// recorded on the source through the ordinary Choose event every other
	// ChooseNumber path emits, so a downstream Card.ChosenNumber reader keeps
	// its meaning.
	controllerPick := picks[0]
	if idx := indexOfPlayer(choosers, c.Controller); idx >= 0 {
		controllerPick = picks[idx]
	}
	h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "number", Amount: controllerPick})
	if secretly {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "ChooseNumber election revealed: " + formatNumberPicks(picks)})
	}
	allEqual := true
	for _, p := range picks[1:] {
		if p != picks[0] {
			allEqual = false
			break
		}
	}
	if allEqual {
		effChooseNumberElectionBranch(h, c, matched)
		return
	}
	effChooseNumberElectionBranch(h, c, unmatched)
}

// effChooseNumberElectionBranch resolves one named branch body of a
// ChooseNumber election (MatchedAbility$/UnmatchedAbility$). A missing or
// unparseable body is one loud Note and no branch -- never a silent nothing.
func effChooseNumberElectionBranch(h Host, c *Ctx, name string) {
	if name == "" {
		return
	}
	sub := cards.ResolveSVar(c.SVars, name)
	if sub == nil {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "ChooseNumber election branch " + name + " is not a body this build can resolve"})
		return
	}
	Resolve(h, c, sub)
}

// indexOfPlayer returns p's position in ps, or -1.
func indexOfPlayer(ps []state.PlayerID, p state.PlayerID) int {
	for i, q := range ps {
		if q == p {
			return i
		}
	}
	return -1
}

// formatNumberPicks renders an election's picks as a comma-separated list for
// the reveal Note.
func formatNumberPicks(picks []int32) string {
	parts := make([]string, len(picks))
	for i, p := range picks {
		parts[i] = strconv.Itoa(int(p))
	}
	return strings.Join(parts, ", ")
}

// effChooseType records a type choice. With the source already carrying a
// ChosenType it is a no-op (the cast-time ask pre-recorded it); on the
// re-entry after its own ask was answered it emits exactly the Choose event
// the fallback emits, with the answered type (Ctx.ChosenType, consumed and
// cleared -- fx42). On the first pass it poses a real KChoose ask over the
// option list its Type$ CATEGORY ranges over when two or more options are
// offerable, so the chooser picks; with zero or one offerable option the
// choice is forced (or empty) and the single legal answer equals the
// fallback's deterministic pick, so no ask is posed (the effDiscard
// strict-supersets convention). A host that cannot ask falls through to the
// same fallback with no extra Note (R-9).
//
// Type$ (Herald's Horn's Creature, Realmwright's Basic Land, Archon of
// Valor's Reach's Card, Deification's Planeswalker, Apex Observatory's
// Shared, Aswan Jaguar's CreatureInTargetedDeck) names the CATEGORY the
// choice ranges over. Every category this build can enumerate now offers its
// REAL list -- effects/type_choices.go is the one home -- so the resolution
// ask and the as-enters ask (rules/cast.go's etbOptions) cannot disagree. Only
// a category this build still cannot name keeps the loud Note plus a
// deterministic fallback, and that fallback is drawn from the category's own
// list when one exists (never a nonsensical creature type).
func effChooseType(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	if o := g.Obj(c.Source); o != nil && o.ChosenType != "" {
		return
	}
	cat := strings.TrimSpace(sa.ParamStr(cards.PKType))
	if answered := c.ChosenType; answered != "" {
		// The "choosetype" resume arm's answer: emit the same Choose event the
		// fallback emits, with the answered type, so events.Apply records
		// o.ChosenType exactly the way every downstream reader already reads.
		c.ChosenType = ""
		h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "type", Text: answered})
		return
	}
	chooser := c.Controller
	if ts := Defined(h, c, sa); len(ts) > 0 && ts[0].IsPlayer {
		chooser = ts[0].Player
	}
	labels, known := chooseTypeLabels(h, c, sa, chooser, cat)
	if !known {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "ChooseType Type$ " + cat + " is not a category this engine can ask; the choice falls back to creature types"})
	}
	d := &decision.Decision{Player: chooser, Kind: decision.KChoose, Min: 1, Max: 1,
		ResumeKind: "choosetype", ResumeSA: sa, Prompt: chooseTypePrompt(cat), Source: c.Source}
	for _, label := range labels {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "type", Label: label})
	}
	if len(d.Options) > 1 {
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand: the Choose event the
			// "choosetype" resume arm's re-entry emits.
			if len(ans) > 0 && ans[0].Label != "" {
				h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "type", Text: ans[0].Label})
				return
			}
		} else if Ask(h, d) == AskAsked {
			return
		}
	}
	// The no-ask fallback. A category with an option list records that list's
	// deterministic first entry; a category whose list is empty (an
	// unresolvable Shared or CreatureInTargetedDeck context, or an unknown
	// category) keeps the historical creature-type scan, so no category ever
	// records a nonsense value from ANOTHER category.
	var fallback string
	if len(d.Options) > 0 && !isCreatureCategory(cat) {
		fallback = d.Options[0].Label
	}
	if fallback == "" {
		fallback = creatureTypeFallback(g, c.Controller)
	}
	h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "type", Text: fallback})
}

// chooseTypeLabels returns the option labels for the resolving ChooseType's
// Type$ category, and whether the category is one this build can enumerate.
// Creature (and an absent Type$) is the owner-scoped list the engine already
// built; the context-scoped Shared and CreatureInTargetedDeck categories read
// the resolving effect's own state; every other enumerable category reads the
// static list effects/type_choices.go defines. A category with no list (and
// not the four above) reports known=false, which is the loud-Note path.
func chooseTypeLabels(h Host, c *Ctx, sa *cards.SA, chooser state.PlayerID, cat string) ([]string, bool) {
	if isCreatureCategory(cat) {
		return optionLabels(h.TypeChoices(chooser, cat)), true
	}
	switch strings.ToLower(cat) {
	case "shared":
		return SharedTypeLabels(h.Game(), c.Source), true
	case "creatureintargeteddeck":
		return CreatureInTargetedDeckLabels(h.Game(), c.Targets), true
	}
	if labels := TypeChoiceLabels(cat, sa.Params["ValidTypes"], sa.Params["InvalidTypes"]); labels != nil {
		return labels, true
	}
	return nil, false
}

// optionLabels reads the labels off an option list (the Host.TypeChoices
// creature list), preserving order.
func optionLabels(opts []decision.Option) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		out = append(out, o.Label)
	}
	return out
}

// isCreatureCategory reports whether a Type$ value is an absent category or
// "Creature" -- the two spellings that ask over the creature-type list.
func isCreatureCategory(cat string) bool {
	return cat == "" || strings.EqualFold(cat, "Creature")
}

// chooseTypePrompt names the category for a client prompt.
func chooseTypePrompt(cat string) string {
	switch strings.ToLower(cat) {
	case "", "creature", "creatureintargeteddeck":
		return "Choose a creature type"
	case "basic land", "land", "nonbasic land":
		return "Choose a land type"
	case "card", "shared":
		return "Choose a card type"
	case "planeswalker":
		return "Choose a planeswalker type"
	}
	return "Choose a type"
}

// creatureTypeFallback is the historical deterministic creature-type
// fallback: the first creature subtype of a creature the controller controls,
// in object order, or "Human" when they control none. It is reached only when
// a category has no option list of its own, so a non-creature category is
// never recorded as a creature type unless its own context was unreadable.
func creatureTypeFallback(g *state.Game, controller state.PlayerID) string {
	fallback := ""
	for i := range g.Objs {
		o := &g.Objs[i]
		if fallback != "" || o.Controller != controller {
			continue
		}
		f := o.Face()
		if f == nil || !hasType(o, "Creature") {
			continue
		}
		for _, t := range f.Types {
			if CreatureTypeWords(t) {
				fallback = t
				break
			}
		}
	}
	if fallback == "" {
		fallback = "Human"
	}
	return fallback
}

// CreatureTypeWords reports whether a Type token is a creature subtype. It
// shares the positive vocabulary Changeling uses, so a cast-time type choice
// cannot offer a spell, plane, or planeswalker subtype as a creature type.
func CreatureTypeWords(t string) bool { return creatureSubtypeWords[t] }
