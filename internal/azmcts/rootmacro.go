package azmcts

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// MacroKeyPrefix marks a root macro's key (Macro.Key).
const MacroKeyPrefix = "macro:"

// IsMacroKey reports whether k names a root macro.
func IsMacroKey(k Key) bool { return strings.HasPrefix(string(k), MacroKeyPrefix) }

// MacroStep is one recorded answer of a root macro: at a decision of Kind
// posed to Player, the options Picks names, matched by identity
// (rules.ScriptPick: kind, object, label, mana symbol), never by index; or,
// with PayCast set, the payment action casting that object, paid with its
// first plan.
type MacroStep struct {
	Player  state.PlayerID
	Kind    decision.Kind
	Picks   []rules.ScriptPick
	PayCast state.ObjID
}

// Intent is the step's answer at decision d of engine e.
func (s MacroStep) Intent(e *rules.Engine, d *decision.Decision) (decision.Intent, error) {
	if d == nil || d.Player != s.Player || d.Kind != s.Kind {
		return decision.Intent{}, fmt.Errorf("azmcts: macro step wants a %s decision of player %d", s.Kind, s.Player)
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	if s.PayCast != 0 {
		if d.Kind == decision.KPriority {
			for _, a := range e.EnsurePaymentActions() {
				if a.Cast.Object == s.PayCast && len(a.Plans) > 0 {
					in.Payment = &decision.PaymentSelection{ActionID: a.ID, Plan: decision.ClonePaymentPlan(a.Plans[0])}
					return in, nil
				}
			}
		}
		return decision.Intent{}, fmt.Errorf("azmcts: macro step: no payment action casts object %d", s.PayCast)
	}
	used := make([]bool, len(d.Options))
	for _, pk := range s.Picks {
		at := -1
		for j := range d.Options {
			if !used[j] && pk.Matches(&d.Options[j]) {
				at = j
				break
			}
		}
		if at < 0 {
			return decision.Intent{}, fmt.Errorf("azmcts: macro step: %s %q is not offered", pk.Kind, pk.Label)
		}
		used[at] = true
		in.Choices = append(in.Choices, d.Options[at].Index)
	}
	return in, nil
}

// Macro is a caller-supplied root candidate that the candidate vocabulary
// cannot express as one intent: a play reached only by activating mana
// first (an ability with a mana cost, a cast in a payment mode, a cast whose
// plan needs a scripted prefix), or an option the vocabulary leaves out (a
// land play). Steps are its answers from the root decision on; the walk
// plays all of them as ONE searched edge (one ply, as a payment action pays
// its whole plan in one submit) and the play's own later asks (targets,
// modes) are the walk's. Key must start with MacroKeyPrefix and be the same
// for the same play in every world.
type Macro struct {
	Key   Key
	Label string
	Steps []MacroStep
}

// fullRoot reports whether r asks for the full root (Root.Macros, BotKey,
// NoBot).
func (r Root) fullRoot() bool { return len(r.Macros) > 0 || r.BotKey != "" || r.NoBot }

// rootCands is the root's candidates. A full root (Root.Macros, BotKey or
// NoBot) at a priority decision under auto-payment is the vocabulary plus
// every macro whose key it does not hold yet, with the bot's candidate first
// -- Root.Bot's vocabulary candidate, else the candidate keyed BotKey, else,
// under NoBot or with BotKey unmatched, no bot candidate (the natural order:
// Pass first) -- instead of skipping a decision whose bot answer is outside
// the vocabulary. botFound reports whether candidate 0 is the bot's. Any
// other root enumerates as before (enumerateCut).
func rootCands(obs *searchprobe.Collector, root Root, kinds Kinds, limit int, autoPayment bool) (cands []cand, kind string, why SkipReason, ok, cut, botFound bool) {
	e, d := root.Engine, root.Decision
	if !root.fullRoot() || !autoPayment || d.Kind != decision.KPriority || !kinds.Priority {
		cands, kind, why, ok, cut = enumerateCut(obs, e, d, root.Bot, kinds, limit, autoPayment)
		return cands, kind, why, ok, cut, ok
	}
	kind = "priority"
	v, why, ok := paymentVocabulary(obs, e, d)
	if !ok {
		return nil, kind, why, false, false, false
	}
	all := v.all
	have := make(map[Key]bool, len(all)+len(root.Macros)) // membership only -- never ranged.
	for _, c := range all {
		have[c.key] = true
	}
	for i := range root.Macros {
		m := &root.Macros[i]
		if !IsMacroKey(m.Key) || len(m.Steps) == 0 || have[m.Key] {
			continue
		}
		in, err := m.Steps[0].Intent(e, d)
		if err != nil {
			continue
		}
		have[m.Key] = true
		all = append(all, cand{key: m.Key, in: in, macro: m, scoreSet: true})
	}
	botAt := v.botIndex(d, root.Bot)
	if botAt < 0 && root.BotKey != "" {
		for i, c := range all {
			if c.key == root.BotKey {
				botAt = i
				break
			}
		}
	}
	if botAt >= 0 {
		cands = botFirst(all, botAt, limit+1)
	} else {
		cands = append([]cand(nil), all[:min(len(all), limit+1)]...)
	}
	if len(cands) > limit {
		cands, cut = cands[:limit], true
	}
	if len(cands) < 2 {
		return nil, kind, SkipFewCandidates, false, cut, false
	}
	return cands, kind, 0, true, cut, botAt >= 0
}

// playMacro plays every step of m on the world.
func (e *engineEnv) playMacro(m *Macro) error {
	for i, st := range m.Steps {
		d := e.e.Pending()
		if e.e.G.Over || d == nil {
			return fmt.Errorf("%w: macro %s ended the game at step %d", ErrSubmit, m.Key, i)
		}
		in, err := st.Intent(e.e, d)
		if err != nil {
			return fmt.Errorf("%w: macro %s step %d: %v", ErrSubmit, m.Key, i, err)
		}
		if err := e.submit(d, in); err != nil {
			return err
		}
	}
	return nil
}

// macroCand is the root candidate keyed k, mapped onto this world's
// decision: a macro whose first step the world offers.
func (e *engineEnv) macroCand(k Key) (cand, bool) {
	for _, c := range e.cfg.rootCands {
		if c.key != k || c.macro == nil {
			continue
		}
		in, err := c.macro.Steps[0].Intent(e.e, e.cur)
		if err != nil {
			return cand{}, false
		}
		return cand{key: k, in: in, macro: c.macro, scoreSet: true}, true
	}
	return cand{}, false
}
