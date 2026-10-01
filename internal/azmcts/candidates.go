package azmcts

import (
	"math"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// cand is one candidate answer at a searched decision: the key of its
// semantic actions (the tree identity, actionsKey) and the intent that plays
// it on THIS decision of THIS engine.
type cand struct {
	key Key
	in  decision.Intent
}

// enumerate builds the candidates of the searching seat's decision d, the
// bot's answer bot first (spec §1), over the exported searchprobe
// enumerators the L10 search seat also uses:
//
//   - attackers: searchprobe.AttackCandidates;
//   - blockers: searchprobe.BlockCandidates, legality from the bot's own
//     block guard on the deciding seat's board (botpolicy.LegalBlockChoices);
//   - target (single-choice only, searchprobe.SingleTarget):
//     searchprobe.TargetCandidates;
//   - priority: searchprobe.Candidates over d's observed options -- the bot's
//     action, then pass, then the other cast/ability options. Land plays and
//     mana activations are never candidates, so a priority the bot answers
//     with one is not searched.
//
// It does not reuse searchseat.Eligible/candidates: that gate's priority arm
// is ">= 2 distinct castable objects" (searchseat.go:250), not the spec's
// rule, and the dispatch is unexported and bound to searchseat.Options.
//
// kind is "" when d is not a searched kind at all; ok is false for a searched
// kind that cannot be searched (fewer than two candidates, an auto-pay
// Payment answer, a collector for another seat, an action that cannot be
// translated). obs must be a collector for d.Player; ObserveDecision runs on
// it first. e is read only by the blockers arm.
func enumerate(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, kinds Kinds, limit int) ([]cand, string, bool) {
	cands, kind, _, ok := enumerateWhyInto(obs, e, d, bot, kinds, limit, nil)
	return cands, kind, ok
}

// enumerateInto is enumerate building its boards into scratch
// (botpolicy.BoardFromGameInto) instead of allocating one per call; nil
// allocates. The boards are read only inside the call, so a caller may
// hand the same scratch to every call it makes.
func enumerateInto(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, kinds Kinds, limit int, scratch *boardScratch) ([]cand, string, bool) {
	cands, kind, _, ok := enumerateWhyInto(obs, e, d, bot, kinds, limit, scratch)
	return cands, kind, ok
}

// enumerateWhy is enumerate plus the reason a searched kind was skipped
// (meaningful only when ok is false and kind is not "").
func enumerateWhy(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, kinds Kinds, limit int) ([]cand, string, SkipReason, bool) {
	return enumerateWhyInto(obs, e, d, bot, kinds, limit, nil)
}

// boardScratch is one reusable Board, allocated on first use
// (botpolicy.NewBoard) and refilled by every later use, so a caller that
// never needs a board never pays for one.
type boardScratch struct {
	b     botpolicy.Board
	built bool
}

// board is the scratch Board, allocated on first use.
func (s *boardScratch) board(players int) *botpolicy.Board {
	if !s.built {
		s.b, s.built = botpolicy.NewBoard(players), true
	}
	return &s.b
}

// boardInto is BoardFromGame, filled into scratch when one is given.
func boardInto(e *rules.Engine, p state.PlayerID, scratch *boardScratch) botpolicy.Board {
	if scratch == nil {
		return botpolicy.BoardFromGame(e.G, e, p)
	}
	return botpolicy.BoardFromGameInto(e.G, e, p, scratch.board(len(e.G.Players)))
}

// enumerateWhyInto is enumerateWhy over a board scratch (enumerateInto).
func enumerateWhyInto(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, kinds Kinds, limit int, scratch *boardScratch) ([]cand, string, SkipReason, bool) {
	var kind string
	switch {
	case d == nil:
		return nil, "", 0, false
	case d.Kind == decision.KAttackers && kinds.Attackers:
		kind = "attackers"
	case d.Kind == decision.KBlockers && kinds.Blockers:
		kind = "blockers"
	case kinds.Target && searchprobe.SingleTarget(d):
		kind = "target"
	case d.Kind == decision.KPriority && kinds.Priority:
		kind = "priority"
	default:
		return nil, "", 0, false
	}
	if bot.Payment != nil {
		return nil, kind, SkipPayment, false
	}
	od, err := obs.ObserveDecision(e, d)
	if err != nil {
		return nil, kind, SkipTranslate, false
	}
	var ins []decision.Intent
	switch kind {
	case "attackers":
		ins = searchprobe.AttackCandidates(d, bot, limit)
	case "blockers":
		b := boardInto(e, d.Player, scratch)
		legal := func(choices []int) []int { return botpolicy.LegalBlockChoices(b, d, choices) }
		ins = searchprobe.BlockCandidates(d, bot, limit, legal)
	case "target":
		ins = searchprobe.TargetCandidates(d, bot, limit)
	case "priority":
		var one [1]searchprobe.Action
		base, err := obs.AppendIntentActions(one[:0], d, bot)
		if err != nil || len(base) != 1 {
			return nil, kind, SkipTranslate, false
		}
		for _, a := range obs.Candidates(worthOptionsInto(od, e, d, scratch), base[0], limit) {
			in, err := obs.Match(d, []searchprobe.Action{a})
			if err != nil {
				return nil, kind, SkipTranslate, false
			}
			ins = append(ins, in)
		}
	}
	if len(ins) < 2 {
		return nil, kind, SkipFewCandidates, false
	}
	out := make([]cand, 0, len(ins))
	for _, in := range ins {
		key, err := obs.IntentKey(d, in)
		if err != nil {
			return nil, kind, SkipTranslate, false
		}
		out = append(out, cand{key: Key(key), in: in})
	}
	return out, kind, 0, true
}

// worthOptions is od without the "ability" options the bot's own activation
// guards decline (botpolicy.Board.AbilityWorthTaking: A1's provable no-ops
// and A5's per-turn budget), so the priority arm never offers one as a
// candidate. The bot's termination argument rests on those guards (A4: once
// nothing is worth activating, the pass ends the turn); a candidate that
// bypasses them lets the search pick a free no-op -- a re-equip onto the
// creature already wearing the Equipment -- over the pass at every priority,
// and the real game's turn never advances (Stage 0, 2026-09-27: seed
// 90000003, uw-tempo:mono-white-equipment, Bonesplitter re-equipped onto a
// Phyrexian Germ until -max-intents). Filtering here covers the root and
// every in-tree searched decision, since both enumerate.
//
// od.Options[i] is d.Options[i] (observeDecision maps them one to one). e
// nil (a synthetic unit-test decision with no engine) reads the zero Board:
// no creature, no activation census, exactly what such a decision shows the
// bot. od is returned unchanged when nothing is dropped.
func worthOptions(od *searchprobe.ObservedDecision, e *rules.Engine, d *decision.Decision) *searchprobe.ObservedDecision {
	return worthOptionsInto(od, e, d, nil)
}

// worthOptionsInto is worthOptions building its board into scratch (nil
// allocates).
func worthOptionsInto(od *searchprobe.ObservedDecision, e *rules.Engine, d *decision.Decision, scratch *boardScratch) *searchprobe.ObservedDecision {
	if od == nil || len(od.Options) != len(d.Options) {
		return od
	}
	var b botpolicy.Board
	built := false
	var keep []searchprobe.ObservedOption
	for i, o := range d.Options {
		if o.Kind == "ability" {
			if !built {
				if e != nil {
					b = boardInto(e, d.Player, scratch)
				}
				built = true
			}
			if !b.AbilityWorthTaking(o, d.Player) {
				if keep == nil {
					keep = append(make([]searchprobe.ObservedOption, 0, len(od.Options)), od.Options[:i]...)
				}
				continue
			}
		}
		if keep != nil {
			keep = append(keep, od.Options[i])
		}
	}
	if keep == nil {
		return od
	}
	out := *od
	out.Options = keep
	return &out
}

// priorityBase classifies the bot's answer at a priority decision by the
// option kind it picks.
func priorityBase(d *decision.Decision, bot decision.Intent) BaseKind {
	if bot.Payment != nil {
		return BasePayment
	}
	if d == nil || len(bot.Choices) != 1 || bot.Choices[0] < 0 || bot.Choices[0] >= len(d.Options) {
		return BaseOther
	}
	switch d.Options[bot.Choices[0]].Kind {
	case "cast":
		return BaseCast
	case "ability":
		return BaseAbility
	case "pass":
		return BasePass
	case "play_land":
		return BasePlayLand
	case "activate":
		return BaseActivate
	}
	return BaseOther
}

// actionsKey is the canonical key of a semantic action list
// (searchprobe.AppendActionsKey): equal exactly when the lists' JSON
// encodings are, which is what the key was before it stopped being JSON.
func actionsKey(acts []searchprobe.Action) Key {
	var buf [128]byte
	return Key(searchprobe.AppendActionsKey(buf[:0], acts))
}

// priors is the candidates' prior (spec §2): uniform without a network;
// otherwise a softmax, across candidates, of policynet.CandidateScore over
// the head's option scores on the deciding seat's redacted view -- the
// subset log-likelihood for attackers and blockers, the single option's
// score for priority and target. The bot's options are marked (BotPick) as a
// residual checkpoint was trained. fellBack reports a network prior that
// could not be formed (every candidate -Inf or NaN) and fell back to uniform.
// The view is projected into pv (view.ProjectInto), a fresh one when nil.
func priors(net *policynet.Model, e *rules.Engine, d *decision.Decision, bot decision.Intent, kind string, cands []cand, pv *view.View) ([]float64, bool) {
	if net == nil {
		return uniform(len(cands)), false
	}
	if pv == nil {
		pv = new(view.View)
	}
	view.ProjectInto(pv, e.G, e, d.Player, d)
	v := *pv
	st := policynet.EncodeStateWith(net.Features, v, d.Player, nil)
	enc := make([]policynet.Option, len(d.Options))
	for i := range d.Options {
		enc[i] = policynet.EncodeOptionWith(net.Features, v, d.Player, d.Kind, d.Options[i], i, len(d.Options))
	}
	seat.MarkBotPicks(d, enc, bot)
	scores := net.Score(st, enc)
	subset := kind == "attackers" || kind == "blockers"
	logits := make([]float64, len(cands))
	for i, c := range cands {
		logits[i] = policynet.CandidateScore(subset, scores, c.in.Choices)
	}
	p, ok := softmax(logits)
	if !ok {
		return uniform(len(cands)), true
	}
	return p, false
}

// softmax normalises logits; ok is false when no logit is finite or any is
// NaN or +Inf.
func softmax(logits []float64) ([]float64, bool) {
	hi := math.Inf(-1)
	for _, x := range logits {
		if math.IsNaN(x) || math.IsInf(x, 1) {
			return nil, false
		}
		if x > hi {
			hi = x
		}
	}
	if math.IsInf(hi, -1) {
		return nil, false
	}
	out := make([]float64, len(logits))
	sum := 0.0
	for i, x := range logits {
		out[i] = math.Exp(x - hi)
		sum += out[i]
	}
	for i := range out {
		out[i] /= sum
	}
	return out, true
}
