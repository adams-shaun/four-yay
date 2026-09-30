package azmcts

import (
	"encoding/json"
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

// cand is one candidate answer at a searched decision: its semantic actions
// (the tree identity), their key, and the intent that plays it on THIS
// decision of THIS engine.
type cand struct {
	acts []searchprobe.Action
	key  Key
	in   decision.Intent
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
	cands, kind, _, ok := enumerateWhy(obs, e, d, bot, kinds, limit)
	return cands, kind, ok
}

// enumerateWhy is enumerate plus the reason a searched kind was skipped
// (meaningful only when ok is false and kind is not "").
func enumerateWhy(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, kinds Kinds, limit int) ([]cand, string, SkipReason, bool) {
	return enumerateWhyAutoPayment(obs, e, d, bot, kinds, limit, false)
}

func enumerateWhyAutoPayment(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, kinds Kinds, limit int, autoPayment bool) ([]cand, string, SkipReason, bool) {
	cands, kind, why, ok, _ := enumerateCut(obs, e, d, bot, kinds, limit, autoPayment)
	return cands, kind, why, ok
}

// enumerateCut is enumerateWhyAutoPayment that also reports whether limit
// cut a candidate: it enumerates one past limit (every enumerator is
// prefix-stable: its first limit candidates do not depend on the cap) and
// drops the extra. A translation failure of that probe falls back to the
// capped enumeration, so the candidates are exactly the capped
// enumerator's whenever that one succeeds.
func enumerateCut(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, kinds Kinds, limit int, autoPayment bool) ([]cand, string, SkipReason, bool, bool) {
	cands, kind, why, ok := enumerateLimit(obs, e, d, bot, kinds, limit+1, autoPayment)
	if ok && len(cands) > limit {
		return cands[:limit], kind, why, ok, true
	}
	if !ok && why == SkipTranslate {
		// The probe past the cap could not be translated: the capped list
		// may still be, exactly as before the probe existed.
		cands, kind, why, ok = enumerateLimit(obs, e, d, bot, kinds, limit, autoPayment)
	}
	return cands, kind, why, ok, false
}

func enumerateLimit(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, kinds Kinds, limit int, autoPayment bool) ([]cand, string, SkipReason, bool) {
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
		cands, why, ok := paymentPriorityCands(obs, e, d, bot, limit)
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
		b := botpolicy.BoardFromGame(e.G, e, d.Player)
		legal := func(choices []int) []int { return botpolicy.LegalBlockChoices(b, d, choices) }
		ins = searchprobe.BlockCandidates(d, bot, limit, legal)
	case "target":
		ins = searchprobe.TargetCandidates(d, bot, limit)
	case "priority":
		base, err := obs.Actions(d, bot)
		if err != nil || len(base) != 1 {
			return nil, kind, SkipTranslate, false
		}
		for _, a := range searchprobe.Candidates(worthOptions(od, e, d), base[0], limit) {
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
		acts, err := obs.Actions(d, in)
		if err != nil {
			return nil, kind, SkipTranslate, false
		}
		out = append(out, cand{acts: acts, key: actionsKey(acts), in: in})
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
func nameKeys(e *rules.Engine, d *decision.Decision, cands []cand, rootRefs int) []cand {
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
		acts := append([]searchprobe.Action(nil), c.acts...)
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
func paymentPriorityCands(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, limit int) ([]cand, SkipReason, bool) {
	if _, err := obs.ObserveDecision(e, d); err != nil {
		return nil, SkipTranslate, false
	}
	payments := e.EnsurePaymentActions()
	var pays, rest []cand
	var pass *cand
	covered := make(map[state.ObjID]bool, len(payments)) // membership only -- never ranged.
	payAt := make(map[state.ObjID]int, len(payments))    // lookup only -- never ranged.
	for _, a := range payments {
		if len(a.Plans) == 0 || covered[a.Cast.Object] {
			continue
		}
		acts, err := paymentActs(obs, e, d, a)
		if err != nil {
			return nil, SkipTranslate, false
		}
		c := cand{acts: acts, key: Key(payKeyPrefix + string(actionsKey(acts))), scoreSet: true,
			in: decision.Intent{Seq: d.Seq, Player: d.Player, Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: decision.ClonePaymentPlan(a.Plans[0])}}}
		if a.BaseOptionIndex != nil {
			c.score = []int{*a.BaseOptionIndex}
		}
		covered[a.Cast.Object] = true
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
				b = botpolicy.BoardFromGame(e.G, e, d.Player)
				built = true
			}
			if !b.AbilityWorthTaking(o, d.Player) {
				continue
			}
		}
		if o.Kind == "cast" && o.Mode == "" && o.AltCostIndex == 0 && covered[o.Obj] {
			continue
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}
		acts, err := obs.Actions(d, in)
		if err != nil {
			return nil, SkipTranslate, false
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
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].key < rest[j].key })
	var all []cand
	if pass != nil {
		all = append(all, *pass)
	}
	all = append(append(all, pays...), rest...)
	botAt := -1
	for i, c := range all {
		switch {
		case bot.Payment != nil:
			if c.in.Payment != nil && c.in.Payment.ActionID == bot.Payment.ActionID {
				botAt = i
			}
		case len(bot.Choices) == 1 && c.in.Payment == nil:
			if c.in.Choices[0] == bot.Choices[0] {
				botAt = i
			}
		}
		if botAt >= 0 {
			break
		}
	}
	if botAt < 0 && bot.Payment == nil && len(bot.Choices) == 1 && bot.Choices[0] >= 0 && bot.Choices[0] < len(d.Options) {
		// A manual bot's plain cast of an object that has a payment action
		// is that payment candidate.
		if o := d.Options[bot.Choices[0]]; o.Kind == "cast" && o.Mode == "" && o.AltCostIndex == 0 {
			if at, ok := payAt[o.Obj]; ok {
				botAt = at
				if pass != nil {
					botAt++ // all is pass, then the payments
				}
			}
		}
	}
	if botAt < 0 {
		return nil, SkipFewCandidates, false
	}
	out := []cand{all[botAt]}
	for i, c := range all {
		if len(out) >= limit {
			break
		}
		if i != botAt {
			out = append(out, c)
		}
	}
	if len(out) < 2 {
		return nil, SkipFewCandidates, false
	}
	return out, 0, true
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
	var acts []searchprobe.Action
	if err := json.Unmarshal([]byte(k), &acts); err != nil {
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
					b = botpolicy.BoardFromGame(e.G, e, d.Player)
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

// actionsKey is the canonical key of a semantic action list: its JSON
// encoding (fixed field order, so equal lists give equal keys).
func actionsKey(acts []searchprobe.Action) Key {
	b, err := json.Marshal(acts)
	if err != nil {
		// Action holds only integers and strings; Marshal cannot fail.
		panic(fmt.Sprintf("azmcts: encoding a semantic action: %v", err))
	}
	return Key(b)
}

// priors is the candidates' prior (spec §2): uniform without a network;
// otherwise a softmax, across candidates, of policynet.CandidateScore over
// the head's option scores on the deciding seat's redacted view -- the
// subset log-likelihood for attackers and blockers, the single option's
// score for priority and target. The bot's options are marked (BotPick) as a
// residual checkpoint was trained. fellBack reports a network prior that
// could not be formed (every candidate -Inf or NaN) and fell back to uniform.
func priors(net *policynet.Model, e *rules.Engine, d *decision.Decision, bot decision.Intent, kind string, cands []cand) ([]float64, bool) {
	if net == nil {
		return uniform(len(cands)), false
	}
	v := view.Project(e.G, e, d.Player, d)
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
		choices, ok := c.scoreChoices()
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
