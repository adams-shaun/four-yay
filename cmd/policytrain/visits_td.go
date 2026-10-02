package main

import (
	"fmt"
	"io"

	"github.com/adams-shaun/gorge/internal/policynet"
)

// The generation loop's additions to the visits mode (MageZero-style
// training, docs/superpowers/plans/2026-10-02-magezero-style-training-checklist.md):
//
//   - -visits-td-lambda l: the value target is the trajectory recursion
//     y_i = l*y_{i+1} + (1-l)*root_value_i ending at the game outcome
//     (policynet.VisitTDTargets), computed per corpus file because game ids
//     repeat between botbench runs;
//   - -visits-window n: only the newest n records train, the corpus list
//     read newest file first so a file outside the window is never opened;
//   - -visits-policy-weight w and -init: Train's ScalePolicy and Init.
//
// With none of them given loadVisitTrain is loadVisitCorpora.

// loadVisitTrain loads the training examples of a visits run, oldest first.
func loadVisitTrain(a visitsArgs, stdout io.Writer) ([]policynet.Example, policynet.FeatureSet, error) {
	o := policynet.VisitLoadOptions{Label: a.label, Diag: a.diag, MaxGames: a.maxGames, Temp: a.temp}
	if a.window <= 0 && a.tdLambda == 0 {
		exs, _, fs, err := loadVisitCorpora(a.corpora, o, stdout)
		return exs, fs, err
	}
	paths := splitCorpora(a.corpora)
	parts := make([][]policynet.Example, len(paths))
	lines := make([]string, len(paths))
	var fs policynet.FeatureSet
	loaded, need := false, a.window
	for i := len(paths) - 1; i >= 0; i-- {
		if a.window > 0 && need <= 0 {
			lines[i] = fmt.Sprintf("visit corpus %s: outside the -visits-window, not read\n", paths[i])
			continue
		}
		e, r, st, err := policynet.LoadVisits(paths[i], o)
		if err != nil {
			return nil, 0, err
		}
		if loaded && st.Features != fs {
			return nil, 0, fmt.Errorf("visit corpus %s is feature set %s, later ones %s", paths[i], st.Features, fs)
		}
		fs, loaded = st.Features, true
		if a.tdLambda > 0 {
			y, ok := policynet.VisitTDTargets(r, a.tdLambda)
			for k := range e {
				e[k].TDTarget, e[k].HasTDTarget = y[k], ok[k]
			}
		}
		kept := len(e)
		if a.window > 0 && kept > need {
			e = append([]policynet.Example(nil), e[kept-need:]...) // frees the file's older records
			kept = need
		}
		need -= kept
		parts[i] = e
		lines[i] = fmt.Sprintf("visit corpus %s: %d records, %d loaded from %d games (%d with outcome, %d teacher overrides %.1f%%), kept %d, world %s sims %d, features %s\n",
			paths[i], st.Records, st.Loaded, st.Games, st.WithOutcome, st.Overrides, 100*ratio(st.Overrides, st.Loaded), kept, st.World, st.Sims, st.Features)
	}
	var exs []policynet.Example
	for i := range paths {
		io.WriteString(stdout, lines[i])
		exs = append(exs, parts[i]...)
		parts[i] = nil
	}
	return exs, fs, nil
}

// checkInit refuses a -init checkpoint whose architecture is not the run's.
func checkInit(m *policynet.Model, cfg Config) error {
	switch {
	case m.Rows != policynet.TableRows || m.H != cfg.Embed || m.Hidden != cfg.Hidden || m.ExtraW != cfg.ExtraW:
		return fmt.Errorf("policytrain: -init is rows %d embed %d hidden %d, this run rows %d embed %d hidden %d (-embed/-hidden must match the checkpoint)",
			m.Rows, m.H, m.Hidden, policynet.TableRows, cfg.Embed, cfg.Hidden)
	case m.EntK != 0:
		return fmt.Errorf("policytrain: -init carries an entity encoder (width %d); this trainer continues the mz geometry only", m.EntK)
	case cfg.ValueWeight > 0 && m.ValueHidden != cfg.ValueHidden:
		return fmt.Errorf("policytrain: -init has value-hidden %d, this run %d (-value-hidden must match the checkpoint)", m.ValueHidden, cfg.ValueHidden)
	case cfg.ResidualInit != 0 && float32(cfg.ResidualInit) != m.ResidualW:
		return fmt.Errorf("policytrain: -init has residual %g, -residual-init is %g (a continued run keeps the checkpoint's)", m.ResidualW, cfg.ResidualInit)
	}
	return nil
}
