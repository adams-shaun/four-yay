package v1agent

import (
	"encoding/json"
	"fmt"
)

// KRef is mtg-kernel's CardStableRefV1: an object incarnation. ArenaID is
// stable for the physical card across zones; ZCC tells incarnations apart.
type KRef struct {
	ArenaID    uint32 `json:"arena_id"`
	CardDBID   uint16 `json:"card_db_id"`
	Owner      string `json:"owner"`
	Controller string `json:"controller"`
	Zone       string `json:"zone"` // "Battlefield", "Hand", ...
	ZCC        uint32 `json:"zone_change_count"`
}

// KKeywords is KeywordFlagsV2.
type KKeywords struct {
	Flying          bool `json:"flying"`
	Reach           bool `json:"reach"`
	Haste           bool `json:"haste"`
	Vigilance       bool `json:"vigilance"`
	Trample         bool `json:"trample"`
	FirstStrike     bool `json:"first_strike"`
	DoubleStrike    bool `json:"double_strike"`
	Deathtouch      bool `json:"deathtouch"`
	Menace          bool `json:"menace"`
	Defender        bool `json:"defender"`
	Lifelink        bool `json:"lifelink"`
	Hexproof        bool `json:"hexproof"`
	Indestructible  bool `json:"indestructible"`
	ProtMonocolored bool `json:"protection_from_monocolored"`
	WardGeneric     int  `json:"ward_generic"`
	MinimumBlockers int  `json:"minimum_blockers"`
	LandwalkMask    int  `json:"landwalk_mask"`
}

// KTypes is CardTypeFlagsV2.
type KTypes struct {
	Land        bool `json:"land"`
	Creature    bool `json:"creature"`
	Instant     bool `json:"instant"`
	Sorcery     bool `json:"sorcery"`
	Artifact    bool `json:"artifact"`
	Enchantment bool `json:"enchantment"`
}

// KCharacteristics is CardCharacteristicsV2.
type KCharacteristics struct {
	Types      KTypes    `json:"type_flags"`
	Power      *int      `json:"effective_power"`
	Toughness  *int      `json:"effective_toughness"`
	ColorMask  uint8     `json:"effective_color_mask"`
	SubtypeIDs []int     `json:"effective_subtype_ids"`
	Keywords   KKeywords `json:"effective_keywords"`
}

// KCounters is CountersV1.
type KCounters struct {
	P1P1 int `json:"plus1_plus1"`
	M1M1 int `json:"minus1_minus1"`
	M0M1 int `json:"minus0_minus1"`
	Stun int `json:"stun"`
	Lore int `json:"lore"`
}

// KCard is CardPublicV2 (a public object: battlefield, graveyard, exile).
type KCard struct {
	Stable          KRef             `json:"stable"`
	Name            string           `json:"card_name"`
	Tapped          bool             `json:"tapped"`
	SummoningSick   bool             `json:"summoning_sick"`
	Damage          int              `json:"damage"`
	Counters        KCounters        `json:"counters"`
	Attachments     []uint32         `json:"attachments"`
	IsToken         bool             `json:"is_token"`
	EnteredTurn     *int             `json:"entered_battlefield_turn"`
	SkipNextUntap   bool             `json:"skip_next_untap"`
	Characteristics KCharacteristics `json:"characteristics"`
}

// Power is the effective power (0 for a non-creature).
func (c *KCard) Power() int {
	if c.Characteristics.Power == nil {
		return 0
	}
	return *c.Characteristics.Power
}

// Toughness is the effective toughness (0 for a non-creature).
func (c *KCard) Toughness() int {
	if c.Characteristics.Toughness == nil {
		return 0
	}
	return *c.Characteristics.Toughness
}

// Remaining is toughness minus marked damage.
func (c *KCard) Remaining() int { return c.Toughness() - c.Damage }

// IsCreature reports the creature type flag.
func (c *KCard) IsCreature() bool { return c.Characteristics.Types.Creature }

// IsLand reports the land type flag.
func (c *KCard) IsLand() bool { return c.Characteristics.Types.Land }

// KTarget is TargetRefV1.
type KTarget struct {
	Kind   string `json:"target_kind"` // "player" | "object"
	Player string `json:"player"`
	Object *KRef  `json:"object"`
}

// KStackItem is StackItemPublicV2.
type KStackItem struct {
	Index      int       `json:"stack_index"`
	Source     KRef      `json:"source"`
	Controller string    `json:"controller"`
	Targets    []KTarget `json:"targets"`
	Kind       string    `json:"stack_item_kind"` // spell | activated_ability | triggered_ability | madness_offer
	IsCopy     bool      `json:"is_copy"`
	Kicked     bool      `json:"kicked"`
	XValue     int       `json:"x_value"`
}

// KCombat is CombatStatePublicV2. attacker_to_ordered_blockers is a list of
// [attacker, [blockers...]] pairs.
type KCombat struct {
	AttackersDeclared bool              `json:"attackers_declared"`
	BlockersDeclared  bool              `json:"blockers_declared"`
	Attackers         []KRef            `json:"ordered_attackers"`
	BlockersRaw       []json.RawMessage `json:"attacker_to_ordered_blockers"`
}

// Blocks decodes attacker -> blockers.
func (c *KCombat) Blocks() map[uint32][]KRef {
	out := map[uint32][]KRef{}
	for _, raw := range c.BlockersRaw {
		var pair []json.RawMessage
		if json.Unmarshal(raw, &pair) != nil || len(pair) != 2 {
			continue
		}
		var a KRef
		var bs []KRef
		if json.Unmarshal(pair[0], &a) != nil || json.Unmarshal(pair[1], &bs) != nil {
			continue
		}
		out[a.ArenaID] = bs
	}
	return out
}

// KPlayerStatus is PlayerStatusV1 (subset).
type KPlayerStatus struct {
	LandsPlayed int `json:"lands_played_this_turn"`
	SpellsCast  int `json:"spells_cast_this_turn"`
	DrawsTurn   int `json:"draws_this_turn"`
}

// KCombatSelection is PrivateCombatSelectionV5: where an attacker/blocker
// scan stands.
type KCombatSelection struct {
	Attacker  *KRef  `json:"attacker"`
	Index     int    `json:"candidate_index"`
	Count     int    `json:"candidate_count"`
	Selected  []KRef `json:"selected"`
	Current   KRef   `json:"current_candidate"`
	Remaining []KRef `json:"remaining_after_current"`
}

// KProjection is PublicObservationProjectionV5 (subset).
type KProjection struct {
	Turn           int              `json:"turn"`
	Phase          string           `json:"phase"` // main1, declare_attackers, ...
	ActivePlayer   string           `json:"active_player"`
	PriorityPlayer string           `json:"priority_player"`
	Initiative     *string          `json:"initiative"`
	Life           [2]int           `json:"life_totals"`
	ManaPools      [2][6]int        `json:"mana_pools"`
	HandCounts     [2]int           `json:"hand_counts"`
	LibraryCounts  [2]int           `json:"library_counts"`
	Status         [2]KPlayerStatus `json:"player_status"`
	Battlefield    [2][]KCard       `json:"battlefield"`
	Graveyards     [2][]KCard       `json:"graveyards"`
	Exile          []KCard          `json:"exile"`
	Stack          []KStackItem     `json:"stack"`
	Combat         KCombat          `json:"combat"`
	EngineContext  struct {
		PendingEffect *struct {
			Source *KRef `json:"source"`
			Choice *struct {
				Kind     string    `json:"choice_kind"`
				Purpose  string    `json:"purpose"`
				Min      int       `json:"min_targets"`
				Max      int       `json:"max_targets"`
				Ordered  bool      `json:"ordered"`
				Selected []KTarget `json:"selected_targets"`
			} `json:"choice"`
		} `json:"pending_effect"`
	} `json:"engine_context"`
	SurfaceContext struct {
		Stage     string            `json:"current_stage"`
		Selection *KCombatSelection `json:"private_combat_selection"`
	} `json:"policy_surface_context"`
}

// KHandCard is CardPrivateV1.
type KHandCard struct {
	Stable KRef   `json:"stable"`
	Name   string `json:"card_name"`
}

// KObservation is ObservationV5 (subset).
type KObservation struct {
	ActingPlayer string      `json:"acting_player"`
	Projection   KProjection `json:"projection"`
	OwnHand      []KHandCard `json:"own_hand"`
}

// KAction is LegalActionV5's semantic (ActionSemanticV1), subset.
type KAction struct {
	Kind           string   `json:"action_kind"`
	Source         *KRef    `json:"source"`
	Target         *KTarget `json:"target"`
	Attacker       *KRef    `json:"attacker"`
	Blocker        *KRef    `json:"blocker"`
	Card           *KRef    `json:"card"`
	Candidate      *KRef    `json:"candidate"`
	Cards          []KRef   `json:"cards"`
	PendingSources []KRef   `json:"pending_sources"`
	CostTarget     *KRef    `json:"cost_target"`
}

type kLegalAction struct {
	SelectedIndex int     `json:"selected_index"`
	Semantic      KAction `json:"semantic"`
}

// KernelView is the decoded x_kernel_v5 extension.
type KernelView struct {
	Obs KObservation
	// Actions[i] is candidate i's kernel action (same order).
	Actions []KAction
}

func seatIndex(seat string) int {
	if seat == "p1" {
		return 1
	}
	return 0
}

func decodeKernel(raw json.RawMessage, candidates int) (*KernelView, error) {
	var ext struct {
		Observation  string `json:"observation_json"`
		LegalActions string `json:"legal_actions_json"`
	}
	if err := json.Unmarshal(raw, &ext); err != nil {
		return nil, fmt.Errorf("x_kernel_v5: %w", err)
	}
	var kv KernelView
	if err := json.Unmarshal([]byte(ext.Observation), &kv.Obs); err != nil {
		return nil, fmt.Errorf("x_kernel_v5.observation_json: %w", err)
	}
	var actions []kLegalAction
	if err := json.Unmarshal([]byte(ext.LegalActions), &actions); err != nil {
		return nil, fmt.Errorf("x_kernel_v5.legal_actions_json: %w", err)
	}
	if len(actions) != candidates {
		return nil, fmt.Errorf("x_kernel_v5: %d legal actions for %d candidates", len(actions), candidates)
	}
	for i, a := range actions {
		if a.SelectedIndex != i {
			return nil, fmt.Errorf("x_kernel_v5: legal action %d has selected_index %d", i, a.SelectedIndex)
		}
		kv.Actions = append(kv.Actions, a.Semantic)
	}
	return &kv, nil
}
