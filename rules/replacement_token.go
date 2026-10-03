package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// continueCreateTokenReplacements applies every applicable CreateToken
// replacement to one TokenCreate event. The engine mints ONE token per
// event, so the plan starts as that single mint. Each match applies at most
// once, in deterministic scan order, and its ValidToken$ gate is re-checked
// PER PLAN MINT — a later match sees the mints earlier matches produced
// (Divine Visitation after a doubler replaces each doubled mint that is
// still a creature token), which is CR 616.1's re-application over the
// changed event. The final plan is emitted directly through events.Emit
// (+ observe + checkTriggers per mint, the composeUpdatedReplacements
// pattern), which BYPASSES applyReplacements: no mint can re-match, so a
// doubler can never loop on its own output. The deviation from CR 616.1 is
// deliberate (the CR 616.1 order choice for competing CreateToken
// replacements is a follow-up ticket, not this build): competing CreateToken
// replacements apply in
// scan order, NOT through a posed KReplacement order choice (non-commuting
// compositions are reachable in Commander, but no repo deck carries any of
// this family, so no golden game exercises one).
func (e *Engine) continueCreateTokenReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	if isCopyTokenProposal(ev) {
		return e.continueCopyTokenProposal(ev, matches)
	}
	plan, parked := e.driveTokenReplacements(ev, matches, []tokenPlanMint{{script: ev.Text}}, 0)
	if parked {
		// A mid-drive ask is outstanding: either the CR 616.1 order choice
		// (parked on the replChoices queue) or the chosen-copy body's
		// election (e.tokenChoice). The mints are emitted from the answer's
		// resume, the siegeMove/attachedChoice discipline.
		return ev, true
	}
	if len(plan) == 1 && plan[0].copyOf == 0 && plan[0].script == ev.Text && !plan[0].hasController {
		// No replacement changed the plan: the ordinary emit path logs the
		// original event untouched (with its full LKI/trigger treatment).
		return ev, false
	}
	if len(plan) == 0 {
		// Every mint was removed (halving_season's HalfDown on a one-token
		// event): the Note is the log's witness that nothing was created.
		e.emit(events.Event{Kind: events.Note, Obj: 0, Player: ev.Player,
			Text: "no tokens created (replacement effect rounded the creation down to zero)"})
		return ev, true
	}
	last := e.emitTokenPlanMints(ev, plan)
	return last, true
}

// tokenPlanMint is one planned mint of a CreateToken replacement plan: an
// ordinary scripted token (script) or a copy of a battlefield permanent
// (copyOf -- the chosen-copy body's TokenScript$ Chosen shape).
type tokenPlanMint struct {
	script string
	copyOf state.ObjID
	// controller, when hasController is set, is the player the mint enters
	// under (a Type$ ReplaceController body's NewController$ -- Crafty
	// Cutpurse's "created under your control instead"). The zero value leaves
	// the mint under the original event's player; PlayerID 0 is a real seat,
	// so the flag, not the value, is the sentinel.
	controller    state.PlayerID
	hasController bool
}

// tokenMintPlayer is the player one plan mint enters under: its own
// ReplaceController answer when it carries one, else the original event's
// creator. Every mint site and the per-mint re-match read this ONE helper so
// a controller rewrite can never drift between the emit and the recheck.
func tokenMintPlayer(mint tokenPlanMint, ev events.Event) state.PlayerID {
	if mint.hasController {
		return mint.controller
	}
	return ev.Player
}

// tokenReplApplies reports whether the drive would apply m at all: a body
// the drive does not read, the deterministic-decline optional and the
// loud-unimplemented ReplaceController are not competition candidates,
// everything else applies.
func tokenReplApplies(m replMatch) bool {
	body := m.repl.With
	if body == nil || body.API != "ReplaceToken" {
		return false
	}
	chosenShape := strings.EqualFold(strings.TrimSpace(body.ParamStr(cards.PKTokenScript)), "Chosen") ||
		strings.TrimSpace(body.ParamStr(cards.PKValidChoices)) != ""
	if !chosenShape && strings.EqualFold(m.repl.ParamStr(cards.PKOptional), "True") {
		return false
	}
	if strings.TrimSpace(body.ParamStr(cards.PKType)) == "ReplaceController" {
		return false
	}
	return true
}

// tokenCompetitionCandidates is matches' applicable candidates, in order:
// exactly the matches the drive would act on.
func tokenCompetitionCandidates(matches []replMatch) []replMatch {
	var out []replMatch
	for _, m := range matches {
		if tokenReplApplies(m) {
			out = append(out, m)
		}
	}
	return out
}

// tokenReplacementsCommute reports whether the candidate ReplaceToken bodies
// compose order-insensitively: pure multipliers (Type$ Amount) commute with
// each other and pure adders (Type$ AddToken) with each other, while a
// script rewriter (Type$ ReplaceToken) or an unclassifiable Type$ makes the
// order observable (a rewriter composed after a multiplier rewrites every
// duplicated mint; composed before, only the original). HalfDown/HalfUp
// multipliers floor, so they do not commute with anything but themselves.
func tokenReplacementsCommute(cands []replMatch) bool {
	kind := ""
	for _, m := range cands {
		body := m.repl.With
		if body == nil || body.API != "ReplaceToken" {
			return false
		}
		cls := ""
		switch tokenReplacementsCommute2271Codes.Code(string(strings.TrimSpace(body.ParamStr(cards.PKType)))) {
		case tokenReplacementsCommute2271Amount:
			raw := strings.TrimSpace(body.ParamStr(cards.PKAmount))
			if raw == "" {
				raw = "Twice"
			}
			if raw == "HalfDown" || raw == "HalfUp" {
				return false
			}
			cls = "mult"
		case tokenReplacementsCommute2271AddToken:
			cls = "add"
		default:
			return false
		}
		if kind != "" && cls != kind {
			return false
		}
		kind = cls
	}
	return true
}

// dropReplMatch is cands minus the one match (source id plus repl pointer
// identity), order kept: the CR 616.1 answer's "apply the chosen one first,
// then the rest in order" walk.
func dropReplMatch(cands []replMatch, m replMatch) []replMatch {
	out := make([]replMatch, 0, len(cands))
	for _, c := range cands {
		if c.id != m.id || c.repl != m.repl {
			out = append(out, c)
		}
	}
	return out
}

// tokenChoiceState is the parked chosen-copy replacement's continuation: the
// original event, the full match list, the plan as rewritten so far, the
// cursor of the next unapplied match, the match the outstanding election
// belongs to and the decline option's index (negative when the body is not
// Optional -- a decline is then not an offered answer).
type tokenChoiceState struct {
	ev         events.Event
	matches    []replMatch
	plan       []tokenPlanMint
	next       int
	match      replMatch
	declineIdx int
	// plainOptional marks an Optional$ True replacement with no copy source to
	// choose (Type$ Amount/AddToken/ReplaceToken): the election is a bare
	// apply/decline, and option declineIdx declines it while any other answer
	// applies the match.
	plainOptional bool
	// mintSink names the parked-mint collector (rules/token_rest.go) the
	// election's answer mints into when a resolving DB$ Token is waiting on
	// this creation; 0 otherwise.
	mintSink uint64
}

// driveTokenReplacements applies matches[from:] to the plan, in the
// dispatcher's deterministic scan order, parking (with e.tokenChoice
// populated) at the first chosen-copy match whose election must be asked.
// A nil/unchanged plan rides out as the caller's signal that the original
// event stands.
func (e *Engine) driveTokenReplacements(ev events.Event, matches []replMatch, plan []tokenPlanMint, from int) ([]tokenPlanMint, bool) {
	for i := from; i < len(matches); i++ {
		m := matches[i]
		body := m.repl.With
		if body == nil || body.API != "ReplaceToken" {
			// A body this dispatcher does not read leaves the plan untouched;
			// the mint stands (the fail-safe direction).
			continue
		}
		chosenShape := strings.EqualFold(strings.TrimSpace(body.ParamStr(cards.PKTokenScript)), "Chosen") ||
			strings.TrimSpace(body.ParamStr(cards.PKValidChoices)) != ""
		if !chosenShape && strings.EqualFold(m.repl.ParamStr(cards.PKOptional), "True") {
			// A "may" replacement with no copy source to point at: pose the
			// bare apply/decline election. The chosen-copy bodies below pose
			// their own election and so never take this arm.
			var parked bool
			plan, parked = e.posePlainOptionalTokenReplacement(ev, matches, plan, i, m)
			if parked {
				return plan, true
			}
			continue
		}
		// CR 616.1's order competition: two or more applicable matches from
		// here whose bodies do not all commute, and a creator who can still
		// decide, park the whole plan on the queue and ask which applies
		// first. The pose is a queue append: a competition that arrived while
		// another decision was outstanding parks behind it and is asked when
		// the queue drains (Submit's tail), never overwriting it; a creator
		// who has left the game makes no choices (CR 800.4a) and the drive
		// continues in scan order.
		if rest := tokenCompetitionCandidates(matches[i:]); len(rest) > 1 && !tokenReplacementsCommute(rest) {
			if p := ev.Player; int(p) < len(e.G.Players) && !e.G.Players[p].Lost {
				applicable := make([]int, 0, len(rest))
				for j, c := range matches[i:] {
					if tokenReplApplies(c) {
						applicable = append(applicable, j)
					}
				}
				e.replChoices = append(e.replChoices, replChoice{kind: replChoiceToken,
					ev: ev, cands: matches[i:], applicable: applicable, before: e.retainTriggerBefore(),
					tokenPlan: plan, tokenNext: i, player: p, inResolution: e.resolvingObj != 0 || e.answerInResolution})
				if e.pending == nil {
					e.askReplacementChoice(p)
				}
				return plan, true
			}
		}
		if chosenShape {
			var parked bool
			plan, parked = e.poseChosenTokenReplacement(ev, matches, plan, i, m)
			if parked {
				return plan, true
			}
			continue
		}
		plan = e.applyTokenReplacementToPlan(ev, plan, m)
	}
	return plan, false
}

// poseChosenTokenReplacement handles ONE Type$ ReplaceToken body whose copy
// source is a player choice (ValidChoices$ <spec> / TokenScript$ Chosen --
// the measured population is exactly Esix, Fractal Bloom, Moonlit Meditation
// and Mirrormind Crown). The controller's election is one KChoose over the
// permanents the spec matches (deterministic battlefield scan order), with a
// decline option FIRST when the R: line carries Optional$ True -- the
// replicate/exert convention, so botpolicy's KChoose default arm (first
// offer) declines a may and never wrongly replaces. On accept every mint the
// body gates onto becomes a copy of the chosen creature; on decline the match
// is skipped and the plan's remaining matches still run (declining one
// CR 616.1 competitor never declines the others). No eligible permanent is
// the forced decline (nobody could answer differently -- the strict-supersets
// convention), and a second concurrent decision keeps the loud stand-in
// rather than overwrite an outstanding ask.
func (e *Engine) poseChosenTokenReplacement(ev events.Event, matches []replMatch, plan []tokenPlanMint, idx int, m replMatch) ([]tokenPlanMint, bool) {
	you := e.controllerOf(m.id)
	cands := e.tokenChosenCandidates(strings.TrimSpace(m.repl.With.ParamStr(cards.PKValidChoices)), m.id, you)
	optional := strings.EqualFold(m.repl.ParamStr(cards.PKOptional), "True")
	if len(cands) == 0 {
		return plan, false
	}
	if !optional && len(cands) == 1 {
		return e.applyChosenToPlan(ev, m, plan, cands[0]), false
	}
	// pending != nil means a real decision is outstanding: the election parks
	// (queue discipline) rather than displacing it. e.resume != nil with
	// pending nil is the CR 616.1 answer window itself -- the order
	// competition's own suspension record, which this pose keeps (e.ask
	// never overwrites) and its answer tail resumes through parkedResume.
	if e.tokenChoice != nil || e.pending != nil {
		e.emit(events.Event{Kind: events.Note, Obj: m.id, Player: ev.Player,
			Text: "ReplaceToken ValidChoices (TokenScript$ Chosen) cannot ask while another decision is pending; the token is created unchanged"})
		return plan, false
	}
	asker := e.tokenElectionAsk
	opts := make([]decision.Option, 0, len(cands)+1)
	declineIdx := -1
	if optional {
		opts = append(opts, decision.Option{Index: 0, Kind: "decline",
			Label: "create the tokens as they would have been"})
		declineIdx = 0
	}
	for i, id := range cands {
		label := ""
		if o := e.G.Obj(id); o != nil && o.Face() != nil {
			label = o.Face().Name
		}
		opts = append(opts, decision.Option{Index: declineIdx + 1 + i, Kind: "creature", Label: label, Obj: id})
	}
	st := &tokenChoiceState{ev: ev, matches: matches, plan: plan,
		next: idx + 1, match: m, declineIdx: declineIdx}
	e.tokenChoice = st
	prompt := "Choose a creature to copy"
	if optional {
		prompt = "You may instead create tokens that are copies of a creature: choose one, or decline"
	}
	d := &decision.Decision{Player: you, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: m.id, Prompt: prompt, Options: opts}
	e.choosing = chooseTokenReplace
	asker(d, st)
	return plan, true
}

// tokenElectionAsk poses a CreateToken replacement election (the chosen-copy
// and the plain Optional$ shapes). Under the resolution kernel the election
// is answered in place (W3 step 5): the settled plan mints at the point of
// the creation.
func (e *Engine) tokenElectionAsk(d *decision.Decision, st *tokenChoiceState) {
	if e.pending == nil && (e.resolvingObj != 0 || e.applyingReplacement) {
		d.ResumeKind = "replacement"
		if in, ok := parkTapeAnswer(e, d); ok {
			e.handle(d, in)
			return
		}
	}
	e.ask(d)
}

// posePlainOptionalTokenReplacement handles ONE Optional$ True
// ReplaceToken body with no copy source to point at (Flitwing Lyev,
// Detective: "if you would create one or more tokens, you may create that
// many Clue tokens instead"). The controlling player's election is one
// KChoose over a bare apply/decline pair, with the decline FIRST so
// botpolicy's KChoose default arm (first offer) never wrongly applies a may
// -- the same replicate/exert convention poseChosenTokenReplacement uses.
// The ask is parked on e.tokenChoice and the flow resumes through
// tokenReplAnswer (the chosen-copy path's own resume).
func (e *Engine) posePlainOptionalTokenReplacement(ev events.Event, matches []replMatch, plan []tokenPlanMint, idx int, m replMatch) ([]tokenPlanMint, bool) {
	if e.tokenChoice != nil || e.pending != nil {
		e.emit(events.Event{Kind: events.Note, Obj: m.id, Player: ev.Player,
			Text: "an optional ReplaceToken election cannot ask while another decision is pending; the replacement is declined"})
		return plan, false
	}
	you := e.controllerOf(m.id)
	opts := []decision.Option{
		{Index: 0, Kind: "decline", Label: "create the tokens as they would have been"},
		{Index: 1, Kind: "apply", Label: "apply this replacement"},
	}
	st := &tokenChoiceState{ev: ev, matches: matches, plan: plan,
		next: idx + 1, match: m, declineIdx: 0, plainOptional: true}
	e.tokenChoice = st
	d := &decision.Decision{Player: you, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: m.id, Prompt: "You may apply this token replacement effect: apply it?", Options: opts}
	e.choosing = chooseTokenReplace
	e.tokenElectionAsk(d, st)
	return plan, true
}

// tokenReplAnswer resumes the parked chosen-copy replacement (the
// chooseTokenReplace case of handleChoose): the answered option either
// rewrites the plan to copies of the chosen creature or skips the match (the
// decline), and the flow then drives the plan's remaining matches.
func (e *Engine) tokenReplAnswer(chosen []decision.Option) {
	st := e.tokenChoice
	e.tokenChoice = nil
	e.choosing = chooseNone
	if st == nil {
		e.emit(events.Event{Kind: events.Note, Text: "token copy choice answered with no replacement pending"})
		return
	}
	// A resolving DB$ Token waiting on this creation collects what the
	// answer mints (and hands the collector to any election or order ask the
	// answer poses next).
	e.withMintSink(st.mintSink, func() { e.settleTokenAnswer(st, chosen) })
}

// settleTokenAnswer is tokenReplAnswer's body: apply the answered election to
// the parked plan, drive the remaining matches and emit the settled plan.
func (e *Engine) settleTokenAnswer(st *tokenChoiceState, chosen []decision.Option) {
	if st.plainOptional {
		// A bare Optional$ election: any non-decline answer applies the
		// match itself; the decline skips it and the remaining matches run.
		if len(chosen) == 1 && chosen[0].Index != st.declineIdx {
			st.plan = e.applyTokenReplacementToPlan(st.ev, st.plan, st.match)
		}
	} else if len(chosen) != 1 || chosen[0].Index == st.declineIdx {
		// The decline (or a malformed answer, treated as one): the match is
		// skipped, the event's remaining replacements still run.
	} else if o := e.G.Obj(chosen[0].Obj); o != nil && o.Zone == state.ZBattlefield {
		st.plan = e.applyChosenToPlan(st.ev, st.match, st.plan, chosen[0].Obj)
	} else {
		// The chosen creature vanished between the ask and the answer: one
		// loud Note and the match is skipped, never a mint of nothing.
		e.emit(events.Event{Kind: events.Note, Obj: st.match.id, Player: st.ev.Player,
			Text: "the chosen creature is no longer on the battlefield; the token is created unchanged"})
	}
	plan, parked := e.driveTokenReplacements(st.ev, st.matches, st.plan, st.next)
	if parked {
		return
	}
	e.emitTokenPlan(st.ev, plan)
	e.askNextReplacementChoice()
}

// emitTokenPlan settles the parked plan after every match has been applied
// or declined. An unchanged plan re-emits the original event verbatim under
// the applyingReplacement guard (the finishParkedPhase convention -- asking
// the ordinary path to re-collect would re-pose the declined elections), the
// empty plan is the rounded-to-zero Note, and each mint rides the
// continueCreateTokenReplacements tail's discipline.
func (e *Engine) emitTokenPlan(ev events.Event, plan []tokenPlanMint) {
	if len(plan) == 1 && plan[0].copyOf == 0 && plan[0].script == ev.Text && !plan[0].hasController {
		saved := e.applyingReplacement
		e.applyingReplacement = true
		e.emit(ev)
		e.applyingReplacement = saved
		return
	}
	if len(plan) == 0 {
		e.emit(events.Event{Kind: events.Note, Obj: 0, Player: ev.Player,
			Text: "no tokens created (replacement effect rounded the creation down to zero)"})
		return
	}
	e.emitTokenPlanMints(ev, plan)
}

// emitTokenPlanMints logs the final plan: one TokenCreate per scripted mint
// through Engine.emit under applyingReplacement (so no doubler can loop on its
// own output), one CopyToken + genuine MoveZone per copy mint (the DB$
// CopyPermanent mint shape -- the entry stays a ChangesZone-matchable event
// every "a creature enters" trigger observes, and the MoveZone rides the
// ordinary entry machinery a real copy gets).
func (e *Engine) emitTokenPlanMints(ev events.Event, plan []tokenPlanMint) events.Event {
	var last events.Event
	for i, mint := range plan {
		if mint.copyOf != 0 {
			last = e.emitChosenCopyToken(ev, mint.copyOf, tokenMintPlayer(mint, ev))
			if e.suspendTokenPlanTail(ev, plan, i) {
				return last
			}
			continue
		}
		mintEv := events.Event{Kind: events.TokenCreate, Player: tokenMintPlayer(mint, ev), Text: mint.script}
		// This plan has already passed token-creation replacements. Keep that
		// no-rematch boundary while routing the actual mint through the entry
		// staging/fold path (which may park for entry-counter order).
		savedApplying := e.applyingReplacement
		e.applyingReplacement = true
		stored := e.emit(mintEv)
		e.applyingReplacement = savedApplying
		if e.suspendTokenPlanTail(ev, plan, i) {
			return last
		}
		last = stored
	}
	return last
}

// suspendTokenPlanTail parks the mints still owed when the just-emitted
// plan[at] mint staged behind an entry-counter order ask, so the plan's
// mints land in plan order after the answer rather than the later ones
// overtaking the staged one. Returns whether it suspended. The stage's
// completion re-drive continues the tail (resumeEntryCounterOrder).
func (e *Engine) suspendTokenPlanTail(ev events.Event, plan []tokenPlanMint, at int) bool {
	if at+1 >= len(plan) {
		return false
	}
	st := e.outstandingEntryStage()
	if st == nil {
		return false
	}
	st.tokenPlan = &tokenPlanResume{ev: ev, plan: append([]tokenPlanMint(nil), plan[at+1:]...)}
	return true
}

// emitChosenCopyToken mints one copy of a battlefield creature: the CopyToken
// mint predicted by id (the effToken/effMyriad prediction pattern), then the
// genuine MoveZone that puts it on the battlefield.
func (e *Engine) emitChosenCopyToken(ev events.Event, src state.ObjID, player state.PlayerID) events.Event {
	want := e.G.NextID
	stored := e.emit(events.Event{Kind: events.CopyToken, Obj: src, Player: player})
	if e.G.Obj(want) == nil {
		return stored
	}
	// The copy is published to the mint sink by the emit tail when this
	// MoveZone's entry actually completes (publishTokenEntry) -- never here:
	// the move may park behind an entry-counter order or an as-enters
	// election, and the id must not reach a rider before it has entered.
	return e.emit(events.Event{Kind: events.MoveZone, Obj: want,
		From: state.ZLibrary, To: state.ZBattlefield})
}

// tokenChosenCandidates resolves one chosen-copy body's ValidChoices$ spec
// against the battlefield: every permanent, any controller's (deterministic
// AliveFrom/zone order), the spec matches, with the replacement's source as
// the filter's referent -- Esix's `Creature.Other` excludes Esix itself and
// Moonlit's `Card.EnchantedBy` reaches exactly the enchanted permanent. An
// unmatchable spec fails closed to the empty set (the forced decline).
func (e *Engine) tokenChosenCandidates(spec string, source state.ObjID, you state.PlayerID) []state.ObjID {
	var out []state.ObjID
	sc := e.specCtx(source, you)
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil {
				continue
			}
			if effects.MatchesObjectCtx(e.G, spec, o, sc) {
				out = append(out, id)
			}
		}
	}
	return out
}

// applyChosenToPlan rewrites every mint ONE chosen-copy match gates onto a
// copy of the chosen creature -- one election covers the whole creation
// event ("choose a creature ... and create that many tokens that are copies
// of that creature").
func (e *Engine) applyChosenToPlan(ev events.Event, m replMatch, plan []tokenPlanMint, chosen state.ObjID) []tokenPlanMint {
	out := make([]tokenPlanMint, 0, len(plan))
	for _, mint := range plan {
		if e.tokenReplacementMatchesMint(ev, m, mint) {
			out = append(out, tokenPlanMint{copyOf: chosen})
		} else {
			out = append(out, mint)
		}
	}
	return out
}

// applyTokenReplacementToPlan transforms the plan by ONE match, per mint,
// with the match's own ValidToken$ re-checked against each mint's script.
func (e *Engine) applyTokenReplacementToPlan(ev events.Event, plan []tokenPlanMint, m replMatch) []tokenPlanMint {
	body := m.repl.With
	switch applyTokenReplacementToPlan2272Codes.Code(string(strings.TrimSpace(body.ParamStr(cards.PKType)))) {
	case applyTokenReplacementToPlan2272ReplaceToken:
		// "... instead create those tokens as <scripts>" — a pure rewrite:
		// each matched mint is replaced by one mint per script in the CSV
		// (Academy Manufactor's one Clue -> Clue+Food+Treasure; Divine
		// Visitation's squirrel -> angel).
		scripts := e.knownTokenScripts(m.id, body.ParamStr(cards.PKTokenScript))
		if len(scripts) == 0 {
			return plan
		}
		out := make([]tokenPlanMint, 0, len(plan)*len(scripts))
		for _, mint := range plan {
			if e.tokenReplacementMatchesMint(ev, m, mint) {
				for _, s := range scripts {
					out = append(out, tokenPlanMint{script: s})
				}
			} else {
				out = append(out, mint)
			}
		}
		return out
	case applyTokenReplacementToPlan2272AddToken:
		// Corpus convention: Amount$ present is a fixed add for the whole
		// creation event; absent Amount$ means "that many" (one per matched
		// mint), as on Chatterfang. Append fixed extras at plan end so their
		// replay-visible mint order is deterministic. (cli-20260927T005250Z-c5ac2e83)
		raw := strings.TrimSpace(body.ParamStr(cards.PKAmount))
		fixed := raw != ""
		n := int32(1)
		if fixed {
			v, ok := e.tokenReplacementAmount(m, ev, raw, 1)
			if !ok || v < 0 {
				e.emit(events.Event{Kind: events.Note, Obj: m.id, Player: ev.Player,
					Text: "ReplaceToken Amount$ " + raw + " is not implemented; the token is created unchanged"})
				return plan
			}
			n = v
		}
		extra := e.knownTokenScripts(m.id, body.ParamStr(cards.PKTokenScript))
		if len(extra) == 0 {
			return plan
		}
		out := make([]tokenPlanMint, 0, len(plan)+int(n)*len(extra))
		var fixedController tokenPlanMint
		matched := false
		for _, mint := range plan {
			out = append(out, mint)
			if e.tokenReplacementMatchesMint(ev, m, mint) {
				if fixed {
					fixedController = mint
					matched = true
					continue
				}
				for i := int32(0); i < n; i++ {
					for _, s := range extra {
						out = append(out, tokenPlanMint{script: s, controller: mint.controller, hasController: mint.hasController})
					}
				}
			}
		}
		if fixed && matched {
			for i := int32(0); i < n; i++ {
				for _, s := range extra {
					out = append(out, tokenPlanMint{script: s, controller: fixedController.controller, hasController: fixedController.hasController})
				}
			}
		}
		return out
	case applyTokenReplacementToPlan2272ReplaceController:
		// "... is created under <NewController$>'s control instead" (Crafty
		// Cutpurse). The new controller is resolved through the ordinary
		// Defined$ player grammar against the replacement source -- `You` is
		// the source's controller, so an opponent's token creation becomes
		// the source controller's -- and every matched mint carries it. A
		// controller the grammar cannot resolve fails closed: the Note names
		// it and the mint is left with its original creator rather than
		// guessed onto a seat.
		p, ok := e.tokenNewController(m, ev)
		if !ok {
			e.emit(events.Event{Kind: events.Note, Obj: m.id, Player: ev.Player,
				Text: "ReplaceToken NewController$ " + strings.TrimSpace(body.ParamStr(cards.PKNewController)) + " is not implemented; the token is created unchanged"})
			return plan
		}
		out := make([]tokenPlanMint, 0, len(plan))
		for _, mint := range plan {
			if e.tokenReplacementMatchesMint(ev, m, mint) {
				mint.controller = p
				mint.hasController = true
			}
			out = append(out, mint)
		}
		return out
	default:
		// "Amount" (and an absent Type$ — the corpus always names one, Amount
		// is the natural default for the "twice that many" doubler family):
		// each matched mint becomes replCountOp(1, Amount$) copies of itself.
		raw := strings.TrimSpace(body.ParamStr(cards.PKAmount))
		if raw == "" {
			raw = "Twice"
		}
		n, ok := e.tokenReplacementAmount(m, ev, raw, 1)
		if !ok || n < 0 {
			e.emit(events.Event{Kind: events.Note, Obj: m.id, Player: ev.Player,
				Text: "ReplaceToken Amount$ " + raw + " is not implemented; the token is created unchanged"})
			return plan
		}
		out := make([]tokenPlanMint, 0, len(plan)*int(n)+1)
		for _, mint := range plan {
			if !e.tokenReplacementMatchesMint(ev, m, mint) {
				out = append(out, mint)
				continue
			}
			if n >= 1 {
				for i := int32(0); i < n; i++ {
					out = append(out, mint)
				}
				continue
			}
			// n == 0 (HalfDown against this engine's one-token events): the
			// matched mint is not created. A halving composed AFTER a
			// multiplier applies per mint rather than to the plan total — a
			// deliberate scan-order composition approximation, tracked with
			// the CR 616.1 order-choice follow-up.
		}
		return out
	}
}

// tokenReplacementMatchesMint re-checks ONE replacement against ONE plan
// mint: a scripted mint rides its script, a copy mint (copyOf) rides the
// copied object's printed face -- the would-be token's characteristics are
// the copy source's (CR 706.2), which is what lets a later match re-check a
// copy mint (Divine Visitation after Esix rewrites each copy that is still a
// creature token).
func (e *Engine) tokenReplacementMatchesMint(ev events.Event, m replMatch, mint tokenPlanMint) bool {
	mintEv := events.Event{Kind: events.TokenCreate, Player: tokenMintPlayer(mint, ev), Text: mint.script}
	// A copy mint's would-be token is the copied object's printed face, not a
	// token-script registry key (CR 706.2), so BOTH the matcher's ValidToken$
	// read and the ValidCard$ read below must see that snapshot -- one helper,
	// never two derivations that can drift (the mint-snapshot class).
	tok := e.mintSnapshot(mintEv, mint)
	// The recheck uses the same matcher class the initial collection used:
	// an effect-created match re-matches through the remembered-scoped effect
	// matcher, a printed match through the ordinary one -- never the ungated
	// effect matcher for a printed key. For a TokenCreate mint the two agree
	// on every printed Repl today (the mint, like ev, carries no To), but the
	// split keeps the recheck from ever widening what the initial gated
	// collection admitted, the same discipline remainingDamageReplacements
	// and counterReplacementMatchesAll follow.
	if m.key != "" {
		if !e.replacementMatchesEffectCreatedToken(*m.repl, m.id, mintEv, m.remembered, m.rememberedPlayers, tok) {
			return false
		}
	} else if !e.replacementMatchesToken(*m.repl, m.id, mintEv, tok) {
		return false
	}
	if m.repl.With != nil {
		if v := strings.TrimSpace(m.repl.With.ParamStr(cards.PKValidCard)); v != "" {
			if tok == nil {
				return false
			}
			// The provenance qualifier split applies here too (task castprov1):
			// a would-be TOKEN was never cast at all, so an alternative carrying
			// the qualifier is dropped for it.
			spec, ok := e.castProvenanceAdmits(v, tok.ID, e.controllerOf(m.id))
			if !ok || !effects.MatchesObjectCtx(e.G, spec, tok,
				e.rememberedSpecContext(e.controllerOf(m.id), m.id, m.remembered)) {
				return false
			}
		}
	}
	return true
}

// knownTokenScripts splits a ReplaceToken body's TokenScript$ CSV and keeps
// only the stems the game's token registry knows, one loud Note per unknown
// stem. An empty result leaves the caller's plan untouched.
func (e *Engine) knownTokenScripts(source state.ObjID, csv string) []string {
	var out []string
	for s := range strings.SplitSeq(csv, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := e.G.Tokens[s]; !ok {
			e.emit(events.Event{Kind: events.Note, Obj: source,
				Text: "unknown token script " + s})
			continue
		}
		out = append(out, s)
	}
	return out
}

// tokenReplaceCount resolves a ReplaceToken body's Amount$: a literal
// integer (the AddToken family's Amount$ 1) or one of replCountOp's word
// ops read against the single-mint base (Twice -> 2, Thrice -> 3, HalfDown
// -> 0 against the one-token event this engine mints — replCountOp is the
// shared word-op parser). An unresolvable value (X, an SVar name) reports
// not-ok and the match is dropped with a loud Note.
func tokenReplaceCount(raw string) (int32, bool) {
	if n, err := strconv.Atoi(raw); err == nil {
		return int32(n), true
	}
	switch {
	case raw == "Twice", raw == "Thrice", raw == "HalfDown", raw == "HalfUp",
		strings.HasPrefix(raw, "Plus."), strings.HasPrefix(raw, "Minus."),
		strings.HasPrefix(raw, "Times."):
		return replCountOp(1, raw), true
	}
	return 0, false
}

// tokenReplacementAmount resolves a ReplaceToken body's Amount$: the literal
// and op-word grammar of tokenReplaceCount first, then the shared numeric
// grammar (effects.NumResolved) so an SVar body or inline Count$ amount
// resolves too -- `Amount$ X` with `X:ReplaceCount$CounterNum/Twice` reads as
// the doubler the corpus's own ReplaceCounter bodies already express. base is
// the single-mint ReplaceCount base. An unpriceable value reports not-ok and
// the match is dropped with a loud Note, never read as zero.
func (e *Engine) tokenReplacementAmount(m replMatch, ev events.Event, raw string, base int32) (int32, bool) {
	if n, ok := tokenReplaceCount(raw); ok {
		return n, true
	}
	ctx := e.replCtx(m, ev)
	ctx.ReplacementAmount = base
	return effects.NumResolved(e, ctx, m.repl.With, "Amount", base)
}

// tokenNewController resolves a Type$ ReplaceController body's NewController$
// value through the ordinary Defined$ player grammar against the replacement
// source (replCtx binds the source's controller, so `You` -- the corpus's
// only spelling, Crafty Cutpurse -- is that controller). An absent,
// unrecognised, or playerless selector reports not-ok and the match is
// skipped with a loud Note, never guessed onto a seat.
func (e *Engine) tokenNewController(m replMatch, ev events.Event) (state.PlayerID, bool) {
	raw := strings.TrimSpace(m.repl.With.ParamStr(cards.PKNewController))
	if raw == "" {
		return 0, false
	}
	for _, t := range effects.DefinedSpec(e, e.replCtx(m, ev), raw) {
		if t.IsPlayer {
			return t.Player, true
		}
	}
	return 0, false
}

// mintSnapshot builds the would-be token a plan mint creates: the token
// script the event names, or -- for a copy mint -- the copied object's
// printed face as a never-added-to-the-game token (CR 706.2, the
// chosenCopySnapshot discipline). Both the ValidToken$ matcher read and the
// ValidCard$ re-check route through this one helper so a copy mint can never
// be rechecked as an empty script again.
func (e *Engine) mintSnapshot(mintEv events.Event, mint tokenPlanMint) *state.Object {
	if mint.copyOf != 0 {
		return e.chosenCopySnapshot(mint.copyOf, mintEv.Player)
	}
	return e.tokenSnapshot(mintEv)
}

// tokenSnapshot builds the would-be token a TokenCreate event would mint,
// as a shallow read-side object for ValidToken$ matching. Never added to
// the game — a value snapshot like StackCopy's discipline. A nil return
// (unknown token key) fails the caller's match closed.
func (e *Engine) tokenSnapshot(ev events.Event) *state.Object {
	if isCopyTokenProposal(ev) {
		// ProposeCopyTokens' would-be token is a copy of ev.Obj (CR 706.2).
		return e.chosenCopySnapshot(ev.Obj, ev.Player)
	}
	def := e.G.Tokens[ev.Text]
	if def == nil {
		return nil
	}
	// Zone is the battlefield: the would-be token is being created ONTO the
	// battlefield, and a `ValidToken$ Permanent...` gate (Flitwing Lyev's
	// `Permanent.YouCtrl`) reads matchesBase's battlefield requirement for
	// the bare Permanent base. Leaving the zero (library) value made that
	// spelling fail closed and the replacement never apply.
	return &state.Object{Card: def, IsToken: true, Owner: ev.Player, Controller: ev.Player,
		Zone: state.ZBattlefield}
}

// chosenCopySnapshot is the ValidCard$ re-check's read-side snapshot of a
// COPY plan mint: the copied object's printed face as a never-added-to-
// the-game token (the tokenSnapshot discipline). A vanished source
// snapshots nothing (fail closed).
func (e *Engine) chosenCopySnapshot(src state.ObjID, player state.PlayerID) *state.Object {
	o := e.G.Obj(src)
	if o == nil || o.Card == nil {
		return nil
	}
	return &state.Object{Card: o.Card, FaceIdx: o.FaceIdx, IsToken: true,
		Owner: player, Controller: player, Zone: state.ZBattlefield}
}

const (
	tokenReplacementsCommute2271Amount   uint16 = 1 // "Amount"
	tokenReplacementsCommute2271AddToken uint16 = 2 // "AddToken"
)

var tokenReplacementsCommute2271Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "Amount", Val: tokenReplacementsCommute2271Amount},
	state.StrEntry[uint16]{Key: "AddToken", Val: tokenReplacementsCommute2271AddToken},
)

const (
	applyTokenReplacementToPlan2272ReplaceToken      uint16 = 1 // "ReplaceToken"
	applyTokenReplacementToPlan2272AddToken          uint16 = 2 // "AddToken"
	applyTokenReplacementToPlan2272ReplaceController uint16 = 3 // "ReplaceController"
)

var applyTokenReplacementToPlan2272Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "ReplaceToken", Val: applyTokenReplacementToPlan2272ReplaceToken},
	state.StrEntry[uint16]{Key: "AddToken", Val: applyTokenReplacementToPlan2272AddToken},
	state.StrEntry[uint16]{Key: "ReplaceController", Val: applyTokenReplacementToPlan2272ReplaceController},
)
