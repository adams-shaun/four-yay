package main

import (
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
)

// cmd/policytrain trains the per-option scorer (plan L9b) on the
// search-teacher label corpus (cmd/searchteacher -labels) and writes a
// versioned checkpoint. Deterministic single-threaded SGD: same seed and
// same corpus ⇒ byte-identical checkpoint (TestTrainerDeterministic). There
// is deliberately NO -workers flag: sharding the update is a data race
// (Hogwild) and sharding the forward pass alone cannot pay for its
// deterministic reduce at this corpus size — see train.go's header.
//
// -ppo-corpus switches to the on-policy mode (ticket pn13, ppo.go): one PPO
// (or, with -vdwm, VDWM) round fine-tuning -init on its own recorded games
// (cmd/botbench -onpolicy-corpus), with -stats-out the round's JSON readout.
func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("policytrain", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		corpora        = fs.String("corpus", "", "comma-separated label-corpus JSONL paths (plain or gzip)")
		out            = fs.String("out", "", "checkpoint output path (required)")
		epochs         = fs.Int("epochs", 30, "training epochs")
		lr             = fs.Float64("lr", 0.1, "SGD learning rate (per-example, scaled by 1/batch)")
		batch          = fs.Int("batch", 64, "batch size")
		seed           = fs.Int64("seed", 1, "run seed: split, shuffles and initialisation")
		holdout        = fs.Float64("holdout", 0.1, "holdout fraction (never updated on)")
		holdoutBy      = fs.String("holdout-by", HoldoutByExample, "holdout split unit: example (shuffle decisions; one game's decisions can land on both sides) or game (hold out whole games by pair/seed/game index, so no held-out board state is trained on)")
		embed          = fs.Int("embed", 128, "H, the shared embedding width")
		hidden         = fs.Int("hidden", 128, "hidden layer width")
		rankWeight     = fs.Float64("rank-weight", policynet.DefaultRankWeight, "weight of the ranking term against the value term (in CE mode the plain CE weight; in hybrid mode scaled by the per-example margin). INERT in pure CE mode: the per-batch gradient clip normalises any uniform loss scale away — tune -lr and -clip instead")
		huberDelta     = fs.Float64("huber-delta", policynet.DefaultHuberDelta, "Huber transition point (teacher values live in [0,1])")
		overrideWeight = fs.Float64("override-weight", 1, "multiplier on examples where the teacher overrode the bot (1 = no reweighting, >1 up-weights the informative decisions)")
		lossMode       = fs.String("loss", "ce", "loss mode: ce (pure argmax cross-entropy, the value term off), hybrid (value + margin-weighted rank) or value (value only)")
		clip           = fs.Float64("clip", 1, "global L2 gradient-norm cap per batch, on the summed batch gradient before the lr/batch scale (<= 0 disables; uncapped CE divergence is rejected with an error and no checkpoint)")
		residualInit   = fs.Float64("residual-init", 0, "fixed bot-prior residual weight: options in the bot's own answer score this much higher (0 disables; a positive value starts the model at the bot baseline)")
		valueHidden    = fs.Int("value-hidden", 32, "value head hidden width (used only when -value-weight > 0)")
		valueWeight    = fs.Float64("value-weight", 0, "weight of the value head's BCE term on the game outcome (0 = no value head; the run is then bit-identical to a pre-value-head trainer)")
		valueBlend     = fs.Float64("value-blend", 0, "value target blend b in [0,1]: (1-b)*game outcome + b*teacher-chosen candidate's rollout mean")
		ppoCorpora     = fs.String("ppo-corpus", "", "comma-separated ON-POLICY corpus paths (cmd/botbench -onpolicy-corpus): switches to the PPO mode (ticket pn13), which fine-tunes -init on its own games; -corpus must then be empty")
		valueCorpora   = fs.String("value-corpus", "", "pn17: comma-separated state/outcome dump paths (JSONL, policynet.LoadStateOutcome): switches to the VALUE-ONLY mode, which trains ONLY the value head on the records' outcomes (BCE/log-loss) — the dump has no labelled options; -corpus/-ppo-corpus must be empty, and the split is always by seed block (-holdout-by is a policy-mode flag)")
		oracleCkpt     = fs.Bool("oracle-checkpoint", false, "pn17-a1, value-only mode with -features mz-opphand: write the model as an ORACLE checkpoint (policynet.WriteOracleCheckpoint) for botbench -search-oracle-checkpoint, instead of the measurement-only no-checkpoint run; refused for any other feature set or mode")
		initCkpt       = fs.String("init", "", "PPO mode: the checkpoint that played the -ppo-corpus games (required; training starts from it). Visits mode: with -epochs > 0 training continues from this checkpoint instead of a fresh model (its geometry must be the run's); with -epochs 0 it is the model -visits-eval scores")
		ppoClip        = fs.Float64("ppo-clip", 0.2, "PPO mode: surrogate clip epsilon")
		ppoKL          = fs.Float64("ppo-kl", 0.1, "PPO mode: KL(pi_old || pi) anchor weight")
		ppoAdvNorm     = fs.Bool("ppo-adv-norm", true, "PPO mode: normalise advantages per batch")
		ppoBaseline    = fs.String("ppo-baseline", BaselineValue, "PPO mode: advantage baseline, value (outcome - V_old(s) from the init's value head) or mean (outcome - train mean outcome)")
		ppoKinds       = fs.String("ppo-kinds", "", "PPO mode: comma list of decision kinds the policy term trains (empty = every recorded kind); other kinds still train the value head")
		vdwm           = fs.Bool("vdwm", false, "PPO mode: train the value-disagreement-weighted margin loss (VDWM) on the on-policy corpus instead of the PPO objective (weight (1 - p_old)*|outcome - baseline|, normalised per batch; -ppo-clip/-ppo-kl/-ppo-adv-norm unused)")
		vdwmMargin     = fs.Float64("vdwm-margin", 1, "PPO mode with -vdwm: the hinge margin m")
		statsOut       = fs.String("stats-out", "", "PPO mode: write the round's machine-readable readout (JSON) to this path")
		upgradeEntity  = fs.Int("upgrade-entity", 0, "pn14: write -init (an mz checkpoint) upgraded to the entity feature set with a per-card encoder of this width to -out, and exit. The pooled projection and the new option inputs start at zero, so the upgraded checkpoint scores exactly as -init until trained")
		setResidual    = fs.Float64("set-residual", -1, "pn14: write -init with its fixed bot-prior residual weight replaced by this value (>= 0) to -out, and exit (the residual-prior ablation: a smaller prior no longer pins the greedy answer to the bot's)")
		visitsCorpora  = fs.String("visits-corpus", "", "M1b: comma-separated az VISIT corpora (cmd/botbench -az-corpus): switches to the visit-distillation mode -- soft candidate cross-entropy against the teacher search's visits (or -visits-label bot), plus the value head on (1-b)*outcome + b*root value (-value-blend b); holdout by game pair; -corpus/-ppo-corpus/-value-corpus must be empty. With -epochs 0 it evaluates -init on -visits-eval")
		visitsLabel    = fs.String("visits-label", "teacher", "visits mode: policy target, teacher (the search's visit distribution) or bot (one-hot on the default bot's answer over the same candidates: the behaviour-cloning control)")
		visitsDiag     = fs.Bool("visits-diag", false, "visits mode: MEASUREMENT ONLY -- the student also reads the opponent's hand (the recorded FeaturesMZOppHand rows); never checkpointed")
		visitsMaxGames = fs.Int("visits-max-games", 0, "visits mode: train only on the first N games of the corpus list (the equal-data-volume cap; 0 = all)")
		visitsEval     = fs.String("visits-eval", "", "visits mode: comma-separated visit corpora to score the model on after training (teacher visits, value AUC within deck)")
		visitsTemp     = fs.Float64("visits-temp", 1, "visits mode: teacher target temperature, pi ∝ visits^(1/T); 1 is the raw visit distribution, small T approaches the teacher's argmax")
		visitsTD       = fs.Float64("visits-td-lambda", 0, "visits mode: value target by the trajectory recursion y_i = l*y_{i+1} + (1-l)*root_value_i within one (game, seat) trajectory in decision order, ending at the game outcome (upstream MageZero's label; 0.95 there). 0 (default) keeps the -value-blend target; records with no known outcome train no value term either way")
		visitsPolicyW  = fs.Float64("visits-policy-weight", 1, "visits mode: weight of the policy (visit cross-entropy) term. 0 trains the value head only: the policy head's parameters do not move (the state trunk both heads read still does)")
		visitsWindow   = fs.Int("visits-window", 0, "visits mode: train only on the newest N records of the -visits-corpus list (later files are newer; within a file, later records); 0 = all")
		visitsReport   = fs.String("visits-report", "", "visits mode: write the -visits-eval readout as JSON to this path")
		kindLoss       = fs.String("kind-loss", "attackers=bce", "per-kind loss overrides as kind=mode,... (e.g. attackers=bce); kinds not listed keep -loss. The attackers default is bce: a per-option binary logistic loss trains the score LEVEL the seat's per-option admission rule reads, which argmax CE (shift-invariant) cannot")
	)
	var grid gridFlags
	fs.StringVar(&grid.features, "features", "", "pn12: encoder feature set: v1 (default, the pinned encoder), mz (MageZero-style per-card properties; checkpointable), mz-ownlib (mz plus the seat's honest remaining own-library composition, folded from the view's own deck list and what it sees; checkpointable; needs corpora whose views carry own_deck), or the DIAGNOSTIC mz-opphand / mz-oracle (read hidden information from a -label-extras corpus; never checkpointed)")
	fs.StringVar(&grid.actions, "actions", "", "pn12: action encoding: split (default) or joint (each targeted cast option expanded into one (card, target) option per legal target; needs a -label-extras corpus)")
	fs.IntVar(&grid.maxGameIndex, "max-game-index", 0, "pn12: train only on games whose per-pair game index is below this (nested subsets for the data axis; 0 = every game)")
	fs.StringVar(&grid.evalCorpus, "eval-corpus", "", "pn12: comma-separated held-out label corpora (a disjoint seed block) to score the trained model on, with the override-only readout")
	fs.StringVar(&grid.evalJSON, "eval-json", "", "pn12: write the -eval-corpus readout as JSON to this path")
	fs.StringVar(&grid.arm, "arm", "", "pn12: arm name recorded in the -eval-json report")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })
	if *setResidual >= 0 {
		return runSetResidual(*initCkpt, *out, float32(*setResidual), stdout, stderr)
	}
	if *upgradeEntity > 0 {
		return runUpgradeEntity(*initCkpt, *out, *upgradeEntity, *seed, stdout, stderr)
	}
	lo, gridLoad, err := grid.loadOptions()
	if err != nil {
		fmt.Fprintf(stderr, "policytrain: %v\n", err)
		return 2
	}
	if *visitsCorpora != "" || (*visitsEval != "" && *epochs == 0) {
		if *corpora != "" || *ppoCorpora != "" || *valueCorpora != "" {
			fmt.Fprintln(stderr, "policytrain: -visits-corpus is mutually exclusive with -corpus, -ppo-corpus and -value-corpus")
			return 2
		}
		label, err := policynet.ParseVisitLabel(*visitsLabel)
		if err != nil {
			fmt.Fprintf(stderr, "policytrain: %v\n", err)
			return 2
		}
		switch {
		case *visitsTD < 0 || *visitsTD > 1 || *visitsPolicyW < 0 || *visitsWindow < 0:
			fmt.Fprintln(stderr, "policytrain: -visits-td-lambda must be in [0,1], -visits-policy-weight and -visits-window >= 0")
			return 2
		case *visitsTD > 0 && *valueBlend != 0:
			fmt.Fprintln(stderr, "policytrain: -visits-td-lambda and -value-blend are two value targets; give one")
			return 2
		case (*visitsTD > 0 || *visitsPolicyW == 0) && *valueWeight <= 0 && *epochs > 0:
			fmt.Fprintln(stderr, "policytrain: -visits-td-lambda and -visits-policy-weight 0 train the value head; they need -value-weight > 0")
			return 2
		case *visitsWindow > 0 && *visitsMaxGames > 0:
			fmt.Fprintln(stderr, "policytrain: -visits-window (the newest records) and -visits-max-games (the first games) are mutually exclusive")
			return 2
		}
		return runVisits(visitsArgs{
			corpora: *visitsCorpora, eval: *visitsEval, init: *initCkpt, out: *out, report: *visitsReport,
			label: label, diag: *visitsDiag, maxGames: *visitsMaxGames, temp: *visitsTemp,
			tdLambda: *visitsTD, window: *visitsWindow,
			cfg: Config{Epochs: *epochs, Batch: *batch, LR: *lr, Seed: *seed, Holdout: *holdout,
				Embed: *embed, Hidden: *hidden, Mode: policynet.LossCE, RankWeight: 1, Clip: *clip, OverrideWeight: 1,
				ResidualInit: *residualInit,
				ValueHidden:  *valueHidden, ValueWeight: *valueWeight, ValueBlend: *valueBlend,
				ScalePolicy: *visitsPolicyW != 1, PolicyWeight: *visitsPolicyW},
		}, stdout, stderr)
	}
	if *ppoCorpora != "" {
		if *corpora != "" {
			fmt.Fprintln(stderr, "policytrain: -ppo-corpus and -corpus are mutually exclusive")
			return 2
		}
		return runPPO(runPPOArgs{
			corpora: *ppoCorpora, init: *initCkpt, out: *out, statsOut: *statsOut, kinds: *ppoKinds,
			cfg: PPOConfig{Epochs: *epochs, Batch: *batch, LR: *lr, Seed: *seed, Holdout: *holdout, Clip: *clip,
				PPOClip: *ppoClip, PPOKL: *ppoKL, ValueWeight: *valueWeight, ValueHidden: *valueHidden,
				AdvNorm: *ppoAdvNorm, Baseline: *ppoBaseline, VDWM: *vdwm, VDWMMargin: *vdwmMargin, Log: stdout},
		}, stdout, stderr)
	}
	if *valueCorpora != "" {
		if *corpora != "" || *ppoCorpora != "" {
			fmt.Fprintln(stderr, "policytrain: -value-corpus is mutually exclusive with -corpus and -ppo-corpus")
			return 2
		}
		if setFlags["holdout-by"] {
			fmt.Fprintln(stderr, "policytrain: -holdout-by is a policy-mode flag; the value-only mode always splits by seed block")
			return 2
		}
		return runValueOnly(*valueCorpora, *out, lo, ValueOnlyConfig{
			Epochs: *epochs, Batch: *batch, LR: *lr, Seed: *seed, Holdout: *holdout,
			Embed: *embed, Hidden: *hidden, ValueHidden: *valueHidden, Clip: *clip,
			OracleCheckpoint: *oracleCkpt,
		}, stdout, stderr)
	}
	if *oracleCkpt {
		fmt.Fprintln(stderr, "policytrain: -oracle-checkpoint is a value-only (-value-corpus) flag")
		return 2
	}
	if *corpora == "" || *out == "" {
		fmt.Fprintln(stderr, "policytrain: -corpus and -out are required")
		fs.Usage()
		return 2
	}

	var examples []policynet.Example
	for _, path := range strings.Split(*corpora, ",") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		exs, stats, err := loadCorpus(path, lo, gridLoad)
		if err != nil {
			fmt.Fprintf(stderr, "policytrain: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "corpus %s: %d records, %d skipped (no candidates), %d loaded (%d with outcome)\n",
			path, stats.Records, stats.Skipped, len(exs), stats.WithOutcome)
		examples = append(examples, exs...)
	}
	if len(examples) == 0 {
		fmt.Fprintln(stderr, "policytrain: no example loaded")
		return 1
	}

	mode, err := policynet.ParseLossMode(*lossMode)
	if err != nil {
		fmt.Fprintf(stderr, "policytrain: %v\n", err)
		return 2
	}
	kindModes, err := parseKindModes(*kindLoss)
	if err != nil {
		fmt.Fprintf(stderr, "policytrain: %v\n", err)
		return 2
	}

	cfg := Config{
		Epochs: *epochs, Batch: *batch, LR: *lr, Seed: *seed, Holdout: *holdout, HoldoutBy: *holdoutBy,
		Embed: *embed, Hidden: *hidden,
		Mode:       mode,
		RankWeight: *rankWeight, HuberDelta: *huberDelta,
		OverrideWeight: *overrideWeight,
		Clip:           *clip,
		ResidualInit:   *residualInit,
		KindModes:      kindModes,
		ValueHidden:    *valueHidden,
		ValueWeight:    *valueWeight,
		ValueBlend:     *valueBlend,
		Log:            stdout,
	}
	res, err := Train(examples, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "policytrain: %v\n", err)
		return 1
	}
	final := res.Epochs[len(res.Epochs)-1]
	res.Model.Features = lo.Features
	if grid.evalCorpus != "" {
		evalLo := lo
		evalLo.Keep = nil // the held-out block is scored whole
		var held []policynet.Example
		for _, path := range strings.Split(grid.evalCorpus, ",") {
			if path = strings.TrimSpace(path); path == "" {
				continue
			}
			exs, _, err := loadCorpus(path, evalLo, true)
			if err != nil {
				fmt.Fprintf(stderr, "policytrain: eval corpus: %v\n", err)
				return 1
			}
			held = append(held, exs...)
		}
		games, ovr := countGames(examples)
		rep := GridReport{Arm: grid.arm, Features: lo.Features.String(), Actions: grid.actions, MaxGameIndex: grid.maxGameIndex,
			TrainExamples: len(examples), TrainGames: games, TrainOverride: ovr, ResidualInit: *residualInit, Epochs: *epochs,
			EvalExamples: len(held), Kinds: evalGrid(res.Model, held), TrainKinds: evalGrid(res.Model, examples)}
		if err := writeGridReport(stdout, grid.evalJSON, rep); err != nil {
			fmt.Fprintf(stderr, "policytrain: %v\n", err)
			return 1
		}
	}
	if lo.Features.Diagnostic() || lo.Joint {
		// A diagnostic feature set reads hidden information, and a joint
		// (card, target) model needs the follow-up target list no seat has at
		// a priority decision: either model is a measurement, never a
		// checkpoint.
		fmt.Fprintf(stdout, "feature set %s / actions %s is a measurement only: no checkpoint written\n", lo.Features, grid.actions)
		return 0
	}
	if err := res.Model.SaveCheckpoint(*out); err != nil {
		fmt.Fprintf(stderr, "policytrain: %v\n", err)
		return 1
	}
	info, err := os.Stat(*out)
	size := int64(-1)
	if err == nil {
		size = info.Size()
	}
	fmt.Fprintf(stdout, "checkpoint %s: rows=%d h=%d hidden=%d value-hidden=%d (%d bytes), encoder hash %#016x\n",
		*out, res.Model.Rows, res.Model.H, res.Model.Hidden, res.Model.ValueHidden, size, policynet.EncoderHashFor(res.Model.Features))
	fmt.Fprintf(stdout, "holdout per-kind top-1 among labelled options (blended below is just that, blended; loss mode %s):\n", mode)
	fmt.Fprintf(stdout, "  (split by %s; m-ovr/m-keep are model top-1 on the teacher-override/kept subsets, ovr-n the override count, m-bot the fraction of model picks that are a bot pick)\n", holdoutUnit(*holdoutBy))
	fmt.Fprintf(stdout, "  %-10s %8s %8s %8s %8s %8s %8s %8s %8s %8s\n", "kind", "model", "bot", "first", "random", "n", "ovr-n", "m-ovr", "m-keep", "m-bot")
	for _, k := range res.ByKind {
		fmt.Fprintf(stdout, "  %-10s %8.3f %8.3f %8.3f %8.3f %8d %8d %8.3f %8.3f %8.3f\n",
			k.Kind, k.ModelTop1, k.BotTop1, k.FirstTop1, k.RandomTop1, k.Eligible,
			k.OverrideN, k.ModelOverrideTop1, k.ModelKeepTop1, k.ModelPicksBot)
	}
	if v := res.Value; v != nil {
		fmt.Fprintf(stdout, "value head holdout (split by %s; blend %g; %d train / %d holdout examples with a target):\n", holdoutUnit(*holdoutBy), *valueBlend, v.TrainN, v.HoldoutN)
		fmt.Fprintf(stdout, "  %-22s %10s %10s\n", "predictor", "log-loss", "brier")
		fmt.Fprintf(stdout, "  %-22s %10.6f %10.6f\n", "value head", v.LogLoss, v.Brier)
		fmt.Fprintf(stdout, "  %-22s %10.6f %10.6f\n", fmt.Sprintf("base rate %.4f", v.BaseRate), v.BaseLogLoss, v.BaseBrier)
	}
	fmt.Fprintf(stdout, "train %d examples, holdout %d, skipped %d; final train loss %.6f top1 %.3f (blended), holdout loss %.6f top1 %.3f (blended)\n",
		res.TrainN, res.HoldoutN, res.Skipped, final.TrainLoss, final.TrainTop1, final.HoldoutLoss, final.HoldoutTop1)
	return 0
}

// holdoutUnit names the split unit for the table header ("" is example).
func holdoutUnit(by string) string {
	if by == "" {
		return HoldoutByExample
	}
	return by
}

// parseKindModes parses a "kind=mode,kind=mode" list into the per-kind loss
// map. An empty list is nil, meaning every kind uses -loss.
func parseKindModes(spec string) (map[decision.Kind]policynet.LossMode, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	out := map[decision.Kind]policynet.LossMode{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("bad kind-loss entry %q (want kind=mode)", part)
		}
		kind := strings.TrimSpace(kv[0])
		m, err := policynet.ParseLossMode(strings.TrimSpace(kv[1]))
		if err != nil {
			return nil, fmt.Errorf("kind-loss %s: %w", kind, err)
		}
		out[decision.Kind(kind)] = m
	}
	return out, nil
}

type runPPOArgs struct {
	corpora, init, out, statsOut, kinds string
	cfg                                 PPOConfig
}

// runPPO is the -ppo-corpus mode's CLI: load the corpora and the init
// checkpoint, run one PPO round, write the checkpoint and the readout.
func runPPO(a runPPOArgs, stdout, stderr io.Writer) int {
	if a.init == "" || a.out == "" {
		fmt.Fprintln(stderr, "policytrain: PPO mode needs -init and -out")
		return 2
	}
	if strings.TrimSpace(a.kinds) != "" {
		for _, k := range strings.Split(a.kinds, ",") {
			switch k = strings.TrimSpace(k); decision.Kind(k) {
			case decision.KAttackers, decision.KPriority, decision.KBlockers, decision.KTarget:
				a.cfg.Kinds = append(a.cfg.Kinds, decision.Kind(k))
			default:
				fmt.Fprintf(stderr, "policytrain: -ppo-kinds: %q is not a scored policynet kind (attackers, priority, blockers, target)\n", k)
				return 2
			}
		}
	}
	var examples []policynet.Example
	var features []policynet.FeatureSet
	for _, path := range strings.Split(a.corpora, ",") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		exs, _, stats, err := policynet.LoadOnPolicy(path)
		if err != nil {
			fmt.Fprintf(stderr, "policytrain: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "on-policy corpus %s: %d records (%d with outcome, %d deviating from the bot, %d sampled), features %s\n",
			path, stats.Records, stats.WithOutcome, stats.Deviated, stats.Sampled, stats.Features)
		examples = append(examples, exs...)
		features = append(features, stats.Features)
	}
	m, err := policynet.LoadCheckpointFile(a.init)
	if err != nil {
		fmt.Fprintf(stderr, "policytrain: -init %s: %v\n", a.init, err)
		return 1
	}
	for _, fs := range features {
		if fs != m.Features {
			fmt.Fprintf(stderr, "policytrain: on-policy corpus is feature set %s, -init is %s\n", fs, m.Features)
			return 1
		}
	}
	res, err := TrainPPO(examples, m, a.cfg)
	if err != nil {
		fmt.Fprintf(stderr, "policytrain: %v\n", err)
		return 1
	}
	if err := res.Model.SaveCheckpoint(a.out); err != nil {
		fmt.Fprintf(stderr, "policytrain: %v\n", err)
		return 1
	}
	if a.statsOut != "" {
		if err := writePPOStats(a.statsOut, res); err != nil {
			fmt.Fprintf(stderr, "policytrain: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(stdout, "checkpoint %s: residual %g value-hidden %d; train %d / holdout %d (policy %d), deviated %d, mean raw advantage %+.4f (%s baseline)\n",
		a.out, res.Model.ResidualW, res.Model.ValueHidden, res.TrainN, res.HoldoutN, res.PolicyN, res.Deviated, res.MeanAdv, res.Baseline)
	fmt.Fprintf(stdout, "  %-10s %6s %6s %7s %7s %8s %7s %8s %7s %7s %7s %8s %7s\n", "kind", "n", "dev", "dev%", "win", "adv", "p_old", "kl", "clip", "flip", "admitΔ", "disagree", "|adv|")
	for _, k := range res.ByKind {
		fmt.Fprintf(stdout, "  %-10s %6d %6d %7.2f %7.4f %+8.4f %7.4f %8.5f %7.4f %7.4f %7.4f %8.4f %7.4f\n",
			k.Kind, k.N, k.Deviated, k.DeviatedPct, k.WinRate, k.MeanAdv, k.MeanPOld, k.FinalKL, k.FinalClip, k.FinalFlip, k.FinalAdmits, k.MeanDisagree, k.MeanAbsAdv)
	}
	for _, k := range res.ByKind {
		if k.Sampled > 0 || k.OffGreedy > 0 {
			fmt.Fprintf(stdout, "  %-10s sampled %d, off-greedy %d (%.2f%%), mean p_beh %.4f, greedy flip %.4f\n", k.Kind, k.Sampled, k.OffGreedy, k.OffGreedyPct, k.MeanPBeh, k.GreedyFlip)
		}
	}
	f := res.Final
	fmt.Fprintf(stdout, "final vs pi_old: kl %.6f clip-frac %.4f; value holdout n %d log loss %.6f (init %.6f) vs base rate %.4f log loss %.6f\n",
		f.KL, f.ClipFrac, f.ValueHoldoutN, f.ValueLogLoss, f.InitValueLogLoss, f.BaseRate, f.BaseLogLoss)
	for _, b := range f.ValueByTurn {
		fmt.Fprintf(stdout, "  value turns %-5s n %5d log loss %.4f vs bucket base rate %.4f log loss %.4f; AUC %.4f\n",
			b.Turns, b.N, b.LogLoss, b.BaseRate, b.BaseLogLoss, b.AUC)
	}
	return 0
}

// runUpgradeEntity is -upgrade-entity: load an mz checkpoint, add a k-wide
// entity encoder (policynet.UpgradeEntity; the encoder drawn from the run
// seed, the projection and the option columns zero) and save it.
func runUpgradeEntity(init, out string, k int, seed int64, stdout, stderr io.Writer) int {
	if init == "" || out == "" {
		fmt.Fprintln(stderr, "policytrain: -upgrade-entity needs -init and -out")
		return 2
	}
	m, err := policynet.LoadCheckpointFile(init)
	if err != nil {
		fmt.Fprintf(stderr, "policytrain: -init %s: %v\n", init, err)
		return 1
	}
	rng := rand.New(rand.NewPCG(uint64(seed), 0x656e74697479^uint64(seed)))
	if err := policynet.UpgradeEntity(m, k, rng); err != nil {
		fmt.Fprintf(stderr, "policytrain: %v\n", err)
		return 1
	}
	if err := m.SaveCheckpoint(out); err != nil {
		fmt.Fprintf(stderr, "policytrain: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "checkpoint %s: %s from %s, entity width %d, encoder hash %#016x\n", out, m.Features, init, k, policynet.EncoderHashFor(m.Features))
	return 0
}

// runSetResidual is -set-residual: rewrite a checkpoint with a different
// fixed residual prior weight (nothing else changes).
func runSetResidual(init, out string, w float32, stdout, stderr io.Writer) int {
	if init == "" || out == "" {
		fmt.Fprintln(stderr, "policytrain: -set-residual needs -init and -out")
		return 2
	}
	m, err := policynet.LoadCheckpointFile(init)
	if err != nil {
		fmt.Fprintf(stderr, "policytrain: -init %s: %v\n", init, err)
		return 1
	}
	old := m.ResidualW
	m.ResidualW = w
	if err := m.SaveCheckpoint(out); err != nil {
		fmt.Fprintf(stderr, "policytrain: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "checkpoint %s: residual %g -> %g\n", out, old, w)
	return 0
}
