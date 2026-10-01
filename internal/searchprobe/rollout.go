package searchprobe

import (
	"bytes"
	"cmp"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func Candidates(d *ObservedDecision, baseline Action, limit int) []Action {
	out, _ := appendCandidates(nil, nil, d, baseline, limit)
	return out
}

// Candidates is the package's Candidates written into c's reusable storage:
// the result is valid until the next Candidates call on c (azmcts reads it
// once per searched priority decision, inside its simulations).
func (c *Collector) Candidates(d *ObservedDecision, baseline Action, limit int) []Action {
	out, pool := appendCandidates(c.candOut[:0], c.candPool[:0], d, baseline, limit)
	c.candPool = pool
	if cap(out) > cap(c.candOut) {
		c.candOut = out
	}
	return out
}

// appendCandidates is Candidates into out[:0], with pool as sorting scratch;
// it returns the candidates (nil when there are none to compare) and pool.
func appendCandidates(out, pool []Action, d *ObservedDecision, baseline Action, limit int) ([]Action, []Action) {
	if d == nil || d.Kind != decision.KPriority || limit < 2 || !candidateKind(baseline.Kind) {
		return nil, pool
	}
	pool = pool[:0]
	var pass Action
	hasPass, hasBaseline := false, false
	for i := range d.Options {
		a := &d.Options[i].Action
		if candidateKind(a.Kind) {
			pool = append(pool, *a)
		}
		if a.Kind == "pass" {
			pass = *a
			hasPass = true
		}
		if *a == baseline {
			hasBaseline = true
		}
	}
	if len(pool) < 2 || !hasPass || !hasBaseline {
		return nil, pool
	}
	sortActionsByEncoding(pool)
	out = append(out[:0], baseline)
	if baseline != pass {
		out = append(out, pass)
	}
	for _, a := range pool {
		if len(out) >= limit {
			break
		}
		if a != baseline && a != pass {
			out = append(out, a)
		}
	}
	return out, pool
}

// sortActionsByEncoding stably sorts actions by their JSON encodings' byte
// order -- the frozen candidate order -- comparing them field by field
// (actionJSONCompare) without encoding either.
func sortActionsByEncoding(actions []Action) {
	slices.SortStableFunc(actions, func(a, b Action) int { return actionJSONCompare(&a, &b) })
}

// actionJSONCompare is bytes.Compare(json.Marshal(*a), json.Marshal(*b)).
//
// Both encodings are the same constant keys in the same order with one value
// token per field, so they agree up to the first field whose tokens differ,
// and the order is decided there: by the first differing byte of the two
// tokens, or -- when one token is a proper prefix of the other -- by the
// byte after the shorter token, which is the delimiter that follows every
// value (',' after each field but the last). A string token can never be a
// proper prefix of another (its closing quote is its only unescaped quote),
// and a number token never contains ',', so that byte decides. Only the
// differing field's two tokens are encoded, into stack buffers.
func actionJSONCompare(a, b *Action) int {
	var ba, bb [64]byte
	str := func(x, y string) int {
		if x == y {
			return 0
		}
		return tokenCompare(appendJSONString(ba[:0], x), appendJSONString(bb[:0], y), ',')
	}
	num := func(x, y int64) int {
		if x == y {
			return 0
		}
		return tokenCompare(strconv.AppendInt(ba[:0], x, 10), strconv.AppendInt(bb[:0], y, 10), ',')
	}
	if c := str(string(a.Decision), string(b.Decision)); c != 0 {
		return c
	}
	if c := num(int64(a.Source), int64(b.Source)); c != 0 {
		return c
	}
	if c := str(a.Kind, b.Kind); c != 0 {
		return c
	}
	for _, xy := range [...][2]int64{
		{int64(a.Obj), int64(b.Obj)}, {int64(a.Attacker), int64(b.Attacker)}, {int64(a.Player), int64(b.Player)},
		{int64(a.Ability), int64(b.Ability)}, {int64(a.AltCostIndex), int64(b.AltCostIndex)}, {int64(a.Amount), int64(b.Amount)},
	} {
		if c := num(xy[0], xy[1]); c != 0 {
			return c
		}
	}
	if c := str(a.Mode, b.Mode); c != 0 {
		return c
	}
	if c := str(a.SVar, b.SVar); c != 0 {
		return c
	}
	if a.Value == b.Value {
		return 0
	}
	return tokenCompare(appendJSONString(ba[:0], a.Value), appendJSONString(bb[:0], b.Value), '}')
}

// tokenCompare is bytes.Compare(x+d+..., y+d+...) for unequal value tokens x
// and y followed by the same delimiter d, where d cannot follow a token's
// proper prefix inside the longer token (see actionJSONCompare).
func tokenCompare(x, y []byte, d byte) int {
	n := min(len(x), len(y))
	if c := bytes.Compare(x[:n], y[:n]); c != 0 {
		return c
	}
	switch {
	case len(x) < len(y):
		return cmp.Compare(d, y[n])
	case len(x) > len(y):
		return cmp.Compare(x[n], d)
	}
	return 0
}

// appendJSONString appends s as json.Marshal encodes a string: quoted, with
// HTML escaping, \b \f \n \r \t and \u00XX for other control bytes, each
// invalid UTF-8 byte as \ufffd, and U+2028/U+2029 escaped (encoding/json's
// appendString with escapeHTML set).
func appendJSONString(dst []byte, s string) []byte {
	const hex = "0123456789abcdef"
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); {
		if b := s[i]; b < utf8.RuneSelf {
			if b >= 0x20 && b != '"' && b != '\\' && b != '<' && b != '>' && b != '&' {
				i++
				continue
			}
			dst = append(dst, s[start:i]...)
			switch b {
			case '\\', '"':
				dst = append(dst, '\\', b)
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				dst = append(dst, '\\', 'u', '0', '0', hex[b>>4], hex[b&0xF])
			}
			i++
			start = i
			continue
		}
		c, size := utf8.DecodeRuneInString(s[i:])
		if c == utf8.RuneError && size == 1 {
			dst = append(dst, s[start:i]...)
			dst = append(dst, `\ufffd`...)
			i += size
			start = i
			continue
		}
		if c == '\u2028' || c == '\u2029' {
			dst = append(dst, s[start:i]...)
			dst = append(dst, '\\', 'u', '2', '0', '2', hex[c&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	dst = append(dst, s[start:]...)
	return append(dst, '"')
}

func candidateKind(kind string) bool { return kind == "cast" || kind == "ability" || kind == "pass" }

// Frozen calibration scorer v1: terminal +/-100000 (draw 0); otherwise
// life + 2*hand-count + material. Land=3; other permanent=10; creatures add
// 2*(power+toughness). Only the projected public board/counts are read.
func LeafScore(v view.View, actor state.PlayerID) float64 {
	if v.Over {
		if v.Draw {
			return 0
		}
		if v.Winner != nil && *v.Winner == actor {
			return 100000
		}
		return -100000
	}
	value := 0.0
	for _, p := range v.Players {
		score := float64(p.Life) + 2*float64(p.HandSize)
		for _, c := range p.Battlefield {
			score += material(c)
		}
		if p.ID == actor {
			value += score
		} else {
			value -= score
		}
	}
	return value
}
func material(c view.CardView) float64 {
	return materialScore(strings.Contains(c.Types, "Land"), strings.Contains(c.Types, "Creature"), c.Power+c.Toughness)
}

func materialScore(land, creature bool, powerToughness int32) float64 {
	if land {
		return 3
	}
	score := 10.0
	if creature {
		score += 2 * float64(powerToughness)
	}
	return score
}

// Frozen static challenger v1: pass=0, activated ability=1, cast=printed
// material score. No simulated outcomes or hidden state enter this ranking.
func StaticChoice(frame Frame, candidates []Action) (int, error) {
	if len(candidates) == 0 {
		return 0, fmt.Errorf("empty candidates")
	}
	best, bestScore := 0, -1.0
	for i, a := range candidates {
		score := 0.0
		if a.Kind == "ability" {
			score = 1
		}
		if a.Kind == "cast" {
			for _, p := range frame.Board.Players {
				for _, zone := range [][]BoardCard{p.Hand, p.Graveyard, p.Exile, p.Command, p.Battlefield} {
					for _, c := range zone {
						if c.ID == a.Obj {
							score = c.material()
						}
					}
				}
			}
		}
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	return best, nil
}

type SearchResult struct {
	Index, Replays, Submits int
	Scores                  []float64
	Fallback                string
}

// SearchChoice never receives the actual engine. Every candidate uses the
// same sampled worlds and independent continuation seeds, through turn end.
func SearchChoice(worlds []World, candidates []Action, seed uint64, maxSubmits int) (SearchResult, error) {
	result := SearchResult{Scores: make([]float64, len(candidates))}
	if len(candidates) == 0 || maxSubmits < 1 {
		return result, fmt.Errorf("invalid search budget/candidates")
	}
	if len(worlds) == 0 {
		result.Fallback = "insufficient sampled worlds/ESS"
		return result, nil
	}
	var lv view.View // the leaf projection, refilled per rollout (view.ProjectInto)
	for wi, w := range worlds {
		if err := VerifyWorld(w); err != nil {
			return result, err
		}
		result.Replays++
		for i, a := range candidates {
			e := w.Engine.Clone()
			in, err := w.Observer.Match(e.Pending(), []Action{a})
			if err != nil {
				return result, fmt.Errorf("root action mismatch: %w", err)
			}
			actor, turn := e.Pending().Player, e.G.Turn
			if err := e.SubmitHypothetical(in); err != nil {
				return result, err
			}
			submits := 1
			rngs := BotRandoms(seed^uint64(wi+1), len(e.G.Players))
			board := botpolicy.NewBoard(len(rngs))
			for !e.G.Over && e.G.Turn == turn && submits < maxSubmits {
				d := e.Pending()
				if d == nil {
					return result, fmt.Errorf("rollout has no pending decision")
				}
				in := botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])
				if err := e.SubmitHypothetical(in); err != nil {
					return result, err
				}
				submits++
			}
			result.Submits += submits
			if !e.G.Over && e.G.Turn == turn {
				result.Fallback = "turn-end submit budget exhausted"
				return result, nil
			}
			view.ProjectInto(&lv, e.G, e, actor, e.Pending())
			result.Scores[i] += LeafScore(lv, actor) / float64(len(worlds))
		}
	}
	for i := 1; i < len(result.Scores); i++ {
		if result.Scores[i] > result.Scores[result.Index] {
			result.Index = i
		}
	}
	return result, nil
}

func VerifyWorld(w World) error {
	e, err := rules.NewHypothetical(w.Config, w.Engine.ChanceTranscript())
	if err != nil {
		return err
	}
	if err := e.AdvanceHypothetical(); err != nil {
		return err
	}
	for _, in := range w.Engine.L.Intents {
		if err := e.SubmitHypothetical(in); err != nil {
			return err
		}
	}
	if e.L.Head() != w.Engine.L.Head() || e.RNGDraws() != w.Engine.RNGDraws() || len(e.L.Events) != len(w.Engine.L.Events) {
		return fmt.Errorf("sampled-world replay diverged")
	}
	return nil
}

func BotRandoms(seed uint64, seats int) []*rand.Rand {
	out := make([]*rand.Rand, seats)
	for i := range out {
		s := seed ^ uint64(i+1)
		out[i] = rand.New(rand.NewPCG(s, s^0x9e3779b97f4a7c15))
	}
	return out
}
