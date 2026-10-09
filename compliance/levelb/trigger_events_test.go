package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestEventTriggerSubFamilies classifies the FRA shapes the event recipes
// serve, and the near-misses that must stay gaps. Every row is one trigger on
// a creature face, so a reclassification shows as a changed Sub with the
// requirement count still 1.
func TestEventTriggerSubFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		params     map[string]string
		wantSub    string
		wantGap    bool
		covered    bool
	}{
		{"Edgar", "ChangesZone", map[string]string{"Origin": "Battlefield", "Destination": "Graveyard", "ValidCard": "Creature.Other+YouCtrl,Planeswalker.Other+YouCtrl"}, "trigger.dies-other", false, false},
		{"Gardenize", "ChangesZone", map[string]string{"Origin": "Battlefield", "Destination": "Graveyard", "ValidCard": "Creature.YouCtrl"}, "trigger.dies-other", false, false},
		{"Ferocity aura", "ChangesZone", map[string]string{"Origin": "Battlefield", "Destination": "Graveyard", "ValidCard": "Card.AttachedBy"}, "trigger.dies-other", false, false},
		{"opponent creature dies", "ChangesZone", map[string]string{"Origin": "Battlefield", "Destination": "Graveyard", "ValidCard": "Creature.OppCtrl"}, "trigger.dies-other", false, false},
		{"self dies stays dies", "ChangesZone", map[string]string{"Origin": "Battlefield", "Destination": "Graveyard", "ValidCard": "Card.Self"}, "trigger.dies", false, false},
		{"scry", "Scry", map[string]string{"ValidPlayer": "You"}, "trigger.scry", false, false},
		{"surveil", "Surveil", map[string]string{"ValidPlayer": "You"}, "trigger.surveil", false, false},
		{"opponent scries", "Scry", map[string]string{"ValidPlayer": "Opponent"}, "trigger.gap:Scry", true, false},
		{"Master of Barbs", "DamageAll", map[string]string{"CombatDamage": "False", "ValidTarget": "Opponent"}, "trigger.noncombat-damage", false, false},
		{"Massacre Girl", "DamageDone", map[string]string{"CombatDamage": "False", "ValidSource": "Card,Emblem", "ValidTarget": "Opponent"}, "trigger.noncombat-damage", false, false},
		{"Hexhaven", "DamageDoneOnce", map[string]string{"ValidTarget": "Card.Self"}, "trigger.noncombat-damage", false, false},
		{"Fblthp", "DamageAll", map[string]string{"CombatDamage": "True", "ValidTarget": "Opponent", "PlayerTurn": "True"}, "trigger.combat-damage-all", false, false},
		{"damage to a creature", "DamageDone", map[string]string{"CombatDamage": "False", "ValidTarget": "Creature"}, DamageSub, false, false},
		{"combat damage by a creature you control", "DamageDone", map[string]string{"CombatDamage": "True", "ValidSource": "Creature.YouCtrl", "ValidTarget": "Player"}, DamageSub, false, false},
		{"self deals damage", "DamageDealtOnce", map[string]string{"ValidSource": "Card.Self", "TriggerZones": "Battlefield"}, DamageSub, false, false},
		{"enchanted creature dealt damage", "DamageDoneOnce", map[string]string{"ValidTarget": "Creature.EnchantedBy", "TriggerZones": "Battlefield"}, DamageSub, false, false},
		{"Ajani Unrelenting", "AbilityCast", map[string]string{"ValidActivatingPlayer": "You", "ValidSA": "Activated.Loyalty"}, "trigger.loyalty-activated", false, false},
		{"Gideon the Oathless", "AbilityCast", map[string]string{"ValidSA": "Activated.Loyalty+OppCtrl"}, "trigger.gap:AbilityCast", true, false},
		{"Way of the Mind Sculptor", "AbilityCast", map[string]string{"ValidActivatingPlayer": "You", "ValidSA": "Activated.Loyalty+CountersRemovedToPayGE2"}, "trigger.gap:AbilityCast", true, false},
		{"Inspired Tethermage", "CounterAddedOnce", map[string]string{"CounterType": "LOYALTY", "ValidCard": "Planeswalker", "ValidSource": "You"}, "trigger.loyalty-activated", false, false},
		{"Stormchaser's Talent", "ClassLevelGained", map[string]string{"ClassLevel": "2", "ValidCard": "Card.Self", "TriggerZones": "Battlefield"}, "trigger.class-level-gained", false, false},
		{"not my Class", "ClassLevelGained", map[string]string{"ClassLevel": "2", "ValidCard": "Card.Other"}, "trigger.gap:ClassLevelGained", true, false},
		{"other counters", "CounterAddedOnce", map[string]string{"CounterType": "P1P1", "ValidSource": "You"}, CounterAddedSub, false, false},
		{"Pensive Professor", "CounterAddedOnce", map[string]string{"CounterType": "P1P1", "ValidCard": "Card.Self", "TriggerZones": "Battlefield"}, CounterAddedSub, false, false},
		{"Ant-Man", "CounterAdded", map[string]string{"CounterType": "P1P1", "ValidCard": "Creature", "ValidSource": "You", "ActivationLimit": "1"}, CounterAddedSub, false, false},
		{"Wildwood Scourge", "CounterAddedOnce", map[string]string{"CounterType": "P1P1", "ValidCard": "Creature.nonHydra+Other+YouCtrl", "TriggerZones": "Battlefield"}, CounterAddedSub, false, false},
		{"Dog Walker", "TurnFaceUp", map[string]string{"ValidCard": "Card.Self", "TriggerZones": "Battlefield"}, TurnedFaceUpSub, false, false},
		{"Pyrotechnic Performer", "TurnFaceUp", map[string]string{"ValidCard": "Card.Self,Creature.Other+YouCtrl", "TriggerZones": "Battlefield"}, TurnedFaceUpSub, false, false},
		{"Sumala Sentry", "TurnFaceUp", map[string]string{"ValidCard": "Permanent.YouCtrl", "TriggerZones": "Battlefield"}, TurnedFaceUpOtherSub, false, false},
		{"Projektor Inspector", "TurnFaceUp", map[string]string{"ValidCard": "Detective.YouCtrl", "TriggerZones": "Battlefield"}, TurnedFaceUpOtherSub, false, false},
		{"Growing Dread", "TurnFaceUp", map[string]string{"ValidCard": "Permanent", "ValidCause": "SpellAbility.YouCtrl"}, "trigger.gap:TurnFaceUp", true, false},
		{"The Great Goblin", "CounterPlayerAddedAll", map[string]string{"ValidObject": "Goblin.YouCtrl+inRealZoneBattlefield,Orc.YouCtrl+inRealZoneBattlefield,Army.YouCtrl+inRealZoneBattlefield", "ValidSource": "You"}, CounterAddedSub, false, false},
		{"Claim the Kingdom", "CounterAdded", map[string]string{"CounterType": "PLAN", "CounterAmount": "EQ4", "ValidCard": "Card.Self"}, CounterAddedSub, false, false},
		{"a non-P1P1 counter stays a gap", "CounterAddedOnce", map[string]string{"CounterType": "CHARGE", "ValidCard": "Creature.YouCtrl"}, "trigger.gap:CounterAddedOnce", true, false},
		{"a PLAN trigger on another card stays a gap", "CounterAdded", map[string]string{"CounterType": "PLAN", "CounterAmount": "EQ4", "ValidCard": "Creature.YouCtrl"}, "trigger.gap:CounterAdded", true, false},
		{"Titanbones", "Discarded", map[string]string{"ValidCard": "Card.Self"}, "trigger.discarded", false, false},
		{"Tinybones", "DiscardedAll", map[string]string{"ValidPlayer": "Player"}, "trigger.discarded", false, false},
		{"Yuriko", "AttackersDeclaredOneTarget", map[string]string{"AttackedTarget": "Player", "ValidAttackers": "Creature.YouCtrl", "ValidAttackersAmount": "EQ1"}, "trigger.attacks-one-target", false, false},
		{"Solarium Sentry", "SpellCast", map[string]string{"ValidActivatingPlayer": "Opponent", "ValidCard": "Card.cmcLE2"}, "trigger.spell-cast-opponent", false, false},
		{"Emrakul", "SpellCast", map[string]string{"Execute": "TrigUntapAll", "TriggerDescription": "When you cast this spell, untap all lands you control.", "ValidCard": "Card.Self"}, "trigger.spell-cast-self", false, true},
		{"conditional cast trigger", "SpellCast", map[string]string{"Execute": "TrigDraw", "CheckSVar": "Y", "ValidCard": "Card.Self"}, "trigger.spell-cast-self-cast", false, false},
		{"Ark of Hunger", "ChangesZone", map[string]string{"Origin": "Graveyard", "Destination": "Any", "ValidCard": "Card.YouOwn"}, "trigger.leaves-graveyard", false, false},
		{"graveyard return to the battlefield stays etb", "ChangesZone", map[string]string{"Origin": "Graveyard", "Destination": "Battlefield", "ValidCard": "Creature.YouOwn"}, "trigger.etb-other", false, false},
		{"Ninja Teen", "ChangesZone", map[string]string{"Origin": "Battlefield", "Destination": "Any", "ValidCard": "Creature.Other+YouCtrl"}, "trigger.ltb-other", false, false},
		{"Ketramose", "ChangesZoneAll", map[string]string{"Origin": "Battlefield,Graveyard", "Destination": "Exile", "ValidCards": "Card.!token"}, "trigger.ltb-other", false, false},
		{"Elvish Archivist", "ChangesZoneAll", map[string]string{"Destination": "Battlefield", "ValidCards": "Creature.YouCtrl"}, "trigger.etb-other", false, false},
		{"Hedge Shredder", "ChangesZoneAll", map[string]string{"Origin": "Library", "Destination": "Graveyard", "ValidCards": "Land.YouOwn"}, "trigger.zone-change-residue", false, false},
		{"Moonshadow", "ChangesZoneAll", map[string]string{"Origin": "Any", "Destination": "Graveyard", "ValidCards": "Card.YouOwn"}, "trigger.zone-change-residue", false, false},
		{"all dies is dies-other", "ChangesZoneAll", map[string]string{"Origin": "Battlefield", "Destination": "Graveyard", "ValidCards": "Creature.YouCtrl"}, "trigger.dies-other", false, false},
		{"Gardenize main1", "Phase", map[string]string{"Phase": "Main1", "ValidPlayer": "You"}, "trigger.phase", false, false},
		{"each player's main1", "Phase", map[string]string{"Phase": "Main1", "ValidPlayer": "Player"}, "trigger.phase-other", false, false},
		{"Theorist", "Phase", map[string]string{"Phase": "Draw", "ValidPlayer": "Opponent"}, "trigger.phase", false, false},
		{"opponent's end step", "Phase", map[string]string{"Phase": "End of Turn", "ValidPlayer": "Opponent"}, "trigger.gap:Phase", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: []string{"Creature"}, Triggers: []cards.Trigger{trig(tc.mode, tc.params)}}))
			if len(got) != 1 {
				t.Fatalf("got %d requirements, want exactly 1 (a reclassification never adds or drops one): %+v", len(got), got)
			}
			r := got[0]
			if r.Sub != tc.wantSub || (r.Gap != "") != tc.wantGap || r.CoveredByA != tc.covered {
				t.Fatalf("got Sub=%q Gap=%q Covered=%v, want Sub=%q gap=%v covered=%v", r.Sub, r.Gap, r.CoveredByA, tc.wantSub, tc.wantGap, tc.covered)
			}
		})
	}
}

// TestSelfCastTriggerNeedsASpellFace: a land is never cast, so a Card.Self
// cast trigger on a land face is never covered by the cast-resolve scenario.
func TestSelfCastTriggerNeedsASpellFace(t *testing.T) {
	tr := trig("SpellCast", map[string]string{"Execute": "TrigDraw", "ValidCard": "Card.Self"})
	got := Requirements(cardOf(&cards.Face{Types: []string{"Land"}, Triggers: []cards.Trigger{tr}}))
	if len(got) != 1 || got[0].CoveredByA {
		t.Fatalf("land cast trigger = %+v, want not covered", got)
	}
}
