package azmcts

import (
	"errors"
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// LeafFunc is a caller-supplied leaf evaluator (Options.Leaf): the searching
// seat actor's win probability, in [0,1], at world e's current position. It
// is called once per simulation whose walk did not end the game, and once
// for the root. e is a simulation's world, never the real engine; the
// function must not submit to it or keep it.
type LeafFunc func(e *rules.Engine, actor state.PlayerID) float64

// Kinds are the searched decision kinds (spec §1). Every other decision --
// and a decision of a searched kind whose candidates cannot be built -- is
// answered by the bot.
//
// Optional and Modes are upstream's micro-decisions beyond the spec's set
// (MCTSPlayer.chooseUse and chooseMode, both tree nodes upstream): an
// optional trigger's yes/no (decision.KTriggerOptional) and a modal choice
// (decision.KModes). Neither is in AllKinds, so a search that does not ask
// for them is unchanged.
type Kinds struct {
	Priority, Attackers, Blockers, Target bool
	Optional, Modes                       bool
}

// AllKinds is the spec's searched set.
func AllKinds() Kinds { return Kinds{Priority: true, Attackers: true, Blockers: true, Target: true} }

// ParseKinds parses a comma list of priority, attackers, blockers, target
// (botbench's -az-kinds). An unknown or empty entry is an error.
func ParseKinds(s string) (Kinds, error) {
	var k Kinds
	for _, part := range strings.Split(s, ",") {
		switch p := strings.TrimSpace(part); p {
		case "priority":
			k.Priority = true
		case "attackers":
			k.Attackers = true
		case "blockers":
			k.Blockers = true
		case "target":
			k.Target = true
		case "optional":
			k.Optional = true
		case "modes":
			k.Modes = true
		default:
			return Kinds{}, fmt.Errorf("unknown searched kind %q (want priority, attackers, blockers, target, optional, modes)", p)
		}
	}
	return k, nil
}

// Options are one Search's knobs; DefaultOptions holds the spec's values.
type Options struct {
	// Sims is the simulation budget per searched decision (-az-sims). 0 or
	// less never builds a tree: the bot's answer is played.
	Sims int
	// CPUCT is PUCT's exploration constant c (spec §2: 1.5).
	CPUCT float64
	// FPU is the first-play-urgency reduction: an unvisited child's Q is its
	// parent's Q minus FPU (spec §2: 0.1).
	FPU float64
	// Limit caps the candidates per searched decision, the bot's answer
	// first (the enumerators' limit argument).
	Limit int
	// MaxSteps caps the environment's submits per simulation, the livelock
	// guard of spec §2; a walk that reaches it is evaluated where it stopped.
	MaxSteps int
	// Kinds are the searched decision kinds.
	Kinds Kinds
	// AutoPayment lifts engine-provided automatic payment witnesses into
	// priority candidates. It is off by default so ordinary seats retain the
	// established manual-payment candidate vocabulary.
	AutoPayment bool
	// Seed is the per-decision seed (DecisionSeed): it seeds the root noise,
	// the move sampling, and -- identically for every simulation -- the
	// environment bots' streams.
	Seed uint64
	// Noise mixes Dirichlet(DirichletAlpha) noise into the root prior with
	// weight DirichletEps. Generation only (spec §2); eval leaves it false.
	Noise                        bool
	DirichletAlpha, DirichletEps float64
	// Sample picks the move with probability proportional to its root
	// visits (tau = 1) instead of the argmax. Generation only, turns 1-4
	// (the seat decides); eval leaves it false.
	Sample bool
	// HeuristicLeaf keeps the frozen heuristic leaf (searchprobe.LeafValue)
	// even when a network supplies the prior: the network's value head is
	// then unused (M1b's prior-only ablation).
	HeuristicLeaf bool
	// UniformPrior keeps the uniform prior even when a network is given: the
	// network then supplies only the leaf value (the search benchmark's
	// "priors off", upstream experiment #2's setting).
	UniformPrior bool

	// AbsoluteUnvisitedQ replaces first-play urgency: an unvisited child's Q
	// is the constant UnvisitedQ instead of its parent's Q minus FPU. The
	// search benchmark sets UnvisitedQ 0.5 with CPUCT 0.5, the [0,1]
	// equivalent of upstream MageZero's c = 1 with unvisited children valued
	// 0 on [-1,1] (values map by v01 = (v+1)/2, which halves Q's scale, so
	// c halves with it).
	AbsoluteUnvisitedQ bool
	UnvisitedQ         float64

	// Discount is the backup discount gamma; 0 and 1 are both off (the
	// default). A simulation's leaf value v reaches a node Delta units above
	// the leaf as 0.5 + (v - 0.5) * gamma^Delta: discounting shrinks the
	// value toward 0.5, "unknown", exactly as upstream's multiplication of a
	// [-1,1] value shrinks it toward 0. DiscountUnit says what Delta counts.
	Discount     float64
	DiscountUnit DiscountUnit
	// RootPerWorld makes every simulation re-derive the root candidates in
	// its own world: each root key is matched against that world's decision
	// (IntentForKey), a key the world does not offer is unavailable there
	// (the availability rule, at the root too), and the world's pending
	// decision need only share the root's kind and player, not its
	// sequence number. A multi-world source whose worlds were built
	// independently (the benchmark's IS-MCTS) needs it: a root intent's
	// option indices and payment witness are only valid on the engine that
	// built them. Every world's Observer must then be a collector that
	// captured THAT world's root.
	RootPerWorld bool
	// NameKeys names, in the key of an in-walk candidate, every object the
	// root observation did not show (a card drawn or a permanent that
	// entered during the walk) by its card name instead of its
	// observer-local reference. References are assigned in introduction
	// order, so across re-dealt worlds "the first card drawn" would share a
	// key whatever card it is; a name is what upstream's keys use (an
	// ability's text and its source's name). Two same-named new objects
	// then share one key, and one candidate is kept. Root keys are
	// unchanged. A face-down object the searching seat does not control is
	// named "face-down".
	NameKeys bool
	// NodeCache caps the tree nodes whose engine state the search stores
	// (nodecache.go) when the world source declares a fixed world
	// (FixedWorldSource: the clairvoyant clone, FixedChance); 0 turns the
	// cache off. It never changes the Result, only its cost counters, and a
	// source whose worlds differ between simulations never uses it.
	NodeCache int
	// Leaf, when set, is the leaf evaluator: it replaces both the frozen
	// heuristic and the network's value head (HeuristicLeaf and the net
	// argument then only decide the prior). Its value is clamped into [0,1]
	// and NaN reads 0.5; a panic inside it discards the simulation
	// (Stats.Panics). Nil leaves every search exactly as it was.
	Leaf LeafFunc

	// OpponentNodes makes the tree branch on the opponent's searched
	// decisions too (oppnodes.go), as upstream MageZero's tree does: a
	// decision of the other seat of a searched kind with at least two
	// candidates is an opponent node, valued from the opponent's side
	// (selectEdge picks the child best for the mover), instead of being
	// answered by the bot. Every stored value stays the searching seat's.
	// Off (the default) leaves every search byte-identical to a search
	// before opponent nodes existed (TestOpponentNodesOffIsByteIdentical).
	// Search refuses it unless the game has two players and the world
	// source is fixed (FixedWorldSource: the clairvoyant clone,
	// FixedChance), and Validate refuses it with RootPerWorld.
	OpponentNodes bool
	// OpponentLimit caps the candidates at an opponent node, the opponent
	// bot's answer first; 0 is Limit.
	OpponentLimit int

	// ReuseTree keeps the search tree from one of the seat's decisions to
	// the next (reuse.go), as upstream MageZero does
	// (ComputerPlayerMCTS2.getNextAction: root = root.getMatchingState(...)):
	// the next Search looks below the candidate the seat played for the
	// stored node whose world is the real position now -- the same event
	// chain head, event count and RNG draw count, the identity package
	// replay verifies -- and, when one is found, carries that node's
	// statistics into the new root: every root candidate whose intent is
	// one of the node's candidates keeps that child's visits, values and
	// subtree; any other root candidate starts fresh. The caller hands the
	// carrier in as Root.Reuse (one per seat and game) and plays the
	// returned choice; a miss, or a search that does not choose from its
	// tree, builds or drops the tree as without reuse.
	//
	// The budget is upstream's (ComputerPlayerMCTS2.applyMCTS: search until
	// root.getVisits() >= searchBudget): Sims is the root's visit target,
	// so a root that already holds k visits from the reused subtree runs
	// only Sims - k new simulations (none when k >= Sims, and the move is
	// then the carried statistics' own). Stats.Simulations counts the new
	// ones; Result.Visits include the carried ones, as upstream's recorded
	// visit counts do.
	//
	// Search refuses it unless the world source is the real one
	// (RealWorldSource: the clairvoyant clone, whose future chance is the
	// real game's): a redeal, IS-MCTS, PIMC or any re-seeded chance source
	// describes a world the real game need not follow. Validate refuses it
	// with RootPerWorld. Off (the default), nothing here runs and every
	// search is byte-identical to one before reuse existed.
	ReuseTree bool

	// ParentVisits counts visits as upstream MageZero's MCTSNode does
	// (MCTSNode.select: sqrtN = sqrt(getVisits())): PUCT's exploration term
	// reads sqrt of the PARENT's visit count instead of sqrt(N_avail + 1),
	// identical in a fixed world but at the search root; a fresh search root's own
	// evaluation is not a visit (MCTSNode2.evaluate backs it up with n = 0),
	// so the root's first selection is the prior-blind first child and
	// RootValue is the root's summed value over its simulations; and under
	// ReuseTree a carried root's own expansion visit counts toward the budget
	// (applyMCTS stops at root.getVisits() >= searchBudget). Off (the
	// default) leaves every search byte-identical.
	ParentVisits bool

	// DeadlineBestChild plays the partial tree's best child when the armed
	// wall-clock bail-out (Search's ctx) stops the tree, as upstream's
	// applyMCTS does on its searchTimeout (bestChild: the most-visited root
	// child), instead of the bot's answer. The choice is a pure function of
	// the tree as it stands (choose over the root visits, ties to the lower
	// index, Sample's draw from the seeded stream), so nothing but the count
	// of simulations the deadline let run depends on the clock. A tree with
	// no root visit (the context was done at the call) still plays the bot.
	// Stats.DeadlineBest counts the searches it answered. Off (the default)
	// leaves every search byte-identical.
	DeadlineBestChild bool

	// CombatSteps splits an attack or block declaration into upstream's
	// per-creature decisions (combat.go; ComputerPlayer.selectAttackersOne
	// AtATime / selectBlockersOneAtATime): one tree point per potential
	// attacker (no / yes, attacking the defending player) and per potential
	// blocker (no block / which attacker), in object order, the declaration
	// submitted after the last creature. It applies to the root (Root.Combat
	// names the step) and to every in-walk attackers or blockers decision of
	// a searched seat (the opponent's too under OpponentNodes). Off (the
	// default) leaves every search byte-identical.
	CombatSteps bool

	// swapFrame is a test-only knob (TestOpponentNodesEngineSymmetry): the
	// value frame is the opponent's instead of the searching seat's, so
	// the root is an opponent node, the searching seat's in-walk points
	// are opponent nodes, the opponent's are not, and every leaf is the
	// opponent's value. Requires OpponentNodes.
	swapFrame bool
}

// oppLimit is the candidate cap at an opponent node.
func (o Options) oppLimit() int {
	if o.OpponentLimit > 0 {
		return o.OpponentLimit
	}
	return o.Limit
}

// DiscountUnit is what one step of the backup discount counts.
type DiscountUnit uint8

const (
	// DiscountPly counts engine decisions on the simulation's path from the
	// node to the leaf: every submit, the searched intents and the
	// environment's (bot and opponent) answers alike.
	DiscountPly DiscountUnit = iota
	// DiscountAction counts searched tree edges between the node and the
	// leaf (upstream's "logical action": only edges out of the searching
	// seat's searched decisions).
	DiscountAction
	// DiscountTurn counts turn boundaries crossed between the node and the
	// leaf.
	DiscountTurn
)

// DiscountUnitNames are the units' names in constant order.
var DiscountUnitNames = [...]string{"ply", "action", "turn"}

func (u DiscountUnit) String() string {
	if int(u) < len(DiscountUnitNames) {
		return DiscountUnitNames[u]
	}
	return fmt.Sprintf("DiscountUnit(%d)", u)
}

// ParseDiscountUnit parses ply, action or turn.
func ParseDiscountUnit(s string) (DiscountUnit, error) {
	for i, n := range DiscountUnitNames {
		if n == s {
			return DiscountUnit(i), nil
		}
	}
	return 0, fmt.Errorf("unknown discount unit %q (want ply, action, turn)", s)
}

// BenchCandidateLimit is a candidate limit no benchmark decision reaches:
// the search benchmark must never cut a legal root option (Stats.Truncated
// and RootTruncated count every cut that happens anyway).
const BenchCandidateLimit = 512

// DefaultOptions are the spec's values (§2) and this plan's candidate and
// step caps.
func DefaultOptions() Options {
	return Options{
		Sims: 100, CPUCT: 1.5, FPU: 0.1, Limit: 6, MaxSteps: 1000, Kinds: AllKinds(),
		DirichletAlpha: 0.3, DirichletEps: 0.25, NodeCache: DefaultNodeCache,
	}
}

// Stats are spec §4's failure-handling counters plus the work counts the
// cost report needs. Every fallback lands here; none is silent.
type Stats struct {
	Searched       int // decisions a tree was built for
	Skipped        int // searched-kind decisions with fewer than two candidates, or a bot answer outside the candidate vocabulary: the bot's answer was played
	Simulations    int // simulations attempted
	Completed      int // simulations that backed up a value
	ChanceFailures int // chance failure in a hypothetical world: simulation discarded
	Panics         int // engine panic inside a world (the livelock watcher included): discarded
	SubmitErrors   int // any other world error (a rejected intent, no pending decision, an unknown key): discarded
	BadWorlds      int // a world not positioned at the root decision: discarded
	NoWorld        int // the source could not produce a world: discarded
	AllFailed      int // every simulation failed: the bot's answer was played
	StepCapped     int // walks stopped by MaxSteps, evaluated where they stopped
	Terminal       int // walks that reached game over
	Expanded       int // new tree nodes
	Unavailable    int // known children a world did not offer, summed over node visits
	EnvSteps       int // environment submits (the searched intents excluded), a discarded simulation's included
	PriorFallbacks int // network priors that fell back to uniform, at the root and at in-walk points (a discarded simulation's included)
	FeedStopped    int // decisions the driver routed around the search (its observation feed stopped): the bot's answer was played
	RedealRefused  int // decisions whose honest (redeal) world source refused to prepare: no world, the bot's answer was played
	// Truncated counts searched points -- the root and in-walk decisions,
	// a discarded simulation's included -- whose candidate list the Limit
	// cut; RootTruncated is the root's share (0 or 1 per Search).
	Truncated     int
	RootTruncated int
	// LeafPlies, LeafEdges and LeafTurns sum, over completed simulations,
	// the leaf's depth below the root: engine decisions (every submit, the
	// environment's included), searched tree edges, and turn boundaries
	// crossed. MeanLeafPlies and friends divide by Completed.
	LeafPlies int
	LeafEdges int
	LeafTurns int
	// DeadlineHits is the armed wall-clock bail-out (Search's ctx, the hosted
	// seats' per-decision budget): the caller's context was done at the call
	// or became done between simulations, the tree stopped where it was and
	// the bot's answer was played. A live context never counts it.
	DeadlineHits int

	// The walk's cost counters. They count work, not outcomes: the node
	// cache (Options.NodeCache) changes them, and EnvSteps above, and
	// nothing else in Stats (a stored point records the Truncated and
	// PriorFallbacks producing it counted, so those stay exact).
	Plays       int // searched submits (Env.Play), a discarded simulation's included
	ReplayPlays int // Plays along an edge the tree already held (expanded, or ended there before)
	ReplaySteps int // the EnvSteps spent inside those ReplayPlays: re-walking known tree edges
	NodeSaves   int // node states the cache stored
	NodeResumes int // simulations that resumed from a stored node state below the root
	NodeEvicts  int // stored node states dropped for a more-visited node (the cache was full)

	// OppPoints counts the selections made at opponent nodes
	// (Options.OpponentNodes) by the simulations' walks, and OppExpanded
	// the opponent nodes created (its share of Expanded). Both stay 0 with
	// the switch off.
	OppPoints   int
	OppExpanded int

	// ReuseHits counts the searches whose root carried a stored node's
	// statistics (Options.ReuseTree), ReusePartial the hits where the
	// root's candidates and the node's differed (some root candidates
	// started fresh, or some of the node's children were dropped), and
	// ReuseCarried the root visits carried over. The ReuseMiss counters
	// split the searches that built a fresh tree with the switch on by why:
	// NoTree, no stored tree (the seat's first search, or its last tree was
	// dropped because the move did not come from it); State, no stored node
	// is the real position (hidden information or chance came out
	// differently, or a player left the tree); Candidates, a node is the
	// real position but none of its children is a root candidate. All stay
	// 0 with the switch off, and omitempty keeps a switched-off Result's
	// JSON byte-identical to one from before reuse existed.
	ReuseHits           int `json:",omitempty"`
	ReusePartial        int `json:",omitempty"`
	ReuseCarried        int `json:",omitempty"`
	ReuseMissNoTree     int `json:",omitempty"`
	ReuseMissState      int `json:",omitempty"`
	ReuseMissCandidates int `json:",omitempty"`

	// DeadlineBest counts the searches the armed bail-out stopped whose move
	// is the partial tree's best child (Options.DeadlineBestChild); each is
	// also a DeadlineHits. OptionalSearched, ModesSearched and the Skipped
	// pair split Searched and Skipped for the kinds outside KindNames
	// (Kinds.Optional, Kinds.Modes); CombatSteps counts the searches whose
	// root is one creature of a split declaration (Options.CombatSteps,
	// Result.Step), each also counted under its kind. All stay 0 with their
	// switches off, and omitempty keeps a switched-off Result's JSON
	// byte-identical.
	DeadlineBest     int `json:",omitempty"`
	OptionalSearched int `json:",omitempty"`
	OptionalSkipped  int `json:",omitempty"`
	ModesSearched    int `json:",omitempty"`
	ModesSkipped     int `json:",omitempty"`
	CombatSteps      int `json:",omitempty"`

	// KindSearched splits Searched by kind (KindNames order).
	KindSearched [NumKinds]int
	// KindSkipped splits Skipped by kind and reason: every skip is counted
	// in exactly one cell.
	KindSkipped [NumKinds][NumSkipReasons]int
	// PrioritySkipped splits the skipped priority decisions (the
	// KindSkipped[KindPriority] row) by the bot's own answer. The manual
	// bot taps mana ("activate") one source at a time before it casts, and
	// those decisions are never searched, so this row is where stage 0 sees
	// how often the search meets a cast only after mana has floated.
	PrioritySkipped [NumBaseKinds][NumSkipReasons]int
}

// The searched kinds, indexing Stats' per-kind breakdowns.
const (
	KindPriority = iota
	KindAttackers
	KindBlockers
	KindTarget
	NumKinds
)

// KindNames are the searched kinds' names in index order (the Result.Kind
// and Diag.Kind strings).
var KindNames = [NumKinds]string{"priority", "attackers", "blockers", "target"}

// kindIndex is kind's index in KindNames, or -1.
func kindIndex(kind string) int {
	for i, n := range KindNames {
		if n == kind {
			return i
		}
	}
	return -1
}

// countSearched counts one searched decision of kind: in KindSearched for
// the spec's kinds, in the omitempty counters for the others.
func (s *Stats) countSearched(kind string) {
	if k := kindIndex(kind); k >= 0 {
		s.KindSearched[k]++
		return
	}
	switch kind {
	case "optional":
		s.OptionalSearched++
	case "modes":
		s.ModesSearched++
	}
}

// countExtraSkipped counts one skipped decision of a kind outside
// KindNames, reporting whether kind is one.
func (s *Stats) countExtraSkipped(kind string) bool {
	switch kind {
	case "optional":
		s.OptionalSkipped++
	case "modes":
		s.ModesSkipped++
	default:
		return false
	}
	return true
}

// SkipReason is why a decision of a searched kind was not searched.
type SkipReason int

const (
	// SkipPayment: the bot answered with an auto-pay Payment intent, which
	// has no semantic-action form (no Payment support yet).
	SkipPayment SkipReason = iota
	// SkipFewCandidates: fewer than two candidates -- a pass-only priority,
	// no legal attacker, or a bot answer outside the candidate vocabulary
	// (a land play or mana activation at priority).
	SkipFewCandidates
	// SkipTranslate: ObserveDecision, Actions or Match failed on the
	// decision or a candidate.
	SkipTranslate
	NumSkipReasons
)

// SkipReasonNames are the skip reasons' report names in index order.
var SkipReasonNames = [NumSkipReasons]string{"payment", "few-candidates", "translate-error"}

// BaseKind is the kind of the bot's own answer at a priority decision.
type BaseKind int

const (
	BaseCast BaseKind = iota
	BaseAbility
	BasePass
	BasePlayLand
	BaseActivate // a mana ability: the manual bot's tap before a cast
	BasePayment  // an auto-pay payment witness (Intent.Payment)
	BaseOther    // any other option kind, or not exactly one choice
	NumBaseKinds
)

// BaseKindNames are the base kinds' report names in index order.
var BaseKindNames = [NumBaseKinds]string{"cast", "ability", "pass", "play_land", "activate", "payment", "other"}

// Add sums o into s.
func (s *Stats) Add(o Stats) {
	s.Searched += o.Searched
	s.Skipped += o.Skipped
	s.Simulations += o.Simulations
	s.Completed += o.Completed
	s.ChanceFailures += o.ChanceFailures
	s.Panics += o.Panics
	s.SubmitErrors += o.SubmitErrors
	s.BadWorlds += o.BadWorlds
	s.NoWorld += o.NoWorld
	s.AllFailed += o.AllFailed
	s.StepCapped += o.StepCapped
	s.Terminal += o.Terminal
	s.Expanded += o.Expanded
	s.Unavailable += o.Unavailable
	s.EnvSteps += o.EnvSteps
	s.PriorFallbacks += o.PriorFallbacks
	s.FeedStopped += o.FeedStopped
	s.RedealRefused += o.RedealRefused
	s.DeadlineHits += o.DeadlineHits
	s.Truncated += o.Truncated
	s.RootTruncated += o.RootTruncated
	s.LeafPlies += o.LeafPlies
	s.LeafEdges += o.LeafEdges
	s.LeafTurns += o.LeafTurns
	s.Plays += o.Plays
	s.ReplayPlays += o.ReplayPlays
	s.ReplaySteps += o.ReplaySteps
	s.NodeSaves += o.NodeSaves
	s.NodeResumes += o.NodeResumes
	s.NodeEvicts += o.NodeEvicts
	s.OppPoints += o.OppPoints
	s.OppExpanded += o.OppExpanded
	s.ReuseHits += o.ReuseHits
	s.ReusePartial += o.ReusePartial
	s.ReuseCarried += o.ReuseCarried
	s.ReuseMissNoTree += o.ReuseMissNoTree
	s.ReuseMissState += o.ReuseMissState
	s.ReuseMissCandidates += o.ReuseMissCandidates
	s.DeadlineBest += o.DeadlineBest
	s.OptionalSearched += o.OptionalSearched
	s.OptionalSkipped += o.OptionalSkipped
	s.ModesSearched += o.ModesSearched
	s.ModesSkipped += o.ModesSkipped
	s.CombatSteps += o.CombatSteps
	for k := range s.KindSearched {
		s.KindSearched[k] += o.KindSearched[k]
		for r := range s.KindSkipped[k] {
			s.KindSkipped[k][r] += o.KindSkipped[k][r]
		}
	}
	for b := range s.PrioritySkipped {
		for r := range s.PrioritySkipped[b] {
			s.PrioritySkipped[b][r] += o.PrioritySkipped[b][r]
		}
	}
}

// MeanLeafPlies is LeafPlies per completed simulation (0 with none).
func (s Stats) MeanLeafPlies() float64 { return perCompleted(s.LeafPlies, s.Completed) }

// MeanLeafEdges is LeafEdges per completed simulation (0 with none).
func (s Stats) MeanLeafEdges() float64 { return perCompleted(s.LeafEdges, s.Completed) }

// MeanLeafTurns is LeafTurns per completed simulation (0 with none).
func (s Stats) MeanLeafTurns() float64 { return perCompleted(s.LeafTurns, s.Completed) }

func perCompleted(sum, n int) float64 {
	if n == 0 {
		return 0
	}
	return float64(sum) / float64(n)
}

// The error classes a world reports; RunTree counts a discarded simulation
// by the first class its error wraps (anything else is a SubmitError).
var (
	ErrChance   = errors.New("azmcts: chance failure in a hypothetical world")
	ErrPanic    = errors.New("azmcts: engine panic inside a world")
	ErrSubmit   = errors.New("azmcts: a world rejected a submit")
	ErrBadWorld = errors.New("azmcts: world is not at the root decision")
	ErrNoWorld  = errors.New("azmcts: no world could be produced")
)
