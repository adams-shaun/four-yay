package events

// This file is the ONE place a new Kind is described. Appending a Kind to the
// const block in event.go needs exactly one more edit: its kindInfo entry
// below. Everything that used to be a separate hand-kept per-Kind list is
// derived from this table:
//
//   - Kind.String() (the log/wire name, formerly the kindNames array);
//   - rules' trigger-interest prefilter (eventTriggerInterest), from Trigger;
//   - view.Describe's transcript line, from Describe, for every kind whose
//     line is a plain template (view keeps a hand-written case only where the
//     line depends on more than the event's own fields).
//
// TestEveryKindHasADescriptor fails, naming this file, for a Kind without a
// complete entry. The table is indexed by the existing Kind constants and
// sized by NumKinds, so ordinals stay append-only and nothing here can move an
// event encoding or the hash chain.

// TriggerClass is the events-level trigger-interest vocabulary: what kind of
// game happening an event is, for the purpose of deciding which triggered
// abilities could possibly care about it. It is deliberately not rules' or
// cards' encoding: rules maps each class onto cards.TriggerInterest bits
// (rules/trigger_eligibility.go), so events never imports rules and a new
// Kind is classified here without touching rules.
//
// The zero value TriggerUnset is not a valid table entry: it is what a missing
// entry reads as, and TestEveryKindHasADescriptor rejects it. Rules treats it
// (and any kind past the table) as the conservative catch-all, so an
// unclassified event can only cost a wider trigger scan, never a lost trigger.
type TriggerClass uint8

const (
	// TriggerUnset marks a missing entry; never write it in the table.
	TriggerUnset TriggerClass = iota
	// TriggerNone is bookkeeping no trigger mode observes: a designation,
	// marker, mint or registry update read back through state, if at all.
	TriggerNone
	// TriggerFullMatch is trigger-relevant without a dedicated prefilter
	// class: every face with any trigger goes through the full matcher.
	TriggerFullMatch
	// TriggerZoneChange is an object changing zones (ChangesZone and kin).
	TriggerZoneChange
	// TriggerDraw is a draw: a zone change that Drawn also observes.
	TriggerDraw
	// TriggerStackPut is a spell put on the stack: a zone change that
	// SpellCast also observes.
	TriggerStackPut
	TriggerLifeChange
	TriggerDamage
	TriggerTap
	TriggerStepChange
	// TriggerAttackDeclaration covers both attacker and blocker declaration.
	TriggerAttackDeclaration
	TriggerTargetsChosen
	// TriggerAbilityPush is an activated ability put on the stack.
	TriggerAbilityPush
	TriggerAttach
	TriggerExplore
	// TriggerCastInfo is the pay-time cast record (trig:ManaExpend).
	TriggerCastInfo
	// TriggerMonarch is the monarch-designation transition (trig:BecomeMonarch).
	TriggerMonarch
	// NumTriggerClasses bounds the enum; append new classes above it.
	NumTriggerClasses = int(TriggerMonarch) + 1
)

// DescribeFields is the placeholder vocabulary a KindInfo.Describe template
// may use, each written as {name}: player (the event's Player, by display
// name), obj (the event's Obj, by name and id), amount (Amount in decimal)
// and text (Text verbatim). view.Describe renders exactly these; adding one
// here without teaching view fails view's template test.
var DescribeFields = [...]string{"player", "obj", "amount", "text"}

// KindInfo is one Kind's metadata.
type KindInfo struct {
	// Name is Kind.String(): the log and wire name. Unique, snake_case, and
	// never changed for an existing kind (clients and recorded logs read it).
	Name string
	// Trigger is the kind's trigger-interest class; see TriggerClass.
	Trigger TriggerClass
	// Describe is the transcript line as a template over DescribeFields.
	// Empty means view.Describe renders the kind with its own case (the line
	// depends on game state beyond a name, or on field values).
	Describe string
}

// kindInfo is declared [NumKinds], never [...]: a Kind appended without an
// entry still compiles but leaves a zero entry, which the descriptor test
// rejects by name.
var kindInfo = [NumKinds]KindInfo{
	GameStart:            {Name: "game_start", Trigger: TriggerNone, Describe: "Game starts with {amount} players"},
	Shuffle:              {Name: "shuffle", Trigger: TriggerNone, Describe: "{player} shuffles their library"},
	MoveZone:             {Name: "move_zone", Trigger: TriggerZoneChange},
	Draw:                 {Name: "draw", Trigger: TriggerDraw},
	LifeChange:           {Name: "life", Trigger: TriggerLifeChange},
	Damage:               {Name: "damage", Trigger: TriggerDamage},
	Tap:                  {Name: "tap", Trigger: TriggerTap, Describe: "{obj} taps"},
	Untap:                {Name: "untap", Trigger: TriggerNone, Describe: "{obj} untaps"},
	StepChange:           {Name: "step", Trigger: TriggerStepChange},
	TurnChange:           {Name: "turn", Trigger: TriggerNone, Describe: "Turn {amount}: {player}"},
	Priority:             {Name: "priority", Trigger: TriggerNone, Describe: "{player} has priority"},
	PutOnStack:           {Name: "stack_push", Trigger: TriggerStackPut, Describe: "{player} casts {obj}"},
	Resolve:              {Name: "stack_resolve", Trigger: TriggerNone, Describe: "{obj} resolves"},
	ManaAdd:              {Name: "mana_add", Trigger: TriggerNone},
	ManaClear:            {Name: "mana_clear", Trigger: TriggerNone, Describe: "{player}'s mana pool empties"},
	CounterChange:        {Name: "counter", Trigger: TriggerNone},
	DeclareAttackers:     {Name: "declare_attackers", Trigger: TriggerAttackDeclaration},
	DeclareBlockers:      {Name: "declare_blockers", Trigger: TriggerAttackDeclaration},
	PlayerLost:           {Name: "player_lost", Trigger: TriggerNone, Describe: "{player} loses the game"},
	GameOver:             {Name: "game_over", Trigger: TriggerNone},
	DecisionAsk:          {Name: "decision_ask", Trigger: TriggerNone, Describe: "{player} is asked: {text}"},
	DecisionMade:         {Name: "decision_made", Trigger: TriggerNone, Describe: "{player} answers {text}"},
	Note:                 {Name: "note", Trigger: TriggerNone},
	LandPlayed:           {Name: "land_played", Trigger: TriggerNone, Describe: "{player} plays a land"},
	TargetsChosen:        {Name: "targets_chosen", Trigger: TriggerTargetsChosen},
	FlipFace:             {Name: "flip_face", Trigger: TriggerNone, Describe: "{obj} turns to face {amount}"},
	ClockTick:            {Name: "clock_tick", Trigger: TriggerNone},
	TriggerPush:          {Name: "trigger_push", Trigger: TriggerNone, Describe: "{obj} triggers"},
	EndCombatReset:       {Name: "end_combat_reset", Trigger: TriggerNone, Describe: "Combat ends"},
	CastInfo:             {Name: "cast_info", Trigger: TriggerCastInfo}, // trig:ManaExpend reads the pay-time CastInfo
	Choose:               {Name: "choose", Trigger: TriggerNone},
	TokenCreate:          {Name: "token_create", Trigger: TriggerZoneChange}, // a token is minted straight onto the battlefield, a zone change ChangesZone sees
	StackCopy:            {Name: "stack_copy", Trigger: TriggerNone},
	Attach:               {Name: "attach", Trigger: TriggerAttach},
	AbilityPush:          {Name: "ability_push", Trigger: TriggerAbilityPush},
	ModeChosen:           {Name: "mode_chosen", Trigger: TriggerNone},
	CmdDamage:            {Name: "commander_damage", Trigger: TriggerNone},
	DelayedRegister:      {Name: "delayed_register", Trigger: TriggerNone},
	DelayedPush:          {Name: "delayed_push", Trigger: TriggerNone},
	LibraryOrder:         {Name: "library_order", Trigger: TriggerNone, Describe: "{player} rearranges the top of their library"},
	ExtraTurn:            {Name: "extra_turn", Trigger: TriggerNone},
	DoorUnlock:           {Name: "door_unlock", Trigger: TriggerNone, Describe: "{obj}'s locked door is unlocked"},
	SpeedChange:          {Name: "speed_change", Trigger: TriggerNone},
	MonarchChange:        {Name: "monarch_change", Trigger: TriggerMonarch, Describe: "{player} becomes the monarch"}, // trig:BecomeMonarch
	ControlChange:        {Name: "control_change", Trigger: TriggerNone, Describe: "{player} gains control of {obj}"},
	CardToken:            {Name: "card_token", Trigger: TriggerZoneChange}, // as TokenCreate; CopyToken is not: its entry is the separate MoveZone that follows
	KeywordTriggerPush:   {Name: "keyword_trigger_push", Trigger: TriggerNone},
	Goad:                 {Name: "goad", Trigger: TriggerNone, Describe: "{obj} is goaded by {player}"},
	PlayerCounterChange:  {Name: "player_counter", Trigger: TriggerNone},
	Imprint:              {Name: "imprint", Trigger: TriggerNone},
	StartingPlayerChange: {Name: "starting_player_change", Trigger: TriggerNone, Describe: "{player} becomes the starting player"},
	Pair:                 {Name: "pair", Trigger: TriggerNone},
	MyriadCopy:           {Name: "myriad_copy", Trigger: TriggerNone},
	MyriadCleanup:        {Name: "myriad_cleanup", Trigger: TriggerNone, Describe: "Myriad tokens are exiled at end of combat"},
	GrantTriggerPush:     {Name: "grant_trigger_push", Trigger: TriggerNone},
	ManaActivate:         {Name: "mana_activate", Trigger: TriggerNone},
	TokenAttacks:         {Name: "token_attacks", Trigger: TriggerNone},
	XChange:              {Name: "x_change", Trigger: TriggerNone},
	NoteNumber:           {Name: "note_number", Trigger: TriggerNone, Describe: "{obj} notes {amount}"},
	ExtraPhase:           {Name: "extra_phase", Trigger: TriggerNone},
	CopyToken:            {Name: "copy_token", Trigger: TriggerNone}, // the copy is created in the library; the following MoveZone is the entry (admitting it would double-fire)
	Exert:                {Name: "exert", Trigger: TriggerNone},
	PlanarRoll:           {Name: "planar_roll", Trigger: TriggerNone},
	Explore:              {Name: "explore", Trigger: TriggerExplore},
	CombatRetarget:       {Name: "combat_retarget", Trigger: TriggerNone},
	RingTemptsYou:        {Name: "ring_tempts_you", Trigger: TriggerNone},
	RingEmblemPush:       {Name: "ring_emblem_push", Trigger: TriggerNone},
	GrantAbilityPush:     {Name: "grant_ability_push", Trigger: TriggerNone},
	Investigate:          {Name: "investigate", Trigger: TriggerFullMatch},
	BlessingChange:       {Name: "blessing_change", Trigger: TriggerNone, Describe: "{player} gets the city's blessing"},
	ClonePermanent:       {Name: "clone_permanent", Trigger: TriggerNone}, // a characteristic change, not an event a mode fires on
	Mutate:               {Name: "mutate", Trigger: TriggerNone},          // trig:Mutates matches through the full matcher via the past-the-mask fail-open path
	MergedTriggerPush:    {Name: "merged_trigger_push", Trigger: TriggerNone},
	Discover:             {Name: "discover", Trigger: TriggerFullMatch},
	Seek:                 {Name: "seek", Trigger: TriggerFullMatch},
	Connive:              {Name: "connive", Trigger: TriggerFullMatch},
	Enlist:               {Name: "enlist", Trigger: TriggerNone}, // trig:Enlisted matches through the full matcher via the past-the-mask fail-open path
	Exploit:              {Name: "exploit", Trigger: TriggerFullMatch},
	AlterAttribute:       {Name: "alter_attribute", Trigger: TriggerNone}, // read through filter predicates (Creature.IsSuspected), never a mode
	GainedAbilityPush:    {Name: "gained_ability_push", Trigger: TriggerNone},
	GainedTriggerPush:    {Name: "gained_trigger_push", Trigger: TriggerNone},
	Surveil:              {Name: "surveil", Trigger: TriggerFullMatch},
	Unattached:           {Name: "unattached", Trigger: TriggerNone},
	PlayerNoted:          {Name: "player_noted", Trigger: TriggerNone, Describe: "{player} is noted for {text}"},
	PlayerNoteCleared:    {Name: "player_note_cleared", Trigger: TriggerNone, Describe: "{player} is no longer noted for {text}"},
	DelayedRemove:        {Name: "delayed_remove", Trigger: TriggerNone, Describe: "delayed trigger registration removed"},
	TurnFaceUp:           {Name: "turn_face_up", Trigger: TriggerFullMatch},
	SearchedLibrary:      {Name: "searched_library", Trigger: TriggerFullMatch},
	KeywordAbilityPush:   {Name: "keyword_ability_push", Trigger: TriggerAbilityPush},
	Scry:                 {Name: "scry", Trigger: TriggerFullMatch}, // not a trigger-interest class yet, so it keeps the conservative catch-all
	StoreSVar:            {Name: "store_svar", Trigger: TriggerNone},
	TurnFaceDown:         {Name: "turn_face_down", Trigger: TriggerNone, Describe: "{obj} is turned face down"},
	CloneStatic:          {Name: "clone_static", Trigger: TriggerNone, Describe: "{obj} gains a copy static ability"},
	DamageProvenance:     {Name: "damage_provenance", Trigger: TriggerNone}, // bookkeeping beside a landed Damage; damage predicates read it from state
	EnduringStoryChange:  {Name: "enduring_story_change", Trigger: TriggerNone, Describe: "{player} has an enduring story"},
	PhaseOut:             {Name: "phase_out", Trigger: TriggerNone}, // trig:PhaseOutAll matches through the full matcher via the past-the-mask fail-open path
	GiftPromise:          {Name: "gift_promise", Trigger: TriggerNone},
	GiveGift:             {Name: "give_gift", Trigger: TriggerNone}, // trig:GiveGift matches through the full matcher via the past-the-mask fail-open path
	RollDice:             {Name: "roll_dice", Trigger: TriggerNone}, // a never-emitted replacement proposal
	Proliferate:          {Name: "proliferate", Trigger: TriggerFullMatch},
	Evolved:              {Name: "evolved", Trigger: TriggerFullMatch},
	DelayedForget:        {Name: "delayed_forget", Trigger: TriggerNone},
	CardNoted:            {Name: "card_noted", Trigger: TriggerNone},
	Cascade:              {Name: "cascade", Trigger: TriggerNone}, // a never-emitted replacement proposal: nothing can observe it
	Clash:                {Name: "clash", Trigger: TriggerNone},   // trig:Clashed matches through the full matcher via the past-the-mask fail-open path
	PlanarDeckShuffle:    {Name: "planar_deck_shuffle", Trigger: TriggerNone, Describe: "{player} shuffles the planar deck"},
	PlanarReveal:         {Name: "planar_reveal", Trigger: TriggerNone, Describe: "{obj} is revealed as the current plane"},
	PlanarWalk:           {Name: "planar_walk", Trigger: TriggerNone, Describe: "Planeswalk to the next plane"}, // trig:PlaneswalkedTo/From match it through the synthetic plane scan, not the per-face prefilter
	Specialize:           {Name: "specialize", Trigger: TriggerFullMatch},
	ChaosEnsues:          {Name: "chaos_ensues", Trigger: TriggerNone},
	ManaUndo:             {Name: "mana_undo", Trigger: TriggerNone}, // a CR 733.1 reversal: nothing triggers from an undone action
	EndTurn:              {Name: "end_turn", Trigger: TriggerNone, Describe: "The turn ends"},
	DungeonCreate:        {Name: "dungeon_create", Trigger: TriggerNone},
	DungeonRoom:          {Name: "dungeon_room", Trigger: TriggerNone},
	DungeonComplete:      {Name: "dungeon_complete", Trigger: TriggerNone},
	DungeonRemove:        {Name: "dungeon_remove", Trigger: TriggerNone, Describe: "{obj} is removed from the command zone"},
	InitiativeChange:     {Name: "initiative_change", Trigger: TriggerNone, Describe: "{player} takes the initiative"}, // read through IsInitiative intervening-if predicates
	SkipTurn:             {Name: "skip_turn", Trigger: TriggerNone},                                                    // CR 500.9 bookkeeping read back through state.Game.SkipTurns
	ControlPlayerChange:  {Name: "control_player_change", Trigger: TriggerNone},                                        // CR 720 bookkeeping read back through state.Game.ControlledBy
	Crew:                 {Name: "crew", Trigger: TriggerNone},                                                         // read through Creature.CrewedThisTurn, never a mode
	ElementalBend:        {Name: "elemental_bend", Trigger: TriggerFullMatch},
	SetupEntered:         {Name: "setup_entered", Trigger: TriggerNone, Describe: "{obj} counts as having entered this turn"}, // compliance fixture provenance; no ETB trigger
	DoorLock:             {Name: "door_lock", Trigger: TriggerNone, Describe: "{obj}'s door is locked"},
	Saddle:               {Name: "saddle", Trigger: TriggerFullMatch}, // trig:Saddled reads the crewer-to-Mount pairing; the designation itself is AlterAttribute "Saddled"
}

// Info returns k's descriptor; ok is false for a value past the enum.
func Info(k Kind) (info KindInfo, ok bool) {
	if int(k) < len(kindInfo) {
		return kindInfo[k], true
	}
	return KindInfo{}, false
}

func (k Kind) String() string {
	if int(k) < len(kindInfo) {
		return kindInfo[k].Name
	}
	return "unknown"
}

// Trigger is k's trigger-interest class, TriggerUnset past the enum.
func (k Kind) Trigger() TriggerClass {
	if int(k) < len(kindInfo) {
		return kindInfo[k].Trigger
	}
	return TriggerUnset
}

// DescribeTemplate is k's transcript template, "" when view renders k itself.
func (k Kind) DescribeTemplate() string {
	if int(k) < len(kindInfo) {
		return kindInfo[k].Describe
	}
	return ""
}
