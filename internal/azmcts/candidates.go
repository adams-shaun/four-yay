package azmcts

import (
	"fmt"
	"math"
	"sort"
	"strings"

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
	// acts is the candidate's semantic actions where its builder already
	// had them (the payment vocabulary); the ordinary path keys candidates
	// without building them (Collector.IntentKey), and nameKeys rebuilds
	// them on demand.
	acts []searchprobe.Action
	// macro, when set, is a caller-supplied root macro (Root.Macros): in
	// is its first step on the root decision, and the walk plays every
	// step (engineEnv.playMacro).
	macro *Macro
	// score is the option-index list a network prior scores the candidate
	// by: in.Choices, or a payment action's legacy cast option (nil when
	// the payment has none, which makes a network prior fall back).
	score []int
	// scoreSet marks score as set (a payment candidate); otherwise
	// in.Choices is scored.
	scoreSet bool
}

// scoreChoices is the option-index list a network prior scores c by.
func (c cand) scoreChoices() ([]int, bool) {
	if c.scoreSet {
		return c.score, c.score != nil
	}
	return c.in.Choices, true
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
		// The scratch boards feed only the bot's and the enumerators'
		// answers, which never read OwnLibrary.
		s.b.SkipOwnLibrary()
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
	return enumerateWhyAutoPayment(obs, e, d, bot, kinds, limit, false, scratch)
}

func enumerateWhyAutoPayment(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, kinds Kinds, limit int, autoPayment bool, scratch *boardScratch) ([]cand, string, SkipReason, bool) {
	cands, kind, why, ok, _ := enumerateCutInto(obs, e, d, bot, kinds, limit, autoPayment, scratch)
	return cands, kind, why, ok
}

// enumerateCut is enumerateWhyAutoPayment that also reports whether limit
// cut a candidate: it enumerates one past limit (every enumerator is
// prefix-stable: its first limit candidates do not depend on the cap) and
// drops the extra. A translation failure of that probe falls back to the
// capped enumeration, so the candidates are exactly the capped
// enumerator's whenever that one succeeds.
func enumerateCut(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, kinds Kinds, limit int, autoPayment bool) ([]cand, string, SkipReason, bool, bool) {
	return enumerateCutInto(obs, e, d, bot, kinds, limit, autoPayment, nil)
}

// enumerateCutInto is enumerateCut over a board scratch (enumerateInto).
func enumerateCutInto(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, kinds Kinds, limit int, autoPayment bool, scratch *boardScratch) ([]cand, string, SkipReason, bool, bool) {
	cands, kind, why, ok := enumerateLimit(obs, e, d, bot, kinds, limit+1, autoPayment, scratch)
	if ok && len(cands) > limit {
		return cands[:limit], kind, why, ok, true
	}
	if !ok && why == SkipTranslate {
		// The probe past the cap could not be translated: the capped list
		// may still be, exactly as before the probe existed.
		cands, kind, why, ok = enumerateLimit(obs, e, d, bot, kinds, limit, autoPayment, scratch)
	}
	return cands, kind, why, ok, false
}

func enumerateLimit(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, kinds Kinds, limit int, autoPayment bool, scratch *boardScratch) ([]cand, string, SkipReason, bool) {
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
	// Payment actions are atomic engine-provided cast witnesses. They are not
	// ordinary Decision.Options because paying may include several mana
	// abilities, but they are nevertheless legal priority actions and must be
	// searchable when an embedding exposes auto-pay (paymentPriorityCands).
	if autoPayment && kind == "priority" && e != nil {
		cands, why, ok := paymentPriorityCands(obs, e, d, bot, limit, scratch)
		return cands, kind, why, ok
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

// payKeyPrefix marks a payment candidate's key: "pay:" then the semantic
// key of a plain cast of the payment's object (paymentActs), so the same
// planned cast has the same key in every world however the engine numbered
// its objects or its action.
const payKeyPrefix = "pay:"

// paymentActs is the semantic identity of payment action a on d: the
// observed action of a plain cast of its object, built on a one-option copy
// of d (a payment action is not a Decision.Option, so the collector has no
// option for it).
func paymentActs(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, a decision.PaymentAction) ([]searchprobe.Action, error) {
	one := *d
	one.Min, one.Max = 1, 1
	one.Options = []decision.Option{{Index: 0, Kind: "cast", Obj: a.Cast.Object, Player: d.Player, Label: a.Label}}
	one.PaymentActions = nil
	// The cast's object is a hand card no legacy option may name yet (no
	// mana floats): introduce it, as ObserveDecision does for d's own.
	if _, err := obs.ObserveDecision(e, &one); err != nil {
		return nil, err
	}
	return obs.Actions(&one, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}})
}

// nameKeys is Options.NameKeys at an in-walk point: every candidate key's
// object references past rootRefs (objects the root observation did not
// show) become the object's card name, and candidates whose keys then
// coincide keep the first. Only keys change; the intents are this world's.
func nameKeys(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, cands []cand, rootRefs int) []cand {
	name := func(id state.ObjID) string {
		o := e.G.Obj(id)
		switch {
		case o == nil:
			return "?"
		case o.FaceDown && o.Owner != d.Player:
			return "face-down"
		case o.Card == nil || len(o.Card.Faces) == 0:
			return "?"
		}
		return o.Card.Faces[0].Name
	}
	late := func(ref uint32, id state.ObjID) bool {
		if _, player := id.PlayerRef(); player || id == 0 {
			return false
		}
		return int(ref) > rootRefs
	}
	out := cands[:0:0]
	seen := make(map[Key]bool, len(cands)) // membership only -- never ranged.
	for _, c := range cands {
		srcActs := c.acts
		if srcActs == nil && c.in.Payment == nil {
			// The ordinary path keyed this candidate without building its
			// actions (Collector.IntentKey); rebuild them, exactly the
			// actions its key encodes.
			if a, err := obs.Actions(d, c.in); err == nil {
				srcActs = a
			}
		}
		acts := append([]searchprobe.Action(nil), srcActs...)
		changed := false
		for k := range acts {
			src, obj, atk := d.Source, state.ObjID(0), state.ObjID(0)
			switch {
			case c.in.Payment != nil:
				for _, a := range d.PaymentActions {
					if a.ID == c.in.Payment.ActionID {
						obj = a.Cast.Object
					}
				}
			case k < len(c.in.Choices) && c.in.Choices[k] >= 0 && c.in.Choices[k] < len(d.Options):
				o := d.Options[c.in.Choices[k]]
				obj, atk = o.Obj, o.Attacker
			}
			a := &acts[k]
			if late(a.Source, src) {
				a.Source, a.Value, changed = 0, a.Value+"|source="+name(src), true
			}
			if late(a.Obj, obj) {
				a.Obj, a.Value, changed = 0, a.Value+"|object="+name(obj), true
			}
			if late(a.Attacker, atk) {
				a.Attacker, a.Value, changed = 0, a.Value+"|attacker="+name(atk), true
			}
		}
		if changed {
			k := actionsKey(acts)
			if c.in.Payment != nil {
				k = Key(payKeyPrefix + string(k))
			}
			c.key = k
		}
		if seen[c.key] {
			continue
		}
		seen[c.key] = true
		out = append(out, c)
	}
	return out
}

// paymentPriorityCands is the auto-payment priority vocabulary, the search
// benchmark's root and in-walk candidate set: Pass; every engine payment
// action (a plain cast with its first offered plan, EnsurePaymentActions);
// and every other cast or ability option -- an alternative-cost or
// non-hand cast, a legacy cast whose object has no plan, an ability the
// bot's own guards would take (worthOptions' rule). A legacy plain cast of
// an object that has a payment action is the same cast and is not offered
// twice. Land plays and mana activations are never candidates.
//
// The bot's answer comes first (the tie-winner): a Payment selection maps
// to its action's candidate, a choice to its option's. A bot answer outside
// the vocabulary -- a land play, a mana activation (the auto-pay bot floats
// mana only for a cast its plans cannot make) -- is not searched: the bot's
// answer is played (SkipFewCandidates, the ordinary path's rule). Then
// Pass, the payment casts in engine order, and the rest by key; capped at
// limit.
func paymentPriorityCands(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, limit int, scratch *boardScratch) ([]cand, SkipReason, bool) {
	v, why, ok := paymentVocabulary(obs, e, d, scratch)
	if !ok {
		return nil, why, false
	}
	botAt := v.botIndex(d, bot)
	if botAt < 0 {
		return nil, SkipFewCandidates, false
	}
	out := botFirst(v.all, botAt, limit)
	if len(out) < 2 {
		return nil, SkipFewCandidates, false
	}
	return out, 0, true
}

// paymentVocab is the auto-payment priority vocabulary of one decision in
// its natural order: Pass, the payment casts in engine order, the rest by
// key (paymentPriorityCands).
type paymentVocab struct {
	all     []cand
	payAt   map[state.ObjID]int // lookup only -- never ranged.
	hasPass bool
}

func paymentVocabulary(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, scratch *boardScratch) (paymentVocab, SkipReason, bool) {
	if _, err := obs.ObserveDecision(e, d); err != nil {
		return paymentVocab{}, SkipTranslate, false
	}
	payments := e.EnsurePaymentActions()
	var pays, rest []cand
	var pass *cand
	payAt := make(map[state.ObjID]int, len(payments)) // lookup only -- never ranged.
	// payAt doubles as the covered set: an object is covered exactly when it
	// has a payment candidate.
	for _, a := range payments {
		if _, dup := payAt[a.Cast.Object]; len(a.Plans) == 0 || dup {
			continue
		}
		acts, err := paymentActs(obs, e, d, a)
		if err != nil {
			return paymentVocab{}, SkipTranslate, false
		}
		c := cand{acts: acts, key: Key(payKeyPrefix + string(actionsKey(acts))), scoreSet: true,
			in: decision.Intent{Seq: d.Seq, Player: d.Player, Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: decision.ClonePaymentPlan(a.Plans[0])}}}
		if a.BaseOptionIndex != nil {
			c.score = []int{*a.BaseOptionIndex}
		}
		payAt[a.Cast.Object] = len(pays)
		pays = append(pays, c)
	}
	var b botpolicy.Board
	built := false
	for i, o := range d.Options {
		switch o.Kind {
		case "pass", "cast", "ability":
		default:
			continue
		}
		if o.Kind == "ability" {
			if !built {
				b = boardInto(e, d.Player, scratch)
				built = true
			}
			if !b.AbilityWorthTaking(o, d.Player) {
				continue
			}
		}
		if o.Kind == "cast" && o.Mode == "" && o.AltCostIndex == 0 {
			if _, covered := payAt[o.Obj]; covered {
				continue
			}
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}
		acts, err := obs.Actions(d, in)
		if err != nil {
			return paymentVocab{}, SkipTranslate, false
		}
		c := cand{acts: acts, key: actionsKey(acts), in: in}
		if o.Kind == "pass" {
			if pass == nil {
				pass = &c
			}
			continue
		}
		rest = append(rest, c)
	}
	sortByJSON(rest)
	v := paymentVocab{payAt: payAt, hasPass: pass != nil}
	v.all = make([]cand, 0, 1+len(pays)+len(rest))
	if pass != nil {
		v.all = append(v.all, *pass)
	}
	v.all = append(append(v.all, pays...), rest...)
	return v, 0, true
}

// sortByJSON orders cands by the JSON encoding of their semantic actions,
// stably (searchprobe.CompareActionsJSON): the order the vocabulary's
// "rest" had while a candidate key WAS that encoding, before
// searchprobe.AppendActionsKey replaced it. The candidate order decides
// PUCT's ties, so the search benchmark's results are pinned to it.
func sortByJSON(cands []cand) {
	sort.SliceStable(cands, func(i, j int) bool { return searchprobe.CompareActionsJSON(cands[i].acts, cands[j].acts) < 0 })
}

// botIndex is the index in v.all of the bot's answer bot, -1 when it is
// outside the vocabulary (a land play, a mana activation).
func (v paymentVocab) botIndex(d *decision.Decision, bot decision.Intent) int {
	for i, c := range v.all {
		switch {
		case bot.Payment != nil:
			if c.in.Payment != nil && c.in.Payment.ActionID == bot.Payment.ActionID {
				return i
			}
		case len(bot.Choices) == 1 && c.in.Payment == nil:
			if c.in.Choices[0] == bot.Choices[0] {
				return i
			}
		}
	}
	if bot.Payment == nil && len(bot.Choices) == 1 && bot.Choices[0] >= 0 && bot.Choices[0] < len(d.Options) {
		// A manual bot's plain cast of an object that has a payment action
		// is that payment candidate.
		if o := d.Options[bot.Choices[0]]; o.Kind == "cast" && o.Mode == "" && o.AltCostIndex == 0 {
			if at, ok := v.payAt[o.Obj]; ok {
				if v.hasPass {
					at++ // all is pass, then the payments
				}
				return at
			}
		}
	}
	return -1
}

// botFirst is all with all[botAt] moved to the front, capped at limit.
func botFirst(all []cand, botAt, limit int) []cand {
	out := []cand{all[botAt]}
	for i, c := range all {
		if len(out) >= limit {
			break
		}
		if i != botAt {
			out = append(out, c)
		}
	}
	return out
}

// IntentForKey is the intent that plays candidate key k on decision d of
// engine e, through obs -- a collector for d's player that has captured e
// at d. A key names the same semantic action in every world, so this is
// how a choice made on one world is played on another (the benchmark's
// honest arms answer on the item's own engine; RootPerWorld maps every
// root key into each simulation's world). An error means d does not offer
// k.
func IntentForKey(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, k Key) (decision.Intent, error) {
	m, err := newKeyMatcher(obs, e, d)
	if err != nil {
		return decision.Intent{}, err
	}
	return m.intent(k)
}

// keyMatcher maps keys onto one decision (IntentForKey), observing it and
// building its payment keys once.
type keyMatcher struct {
	obs  *searchprobe.Collector
	d    *decision.Decision
	pays []cand
}

func newKeyMatcher(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision) (*keyMatcher, error) {
	if obs == nil || e == nil || d == nil {
		return nil, fmt.Errorf("azmcts: matching a key needs an observer, an engine and a decision")
	}
	if _, err := obs.ObserveDecision(e, d); err != nil {
		return nil, err
	}
	m := &keyMatcher{obs: obs, d: d}
	if d.Kind == decision.KPriority {
		for _, a := range e.EnsurePaymentActions() {
			if len(a.Plans) == 0 {
				continue
			}
			acts, err := paymentActs(obs, e, d, a)
			if err != nil {
				return nil, err
			}
			m.pays = append(m.pays, cand{key: Key(payKeyPrefix + string(actionsKey(acts))),
				in: decision.Intent{Seq: d.Seq, Player: d.Player, Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: decision.ClonePaymentPlan(a.Plans[0])}}})
		}
	}
	return m, nil
}

func (m *keyMatcher) intent(k Key) (decision.Intent, error) {
	if strings.HasPrefix(string(k), payKeyPrefix) {
		for _, c := range m.pays {
			if c.key == k {
				in := c.in
				in.Payment = &decision.PaymentSelection{ActionID: c.in.Payment.ActionID, Plan: decision.ClonePaymentPlan(c.in.Payment.Plan)}
				return in, nil
			}
		}
		return decision.Intent{}, fmt.Errorf("azmcts: payment candidate %s is not offered here", k)
	}
	acts, err := searchprobe.ParseActionsKey([]byte(k))
	if err != nil {
		return decision.Intent{}, fmt.Errorf("azmcts: key %q is not a semantic action list: %v", k, err)
	}
	return m.obs.Match(m.d, acts)
}

// CandidateLabel is a human-readable label for candidate intent in on
// decision d: the chosen options' labels joined by " + " ("no attack" or
// "no block" for an empty declaration), or a payment action's cast label.
func CandidateLabel(d *decision.Decision, in decision.Intent) string {
	if d == nil {
		return ""
	}
	if in.Payment != nil {
		for _, a := range d.PaymentActions {
			if a.ID == in.Payment.ActionID {
				return a.Label + " (auto-pay)"
			}
		}
		return "payment " + in.Payment.ActionID
	}
	if len(in.Choices) == 0 {
		switch d.Kind {
		case decision.KAttackers:
			return "no attack"
		case decision.KBlockers:
			return "no block"
		}
		return "none"
	}
	parts := make([]string, 0, len(in.Choices))
	for _, c := range in.Choices {
		if c >= 0 && c < len(d.Options) {
			parts = append(parts, d.Options[c].Label)
		}
	}
	return strings.Join(parts, " + ")
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
	return priorsWith(net, e, d, bot, kind, cands, pv, false)
}

// priorsWith is priors; payCasts is the PriorTopK ranking prior's form
// (rankedPrior). There a payment candidate with no legacy cast option is
// scored as the plain cast option it stands for (payCastOption), appended
// after d's own options and encoded as one more option of the list,
// instead of the whole prior falling back to uniform: under auto-pay no
// mana floats, so the engine offers almost no legacy cast and the ordinary
// prior could rank no cast at all. The bot's payment candidate's scored
// option is marked BotPick, as MarkBotPicks marks a choice answer's. With
// no such candidate and a choice answer the two forms are the same prior.
func priorsWith(net *policynet.Model, e *rules.Engine, d *decision.Decision, bot decision.Intent, kind string, cands []cand, pv *view.View, payCasts bool) ([]float64, bool) {
	if net == nil {
		return uniform(len(cands)), false
	}
	if pv == nil {
		pv = new(view.View)
	}
	view.ProjectInto(pv, e.G, e, d.Player, d)
	v := *pv
	st := policynet.EncodeStateWith(net.Features, v, d.Player, nil)
	opts := d.Options
	// synth[i] is the index in opts of candidate i's synthetic cast option,
	// -1 for none; nil when no candidate has one.
	var synth []int
	if payCasts {
		for i, c := range cands {
			o, ok := payCastOption(e, d, c)
			if !ok {
				continue
			}
			if synth == nil {
				synth = make([]int, len(cands))
				for j := range synth {
					synth[j] = -1
				}
				opts = append(make([]decision.Option, 0, len(d.Options)+len(cands)), d.Options...)
			}
			o.Index = len(opts)
			synth[i] = len(opts)
			opts = append(opts, o)
		}
	}
	enc := make([]policynet.Option, len(opts))
	for i := range opts {
		enc[i] = policynet.EncodeOptionWith(net.Features, v, d.Player, d.Kind, opts[i], i, len(opts))
	}
	seat.MarkBotPicks(d, enc[:len(d.Options)], bot)
	if payCasts && bot.Payment != nil {
		for i, c := range cands {
			if c.in.Payment == nil || c.in.Payment.ActionID != bot.Payment.ActionID {
				continue
			}
			if choices, ok := c.scoreChoices(); ok {
				for _, k := range choices {
					if k >= 0 && k < len(d.Options) {
						enc[k].BotPick = true
					}
				}
			} else if synth != nil && synth[i] >= 0 {
				enc[synth[i]].BotPick = true
			}
		}
	}
	scores := net.Score(st, enc)
	subset := kind == "attackers" || kind == "blockers"
	logits := make([]float64, len(cands))
	for i, c := range cands {
		choices, ok := c.scoreChoices()
		if !ok && synth != nil && synth[i] >= 0 {
			choices, ok = synth[i:i+1], true
		}
		if !ok {
			// A payment cast with no legacy option has no option score.
			return uniform(len(cands)), true
		}
		logits[i] = policynet.CandidateScore(subset, scores, choices)
	}
	p, ok := softmax(logits)
	if !ok {
		return uniform(len(cands)), true
	}
	return p, false
}

// payCastOption is the option a network scores payment candidate c by when
// its action has no legacy cast option (score nil): the plain cast of its
// object, as paymentActs keys it. ok is false for any other candidate (a
// choice, a payment scored by its legacy option, a root macro) and when
// e's pending decision is not d (no payment action to read).
func payCastOption(e *rules.Engine, d *decision.Decision, c cand) (decision.Option, bool) {
	if c.in.Payment == nil || c.macro != nil {
		return decision.Option{}, false
	}
	if _, ok := c.scoreChoices(); ok {
		return decision.Option{}, false
	}
	if pd := e.Pending(); pd == nil || pd.Seq != d.Seq || pd.Player != d.Player {
		return decision.Option{}, false
	}
	// Built and kept on the pending decision when the vocabulary was
	// enumerated: this reads them again, it builds nothing.
	for _, a := range e.EnsurePaymentActions() {
		if a.ID == c.in.Payment.ActionID {
			return decision.Option{Kind: "cast", Obj: a.Cast.Object, Player: d.Player, Label: a.Label}, true
		}
	}
	return decision.Option{}, false
}

// rankedPrior is a searched point's candidates and prior. With k == 0 (no
// PriorTopK, or no network prior) they are cands and priors' prior. With
// k > 0 the ranking prior (priorsWith's payCasts form) is formed over every
// candidate and priorTopK cuts it: before is priorTopK's.
func rankedPrior(net *policynet.Model, k int, e *rules.Engine, d *decision.Decision, bot decision.Intent, kind string, cands []cand, pv *view.View) (kept []cand, prior []float64, fell bool, before int) {
	if k <= 0 {
		prior, fell = priors(net, e, d, bot, kind, cands, pv)
		return cands, prior, fell, 0
	}
	prior, fell = priorsWith(net, e, d, bot, kind, cands, pv, true)
	kept, prior, before = priorTopK(cands, prior, k)
	return kept, prior, fell, before
}

// priorTopK is Options.PriorTopK's cut: cands[0] -- the bot's answer, the
// tie-winner -- then the k-1 other candidates of highest prior, prior
// descending with ties to the lower index (the enumeration order, so a
// uniform prior keeps the first k), their prior renormalised over the kept
// ones. before is len(cands) when the cut dropped a candidate, 0 when it
// kept them all (reordered all the same, the prior unchanged). Neither
// input is modified.
func priorTopK(cands []cand, prior []float64, k int) ([]cand, []float64, int) {
	order := make([]int, 0, len(cands))
	for i := 1; i < len(cands); i++ {
		order = append(order, i)
	}
	sort.SliceStable(order, func(a, b int) bool { return prior[order[a]] > prior[order[b]] })
	before := 0
	if len(order) > k-1 {
		order, before = order[:k-1], len(cands)
	}
	kept := append(make([]cand, 0, len(order)+1), cands[0])
	p := append(make([]float64, 0, len(order)+1), prior[0])
	sum := prior[0]
	for _, i := range order {
		kept = append(kept, cands[i])
		p = append(p, prior[i])
		sum += prior[i]
	}
	if before > 0 {
		for i := range p {
			if sum > 0 {
				p[i] /= sum
			} else {
				p[i] = 1 / float64(len(p))
			}
		}
	}
	return kept, p, before
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
