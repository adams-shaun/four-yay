package levelb

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// face builds a face with the given types and keyword lines.
func face(types []string, keywords ...string) *cards.Face {
	return &cards.Face{Types: types, Keywords: keywords}
}

// cardOf wraps faces into a card.
func cardOf(faces ...*cards.Face) *cards.Card { return &cards.Card{Faces: faces} }

// ab builds an activated ability with the given API and params.
func ab(api string, params map[string]string) *cards.SA {
	return &cards.SA{Kind: "AB", API: api, Params: params}
}

// trig builds a trigger with the given mode and params.
func trig(mode string, params map[string]string) cards.Trigger {
	return cards.Trigger{Mode: mode, Params: params}
}

// stat builds a static with the given mode and params.
func stat(mode string, params map[string]string) cards.Static {
	return cards.Static{Mode: mode, Params: params}
}

func TestRequirementsClassificationTable(t *testing.T) {
	selfETB := map[string]string{
		"Origin": "Any", "Destination": "Battlefield", "ValidCard": "Card.Self",
	}
	selfETBKicked := map[string]string{
		"Origin": "Any", "Destination": "Battlefield", "ValidCard": "Card.Self+kicked",
	}
	selfETBWasCast := map[string]string{
		"Origin": "Any", "Destination": "Battlefield", "ValidCard": "Card.wasCastByYou+Self",
	}
	otherETB := map[string]string{
		"Origin": "Any", "Destination": "Battlefield", "ValidCard": "Creature.YouCtrl",
	}
	dies := map[string]string{
		"Origin": "Battlefield", "Destination": "Graveyard", "ValidCard": "Card.Self",
	}
	attacksSelf := map[string]string{"ValidCard": "Card.Self"}
	attacksCtrl := map[string]string{"ValidCard": "Creature.YouCtrl"}
	attackersDeclared := map[string]string{"AttackingPlayer": "You"}
	combatDamage := map[string]string{"ValidSource": "Card.Self", "CombatDamage": "True"}
	notCombatDamage := map[string]string{"ValidSource": "Card.Self"}
	spellCast := map[string]string{"ValidActivatingPlayer": "You"}
	spellCastNoYou := map[string]string{"ValidCard": "Creature"}
	becomesTarget := map[string]string{"ValidTarget": "Card.Self", "ValidSource": "Spell"}
	lifeGained := map[string]string{"ValidPlayer": "You"}
	drawn := map[string]string{"ValidPlayer": "You"}
	drawnByCard := map[string]string{"ValidCard": "Card.YouCtrl"}
	phaseEnd := map[string]string{"Phase": "End of Turn", "ValidPlayer": "You"}
	phaseEndOpp := map[string]string{"Phase": "Draw", "ValidPlayer": "Opponent"}
	phaseUntap := map[string]string{"Phase": "Untap", "ValidPlayer": "You"}

	tests := []struct {
		name string
		c    *cards.Card
		want []Requirement
	}{
		{
			name: "activate battlefield",
			c:    cardOf(&cards.Face{Types: []string{"Artifact"}, Abilities: []*cards.SA{ab("Pump", nil)}}),
			want: []Requirement{{
				Key: "activate#0.0", Family: "activate", Face: 0, Slot: "0",
				Sub: "activate.battlefield",
			}},
		},
		{
			name: "activate mana",
			c:    cardOf(&cards.Face{Types: []string{"Land"}, Abilities: []*cards.SA{ab("Mana", nil)}}),
			want: []Requirement{{
				Key: "activate#0.0", Family: "activate", Face: 0, Slot: "0",
				Sub: "activate.mana",
			}},
		},
		{
			name: "activate explicit battlefield zone",
			c: cardOf(&cards.Face{Types: []string{"Artifact"}, Abilities: []*cards.SA{
				ab("Pump", map[string]string{"ActivationZone": "Battlefield"}),
			}}),
			want: []Requirement{{
				Key: "activate#0.0", Family: "activate", Face: 0, Slot: "0",
				Sub: "activate.battlefield",
			}},
		},
		{
			name: "activate from hand is a zone gap",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Abilities: []*cards.SA{
				ab("Pump", map[string]string{"ActivationZone": "Hand"}),
			}}),
			want: []Requirement{{
				Key: "activate#0.0", Family: "activate", Face: 0, Slot: "0",
				Sub: "activate.zone:Hand", Gap: "activation zone Hand",
			}},
		},
		{
			name: "activate slot indexes abilities not just ABs",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Abilities: []*cards.SA{
				{Kind: "SP", API: "Pump"},
				ab("Pump", nil),
			}}),
			want: []Requirement{{
				Key: "activate#0.1", Family: "activate", Face: 0, Slot: "1",
				Sub: "activate.battlefield",
			}},
		},
		{
			name: "trigger etb-other",
			c:    cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{trig("ChangesZone", otherETB)}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.etb-other",
			}},
		},
		{
			name: "trigger self-etb is covered by A",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{
				trig("ChangesZone", selfETB),
			}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.etb-other", CoveredByA: true,
			}},
		},
		{
			name: "land self-etb is NOT covered by A",
			c: cardOf(&cards.Face{Types: []string{"Land"}, Triggers: []cards.Trigger{
				trig("ChangesZone", selfETB),
			}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.gap:ChangesZone", Gap: "trigger mode ChangesZone",
			}},
		},
		{
			name: "trigger self-etb with a qualifier is covered by A",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{
				trig("ChangesZone", selfETBKicked),
			}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.etb-other", CoveredByA: true,
			}},
		},
		{
			name: "trigger self-etb token order does not matter",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{
				trig("ChangesZone", selfETBWasCast),
			}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.etb-other", CoveredByA: true,
			}},
		},
		{
			name: "trigger dies",
			c:    cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{trig("ChangesZone", dies)}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.dies",
			}},
		},
		{
			name: "trigger attacks self",
			c:    cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{trig("Attacks", attacksSelf)}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.attacks",
			}},
		},
		{
			name: "trigger attackers-declared by you",
			c: cardOf(&cards.Face{Types: []string{"Enchantment"}, Triggers: []cards.Trigger{
				trig("AttackersDeclared", attackersDeclared),
			}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.attacks",
			}},
		},
		{
			name: "trigger attacks self controlled by you",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{
				trig("Attacks", attacksCtrl),
			}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.attacks",
			}},
		},
		{
			name: "trigger combat damage",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{
				trig("DamageDone", combatDamage),
			}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.combat-damage",
			}},
		},
		{
			name: "damage without CombatDamage is a gap",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{
				trig("DamageDone", notCombatDamage),
			}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.gap:DamageDone", Gap: "trigger mode DamageDone",
			}},
		},
		{
			name: "trigger spell-cast by you",
			c:    cardOf(&cards.Face{Types: []string{"Enchantment"}, Triggers: []cards.Trigger{trig("SpellCast", spellCast)}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.spell-cast",
			}},
		},
		{
			name: "spell-cast without You is a gap",
			c: cardOf(&cards.Face{Types: []string{"Enchantment"}, Triggers: []cards.Trigger{
				trig("SpellCast", spellCastNoYou),
			}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.gap:SpellCast", Gap: "trigger mode SpellCast",
			}},
		},
		{
			name: "trigger becomes-target",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{
				trig("BecomesTarget", becomesTarget),
			}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.becomes-target",
			}},
		},
		{
			name: "trigger life-gained",
			c:    cardOf(&cards.Face{Types: []string{"Enchantment"}, Triggers: []cards.Trigger{trig("LifeGained", lifeGained)}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.life-gained",
			}},
		},
		{
			name: "trigger drawn by player",
			c:    cardOf(&cards.Face{Types: []string{"Enchantment"}, Triggers: []cards.Trigger{trig("Drawn", drawn)}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.drawn",
			}},
		},
		{
			name: "trigger drawn by your card",
			c:    cardOf(&cards.Face{Types: []string{"Enchantment"}, Triggers: []cards.Trigger{trig("Drawn", drawnByCard)}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.drawn",
			}},
		},
		{
			name: "trigger phase for another player is a gap",
			c: cardOf(&cards.Face{Types: []string{"Enchantment"}, Triggers: []cards.Trigger{
				trig("Phase", phaseEndOpp),
			}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.gap:Phase", Gap: "trigger mode Phase",
			}},
		},
		{
			name: "trigger phase end of turn",
			c:    cardOf(&cards.Face{Types: []string{"Enchantment"}, Triggers: []cards.Trigger{trig("Phase", phaseEnd)}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.phase",
			}},
		},
		{
			name: "trigger unsupported phase is a gap",
			c: cardOf(&cards.Face{Types: []string{"Enchantment"}, Triggers: []cards.Trigger{
				trig("Phase", phaseUntap),
			}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.gap:Phase", Gap: "trigger mode Phase",
			}},
		},
		{
			name: "trigger unknown mode is a gap",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{
				trig("Taps", map[string]string{"ValidCard": "Card.Self"}),
			}}),
			want: []Requirement{{
				Key: "trigger#0.0", Family: "trigger", Face: 0, Slot: "0",
				Sub: "trigger.gap:Taps", Gap: "trigger mode Taps",
			}},
		},
		{
			name: "static continuous",
			c:    cardOf(&cards.Face{Types: []string{"Creature"}, Statics: []cards.Static{stat("Continuous", nil)}}),
			want: []Requirement{{
				Key: "static#0.0", Family: "static", Face: 0, Slot: "0",
				Sub: "static.continuous",
			}},
		},
		{
			name: "static cost reduce",
			c:    cardOf(&cards.Face{Types: []string{"Creature"}, Statics: []cards.Static{stat("ReduceCost", nil)}}),
			want: []Requirement{{
				Key: "static#0.0", Family: "static", Face: 0, Slot: "0",
				Sub: "static.cost", Gap: "cost static",
			}},
		},
		{
			name: "static cost raise",
			c:    cardOf(&cards.Face{Types: []string{"Creature"}, Statics: []cards.Static{stat("RaiseCost", nil)}}),
			want: []Requirement{{
				Key: "static#0.0", Family: "static", Face: 0, Slot: "0",
				Sub: "static.cost", Gap: "cost static",
			}},
		},
		{
			name: "static disable triggers is served",
			c:    cardOf(&cards.Face{Types: []string{"Creature"}, Statics: []cards.Static{stat("DisableTriggers", nil)}}),
			want: []Requirement{{
				Key: "static#0.0", Family: "static", Face: 0, Slot: "0",
				Sub: "static.disable-triggers",
			}},
		},
		{
			name: "static combat damage toughness is served",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Statics: []cards.Static{
				stat("CombatDamageToughness", nil),
			}}),
			want: []Requirement{
				{
					Key: "static#0.0", Family: "static", Face: 0, Slot: "0",
					Sub: "static.combat-damage-toughness",
				},
				{Key: "combat#0.attack", Family: "combat", Face: 0, Slot: "attack", Sub: "combat.attack"},
				{Key: "combat#0.block", Family: "combat", Face: 0, Slot: "block", Sub: "combat.block"},
			},
		},
		{
			name: "static combat legality requirement and gap",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Statics: []cards.Static{
				stat("CantBlock", nil),
			}}),
			want: []Requirement{
				{
					Key: "static#0.0", Family: "static", Face: 0, Slot: "0",
					Sub: "static.combat", Gap: "legality static",
				},
				{
					Key: "combat#0.attack", Family: "combat", Face: 0, Slot: "attack",
					Sub: "combat.attack",
				},
				{
					Key: "combat#0.block", Family: "combat", Face: 0, Slot: "block",
					Sub: "combat.block",
				},
			},
		},
		{
			name: "CanAttackDefender creature-you-control shape is served",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Statics: []cards.Static{
				stat("CanAttackDefender", map[string]string{"ValidCard": "Creature.YouCtrl"}),
			}}),
			want: []Requirement{{Key: "static#0.0", Family: "static", Face: 0, Slot: "0", Sub: "static.can-attack-defender"},
				{Key: "combat#0.attack", Family: "combat", Face: 0, Slot: "attack", Sub: "combat.attack"},
				{Key: "combat#0.block", Family: "combat", Face: 0, Slot: "block", Sub: "combat.block"}},
		},
		{
			name: "CanAttackDefender self SVar shape is served",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, SVars: map[string]string{"X": "Count$YouScryThisTurn/Plus.Y", "Y": "Count$YouSurveilThisTurn"}, Statics: []cards.Static{
				stat("CanAttackDefender", map[string]string{"ValidCard": "Card.Self", "CheckSVar": "X"}),
			}}),
			want: []Requirement{{Key: "static#0.0", Family: "static", Face: 0, Slot: "0", Sub: "static.can-attack-defender-svar"},
				{Key: "combat#0.attack", Family: "combat", Face: 0, Slot: "attack", Sub: "combat.attack"},
				{Key: "combat#0.block", Family: "combat", Face: 0, Slot: "block", Sub: "combat.block"}},
		},
		{
			name: "CantBlockBy supported filter shape is served",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Statics: []cards.Static{
				stat("CantBlockBy", map[string]string{"ValidAttacker": "Creature.YouCtrl+powerLE1,Creature.YouCtrl+toughnessLE1"}),
			}}),
			want: []Requirement{{Key: "static#0.0", Family: "static", Face: 0, Slot: "0", Sub: "static.cant-block-by"},
				{Key: "combat#0.attack", Family: "combat", Face: 0, Slot: "attack", Sub: "combat.attack"},
				{Key: "combat#0.block", Family: "combat", Face: 0, Slot: "block", Sub: "combat.block"}},
		},
		{
			name: "Proft CantBeCast threshold shape is served",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Statics: []cards.Static{
				stat("CantBeCast", map[string]string{"ValidCard": "Card.Self", "CheckSVar": "X", "SVarCompare": "LT7", "EffectZone": "All"}),
			}}),
			want: []Requirement{{Key: "static#0.0", Family: "static", Face: 0, Slot: "0", Sub: "static.cant-be-cast-threshold"}},
		},
		{
			name: "Yuriko CantBeCast combat shape is served",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Statics: []cards.Static{
				stat("CantBeCast", map[string]string{"ValidCard": "Card", "Phases": "BeginCombat->EndCombat"}),
			}}),
			want: []Requirement{{Key: "static#0.0", Family: "static", Face: 0, Slot: "0", Sub: "static.cant-be-cast-combat"}},
		},
		{
			name: "Yuriko CantBeActivated combat shape is served",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Statics: []cards.Static{
				stat("CantBeActivated", map[string]string{"ValidCard": "Card", "ValidSA": "Activated.!ManaAbility", "Phases": "BeginCombat->EndCombat"}),
			}}),
			want: []Requirement{{Key: "static#0.0", Family: "static", Face: 0, Slot: "0", Sub: "static.cant-be-activated-combat"}},
		},
		{
			name: "CantBeCast generic shape remains a gap",
			c:    cardOf(&cards.Face{Types: []string{"Creature"}, Statics: []cards.Static{stat("CantBeCast", map[string]string{"ValidCard": "Card"})}}),
			want: []Requirement{{Key: "static#0.0", Family: "static", Face: 0, Slot: "0", Sub: "static.gap:CantBeCast", Gap: "static mode CantBeCast"}},
		},
		{
			name: "static unrecognized mode is a gap",
			c: cardOf(&cards.Face{Types: []string{"Creature"}, Statics: []cards.Static{
				stat("UnknownStatic", nil),
			}}),
			want: []Requirement{{
				Key: "static#0.0", Family: "static", Face: 0, Slot: "0",
				Sub: "static.gap:UnknownStatic", Gap: "static mode UnknownStatic",
			}},
		},
		{
			name: "combat from keyword on a creature",
			c:    cardOf(&cards.Face{Types: []string{"Creature"}, Keywords: []string{"Flying"}}),
			want: []Requirement{
				{Key: "combat#0.attack", Family: "combat", Face: 0, Slot: "attack", Sub: "combat.attack"},
				{Key: "combat#0.block", Family: "combat", Face: 0, Slot: "block", Sub: "combat.block"},
			},
		},
		{
			name: "combat keyword on a non-creature is ignored",
			c:    cardOf(&cards.Face{Types: []string{"Artifact"}, Keywords: []string{"Flying"}}),
			want: nil,
		},
		{
			name: "non-combat keyword creature has no combat requirement",
			c:    cardOf(&cards.Face{Types: []string{"Creature"}, Keywords: []string{"Haste"}}),
			want: nil,
		},
		{
			name: "face 1 requirements are face gaps",
			c: cardOf(
				&cards.Face{Types: []string{"Creature"}},
				&cards.Face{
					Name:     "Back",
					Types:    []string{"Creature"},
					Keywords: []string{"Flying"},
					Abilities: []*cards.SA{
						ab("Pump", nil),
					},
				},
			),
			want: []Requirement{
				{Key: "activate#1.0", Family: "activate", Face: 1, Slot: "0",
					Sub: "activate.battlefield.face:1", Gap: "face 1"},
				{Key: "combat#1.attack", Family: "combat", Face: 1, Slot: "attack",
					Sub: "combat.attack.face:1", Gap: "face 1"},
				{Key: "combat#1.block", Family: "combat", Face: 1, Slot: "block",
					Sub: "combat.block.face:1", Gap: "face 1"},
			},
		},
		{
			name: "basic land is skipped",
			c:    cardOf(&cards.Face{Types: []string{"Basic", "Land"}, Abilities: []*cards.SA{ab("Mana", nil)}}),
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(tc.c)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Requirements mismatch:\n got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

// TestRequirementsOrderAndCoverage pins the whole-card ordering: face order
// then IR order, combat last, and the covered-by-A flag only on the one
// applicable self-ETB trigger.
func TestRequirementsOrderAndCoverage(t *testing.T) {
	f := &cards.Face{
		Types:    []string{"Creature"},
		Keywords: []string{"Flying"},
		Abilities: []*cards.SA{
			{Kind: "SP", API: "Pump"}, // not an activated ability: no requirement
			ab("Pump", nil),
		},
		Triggers: []cards.Trigger{
			trig("ChangesZone", map[string]string{"Origin": "Any", "Destination": "Battlefield", "ValidCard": "Card.Self"}),
			trig("Taps", nil),
		},
		Statics: []cards.Static{stat("Continuous", nil)},
	}
	got := Requirements(cardOf(f))
	wantKeys := []string{
		"activate#0.1",
		"trigger#0.0",
		"trigger#0.1",
		"static#0.0",
		"combat#0.attack",
		"combat#0.block",
	}
	if len(got) != len(wantKeys) {
		t.Fatalf("got %d requirements, want %d: %+v", len(got), len(wantKeys), got)
	}
	for i, k := range wantKeys {
		if got[i].Key != k {
			t.Errorf("requirement %d = %q, want %q", i, got[i].Key, k)
		}
	}
	if !got[1].CoveredByA {
		t.Errorf("self-ETB trigger %s should be covered by A", got[1].Key)
	}
	for _, r := range got {
		if r.Key != "trigger#0.0" && r.CoveredByA {
			t.Errorf("%s must not be covered by A", r.Key)
		}
	}
	if got[2].Gap == "" {
		t.Errorf("Taps trigger must be a gap: %+v", got[2])
	}
}

// TestRequirementsCoveredByARequiresSelfETB checks each clause of the
// covered-by-A rule independently, so a classifier that drops one is caught.
func TestRequirementsCoveredByARequiresSelfETB(t *testing.T) {
	cases := map[string]cards.Trigger{
		"not battlefield": trig("ChangesZone", map[string]string{"Destination": "Graveyard", "ValidCard": "Card.Self"}),
		"not self":        trig("ChangesZone", map[string]string{"Destination": "Battlefield", "ValidCard": "Creature.YouCtrl"}),
		"not changeszone": trig("Attacks", map[string]string{"Destination": "Battlefield", "ValidCard": "Card.Self"}),
	}
	for name, tr := range cases {
		t.Run(name, func(t *testing.T) {
			c := cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{tr}})
			got := Requirements(c)
			if len(got) != 1 {
				t.Fatalf("got %d requirements, want 1", len(got))
			}
			if got[0].CoveredByA {
				t.Fatalf("%s: %+v must not be covered by A", name, got[0])
			}
		})
	}
}
