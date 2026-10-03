package main

// Named az variants (-az-variant, -spellbench mode): az seats that differ in
// checkpoint, leaf, prior, budget or visit corpus and meet in one run. The
// generation loop needs two differently configured az seats at one table --
// the current network against an older one in self-play, the network leaf
// against the heuristic leaf in evaluation -- and the plain az policy has one
// process-wide configuration (azCfg, azNet).
//
//	-az-variant name=key:value,key:value,...
//
// defines policy "az:<name>": the plain az seat (every -az-* flag, and
// -checkpoint) with these overrides:
//
//	checkpoint:<path>      its own network (two variants naming one file
//	                       share one loaded model)
//	heuristic-leaf[:bool]  the frozen heuristic leaf / the network's value
//	uniform-prior[:bool]   the uniform prior / the network's policy head
//	sims:<n>               simulations per searched decision
//	corpus:<path>          a visit corpus of this variant's own searched
//	                       decisions only (-az-corpus's format)
//
// A value may not contain a comma. The table is written by flag parsing and
// resolved once by azResolveVariants before any game; it is read-only while
// games run, and a variant's model is shared read-only by every worker.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/internal/azmcts"
	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/internal/spellbench/registry"
	"github.com/adams-shaun/gorge/seat"
)

// azVariantPrefix starts a variant's policy name.
const azVariantPrefix = "az:"

// azVariant is one -az-variant definition and, after azResolveVariants, the
// seat configuration and network it plays with.
type azVariant struct {
	name       string
	checkpoint string
	corpus     string
	sims       int
	simsSet    bool
	leaf       bool // heuristic-leaf
	leafSet    bool
	uniform    bool // uniform-prior
	uniformSet bool

	net *policynet.Model
	cfg azmcts.SeatConfig
}

// azVariants is the variant table in flag order.
var azVariants []*azVariant

// parseAZVariant parses one -az-variant value.
func parseAZVariant(s string) (*azVariant, error) {
	name, rest, _ := strings.Cut(s, "=")
	if name == "" || strings.ContainsAny(name, "+:, ") {
		return nil, fmt.Errorf("-az-variant %q: want name=key:value,... with a name free of '+', ':', ',' and spaces", s)
	}
	v := &azVariant{name: name}
	seen := map[string]bool{} // membership only
	for _, kv := range strings.Split(rest, ",") {
		if kv == "" {
			continue
		}
		key, val, hasVal := strings.Cut(kv, ":")
		if seen[key] {
			return nil, fmt.Errorf("-az-variant %s: %s given twice", name, key)
		}
		seen[key] = true
		flagVal := func() (bool, error) {
			if !hasVal {
				return true, nil
			}
			b, err := strconv.ParseBool(val)
			if err != nil {
				return false, fmt.Errorf("-az-variant %s: %s:%q is not a boolean", name, key, val)
			}
			return b, nil
		}
		var err error
		switch key {
		case "checkpoint":
			v.checkpoint = val
		case "corpus":
			v.corpus = val
		case "sims":
			v.sims, err = strconv.Atoi(val)
			if err != nil || v.sims < 0 {
				return nil, fmt.Errorf("-az-variant %s: sims:%q is not a simulation count", name, val)
			}
			v.simsSet = true
		case "heuristic-leaf":
			v.leaf, err = flagVal()
			v.leafSet = true
		case "uniform-prior":
			v.uniform, err = flagVal()
			v.uniformSet = true
		default:
			return nil, fmt.Errorf("-az-variant %s: unknown key %q (want checkpoint, heuristic-leaf, uniform-prior, sims, corpus)", name, key)
		}
		if err != nil {
			return nil, err
		}
		if (key == "checkpoint" || key == "corpus") && val == "" {
			return nil, fmt.Errorf("-az-variant %s: %s needs a path", name, key)
		}
	}
	return v, nil
}

// azVariantFlag is the repeatable -az-variant flag.
type azVariantFlag struct{}

func (azVariantFlag) String() string { return "" }

func (azVariantFlag) Set(s string) error {
	v, err := parseAZVariant(s)
	if err != nil {
		return err
	}
	if azVariantNamed(azVariantPrefix+v.name) != nil {
		return fmt.Errorf("-az-variant %s defined twice", v.name)
	}
	azVariants = append(azVariants, v)
	return nil
}

// registerAZVariantFlags defines -az-variant and the two search knobs the
// generation loop sets; with none of them given nothing changes.
func registerAZVariantFlags(fs *flag.FlagSet) {
	fs.Var(azVariantFlag{}, "az-variant", "-spellbench mode, repeatable: name=key:value,... defines policy az:<name>, the az seat with its own checkpoint:<path>, heuristic-leaf, uniform-prior, sims:<n> and corpus:<path> (a visit corpus of that variant's decisions only); everything else is the -az-* flags'")
	fs.BoolVar(&azCfg.Search.UniformPrior, "az-uniform-prior", false, "az policy with -checkpoint: keep the uniform prior; the network supplies only the leaf value (upstream experiment #2's \"priors off\")")
	fs.Float64Var(&azCfg.Search.Discount, "az-discount", 0, "az policy: backup discount per ply, a leaf value v reaching a node d engine decisions above it as 0.5 + (v-0.5)*factor^d (0, the default, and 1 are off)")
}

// isAZVariant reports whether a base policy name is a variant's.
func isAZVariant(base string) bool { return strings.HasPrefix(base, azVariantPrefix) }

// azVariantNamed finds the variant a base policy name ("az:<name>") names.
func azVariantNamed(base string) *azVariant {
	if !isAZVariant(base) {
		return nil
	}
	for _, v := range azVariants {
		if v.name == base[len(azVariantPrefix):] {
			return v
		}
	}
	return nil
}

// azRegisterVariants registers every defined variant as a -spellbench
// policy. The factory reads the table at build time, so a name registered by
// an earlier run in this process (tests) plays the current definition.
func azRegisterVariants() {
	for _, v := range azVariants {
		policy := azVariantPrefix + v.name
		if registry.Has(policy) {
			continue
		}
		registry.Register(policy, func(seed uint64, _ builtins.ManaMode) seat.Seat {
			cur := azVariantNamed(policy)
			if cur == nil {
				panic("botbench: az variant " + policy + " is not defined") // validated by spellbenchExit before any game
			}
			s, err := azmcts.NewSeat(seed, cur.net, cur.cfg)
			if err != nil {
				panic("botbench: " + err.Error()) // validated by azResolveVariants before any game
			}
			return s
		})
	}
}

// azResolveVariants builds every variant's seat configuration from the plain
// az seat's (so it runs after azFrontDoor) and loads its checkpoint, each
// file once. A network a variant cannot search with -- no value head, a
// diagnostic feature set -- is an error here, never a silent heuristic leaf.
func azResolveVariants() error {
	loaded := map[string]*policynet.Model{} // lookup only
	corpora := map[string]string{}          // lookup only
	for _, v := range azVariants {
		v.cfg, v.net = azSeatConfig("az"), azNet
		if v.checkpoint != "" {
			m, ok := loaded[v.checkpoint]
			if !ok {
				var err error
				if m, err = policynet.LoadCheckpointFile(v.checkpoint); err != nil {
					return fmt.Errorf("-az-variant %s: checkpoint %s: %w", v.name, v.checkpoint, err)
				}
				loaded[v.checkpoint] = m
			}
			v.net = m
		}
		if v.simsSet {
			v.cfg.Search.Sims = v.sims
		}
		if v.leafSet {
			v.cfg.Search.HeuristicLeaf = v.leaf
		}
		if v.uniformSet {
			v.cfg.Search.UniformPrior = v.uniform
		}
		if err := v.cfg.Search.Validate(v.net); err != nil {
			return fmt.Errorf("-az-variant %s: %w", v.name, err)
		}
		if v.corpus == "" {
			continue
		}
		if v.cfg.PriorOnly {
			return fmt.Errorf("-az-variant %s: corpus records searched decisions; -az-world prior searches none", v.name)
		}
		if other, dup := corpora[v.corpus]; dup {
			return fmt.Errorf("-az-variant %s and %s write the same corpus %s", other, v.name, v.corpus)
		}
		corpora[v.corpus] = v.name
		if v.corpus == azCorpusPath {
			return fmt.Errorf("-az-variant %s and -az-corpus write the same corpus %s", v.name, v.corpus)
		}
		if _, err := os.Stat(v.corpus); err == nil {
			return fmt.Errorf("-az-variant %s: corpus %s already exists", v.name, v.corpus)
		}
	}
	return nil
}

// displayName is the variant's ledger name: the plain az name's world and
// budget marker, then the variant.
func (v *azVariant) displayName() string {
	world := v.cfg.World
	if world == "" {
		world = azmcts.WorldClairvoyant
	}
	if v.cfg.PriorOnly {
		return "az-prior-" + v.name
	}
	suffix := ""
	if v.cfg.Search.OpponentNodes {
		suffix += azOppLedgerSuffix
	}
	if v.cfg.Search.ReuseTree {
		suffix += azReuseLedgerSuffix
	}
	return fmt.Sprintf("az-%s-sims%d%s-%s", world, v.cfg.Search.Sims, suffix, v.name)
}

// azVariantRecorders installs a recorder on every seat of g that is a
// variant with its own corpus (replacing -az-corpus's on that seat) and
// returns the per-seat record lists, nil when no seat records.
func azVariantRecorders(g sbGame, seats []seat.Seat) *[2][]policynet.VisitRecord {
	var recs *[2][]policynet.VisitRecord
	for s := 0; s < 2; s++ {
		v := azVariantNamed(basePolicy(g.seats[s]))
		if v == nil || v.corpus == "" {
			continue
		}
		az, ok := registry.UnwrapSeat(seats[s]).(*azmcts.Seat)
		if !ok {
			continue
		}
		if recs == nil {
			recs = new([2][]policynet.VisitRecord)
		}
		s, opp := s, g.seats[1-s]
		az.SetRecorder(func(r policynet.VisitRecord) {
			r.GameID, r.Deck, r.Seed, r.Opponent = g.id, g.deck, g.seed, opp
			recs[s] = append(recs[s], r)
		})
	}
	return recs
}

// azVariantMembers closes a game's variant records into res: one gzip member
// per recording seat (sbCorpusMember).
func azVariantMembers(recs *[2][]policynet.VisitRecord, res *sbResult, o gbench.Outcome, err error) {
	if recs == nil {
		return
	}
	for s := 0; s < 2; s++ {
		if len(recs[s]) > 0 {
			res.vcorpus[s], res.vvisits[s] = sbCorpusMember(recs[s], o, err)
		}
	}
}

// azWriteVariantCorpora writes each variant's corpus in schedule order, as
// sbWriteCorpus does: a pure function of the run's flags.
func azWriteVariantCorpora(sched []sbGame, results []sbResult, stdout io.Writer) error {
	for _, v := range azVariants {
		if v.corpus == "" {
			continue
		}
		policy := azVariantPrefix + v.name
		var members []sbResult
		for i, g := range sched {
			for s := 0; s < 2; s++ {
				if basePolicy(g.seats[s]) == policy {
					members = append(members, sbResult{corpus: results[i].vcorpus[s], visits: results[i].vvisits[s]})
				}
			}
		}
		if len(members) == 0 {
			continue // defined but not seated here: no file
		}
		n, err := sbWriteCorpus(v.corpus, members)
		if err != nil {
			return fmt.Errorf("-az-variant %s corpus: %w", v.name, err)
		}
		fmt.Fprintf(stdout, "az variant %s visit corpus %s: %d records\n", v.name, v.corpus, n)
	}
	return nil
}

// azVariantGames counts the games a variant sat in (the cost report's
// denominator beside the plain az policies').
func azVariantGames(sched []sbGame) int {
	n := 0
	for _, g := range sched {
		if isAZVariant(basePolicy(g.seats[0])) || isAZVariant(basePolicy(g.seats[1])) {
			n++
		}
	}
	return n
}

// azRunRecord is run.json's "az" block. Its three historical keys are all
// there is unless a new knob is set.
func azRunRecord() map[string]any {
	rec := map[string]any{"sims": azCfg.Search.Sims, "world": azWorldArg, "worlds": azCfg.Worlds}
	if azCfg.Search.UniformPrior {
		rec["uniform_prior"] = true
	}
	if azCfg.Search.Discount != 0 {
		rec["discount"] = azCfg.Search.Discount
	}
	if azCfg.Search.OpponentNodes {
		rec["opponent_nodes"] = true
		if azCfg.Search.OpponentLimit != 0 {
			rec["opponent_candidates"] = azCfg.Search.OpponentLimit
		}
	}
	if azCfg.Search.ReuseTree {
		rec["reuse_tree"] = true
	}
	return rec
}

// azVariantRunRecords is run.json's "az_variants" block.
func azVariantRunRecords() []map[string]any {
	out := make([]map[string]any, len(azVariants))
	for i, v := range azVariants {
		out[i] = map[string]any{
			"name": v.name, "display_name": v.displayName(), "checkpoint": v.checkpoint, "corpus": v.corpus,
			"sims": v.cfg.Search.Sims, "heuristic_leaf": v.cfg.Search.HeuristicLeaf || v.net == nil,
			"uniform_prior": v.cfg.Search.UniformPrior || v.net == nil,
		}
	}
	return out
}
