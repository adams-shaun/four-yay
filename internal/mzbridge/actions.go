package mzbridge

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Port of ActionEncoder.java (mage/player/ai/encoder/ActionEncoder.java) and
// the CHOOSE_USE rule of MCTSNode.getActionIndex (MCTSNode.java:210-222).
//
// The vocabulary file is draft-zero's assets/vocab/FDN_SPG.tsv. It is NOT
// copied into this repository: the trainer and server read the same file
// through MZ_ACTION_VOCAB, so the one copy in the draft-zero clone is the
// only one that can be right, and its ability lines are card rules text.

// ActionType is ActionEncoder.ActionType; the ordinal is what a shard row
// stores in column A+3 (LabeledStateWriter.java:122) and what MageZero's
// trainer switches on (model.py ActionType: 0, 3 and 5 are the trained ones).
type ActionType int

const (
	Priority     ActionType = 0
	ChooseNum    ActionType = 1
	Blank        ActionType = 2
	ChooseTarget ActionType = 3
	MakeChoice   ActionType = 4
	ChooseUse    ActionType = 5
)

var actionTypeNames = [...]string{"PRIORITY", "CHOOSE_NUM", "BLANK", "CHOOSE_TARGET", "MAKE_CHOICE", "CHOOSE_USE"}

// String is the Java enum constant's name, which StateEncoder hashes as a
// root feature (StateEncoder.java:94).
func (a ActionType) String() string {
	if a < 0 || int(a) >= len(actionTypeNames) {
		return "ActionType(" + strconv.Itoa(int(a)) + ")"
	}
	return actionTypeNames[a]
}

// MZActionVocabEnv is the variable both the JVM and MageZero's Python read
// the vocabulary path from.
const MZActionVocabEnv = "MZ_ACTION_VOCAB"

// legacyDim is the policy width with no vocabulary configured.
const legacyDim = 128

// Vocab maps priority-action labels and target names to policy indices.
type Vocab struct {
	dim             int
	loaded          bool
	actions         map[string]int
	targets         map[string]int
	actionHashStart int
	targetHashStart int
}

// LegacyVocab is the encoder with no vocabulary file: a 128-wide head, a few
// reserved names, everything else hashed into 1..127.
func LegacyVocab() *Vocab {
	return &Vocab{
		dim: legacyDim,
		actions: map[string]int{"Pass": 0, "{T}: Add {B}.": 1, "{T}: Add {G}.": 2, "{T}: Add {R}.": 3,
			"{T}: Add {U}.": 4, "{T}: Add {W}.": 5, "{T}: Add {C}.": 6},
		targets: map[string]int{"Stop Choosing": 0, "PlayerA": 1, "PlayerB": 2},
	}
}

// LoadVocab reads a vocabulary file.
func LoadVocab(path string) (*Vocab, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("mzbridge: action vocabulary: %w", err)
	}
	defer f.Close()
	v, err := ParseVocab(f)
	if err != nil {
		return nil, fmt.Errorf("mzbridge: action vocabulary %s: %w", path, err)
	}
	return v, nil
}

// ParseVocab reads the format ActionEncoder's static initialiser reads:
// UTF-8, one entry per line, empty lines and lines starting '#' skipped;
//
//	dim<TAB>1024
//	A<TAB><index><TAB><ability.toString()>
//	T<TAB><index><TAB><entity name>
//
// with a literal backslash-n in the third field standing for a newline. A
// line of any other shape is ignored, as upstream ignores it; a repeated
// key keeps its last index.
func ParseVocab(r io.Reader) (*Vocab, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	// BufferedReader.readLine ends a line at \n, \r or \r\n
	text := strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n")
	v := &Vocab{dim: legacyDim, loaded: true, actions: map[string]int{}, targets: map[string]int{}}
	aMax, tMax := -1, -1
	for ln, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if parts[0] == "dim" {
			if len(parts) < 2 {
				return nil, fmt.Errorf("line %d: dim with no value", ln+1)
			}
			if v.dim, err = strconv.Atoi(strings.TrimSpace(parts[1])); err != nil {
				return nil, fmt.Errorf("line %d: dim: %w", ln+1, err)
			}
			continue
		}
		if len(parts) != 3 {
			continue
		}
		idx, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("line %d: index: %w", ln+1, err)
		}
		key := strings.ReplaceAll(parts[2], `\n`, "\n")
		switch parts[0] {
		case "A":
			v.actions[key] = idx
			aMax = max(aMax, idx)
		case "T":
			v.targets[key] = idx
			tMax = max(tMax, idx)
		}
	}
	if aMax >= v.dim-1 || tMax >= v.dim-1 {
		return nil, fmt.Errorf("does not fit dim %d with a hash tail (max action idx %d, max target idx %d)", v.dim, aMax, tMax)
	}
	v.actionHashStart, v.targetHashStart = aMax+1, tMax+1
	return v, nil
}

// Dim is the width of the priority and target policy vectors (A in a shard
// row of A+4 columns).
func (v *Vocab) Dim() int { return v.dim }

// NumActions and NumTargets count the listed labels.
func (v *Vocab) NumActions() int { return len(v.actions) }
func (v *Vocab) NumTargets() int { return len(v.targets) }

// ActionHashStart and TargetHashStart are the first indices of the hashed
// tails (one past the largest listed index).
func (v *Vocab) ActionHashStart() int { return v.actionHashStart }
func (v *Vocab) TargetHashStart() int { return v.targetHashStart }

// KnownAction reports whether label has a slot of its own.
func (v *Vocab) KnownAction(label string) bool { _, ok := v.actions[label]; return ok }

// KnownTarget reports whether name has a slot of its own.
func (v *Vocab) KnownTarget(name string) bool { _, ok := v.targets[name]; return ok }

// ActionIndex is ActionEncoder.getActionIndex for a PRIORITY decision: the
// label's own slot, else the hashed tail
// [ActionHashStart, Dim) by Math.floorMod(label.hashCode(), tail width).
// With a vocabulary loaded both players share one map, so there is no
// isPlayer argument.
func (v *Vocab) ActionIndex(label string) int {
	if i, ok := v.actions[label]; ok {
		return i
	}
	if v.loaded {
		return v.actionHashStart + floorMod(JavaHashCode(label), v.dim-v.actionHashStart)
	}
	return legacyHashedIndex(label)
}

// TargetIndex is ActionEncoder.getTargetIndex for a CHOOSE_TARGET decision.
func (v *Vocab) TargetIndex(name string) int {
	if i, ok := v.targets[name]; ok {
		return i
	}
	if v.loaded {
		return v.targetHashStart + floorMod(JavaHashCode(name), v.dim-v.targetHashStart)
	}
	return legacyHashedIndex(name)
}

// UseIndex is the CHOOSE_USE index: 1 for yes, 0 for no.
func UseIndex(use bool) int {
	if use {
		return 1
	}
	return 0
}

// legacyHashedIndex is the no-vocabulary tail, (abs(hashCode) % 127) + 1,
// kept bug for bug: Math.abs(Integer.MIN_VALUE) is negative, so a label
// hashing to it lands on -7.
func legacyHashedIndex(s string) int {
	h := JavaHashCode(s)
	if h < 0 {
		h = -h
	}
	return int(h)%127 + 1
}
