package v1agent

// Role is what a card is for, for the tactical policy.
type Role int

const (
	RoleNone Role = iota
	RoleCreature
	RoleBurn     // damage to any target (or players)
	RoleRemoval  // kills/exiles/bounces a creature
	RoleCounter  // counters a spell
	RoleDraw     // card advantage / selection
	RoleToken    // makes creatures
	RoleArtifact // cheap value artifact (affinity fuel)
	RolePump     // combat trick
	RoleProtect  // fog-like
	RoleReanimate
	RoleSweeper // damages each opposing creature
	RoleRamp
	RoleNever // never cast voluntarily
)

// Ability use policies for activate_ability candidates of a source.
type abUse int

const (
	abNever    abUse = iota // do not activate
	abEndStep               // value activation: opponent's end step, or our main2 with spare mana
	abCombat                // pump an attacker that got through
	abNinjutsu              // swap an unblocked attacker for this card
	abEquip                 // equip our best creature in main1
	abCycle                 // cycle from hand when land-light
	abLoot                  // discard-to-draw when flooded
	abShaman                // Krark-Clan Shaman sweep
	abPing                  // 1 damage for a sacrifice (Makeshift Munitions)
	abStun                  // Cryogen Relic: stun their best creature
	abMain2                 // sorcery-speed value activation: our main2 with spare mana (derived only)
)

type hint struct {
	role     Role
	dmg      int     // burn damage
	pol      int     // target polarity: -1 harmful (theirs), +1 beneficial (mine), 0 derive
	tapped   bool    // land enters tapped
	flash    bool    // prefer casting on the opponent's turn
	bonus    float64 // creature value bonus (engines, key threats)
	ab       abUse
	sacCost  bool // the cast needs a sacrifice (only with fodder)
	selfLand bool // Cleansing Wildfire: target our own indestructible land
}

// hints is the hand-written play knowledge for the pauper-kernel pool.
// Numbers and types come from the kernel observation and cardfacts.json;
// this table records only what the IR summary cannot say: roles, timing
// and target polarity.
var hints = map[string]hint{
	// lands that enter tapped
	"Drossforge Bridge": {tapped: true}, "Slagwoods Bridge": {tapped: true}, "Mistvault Bridge": {tapped: true},
	"Silverbluff Bridge": {tapped: true}, "Sea Gate": {tapped: true}, "Citadel Gate": {tapped: true},
	"Azorius Guildgate": {tapped: true}, "Idyllic Beachfront": {tapped: true},
	"Twisted Landscape": {ab: abEndStep},
	"Basilisk Gate":     {ab: abCombat, pol: 1},
	"Heap Gate":         {ab: abEndStep},

	// Burn
	"Lightning Bolt":      {role: RoleBurn, dmg: 3},
	"Chain Lightning":     {role: RoleBurn, dmg: 3},
	"Fiery Temper":        {role: RoleBurn, dmg: 3},
	"Fireblast":           {role: RoleBurn, dmg: 4},
	"Lava Dart":           {role: RoleBurn, dmg: 1},
	"Galvanic Blast":      {role: RoleBurn, dmg: 2},
	"Grab the Prize":      {role: RoleDraw},
	"Faithless Looting":   {role: RoleDraw},
	"Highway Robbery":     {role: RoleDraw},
	"End the Festivities": {role: RoleSweeper, dmg: 1},
	"Guttersnipe":         {role: RoleCreature, bonus: 2},
	"Voldaren Epicure":    {role: RoleCreature},
	"Masked Meower":       {role: RoleCreature, ab: abLoot},
	"Sneaky Snacker":      {role: RoleCreature},
	"Blood Token":         {ab: abLoot},

	// Rally
	"Clockwork Percussionist":  {role: RoleCreature},
	"Goblin Bushwhacker":       {role: RoleCreature},
	"Goblin Tomb Raider":       {role: RoleCreature},
	"Burning-Tree Emissary":    {role: RoleCreature},
	"Experimental Synthesizer": {role: RoleArtifact, ab: abEndStep},
	"Reckless Impulse":         {role: RoleDraw},
	"Rally at the Hornburg":    {role: RoleToken},

	// Wildfire / Affinity
	"Fanatical Offering":    {role: RoleDraw, sacCost: true},
	"Eviscerator's Insight": {role: RoleDraw, sacCost: true},
	"Reckoner's Bargain":    {role: RoleDraw, sacCost: true},
	"Ichor Wellspring":      {role: RoleArtifact},
	"Blood Fountain":        {role: RoleArtifact, ab: abEndStep},
	"Lembas":                {role: RoleArtifact, ab: abEndStep},
	"Nihil Spellbomb":       {role: RoleArtifact, ab: abNever},
	"Cryogen Relic":         {role: RoleArtifact, ab: abStun, pol: -1},
	"Makeshift Munitions":   {role: RoleArtifact, ab: abPing, pol: -1},
	"Hunter's Blowgun":      {role: RoleArtifact, ab: abEquip, pol: 1},
	"Black Mage's Rod":      {role: RoleArtifact, ab: abEquip, pol: 1},
	"Cleansing Wildfire":    {role: RoleDraw, selfLand: true},
	"Cast Down":             {role: RoleRemoval},
	"Writhing Chrysalis":    {role: RoleCreature, bonus: 1},
	"Nyxborn Hydra":         {role: RoleCreature},
	"Refurbished Familiar":  {role: RoleCreature},
	"Krark-Clan Shaman":     {role: RoleCreature, ab: abShaman},
	"Myr Enforcer":          {role: RoleCreature},
	"Thoughtcast":           {role: RoleDraw},
	"Toxin Analysis":        {role: RolePump, pol: 1},
	"Pulse of Murasa":       {role: RoleReanimate, pol: 1},

	// Elves / Spy
	"Llanowar Elves":       {role: RoleCreature, bonus: 0.5},
	"Fyndhorn Elves":       {role: RoleCreature, bonus: 0.5},
	"Elvish Mystic":        {role: RoleCreature, bonus: 0.5},
	"Elves of Deep Shadow": {role: RoleCreature, bonus: 0.5},
	"Priest of Titania":    {role: RoleCreature, bonus: 2},
	"Timberwatch Elf":      {role: RoleCreature, bonus: 1, ab: abCombat, pol: 1},
	"Quirion Ranger":       {role: RoleCreature, ab: abNever, pol: 1},
	"Wellwisher":           {role: RoleCreature, ab: abEndStep},
	"Lead the Stampede":    {role: RoleDraw},
	"Winding Way":          {role: RoleDraw},
	"Generous Ent":         {role: RoleCreature, ab: abCycle},
	"Avenging Hunter":      {role: RoleCreature, bonus: 1},
	"Masked Vandal":        {role: RoleCreature, pol: -1},
	"Mesmeric Fiend":       {role: RoleCreature, pol: -1},
	"Overgrown Battlement": {role: RoleCreature},
	"Saruli Caretaker":     {role: RoleCreature},
	"Gatecreeper Vine":     {role: RoleCreature},
	"Sagu Wildling":        {role: RoleCreature},
	"Land Grant":           {role: RoleRamp},
	"Balustrade Spy":       {role: RoleCreature, pol: -1},
	"Lotleth Giant":        {role: RoleCreature, pol: -1},
	"Dread Return":         {role: RoleReanimate, pol: 1},
	"Wall of Roots":        {role: RoleCreature},
	"Troll of Khazad-dum":  {role: RoleCreature, ab: abCycle, bonus: 1},
	"Lotus Petal":          {role: RoleNever},
	"Tinder Wall":          {role: RoleCreature, ab: abNever},

	// CawGates
	"Counterspell":              {role: RoleCounter},
	"Spell Pierce":              {role: RoleCounter},
	"Brainstorm":                {role: RoleDraw},
	"Preordain":                 {role: RoleDraw},
	"Journey to Nowhere":        {role: RoleRemoval},
	"Lorien Revealed":           {role: RoleDraw, ab: abCycle},
	"Outlaw Medic":              {role: RoleCreature},
	"Sacred Cat":                {role: RoleCreature, ab: abEndStep},
	"The Modern Age":            {role: RoleDraw},
	"Thraben Charm":             {role: RoleRemoval},
	"Prismatic Strands":         {role: RoleProtect},
	"Squadron Hawk":             {role: RoleCreature},
	"Guardian of the Guildpact": {role: RoleCreature, bonus: 2},

	// Faeries
	"Snap":                    {role: RoleRemoval},
	"Faerie Seer":             {role: RoleCreature},
	"Moon-Circuit Hacker":     {role: RoleCreature, ab: abNinjutsu},
	"Ninja of the Deep Hours": {role: RoleCreature, ab: abNinjutsu, bonus: 1},
	"Of One Mind":             {role: RoleDraw},
	"Dispel":                  {role: RoleCounter},
	"Force Spike":             {role: RoleCounter},
	"Spellstutter Sprite":     {role: RoleCreature, flash: true},
	"Harrier Strix":           {role: RoleCreature, pol: -1, ab: abLoot},
	"Humbling Elder":          {role: RoleCreature, flash: true, pol: -1},
	"Saiba Cryptomancer":      {role: RoleCreature, flash: true, pol: 1},
	"Faerie Miscreant":        {role: RoleCreature},

	// tokens
	"Eldrazi Spawn Token": {role: RoleCreature, bonus: -0.5},
}

// hintLookups counts hint-table reads (tests assert the generic build
// makes none).
var hintLookups int

// HintLookups reports how many times this process read the hint table (0
// for the generic build).
func HintLookups() int { return hintLookups }

func hintFor(name string) hint {
	hintLookups++
	return hints[normName(name)]
}
