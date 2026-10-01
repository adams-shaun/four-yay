package decision

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/adams-shaun/gorge/state"
)

const maxPaymentQuantity = uint32(^uint32(0) >> 1)

// PaymentPlanV1 is the only payment witness version this build understands,
// and the newest planner is always V1 (spec amendment 2026-09-26, item 1).
// The codec changes only additively -- the last-resort consequence trailer is
// such a change, present only when a step carries a consequence, so every
// earlier plan identity is byte-identical -- and each change is pinned by the
// identity golden (TestPaymentPlanIdentityIsIndependentOfPresentation,
// TestPaymentPlanConsequenceIdentityPinned) and rules' DecisionMade golden.
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

// ManaAmountLen is the exact number of quantities a wire mana vector carries.
const ManaAmountLen = 6

// UnmarshalJSON decodes a mana vector from exactly ManaAmountLen non-negative
// integers bounded by the engine's int32 amount. encoding/json silently
// discards surplus array elements when it decodes into a Go array, so a
// 7-element vector would otherwise decode clean and quietly drop a quantity
// the client sent; a payload outside the declared contract must be rejected.
//
// Each element is decoded through the standard library's own uint32 token
// decoder, so the accepted token set is exactly main's (an unquoted JSON
// number; a quoted string, fraction or negative is rejected). Decoding into
// json.Number here instead would WIDEN the contract, because json.Number
// accepts a quoted numeric string; the per-element uint32 decode does not.
// The engine's int32 bound is applied on top. A JSON null keeps the standard
// library's no-op semantics: a null vector (or a null element) leaves the
// destination unchanged, matching main's array decode.
func (m *ManaAmount) UnmarshalJSON(data []byte) error {
	if string(bytes.TrimSpace(data)) == "null" {
		return nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("mana vector: %w", err)
	}
	if len(raw) != ManaAmountLen {
		return fmt.Errorf("mana vector has %d quantities, want exactly %d", len(raw), ManaAmountLen)
	}
	// Seed the result from the receiver: a null element must leave that slot
	// unchanged (standard library semantics), not zero it.
	out := *m
	for i, elem := range raw {
		// A JSON null leaves the destination unchanged (standard library
		// semantics); skip it rather than writing a zero into a slot the
		// caller may have pre-populated.
		if string(bytes.TrimSpace(elem)) == "null" {
			continue
		}
		var v uint32
		if err := json.Unmarshal(elem, &v); err != nil {
			return fmt.Errorf("mana quantity %d: %w", i, err)
		}
		if v > maxPaymentQuantity {
			return fmt.Errorf("mana quantity %d overflows engine amount", i)
		}
		out[i] = v
	}
	*m = out
	return nil
}

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
// Consequence is present exactly on a last-resort step (spec §3.2, §4): it
// discloses what the activation costs beyond the tap, and it is part of the
// witness, so validation compares it and the plan identity binds it.
type PaymentActivation struct {
	Source        state.ObjID         `json:"source"`
	SourceZoneSeq uint64              `json:"source_zone_seq"`
	Ability       PaymentAbility      `json:"ability"`
	Produces      ManaAmount          `json:"produces"`
	Consequence   *PaymentConsequence `json:"consequence,omitempty"`
}

// PaymentConsequence is a last-resort step's disclosed consequence. A present
// consequence sets at least one field, so "absent" and "nothing" have exactly
// one spelling (nil). Clients derive "needs confirmation" from Life > 0.
type PaymentConsequence struct {
	// Sacrifice: the source itself is sacrificed as part of the cost.
	Sacrifice bool `json:"sacrifice,omitempty"`
	// Life is life paid as a cost (PayLife<N>).
	Life uint32 `json:"life,omitempty"`
	// Damage is damage the source deals to its controller.
	Damage uint32 `json:"damage,omitempty"`
	// NoUntap: the source doesn't untap during its controller's untap step.
	NoUntap bool `json:"no_untap,omitempty"`
	// ReturnToHand: the source returns to its owner's hand.
	ReturnToHand bool `json:"return_to_hand,omitempty"`
}

// IsZero reports whether c sets no consequence at all.
func (c PaymentConsequence) IsZero() bool { return c == PaymentConsequence{} }

// consequence trailer flag bits (codec note, payment-plan-v1-codec.md).
const (
	paymentConsequenceSacrifice    uint32 = 1 << 0
	paymentConsequenceNoUntap      uint32 = 1 << 1
	paymentConsequenceReturnToHand uint32 = 1 << 2
)

func (c PaymentConsequence) flags() uint32 {
	var f uint32
	if c.Sacrifice {
		f |= paymentConsequenceSacrifice
	}
	if c.NoUntap {
		f |= paymentConsequenceNoUntap
	}
	if c.ReturnToHand {
		f |= paymentConsequenceReturnToHand
	}
	return f
}

func (c PaymentConsequence) validate() error {
	if c.IsZero() {
		return fmt.Errorf("present consequence sets no field")
	}
	if c.Life > maxPaymentQuantity {
		return fmt.Errorf("consequence life overflows engine amount")
	}
	if c.Damage > maxPaymentQuantity {
		return fmt.Errorf("consequence damage overflows engine amount")
	}
	return nil
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
		for i := range p.Activations {
			if c := p.Activations[i].Consequence; c != nil {
				cc := *c
				p.Activations[i].Consequence = &cc
			}
		}
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
	var c Decision
	d.CloneInto(&c)
	return c
}

// CloneInto writes CloneValue's copy of d into dst, reusing the storage dst
// already owns: its Options, WindowReasons and PaymentActions backing arrays
// and its PaymentFallback and ManaPayment structs (and that window's
// AutoFill). Whatever dst held is overwritten, so a caller that refills one
// Decision per call (view.ProjectInto) pays nothing once the buffers have
// grown. The copy is CloneValue's exactly, nil-ness included: Options,
// WindowReasons and AutoFill are nil when d's are empty, PaymentActions is
// always non-nil. dst must not be d or share storage with it.
func (d *Decision) CloneInto(dst *Decision) {
	opts, reasons, pays := dst.Options[:0], dst.WindowReasons[:0], dst.PaymentActions[:0]
	fb, mp := dst.PaymentFallback, dst.ManaPayment
	*dst = *d
	dst.Options, dst.WindowReasons = nil, nil
	if len(d.Options) > 0 {
		dst.Options = append(opts, d.Options...)
	}
	if len(d.WindowReasons) > 0 {
		dst.WindowReasons = append(reasons, d.WindowReasons...)
	}
	if pays == nil {
		pays = []PaymentAction{}
	}
	for i := range d.PaymentActions {
		pays = append(pays, ClonePaymentAction(d.PaymentActions[i]))
	}
	dst.PaymentActions = pays
	if d.PaymentFallback != nil {
		if fb == nil {
			fb = new(PaymentFallback)
		}
		*fb = *d.PaymentFallback
		dst.PaymentFallback = fb
	}
	if w := d.ManaPayment; w != nil {
		if mp == nil {
			mp = new(ManaPaymentWindow)
		}
		fill := mp.AutoFill[:0]
		*mp = *w
		mp.AutoFill = nil
		if len(w.AutoFill) > 0 {
			mp.AutoFill = append(fill, w.AutoFill...)
		}
		dst.ManaPayment = mp
	}
}

// CloneIntent makes an owned copy at an admission or persistence boundary.
func CloneIntent(in Intent) Intent {
	c := in
	c.Choices = append([]int(nil), in.Choices...)
	c.Rest = append([]int(nil), in.Rest...)
	c.Payment = ClonePaymentSelection(in.Payment)
	c.Announce = CloneAnnounceSelection(in.Announce)
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
		if a.Consequence != nil {
			if err := a.Consequence.validate(); err != nil {
				return fmt.Errorf("payment activation %d: %w", i, err)
			}
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
	b = appendMana(b, p.PoolAfter)
	// The consequence trailer (spec §4 amended) is appended only when some
	// step carries a consequence, so every plan without one keeps its exact
	// pre-trailer identity. The base encoding's length is fixed by its
	// activation count, so the trailer's presence is unambiguous.
	has := false
	for _, a := range p.Activations {
		if a.Consequence != nil {
			has = true
			break
		}
	}
	if !has {
		return b
	}
	b = appendString(b, "consequences")
	for _, a := range p.Activations {
		var c PaymentConsequence
		if a.Consequence != nil {
			c = *a.Consequence
		}
		b = appendU32(b, c.flags())
		b = appendU32(b, c.Life)
		b = appendU32(b, c.Damage)
	}
	return b
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
