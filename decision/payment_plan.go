package decision

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"reflect"

	"github.com/adams-shaun/gorge/state"
)

const maxPaymentQuantity = uint32(^uint32(0) >> 1)

// PaymentPlanV1 is the only payment witness version this build understands.
// V1's encoding is frozen: a changed witness format must use a new version.
const PaymentPlanV1 uint32 = 1

// MaxPaymentActivations bounds one V1 witness before it is hashed or searched.
const MaxPaymentActivations = 64

// MaxPaymentPlanSearchNodes is the rules planner's per-cast deterministic
// resource budget. The planner lands in payplan-02; the public bound belongs
// here because it is part of the V1 contract.
const MaxPaymentPlanSearchNodes = 65536

// GenesisZoneSeq identifies an object which has not changed zones since the
// game's genesis. Every later zone incarnation is identified by its MoveZone
// event sequence.
const GenesisZoneSeq uint64 = 0

const (
	PaymentAbilityPrinted   = "printed"
	PaymentAbilityIntrinsic = "intrinsic"
)

// ManaAmount is a fixed-order W/U/B/R/G/C quantity vector. Arrays, rather
// than maps, make both its wire representation and payment identity stable.
// Values are uint32 so a negative JSON number and quantities outside the
// engine's int32 capacity are rejected at decode/validation boundaries.
type ManaAmount [6]uint32

// PaymentCost is the resolved mana requirement. Generic is deliberately
// separate from true colourless (Mana[5]).
type PaymentCost struct {
	Generic uint32     `json:"generic"`
	Mana    ManaAmount `json:"mana"`
}

// PaymentAbility is a stable source ability discriminator. Printed abilities
// use the card face and its printed ability index. Intrinsic abilities use a
// frozen name (for example "basic_land"); they never use a transient offered
// option index. Fields outside the selected discriminator must be zero.
type PaymentAbility struct {
	Kind      string `json:"kind"`
	Face      uint32 `json:"face,omitempty"`
	Index     uint32 `json:"index,omitempty"`
	Intrinsic string `json:"intrinsic,omitempty"`
}

// PaymentActivation is one source activation authorized by a plan.
type PaymentActivation struct {
	Source        state.ObjID    `json:"source"`
	SourceZoneSeq uint64         `json:"source_zone_seq"`
	Ability       PaymentAbility `json:"ability"`
	Produces      ManaAmount     `json:"produces"`
}

// PaymentPlan is a complete V1 execution witness. PoolSpend records which
// pre-existing mana units pay the cast; PoolAfter records the expected pool
// after activations and payment, and is independently revalidated by rules.
type PaymentPlan struct {
	Version     uint32              `json:"version"`
	ID          string              `json:"id"`
	Cost        PaymentCost         `json:"cost"`
	Activations []PaymentActivation `json:"activations"`
	PoolSpend   ManaAmount          `json:"pool_spend"`
	PoolAfter   ManaAmount          `json:"pool_after"`
}

// PlannedCast is the exact cast identity a payment action authorizes.
type PlannedCast struct {
	Object state.ObjID `json:"object"`
	Face   int         `json:"face"`
	Origin string      `json:"origin"`
}

// PaymentAction is an additive priority-decision action. BaseOptionIndex is
// absent when floating mana is required before the cast becomes a legacy
// option, preserving the existing option list and its indices.
type PaymentAction struct {
	ID              string        `json:"id"`
	Cast            PlannedCast   `json:"cast"`
	BaseOptionIndex *int          `json:"base_option_index,omitempty"`
	Label           string        `json:"label"`
	Plans           []PaymentPlan `json:"plans"`
}

// PaymentSelection is the exclusive intent selector for an offered action and
// its complete, byte-for-byte equivalent plan witness.
type PaymentSelection struct {
	ActionID string      `json:"action_id"`
	Plan     PaymentPlan `json:"plan"`
}

// PaymentFallback explains why an accepted plan returned to the ordinary
// manual payment flow. Execution publishes it in a later ticket.
type PaymentFallback struct {
	PlanID string `json:"plan_id"`
	Reason string `json:"reason"`
}

// ClonePaymentPlan copies every mutable part of a V1 witness.
func ClonePaymentPlan(p PaymentPlan) PaymentPlan {
	// Preserve nil versus an explicitly empty list.  The distinction is part
	// of exact offered-witness membership: a planner-produced pool-only plan
	// carries a non-nil empty activation list, and turning it into nil in a
	// host/view clone would make a verbatim selected witness fail DeepEqual
	// against the engine's still-offered plan.
	if p.Activations != nil {
		activations := p.Activations
		p.Activations = make([]PaymentActivation, len(activations))
		copy(p.Activations, activations)
	}
	return p
}

// ClonePaymentAction copies every mutable part of an offered action.
func ClonePaymentAction(a PaymentAction) PaymentAction {
	if a.BaseOptionIndex != nil {
		i := *a.BaseOptionIndex
		a.BaseOptionIndex = &i
	}
	plans := a.Plans
	a.Plans = make([]PaymentPlan, len(plans))
	for i := range plans {
		a.Plans[i] = ClonePaymentPlan(plans[i])
	}
	return a
}

// ClonePaymentSelection copies a submitted witness.
func ClonePaymentSelection(s *PaymentSelection) *PaymentSelection {
	if s == nil {
		return nil
	}
	c := *s
	c.Plan = ClonePaymentPlan(s.Plan)
	return &c
}

// Clone copies the mutable payment-plan extension on a decision. Existing
// option and runtime fields retain their established ownership handling at
// their individual boundaries; callers use this helper wherever the new
// extension crosses one.
func (d *Decision) Clone() *Decision {
	if d == nil {
		return nil
	}
	c := d.CloneValue()
	return &c
}

// CloneValue is Clone returning the copy by value, so a caller that only
// needs a Decision (view's projection copy) does not pay a heap allocation
// for the struct itself.
func (d *Decision) CloneValue() Decision {
	c := *d
	c.Options = append([]Option(nil), d.Options...)
	c.PaymentActions = make([]PaymentAction, len(d.PaymentActions))
	for i := range d.PaymentActions {
		c.PaymentActions[i] = ClonePaymentAction(d.PaymentActions[i])
	}
	if d.PaymentFallback != nil {
		f := *d.PaymentFallback
		c.PaymentFallback = &f
	}
	return c
}

// CloneIntent makes an owned copy at an admission or persistence boundary.
func CloneIntent(in Intent) Intent {
	c := in
	c.Choices = append([]int(nil), in.Choices...)
	c.Rest = append([]int(nil), in.Rest...)
	c.Payment = ClonePaymentSelection(in.Payment)
	return c
}

// PaymentActionID returns the full lower-case SHA-256 identity for a cast
// wrapper. It binds V1, decision sequence, actor and exact cast, but never a
// display label, rank or legacy option index.
func PaymentActionID(version uint32, seq uint64, player state.PlayerID, cast PlannedCast) (string, error) {
	if err := validateVersionAndCast(version, cast); err != nil {
		return "", err
	}
	b := canonicalPaymentPrefix("gorge.payment-action.v1", version, seq, player, cast)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}

// PaymentPlanID returns the full lower-case SHA-256 identity for a complete
// plan witness. Its encoding is map-free and preserves activation order;
// unrelated action/plan list order cannot affect it.
func PaymentPlanID(seq uint64, player state.PlayerID, cast PlannedCast, plan PaymentPlan) (string, error) {
	if err := plan.validateShape(); err != nil {
		return "", err
	}
	b := canonicalPaymentPrefix("gorge.payment-plan.v1", plan.Version, seq, player, cast)
	b = appendPaymentPlanCanonical(b, plan)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}

func validateVersionAndCast(version uint32, cast PlannedCast) error {
	if version != PaymentPlanV1 {
		return fmt.Errorf("unknown payment plan version %d", version)
	}
	if cast.Object == 0 || cast.Face < 0 || cast.Origin == "" {
		return fmt.Errorf("malformed planned cast")
	}
	return nil
}

func (p PaymentPlan) validateShape() error {
	if p.Version != PaymentPlanV1 {
		return fmt.Errorf("unknown payment plan version %d", p.Version)
	}
	if len(p.Activations) > MaxPaymentActivations {
		return fmt.Errorf("payment plan has %d activations, maximum is %d", len(p.Activations), MaxPaymentActivations)
	}
	if err := p.Cost.validate(); err != nil {
		return err
	}
	if err := p.PoolSpend.validate(); err != nil {
		return fmt.Errorf("payment pool spend: %w", err)
	}
	if err := p.PoolAfter.validate(); err != nil {
		return fmt.Errorf("payment pool after: %w", err)
	}
	seen := make(map[state.ObjID]struct{}, len(p.Activations))
	for i, a := range p.Activations {
		if a.Source == 0 {
			return fmt.Errorf("payment activation %d has no source", i)
		}
		if _, ok := seen[a.Source]; ok {
			return fmt.Errorf("payment activation %d reuses source %d", i, a.Source)
		}
		seen[a.Source] = struct{}{}
		if err := a.Ability.validate(); err != nil {
			return fmt.Errorf("payment activation %d: %w", i, err)
		}
		if err := a.Produces.validate(); err != nil {
			return fmt.Errorf("payment activation %d production: %w", i, err)
		}
	}
	return nil
}

func (c PaymentCost) validate() error {
	if c.Generic > maxPaymentQuantity {
		return fmt.Errorf("payment generic quantity overflows engine amount")
	}
	if err := c.Mana.validate(); err != nil {
		return fmt.Errorf("payment cost mana: %w", err)
	}
	return nil
}

func (m ManaAmount) validate() error {
	for i, n := range m {
		if n > maxPaymentQuantity {
			return fmt.Errorf("mana quantity %d overflows engine amount", i)
		}
	}
	return nil
}

func (a PaymentAbility) validate() error {
	switch a.Kind {
	case PaymentAbilityPrinted:
		if a.Intrinsic != "" {
			return fmt.Errorf("printed ability has intrinsic discriminator")
		}
	case PaymentAbilityIntrinsic:
		if a.Intrinsic == "" || a.Face != 0 || a.Index != 0 {
			return fmt.Errorf("malformed intrinsic ability")
		}
	default:
		return fmt.Errorf("unknown payment ability kind %q", a.Kind)
	}
	return nil
}

func canonicalPaymentPrefix(domain string, version uint32, seq uint64, player state.PlayerID, cast PlannedCast) []byte {
	b := make([]byte, 0, 96)
	b = appendString(b, domain)
	b = appendU32(b, version)
	b = appendU64(b, seq)
	b = appendU32(b, uint32(player))
	b = appendU64(b, uint64(cast.Object))
	b = appendU64(b, uint64(cast.Face))
	b = appendString(b, cast.Origin)
	return b
}

func appendPaymentPlanCanonical(b []byte, p PaymentPlan) []byte {
	b = appendU32(b, p.Cost.Generic)
	b = appendMana(b, p.Cost.Mana)
	b = appendU32(b, uint32(len(p.Activations)))
	for _, a := range p.Activations {
		b = appendU64(b, uint64(a.Source))
		b = appendU64(b, a.SourceZoneSeq)
		b = appendString(b, a.Ability.Kind)
		b = appendU32(b, a.Ability.Face)
		b = appendU32(b, a.Ability.Index)
		b = appendString(b, a.Ability.Intrinsic)
		b = appendMana(b, a.Produces)
	}
	b = appendMana(b, p.PoolSpend)
	return appendMana(b, p.PoolAfter)
}

func appendMana(b []byte, m ManaAmount) []byte {
	for _, n := range m {
		b = appendU32(b, n)
	}
	return b
}

func appendU32(b []byte, n uint32) []byte {
	var x [4]byte
	binary.BigEndian.PutUint32(x[:], n)
	return append(b, x[:]...)
}

func appendU64(b []byte, n uint64) []byte {
	var x [8]byte
	binary.BigEndian.PutUint64(x[:], n)
	return append(b, x[:]...)
}

func appendString(b []byte, s string) []byte {
	b = appendU32(b, uint32(len(s)))
	return append(b, s...)
}

func (d *Decision) validatePayment(in Intent) error {
	if d.Kind != KPriority {
		return fmt.Errorf("payment selector is only accepted on a priority answer, not %s", d.Kind)
	}
	if len(in.Choices) != 0 || len(in.Rest) != 0 {
		return fmt.Errorf("payment selector requires empty choices and rest")
	}
	if in.Payment.ActionID == "" {
		return fmt.Errorf("payment selector has no action id")
	}
	if err := in.Payment.Plan.validateShape(); err != nil {
		return err
	}
	for _, a := range d.PaymentActions {
		if a.ID != in.Payment.ActionID {
			continue
		}
		if err := validateVersionAndCast(in.Payment.Plan.Version, a.Cast); err != nil {
			return err
		}
		wantAction, err := PaymentActionID(in.Payment.Plan.Version, d.Seq, d.Player, a.Cast)
		if err != nil || a.ID != wantAction {
			return fmt.Errorf("payment action id is malformed")
		}
		wantPlan, err := PaymentPlanID(d.Seq, d.Player, a.Cast, in.Payment.Plan)
		if err != nil || in.Payment.Plan.ID != wantPlan {
			return fmt.Errorf("payment plan id is malformed")
		}
		for _, offered := range a.Plans {
			if reflect.DeepEqual(offered, in.Payment.Plan) {
				return nil
			}
		}
		return fmt.Errorf("payment plan is not offered for action %q", a.ID)
	}
	return fmt.Errorf("payment action %q is not offered", in.Payment.ActionID)
}
