package cards

import "testing"

func assertUniqueKnownCodes[T comparable](t *testing.T, domain string, names []string, code func(string) T, zero T) {
	t.Helper()
	seen := make(map[T]string, len(names))
	for _, name := range names {
		got := code(name)
		if got == zero {
			t.Errorf("%s %q has unknown code", domain, name)
			continue
		}
		if prior, ok := seen[got]; ok {
			t.Errorf("%s %q and %q share code %v", domain, prior, name, got)
		}
		seen[got] = name
	}
}

func TestCompiledCodeMappings(t *testing.T) {
	t.Run("SA kinds", func(t *testing.T) {
		assertUniqueKnownCodes(t, "SA kind", []string{"SP", "AB", "DB", "ST"}, saKindCode, SAKindUnknown)
		if got := saKindCode("future"); got != SAKindUnknown {
			t.Fatalf("unknown SA kind = %d, want zero", got)
		}
	})

	t.Run("APIs", func(t *testing.T) {
		// Every non-test effects.Register spelling plus Meld, whose opcode
		// is reserved for its compiled operand record before registration.
		// This literal inventory pins their stable opcodes.
		names := []string{
			"AddTurn", "Amass", "Animate", "AnimateAll", "Attach", "BecomeMonarch", "Branch",
			"ChangeTargets", "ChangeZone", "ChangeZoneAll", "Charm", "ChooseCard",
			"ChangeX", "ChooseNumber", "ChoosePlayer", "ChooseType", "Cleanup",
			"ControlSpell", "CopySpellAbility", "Counter", "CumulativeUpkeep", "DamageAll", "DealDamage",
			"DelayedTrigger", "Destroy", "DestroyAll", "Dig", "DigMultiple", "Discard", "Draw", "Echo", "Effect",
			"Encore", "Extort", "Fog", "GainControl", "GainLife", "Goad", "Hideaway",
			"LoseLife", "LosesGame", "Mana", "ManaReflected", "Mill", "Myriad", "NameCard",
			"Pair", "PeekAndReveal", "PermanentCreature", "Play", "Protection", "Pump",
			"PumpAll", "PutCounter", "RearrangeTopOfLibrary", "Regenerate", "RemoveCounterAll",
			"Repeat", "RepeatEach", "ReplaceEffect", "ReplaceMana", "RestartGame", "Reveal",
			"RevealHand", "RollDice", "Sacrifice", "SacrificeAll", "Scry", "SetState",
			"Shuffle", "Surveil", "Tap", "TapAll", "Token", "Untap", "UntapAll", "Vote", "Ward",
			"AlterAttribute", "WinsGame", "Meld",
		}
		assertUniqueKnownCodes(t, "API", names, APICodeForName, APIUnknown)
		if len(names)+1 != APICodeCount {
			t.Fatalf("%d known APIs require code count %d, got %d", len(names), len(names)+1, APICodeCount)
		}
		for _, name := range names {
			if got := APICodeForName(name); int(got) >= APICodeCount {
				t.Errorf("API %q code %d is outside dense table size %d", name, got, APICodeCount)
			}
		}
		if got := APICodeForName("FutureAPI"); got != APIUnknown {
			t.Fatalf("unknown API = %d, want zero", got)
		}
	})

	t.Run("trigger modes", func(t *testing.T) {
		names := []string{
			"ChangesZone", "SpellCast", "AbilityCast", "SpellAbilityCast", "Attacks",
			"AttackersDeclaredOneTarget", "AttackersDeclared", "AttackerBlocked", "AttackerBlockedByCreature", "Sacrificed",
			"Discarded", "LandPlayed", "Cycled", "CommitCrime", "BecomesTarget", "Taps",
			"TapsForMana", "DamageDone", "DamageDealtOnce", "DamageDoneOnce", "CounterAdded",
			"Drawn", "LifeLost", "LifeLostAll", "Phase", "Always", "Attached",
		}
		assertUniqueKnownCodes(t, "trigger mode", names, TriggerModeOf, TriggerMode(0))
		if got := TriggerModeOf("FutureTrigger"); got != 0 {
			t.Fatalf("unknown trigger mode = %d, want zero", got)
		}
	})

	t.Run("static modes", func(t *testing.T) {
		names := []string{
			"Continuous", "CantBeCast", "CantBeActivated", "RaiseCost", "ReduceCost", "SetCost",
			"AlternativeCost", "CastWithFlash", "CantBlock", "CantBlockBy", "Panharmonicon",
			"ManaConvert", "MustAttack", "AttackRestrict", "NumLoyaltyAct", "CantGainLife",
			"CantPreventDamage", "UntapOtherPlayer",
		}
		assertUniqueKnownCodes(t, "static mode", names, StaticModeOf, StaticMode(0))
		if got := StaticModeOf("FutureStatic"); got != 0 {
			t.Fatalf("unknown static mode = %d, want zero", got)
		}
	})

	t.Run("replacement events", func(t *testing.T) {
		names := []string{
			"Moved", "Untap", "BeginPhase", "Transform", "ProduceMana",
			"GainLife", "LifeReduced", "DamageDone", "Counter", "Draw",
		}
		assertUniqueKnownCodes(t, "replacement event", names, ReplEventOf, ReplEvent(0))
		if got := ReplEventOf("FutureReplacement"); got != 0 {
			t.Fatalf("unknown replacement event = %d, want zero", got)
		}
	})
}

func TestCompiledTypeAndKeywordMasks(t *testing.T) {
	typeNames := []string{
		"Artifact", "Battle", "Conspiracy", "Creature", "Dungeon", "Enchantment", "Instant",
		"Kindred", "Land", "Phenomenon", "Plane", "Planeswalker", "Scheme", "Sorcery", "Tribal",
		"Vanguard", "Basic", "Legendary", "Ongoing", "Snow", "World", "Spacecraft", "Vehicle", "Room",
	}
	assertUniqueKnownCodes(t, "type", typeNames, typeMaskFor, TypeMask(0))
	if got := typeMaskFor("ContraptionSubtype"); got != 0 {
		t.Fatalf("unknown type mask = %x, want zero", got)
	}
	if typeMaskFor("creature") != typeMaskFor("Creature") {
		t.Fatal("type classification is not case-insensitive")
	}

	keywordNames := []string{
		"AlternateAdditionalCost", "Buyback", "Chapter", "Cycling", "Dethrone", "Devoid",
		"Dredge", "Enchant", "Flash", "Flashback", "Harmonize", "Kicker", "Madness",
		"MayEffectFromOpeningHand", "Miracle", "Riot", "Surge", "Suspend",
	}
	assertUniqueKnownCodes(t, "keyword", keywordNames, keywordMaskFor, KeywordMask(0))
	if got := keywordMaskFor("FutureKeyword"); got != 0 {
		t.Fatalf("unknown keyword mask = %x, want zero", got)
	}
	if keywordMaskFor("ward") != keywordMaskFor("Ward") {
		// Ward is deliberately not in the fixed queried-head inventory. This
		// comparison remains true at zero and documents unknown case handling.
		t.Fatal("unknown keyword classification depends on case")
	}
}

func TestUnknownTriggerInterestIsCatchAll(t *testing.T) {
	if got := triggerInterestForMode(TriggerModeOf("FutureTrigger")); got != TriggerInterestAny {
		t.Fatalf("unknown interest = %x, want catch-all", got)
	}
}

func TestCompiledTriggerInterests(t *testing.T) {
	tests := []struct {
		mode string
		want TriggerInterest
	}{
		{"ChangesZone", TriggerInterestZoneChange},
		{"SpellCast", TriggerInterestStackPut},
		{"AbilityCast", TriggerInterestAbilityPush},
		{"SpellAbilityCast", TriggerInterestAbilityPush | TriggerInterestStackPut},
		{"Attacks", TriggerInterestAttackDeclaration},
		{"AttackersDeclared", TriggerInterestAttackDeclaration},
		{"AttackersDeclaredOneTarget", TriggerInterestAttackDeclaration},
		{"AttackerBlocked", TriggerInterestAttackDeclaration},
		{"AttackerBlockedByCreature", TriggerInterestAttackDeclaration},
		{"Blocks", TriggerInterestAttackDeclaration},
		{"Sacrificed", TriggerInterestZoneChange},
		{"Discarded", TriggerInterestZoneChange},
		{"DiscardedAll", TriggerInterestZoneChange},
		{"LandPlayed", TriggerInterestZoneChange},
		{"Cycled", TriggerInterestZoneChange},
		{"CommitCrime", TriggerInterestTargetsChosen},
		{"BecomesTarget", TriggerInterestTargetsChosen},
		{"Taps", TriggerInterestTap},
		{"TapsForMana", TriggerInterestTap},
		{"DamageDone", TriggerInterestDamage},
		{"DamageDealtOnce", TriggerInterestDamage},
		{"DamageDoneOnce", TriggerInterestDamage},
		{"Drawn", TriggerInterestDraw},
		{"LifeLost", TriggerInterestDamage | TriggerInterestLifeChange},
		{"CounterAdded", TriggerInterestAny},
		{"LifeLostAll", TriggerInterestAny},
		{"Phase", TriggerInterestStepChange},
		{"Always", TriggerInterestAny},
	}
	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			if got := triggerInterestForMode(TriggerModeOf(tc.mode)); got != tc.want {
				t.Fatalf("interest = %x, want %x", got, tc.want)
			}
		})
	}
}

// keywordMaskFor's case-insensitive fallback is on the legal-offer walk's hot
// path (every Face.HasKeyword of an unmasked keyword reaches it); it must not
// allocate, and must keep folding exactly as strings.ToLower did.
func TestKeywordMaskForFallbackDoesNotAllocate(t *testing.T) {
	for _, head := range []string{"Flying", "HASTE", "fLaShBaCk", "MayEffectFromOpeningHand", "AKeywordLongerThanEveryMaskedKeywordName"} {
		head := head
		if n := testing.AllocsPerRun(100, func() { _ = keywordMaskFor(head) }); n != 0 {
			t.Errorf("keywordMaskFor(%q) allocates %v times", head, n)
		}
	}
	if keywordMaskFor("fLaShBaCk") != KeywordFlashback || keywordMaskFor("SUSPEND") != KeywordSuspend {
		t.Fatal("ASCII fold lost a masked keyword")
	}
	// U+212A KELVIN SIGN lower-cases to ASCII 'k': the Unicode path is kept.
	if keywordMaskFor("Kicker") != KeywordKicker {
		t.Fatal("non-ASCII head no longer folds through strings.ToLower")
	}
}

// TestLineModeCatalogValuesArePinned holds the append-only contract of the
// merged line-head vocabularies: the catalog's historical assignments keep
// their values, so a name added anywhere but the end fails here.
func TestLineModeCatalogValuesArePinned(t *testing.T) {
	triggers := []string{
		"ChangesZone", "SpellCast", "AbilityCast", "SpellAbilityCast", "Attacks",
		"AttackersDeclaredOneTarget", "AttackersDeclared", "AttackerBlocked", "Sacrificed",
		"Discarded", "LandPlayed", "Cycled", "CommitCrime", "BecomesTarget", "Taps",
		"TapsForMana", "DamageDone", "DamageDealtOnce", "DamageDoneOnce", "CounterAdded",
		"Drawn", "LifeLost", "LifeLostAll", "Phase", "Always", "Attached", "AttackerBlockedByCreature",
	}
	for i, n := range triggers {
		if got := TriggerModeOf(n); int(got) != i+1 {
			t.Errorf("trigger mode %q = %d, want %d", n, got, i+1)
		}
	}
	statics := []string{
		"Continuous", "CantBeCast", "CantBeActivated", "RaiseCost", "ReduceCost", "SetCost",
		"AlternativeCost", "CastWithFlash", "CantBlock", "CantBlockBy", "Panharmonicon",
		"ManaConvert", "MustAttack", "AttackRestrict", "NumLoyaltyAct", "CantGainLife",
		"CantPreventDamage", "UntapOtherPlayer", "OptionalCost",
	}
	for i, n := range statics {
		if got := StaticModeOf(n); int(got) != i+1 {
			t.Errorf("static mode %q = %d, want %d", n, got, i+1)
		}
	}
	repls := []string{
		"Moved", "Untap", "BeginPhase", "Transform", "ProduceMana",
		"GainLife", "LifeReduced", "DamageDone", "Counter", "Draw",
		"AddCounter", "Attached", "BeginTurn", "Cascade", "CreateToken",
		"DrawCards", "Explore", "GameLoss", "GameWin", "RollDice",
		"RollPlanarDice", "Scry", "TurnFaceUp", "Mill", "PayLife", "LoseMana",
	}
	for i, n := range repls {
		if got := ReplEventOf(n); int(got) != i+1 {
			t.Errorf("replacement event %q = %d, want %d", n, got, i+1)
		}
	}
}
