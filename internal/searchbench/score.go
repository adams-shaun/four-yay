package searchbench

import "fmt"

// Decision is one completed benchmark choice. AgentAct is carried separately
// because a spell's accepted labels can include Pass when the source log does
// not reveal whether a cast happened before combat; the human still acted for
// the balanced cast-or-hold question.
type Decision struct {
	ID           string
	Type         DecisionType
	Human        Label
	AgentChoices []int
	AgentAct     bool
}

type BinaryScore struct {
	Positive, Negative         int
	TruePositive, TrueNegative int
}

func (s BinaryScore) BalancedAccuracy() (float64, bool) {
	if s.Positive == 0 || s.Negative == 0 {
		return 0, false
	}
	return (float64(s.TruePositive)/float64(s.Positive) + float64(s.TrueNegative)/float64(s.Negative)) / 2, true
}

type Summary struct {
	Count, Matches, AgentActs, HumanActs [4]int
	WhichCount, WhichMatches             [4]int
	CastHold, Attack, Block              BinaryScore
}

func typeIndex(t DecisionType) int {
	switch t {
	case DecisionSpell:
		return 0
	case DecisionHold:
		return 1
	case DecisionAttack:
		return 2
	case DecisionBlock:
		return 3
	default:
		return -1
	}
}

// Score validates canonical inputs and aggregates the two distinct measures:
// exact/allowed agreement and the balanced act-or-wait questions.
func Score(rows []Decision) (Summary, error) {
	var out Summary
	for i := range rows {
		r := &rows[i]
		idx := typeIndex(r.Type)
		if r.ID == "" || idx < 0 {
			return Summary{}, fmt.Errorf("searchbench: result %d has invalid id or decision type", i)
		}
		if err := alternatives(r.Human.Alternatives); err != nil {
			return Summary{}, fmt.Errorf("searchbench: result %q label: %w", r.ID, err)
		}
		if err := choices(r.AgentChoices); err != nil {
			return Summary{}, fmt.Errorf("searchbench: result %q agent choices: %w", r.ID, err)
		}
		out.Count[idx]++
		if r.Human.Act {
			out.HumanActs[idx]++
		}
		if r.AgentAct {
			out.AgentActs[idx]++
		}
		match := false
		for _, candidate := range r.Human.Alternatives {
			if compareChoices(candidate, r.AgentChoices) == 0 {
				match = true
				break
			}
		}
		if match {
			out.Matches[idx]++
		}
		if r.Human.Act && r.AgentAct {
			out.WhichCount[idx]++
			if match {
				out.WhichMatches[idx]++
			}
		}
		binary := &out.CastHold
		if r.Type == DecisionAttack {
			binary = &out.Attack
		} else if r.Type == DecisionBlock {
			binary = &out.Block
		}
		if r.Human.Act {
			binary.Positive++
			if r.AgentAct {
				binary.TruePositive++
			}
		} else {
			binary.Negative++
			if !r.AgentAct {
				binary.TrueNegative++
			}
		}
	}
	return out, nil
}

// MacroAgreement is the unbalanced, compatibility-only score used by the
// original study. It requires all four decision types to be represented.
func (s Summary) MacroAgreement() (float64, bool) {
	total := 0.0
	for i, n := range s.Count {
		if n == 0 {
			return 0, false
		}
		total += float64(s.Matches[i]) / float64(n)
	}
	return total / float64(len(s.Count)), true
}

// BalancedAgreement is the native headline. It gives any constant policy
// 0.50 on every populated binary question.
func (s Summary) BalancedAgreement() (float64, bool) {
	cast, ok := s.CastHold.BalancedAccuracy()
	if !ok {
		return 0, false
	}
	attack, ok := s.Attack.BalancedAccuracy()
	if !ok {
		return 0, false
	}
	block, ok := s.Block.BalancedAccuracy()
	if !ok {
		return 0, false
	}
	return (cast + attack + block) / 3, true
}
