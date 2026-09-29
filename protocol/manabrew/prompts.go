package manabrew

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// Prompt inputs and outputs, one pair per prompt type
// (https://docs.manabrew.app/protocol/<kebab-name>/ for each of the 19 types).
//
// PromptInput is discriminated on input.type and PromptOutputData on
// output.type; both unions marshal canonically (the discriminator is always
// emitted) and unmarshal strictly (an unknown discriminator is an error).

// PromptInputData is the closed set of prompt input payloads. Go cannot hang
// a method on an interface type, so the wire union is the PromptInput struct
// carrier below; Value holds exactly one of the members after a decode (or
// whatever the caller stored). The isPromptInput marker keeps the set closed
// to this package.
type PromptInputData interface {
	isPromptInput()
	PromptType() string
}

// PromptInput is the prompt input union, discriminated on input.type.
type PromptInput struct {
	Value PromptInputData `json:"-"`
}

// PromptOutputValue is the closed set of prompt output payloads; see
// PromptInputData for why the union is a struct carrier.
type PromptOutputValue interface {
	isPromptOutput()
	OutputType() string
}

// PromptOutputData is the prompt output union, discriminated on output.type.
type PromptOutputData struct {
	Value PromptOutputValue `json:"-"`
}

// PromptPresentation is the common prompt framing (shared-types page).
type PromptPresentation struct {
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	Text        string      `json:"text,omitempty"`
	Targets     []TargetRef `json:"targets"`
}

// PromptBase is the presentation-bearing prefix shared by most inputs. It is
// embedded anonymously, so its fields flatten into the input object.
type PromptBase struct {
	Presentation PromptPresentation `json:"presentation"`
}

// --- inputs, in the Appendix A.4 order ---

// ChooseNumberInput: chooseNumber.
type ChooseNumberInput struct {
	PromptBase
	Min int `json:"min"`
	Max int `json:"max"`
}

func (ChooseNumberInput) isPromptInput()     {}
func (ChooseNumberInput) PromptType() string { return "chooseNumber" }

// ChooseCardsInput: chooseCards.
type ChooseCardsInput struct {
	PromptBase
	Cards []CardDto `json:"cards"`
	Min   int       `json:"min"`
	Max   int       `json:"max"`
}

func (ChooseCardsInput) isPromptInput()     {}
func (ChooseCardsInput) PromptType() string { return "chooseCards" }

// ChooseColorInput: chooseColor.
type ChooseColorInput struct {
	PromptBase
	ValidColors   []string `json:"validColors"`
	Amount        int      `json:"amount"`
	RepeatAllowed bool     `json:"repeatAllowed"`
}

func (ChooseColorInput) isPromptInput()     {}
func (ChooseColorInput) PromptType() string { return "chooseColor" }

// ChooseBooleanInput: chooseBoolean.
type ChooseBooleanInput struct {
	PromptBase
	ConfirmLabel string `json:"confirmLabel"`
	DenyLabel    string `json:"denyLabel"`
}

func (ChooseBooleanInput) isPromptInput()     {}
func (ChooseBooleanInput) PromptType() string { return "chooseBoolean" }

// SelectionOption is UNDEFINED in the published spec; {label, weight,
// canRepeat} is INFERRED from the chooseFromSelection example.
type SelectionOption struct {
	Label     string `json:"label"`
	Weight    int    `json:"weight"`
	CanRepeat bool   `json:"canRepeat"`
}

// ChooseFromSelectionInput: chooseFromSelection.
type ChooseFromSelectionInput struct {
	PromptBase
	Options  []SelectionOption `json:"options"`
	MinTotal int               `json:"minTotal"`
	MaxTotal int               `json:"maxTotal"`
}

func (ChooseFromSelectionInput) isPromptInput()     {}
func (ChooseFromSelectionInput) PromptType() string { return "chooseFromSelection" }

// RevealCardsInput: revealCards.
type RevealCardsInput struct {
	PromptBase
	Cards         []CardDto `json:"cards"`
	Zone          ZoneKind  `json:"zone"`
	OwnerPlayerID string    `json:"ownerPlayerId"`
}

func (RevealCardsInput) isPromptInput()     {}
func (RevealCardsInput) PromptType() string { return "revealCards" }

// ScryInput: scry.
type ScryInput struct {
	PromptBase
	Cards []CardDto         `json:"cards"`
	Zones []ScryDestination `json:"zones"`
}

func (ScryInput) isPromptInput()     {}
func (ScryInput) PromptType() string { return "scry" }

// ReorderItem is UNDEFINED in the published spec; {id, card, oracle?} is
// INFERRED from the reorder example.
type ReorderItem struct {
	ID     string   `json:"id"`
	Card   *CardDto `json:"card,omitempty"`
	Oracle string   `json:"oracle,omitempty"`
}

// ReorderInput: reorder.
type ReorderInput struct {
	PromptBase
	Items []ReorderItem `json:"items"`
}

func (ReorderInput) isPromptInput()     {}
func (ReorderInput) PromptType() string { return "reorder" }

// DiceRollEntry is one roll group of a diceRolled prompt.
type DiceRollEntry struct {
	Label          string `json:"label,omitempty"`
	PlayerID       string `json:"playerId,omitempty"`
	Round          int    `json:"round"`
	NaturalResults []int  `json:"naturalResults"`
	FinalResults   []int  `json:"finalResults"`
	IgnoredRolls   []int  `json:"ignoredRolls"`
	Highlighted    bool   `json:"highlighted"`
}

// DiceRolledInput: diceRolled.
type DiceRolledInput struct {
	PromptBase
	Sides int             `json:"sides"`
	Rolls []DiceRollEntry `json:"rolls"`
}

func (DiceRolledInput) isPromptInput()     {}
func (DiceRolledInput) PromptType() string { return "diceRolled" }

// ChooseActionInput: chooseAction. It carries no presentation; the actions
// list is the whole prompt.
type ChooseActionInput struct {
	Actions []AvailableAction `json:"actions"`
}

func (ChooseActionInput) isPromptInput()     {}
func (ChooseActionInput) PromptType() string { return "chooseAction" }

// PaymentAction is UNDEFINED in the published spec; the example is
// activateAbility-shaped, so the fields mirror ActivatableAbilityInfo.
type PaymentAction struct {
	ID            string `json:"id"`
	Type          string `json:"type,omitempty"`
	CardID        string `json:"cardId,omitempty"`
	AbilityIndex  int    `json:"abilityIndex,omitempty"`
	Description   string `json:"description,omitempty"`
	IsManaAbility bool   `json:"isManaAbility,omitempty"`
	ProducedMana  []Mana `json:"producedMana,omitempty"`
}

// PayManaCostInput: payManaCost.
type PayManaCostInput struct {
	PromptBase
	CardID             string          `json:"cardId"`
	CardName           string          `json:"cardName"`
	ManaCost           string          `json:"manaCost"`
	CanConfirmFromPool bool            `json:"canConfirmFromPool"`
	Actions            []PaymentAction `json:"actions"`
}

func (PayManaCostInput) isPromptInput()     {}
func (PayManaCostInput) PromptType() string { return "payManaCost" }

// MulliganInput: mulligan.
type MulliganInput struct {
	HandCardIDs   []string `json:"handCardIds"`
	MulliganCount int      `json:"mulliganCount"`
}

func (MulliganInput) isPromptInput()     {}
func (MulliganInput) PromptType() string { return "mulligan" }

// MulliganPutBackInput: mulliganPutBack.
type MulliganPutBackInput struct {
	HandCardIDs []string  `json:"handCardIds"`
	Cards       []CardDto `json:"cards"`
	Count       int       `json:"count"`
}

func (MulliganPutBackInput) isPromptInput()     {}
func (MulliganPutBackInput) PromptType() string { return "mulliganPutBack" }

// AttackerOptionDto is one attackable creature with its legal targets.
type AttackerOptionDto struct {
	AttackerID     string   `json:"attackerId"`
	ValidTargetIDs []string `json:"validTargetIds"`
	MustAttack     bool     `json:"mustAttack"`
}

// AttackTargetDto is one legal attack target.
type AttackTargetDto struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Kind is "player" | "planeswalker" | "battle".
	Kind string `json:"kind"`
}

// ChooseAttackersInput: chooseAttackers.
type ChooseAttackersInput struct {
	Attackers     []AttackerOptionDto `json:"attackers"`
	AttackTargets []AttackTargetDto   `json:"attackTargets"`
}

func (ChooseAttackersInput) isPromptInput()     {}
func (ChooseAttackersInput) PromptType() string { return "chooseAttackers" }

// BlockableAttackerDto is one attacking creature blockers may be assigned to.
type BlockableAttackerDto struct {
	AttackerID      string   `json:"attackerId"`
	ValidBlockerIDs []string `json:"validBlockerIds"`
	MinBlockers     int      `json:"minBlockers"`
	MaxBlockers     *int     `json:"maxBlockers,omitempty"`
	MustBeBlocked   bool     `json:"mustBeBlocked"`
}

// ChooseBlockersInput: chooseBlockers. Error carries a previous assignment's
// rejection reason when the engine re-asks.
type ChooseBlockersInput struct {
	Attackers           []BlockableAttackerDto `json:"attackers"`
	AvailableBlockerIDs []string               `json:"availableBlockerIds"`
	Error               string                 `json:"error,omitempty"`
}

func (ChooseBlockersInput) isPromptInput()     {}
func (ChooseBlockersInput) PromptType() string { return "chooseBlockers" }

// ChooseDamageAssignmentOrderInput: chooseDamageAssignmentOrder.
type ChooseDamageAssignmentOrderInput struct {
	AttackerID   string    `json:"attackerId"`
	BlockerIDs   []string  `json:"blockerIds"`
	BlockerCards []CardDto `json:"blockerCards"`
}

func (ChooseDamageAssignmentOrderInput) isPromptInput() {}
func (ChooseDamageAssignmentOrderInput) PromptType() string {
	return "chooseDamageAssignmentOrder"
}

// ChooseCombatDamageAssignmentInput: chooseCombatDamageAssignment.
type ChooseCombatDamageAssignmentInput struct {
	AttackerID            string   `json:"attackerId"`
	BlockerIDs            []string `json:"blockerIds"`
	DefenderID            string   `json:"defenderId,omitempty"`
	TotalDamage           int      `json:"totalDamage"`
	AttackerHasDeathtouch bool     `json:"attackerHasDeathtouch"`
}

func (ChooseCombatDamageAssignmentInput) isPromptInput() {}
func (ChooseCombatDamageAssignmentInput) PromptType() string {
	return "chooseCombatDamageAssignment"
}

// ChooseBoardTargetsInput: chooseBoardTargets.
type ChooseBoardTargetsInput struct {
	PromptBase
	Candidates    []TargetRef     `json:"candidates"`
	Hostile       bool            `json:"hostile"`
	Intent        TargetingIntent `json:"intent"`
	MinTargets    int             `json:"minTargets"`
	MaxTargets    int             `json:"maxTargets"`
	ChosenTargets []TargetRef     `json:"chosenTargets"`
	Cancellable   bool            `json:"cancellable"`
}

func (ChooseBoardTargetsInput) isPromptInput()     {}
func (ChooseBoardTargetsInput) PromptType() string { return "chooseBoardTargets" }

// GameOverInput: gameOver, the terminal prompt. It has no response.
type GameOverInput struct{}

func (GameOverInput) isPromptInput()     {}
func (GameOverInput) PromptType() string { return "gameOver" }

// --- outputs, in the Appendix A.4 order ---

// NumberDecision: chooseNumber's answer. Nil ChosenNumber is the null that is
// legal only where cancel is legal.
type NumberDecision struct {
	ChosenNumber *int `json:"chosenNumber"`
}

func (NumberDecision) isPromptOutput()    {}
func (NumberDecision) OutputType() string { return "numberDecision" }

// ChooseCardsDecision: chooseCards's answer.
type ChooseCardsDecision struct {
	ChosenCardIDs []string `json:"chosenCardIds"`
}

func (ChooseCardsDecision) isPromptOutput()    {}
func (ChooseCardsDecision) OutputType() string { return "chooseCardsDecision" }

// ColorDecision: chooseColor's answer; the counts sum to the prompt's amount.
type ColorDecision struct {
	ChosenColors map[string]int `json:"chosenColors"`
}

func (ColorDecision) isPromptOutput()    {}
func (ColorDecision) OutputType() string { return "colorDecision" }

// BooleanDecision: chooseBoolean's answer.
type BooleanDecision struct {
	Value bool `json:"value"`
}

func (BooleanDecision) isPromptOutput()    {}
func (BooleanDecision) OutputType() string { return "decision" }

// SelectionDecision: chooseFromSelection's answer.
type SelectionDecision struct {
	ChosenIndices []int `json:"chosenIndices"`
}

func (SelectionDecision) isPromptOutput()    {}
func (SelectionDecision) OutputType() string { return "selectionDecision" }

// RevealCardsAcknowledged: revealCards's answer.
type RevealCardsAcknowledged struct{}

func (RevealCardsAcknowledged) isPromptOutput()    {}
func (RevealCardsAcknowledged) OutputType() string { return "revealCardsAcknowledged" }

// ScryDecision: scry's answer; ZoneCardIDs is parallel to the prompt's zones,
// in final order, every card exactly once.
type ScryDecision struct {
	ZoneCardIDs [][]string `json:"zoneCardIds"`
}

func (ScryDecision) isPromptOutput()    {}
func (ScryDecision) OutputType() string { return "scryDecision" }

// ReorderDecision: reorder's answer; the first id ends on top (and, for
// triggers, resolves first).
type ReorderDecision struct {
	OrderedIDs []string `json:"orderedIds"`
}

func (ReorderDecision) isPromptOutput()    {}
func (ReorderDecision) OutputType() string { return "reorderDecision" }

// DiceRolledAcknowledged: diceRolled's answer.
type DiceRolledAcknowledged struct{}

func (DiceRolledAcknowledged) isPromptOutput()    {}
func (DiceRolledAcknowledged) OutputType() string { return "diceRolledAcknowledged" }

// PassUntil is pass's optional until clause.
type PassUntil struct {
	PlayerID string   `json:"playerId"`
	Phase    StepKind `json:"phase"`
}

// PassOutput: chooseAction's pass answer.
type PassOutput struct {
	Until        *PassUntil `json:"until,omitempty"`
	ExhaustStack bool       `json:"exhaustStack"`
}

func (PassOutput) isPromptOutput()    {}
func (PassOutput) OutputType() string { return "pass" }

// RestoreSnapshotOutput: chooseAction's restoreSnapshot answer. The published
// spec never says which checkpoint ids exist.
type RestoreSnapshotOutput struct {
	CheckpointID int64 `json:"checkpointId"`
}

func (RestoreSnapshotOutput) isPromptOutput()    {}
func (RestoreSnapshotOutput) OutputType() string { return "restoreSnapshot" }

// ActOutput: chooseAction's act answer and payManaCost's per-step answer (the
// engine re-sends an updated prompt after each act).
type ActOutput struct {
	ActionID string `json:"actionId"`
}

func (ActOutput) isPromptOutput()    {}
func (ActOutput) OutputType() string { return "act" }

// PayOutput: payManaCost's auto-pay answer.
type PayOutput struct {
	Auto bool `json:"auto"`
}

func (PayOutput) isPromptOutput()    {}
func (PayOutput) OutputType() string { return "pay" }

// CancelOutput: payManaCost's cancel answer.
type CancelOutput struct{}

func (CancelOutput) isPromptOutput()    {}
func (CancelOutput) OutputType() string { return "cancel" }

// MulliganDecision: mulligan's answer.
type MulliganDecision struct {
	Keep bool `json:"keep"`
}

func (MulliganDecision) isPromptOutput()    {}
func (MulliganDecision) OutputType() string { return "mulliganDecision" }

// MulliganPutBackDecision: mulliganPutBack's answer; exactly Count cards.
type MulliganPutBackDecision struct {
	CardIDs []string `json:"cardIds"`
}

func (MulliganPutBackDecision) isPromptOutput()    {}
func (MulliganPutBackDecision) OutputType() string { return "mulliganPutBackDecision" }

// AttackerAssignment is one attacker→target pairing.
type AttackerAssignment struct {
	AttackerID string `json:"attackerId"`
	TargetID   string `json:"targetId"`
}

// DeclareAttackersDecision: chooseAttackers's answer.
type DeclareAttackersDecision struct {
	Assignments []AttackerAssignment `json:"assignments"`
}

func (DeclareAttackersDecision) isPromptOutput()    {}
func (DeclareAttackersDecision) OutputType() string { return "declareAttackers" }

// BlockerAssignment is one blocker→attacker pairing.
type BlockerAssignment struct {
	BlockerID  string `json:"blockerId"`
	AttackerID string `json:"attackerId"`
}

// DeclareBlockersDecision: chooseBlockers's answer.
type DeclareBlockersDecision struct {
	Assignments []BlockerAssignment `json:"assignments"`
}

func (DeclareBlockersDecision) isPromptOutput()    {}
func (DeclareBlockersDecision) OutputType() string { return "declareBlockers" }

// DamageAssignmentOrderDecision: chooseDamageAssignmentOrder's answer.
type DamageAssignmentOrderDecision struct {
	OrderedBlockerIDs []string `json:"orderedBlockerIds"`
}

func (DamageAssignmentOrderDecision) isPromptOutput() {}
func (DamageAssignmentOrderDecision) OutputType() string {
	return "damageAssignmentOrderDecision"
}

// DamageAssignment is one assignee→damage pairing summing to TotalDamage.
type DamageAssignment struct {
	AssigneeID string `json:"assigneeId"`
	Damage     int    `json:"damage"`
}

// DamageAssignmentDecision: chooseCombatDamageAssignment's answer.
type DamageAssignmentDecision struct {
	Assignments []DamageAssignment `json:"assignments"`
}

func (DamageAssignmentDecision) isPromptOutput()    {}
func (DamageAssignmentDecision) OutputType() string { return "combatDamageAssignmentDecision" }

// BoardTargetsDecision: chooseBoardTargets's answer.
type BoardTargetsDecision struct {
	Chosen []TargetRef `json:"chosen"`
}

func (BoardTargetsDecision) isPromptOutput()    {}
func (BoardTargetsDecision) OutputType() string { return "boardTargets" }

// --- union codecs ---

// promptInputCases maps every input.type to a fresh empty value. The
// discriminator dispatch and the lenient unknown-path walker both read it, so
// a prompt type registered here is covered by both.
var promptInputCases = map[string]func() PromptInputData{
	"chooseNumber":                 func() PromptInputData { return &ChooseNumberInput{} },
	"chooseCards":                  func() PromptInputData { return &ChooseCardsInput{} },
	"chooseColor":                  func() PromptInputData { return &ChooseColorInput{} },
	"chooseBoolean":                func() PromptInputData { return &ChooseBooleanInput{} },
	"chooseFromSelection":          func() PromptInputData { return &ChooseFromSelectionInput{} },
	"revealCards":                  func() PromptInputData { return &RevealCardsInput{} },
	"scry":                         func() PromptInputData { return &ScryInput{} },
	"reorder":                      func() PromptInputData { return &ReorderInput{} },
	"diceRolled":                   func() PromptInputData { return &DiceRolledInput{} },
	"chooseAction":                 func() PromptInputData { return &ChooseActionInput{} },
	"payManaCost":                  func() PromptInputData { return &PayManaCostInput{} },
	"mulligan":                     func() PromptInputData { return &MulliganInput{} },
	"mulliganPutBack":              func() PromptInputData { return &MulliganPutBackInput{} },
	"chooseAttackers":              func() PromptInputData { return &ChooseAttackersInput{} },
	"chooseBlockers":               func() PromptInputData { return &ChooseBlockersInput{} },
	"chooseDamageAssignmentOrder":  func() PromptInputData { return &ChooseDamageAssignmentOrderInput{} },
	"chooseCombatDamageAssignment": func() PromptInputData { return &ChooseCombatDamageAssignmentInput{} },
	"chooseBoardTargets":           func() PromptInputData { return &ChooseBoardTargetsInput{} },
	"gameOver":                     func() PromptInputData { return &GameOverInput{} },
}

// promptOutputCases maps every output.type to a fresh empty value.
var promptOutputCases = map[string]func() PromptOutputValue{
	"numberDecision":                 func() PromptOutputValue { return &NumberDecision{} },
	"chooseCardsDecision":            func() PromptOutputValue { return &ChooseCardsDecision{} },
	"colorDecision":                  func() PromptOutputValue { return &ColorDecision{} },
	"decision":                       func() PromptOutputValue { return &BooleanDecision{} },
	"selectionDecision":              func() PromptOutputValue { return &SelectionDecision{} },
	"revealCardsAcknowledged":        func() PromptOutputValue { return &RevealCardsAcknowledged{} },
	"scryDecision":                   func() PromptOutputValue { return &ScryDecision{} },
	"reorderDecision":                func() PromptOutputValue { return &ReorderDecision{} },
	"diceRolledAcknowledged":         func() PromptOutputValue { return &DiceRolledAcknowledged{} },
	"pass":                           func() PromptOutputValue { return &PassOutput{} },
	"restoreSnapshot":                func() PromptOutputValue { return &RestoreSnapshotOutput{} },
	"act":                            func() PromptOutputValue { return &ActOutput{} },
	"pay":                            func() PromptOutputValue { return &PayOutput{} },
	"cancel":                         func() PromptOutputValue { return &CancelOutput{} },
	"mulliganDecision":               func() PromptOutputValue { return &MulliganDecision{} },
	"mulliganPutBackDecision":        func() PromptOutputValue { return &MulliganPutBackDecision{} },
	"declareAttackers":               func() PromptOutputValue { return &DeclareAttackersDecision{} },
	"declareBlockers":                func() PromptOutputValue { return &DeclareBlockersDecision{} },
	"damageAssignmentOrderDecision":  func() PromptOutputValue { return &DamageAssignmentOrderDecision{} },
	"combatDamageAssignmentDecision": func() PromptOutputValue { return &DamageAssignmentDecision{} },
	"boardTargets":                   func() PromptOutputValue { return &BoardTargetsDecision{} },
}

// typedJSON re-marshals a concrete struct's JSON with the discriminator field
// set to want, rejecting a conflicting discriminator that was already present.
func typedJSON(raw []byte, field, want string) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if old, ok := m[field]; ok {
		var s string
		if err := json.Unmarshal(old, &s); err != nil {
			return nil, fmt.Errorf("manabrew: %s must be a string: %w", field, err)
		}
		if s != "" && s != want {
			return nil, unknownDiscriminator(field, s)
		}
	}
	m[field] = []byte(`"` + want + `"`)
	return json.Marshal(m)
}

func (p *PromptInput) UnmarshalJSON(b []byte) error {
	var h struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &h); err != nil {
		return err
	}
	newCase, ok := promptInputCases[h.Type]
	if !ok {
		return unknownDiscriminator("input.type", h.Type)
	}
	v := newCase()
	if err := json.Unmarshal(b, v); err != nil {
		return err
	}
	*p = PromptInput{Value: derefPromptInput(v)}
	return nil
}

// derefPromptInput turns the pointer a promptInputCases entry allocates (so
// json.Unmarshal has an addressable target) back into the value every
// consumer type-switches on (mb.ChooseNumberInput, not *mb.ChooseNumberInput).
// Every concrete type's isPromptInput/PromptType methods have value
// receivers, so the dereferenced value still satisfies PromptInputData.
func derefPromptInput(v PromptInputData) PromptInputData {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return v
	}
	if elem, ok := rv.Elem().Interface().(PromptInputData); ok {
		return elem
	}
	return v
}

func (p PromptInput) MarshalJSON() ([]byte, error) {
	if p.Value == nil {
		return nil, fmt.Errorf("manabrew: nil PromptInput")
	}
	return json.Marshal(p.Value)
}

func (p *PromptOutputData) UnmarshalJSON(b []byte) error {
	var h struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &h); err != nil {
		return err
	}
	newCase, ok := promptOutputCases[h.Type]
	if !ok {
		return unknownDiscriminator("output.type", h.Type)
	}
	v := newCase()
	if err := json.Unmarshal(b, v); err != nil {
		return err
	}
	*p = PromptOutputData{Value: derefPromptOutput(v)}
	return nil
}

// derefPromptOutput is derefPromptInput's counterpart for PromptOutputValue;
// see that function's doc.
func derefPromptOutput(v PromptOutputValue) PromptOutputValue {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return v
	}
	if elem, ok := rv.Elem().Interface().(PromptOutputValue); ok {
		return elem
	}
	return v
}

func (p PromptOutputData) MarshalJSON() ([]byte, error) {
	if p.Value == nil {
		return nil, fmt.Errorf("manabrew: nil PromptOutputData")
	}
	return json.Marshal(p.Value)
}

// Each concrete union member carries its own MarshalJSON (an interface type
// cannot), so the discriminator is always emitted on encode and encoding/json
// reaches it through the wrapper's dynamic value.

func (x ChooseNumberInput) MarshalJSON() ([]byte, error) {
	type plain ChooseNumberInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x ChooseCardsInput) MarshalJSON() ([]byte, error) {
	type plain ChooseCardsInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x ChooseColorInput) MarshalJSON() ([]byte, error) {
	type plain ChooseColorInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x ChooseBooleanInput) MarshalJSON() ([]byte, error) {
	type plain ChooseBooleanInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x ChooseFromSelectionInput) MarshalJSON() ([]byte, error) {
	type plain ChooseFromSelectionInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x RevealCardsInput) MarshalJSON() ([]byte, error) {
	type plain RevealCardsInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x ScryInput) MarshalJSON() ([]byte, error) {
	type plain ScryInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x ReorderInput) MarshalJSON() ([]byte, error) {
	type plain ReorderInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x DiceRolledInput) MarshalJSON() ([]byte, error) {
	type plain DiceRolledInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x ChooseActionInput) MarshalJSON() ([]byte, error) {
	type plain ChooseActionInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x PayManaCostInput) MarshalJSON() ([]byte, error) {
	type plain PayManaCostInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x MulliganInput) MarshalJSON() ([]byte, error) {
	type plain MulliganInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x MulliganPutBackInput) MarshalJSON() ([]byte, error) {
	type plain MulliganPutBackInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x ChooseAttackersInput) MarshalJSON() ([]byte, error) {
	type plain ChooseAttackersInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x ChooseBlockersInput) MarshalJSON() ([]byte, error) {
	type plain ChooseBlockersInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x ChooseDamageAssignmentOrderInput) MarshalJSON() ([]byte, error) {
	type plain ChooseDamageAssignmentOrderInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x ChooseCombatDamageAssignmentInput) MarshalJSON() ([]byte, error) {
	type plain ChooseCombatDamageAssignmentInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x ChooseBoardTargetsInput) MarshalJSON() ([]byte, error) {
	type plain ChooseBoardTargetsInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x GameOverInput) MarshalJSON() ([]byte, error) {
	type plain GameOverInput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.PromptType())
}

func (x NumberDecision) MarshalJSON() ([]byte, error) {
	type plain NumberDecision
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x ChooseCardsDecision) MarshalJSON() ([]byte, error) {
	type plain ChooseCardsDecision
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x ColorDecision) MarshalJSON() ([]byte, error) {
	type plain ColorDecision
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x BooleanDecision) MarshalJSON() ([]byte, error) {
	type plain BooleanDecision
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x SelectionDecision) MarshalJSON() ([]byte, error) {
	type plain SelectionDecision
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x RevealCardsAcknowledged) MarshalJSON() ([]byte, error) {
	type plain RevealCardsAcknowledged
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x ScryDecision) MarshalJSON() ([]byte, error) {
	type plain ScryDecision
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x ReorderDecision) MarshalJSON() ([]byte, error) {
	type plain ReorderDecision
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x DamageAssignmentOrderDecision) MarshalJSON() ([]byte, error) {
	type plain DamageAssignmentOrderDecision
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x DiceRolledAcknowledged) MarshalJSON() ([]byte, error) {
	type plain DiceRolledAcknowledged
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x PassOutput) MarshalJSON() ([]byte, error) {
	type plain PassOutput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x RestoreSnapshotOutput) MarshalJSON() ([]byte, error) {
	type plain RestoreSnapshotOutput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x ActOutput) MarshalJSON() ([]byte, error) {
	type plain ActOutput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x PayOutput) MarshalJSON() ([]byte, error) {
	type plain PayOutput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x CancelOutput) MarshalJSON() ([]byte, error) {
	type plain CancelOutput
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x MulliganDecision) MarshalJSON() ([]byte, error) {
	type plain MulliganDecision
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x MulliganPutBackDecision) MarshalJSON() ([]byte, error) {
	type plain MulliganPutBackDecision
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x DeclareAttackersDecision) MarshalJSON() ([]byte, error) {
	type plain DeclareAttackersDecision
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x DeclareBlockersDecision) MarshalJSON() ([]byte, error) {
	type plain DeclareBlockersDecision
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x DamageAssignmentDecision) MarshalJSON() ([]byte, error) {
	type plain DamageAssignmentDecision
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

func (x BoardTargetsDecision) MarshalJSON() ([]byte, error) {
	type plain BoardTargetsDecision
	raw, err := json.Marshal(plain(x))
	if err != nil {
		return nil, err
	}
	return typedJSON(raw, "type", x.OutputType())
}

// PromptOutput is the response's action: {type:<promptType>, output:{type:
// <outputType>,…}}. The two type vocabularies are paired by the protocol but
// the codec only requires both to be present and known.
type PromptOutput struct {
	Type   string           `json:"type"`
	Output PromptOutputData `json:"output"`
}

func (p *PromptOutput) UnmarshalJSON(b []byte) error {
	var h struct {
		Type   string          `json:"type"`
		Output json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(b, &h); err != nil {
		return err
	}
	if h.Type == "" {
		return fmt.Errorf("manabrew: prompt output is missing type")
	}
	if len(h.Output) == 0 {
		return fmt.Errorf("manabrew: prompt output is missing output")
	}
	var data PromptOutputData
	if err := json.Unmarshal(h.Output, &data); err != nil {
		return err
	}
	*p = PromptOutput{Type: h.Type, Output: data}
	return nil
}

func (p PromptOutput) MarshalJSON() ([]byte, error) {
	if p.Output.Value == nil {
		return nil, fmt.Errorf("manabrew: nil prompt output data")
	}
	raw, err := json.Marshal(p.Output)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Type   string          `json:"type"`
		Output json.RawMessage `json:"output"`
	}{Type: p.Type, Output: raw})
}
