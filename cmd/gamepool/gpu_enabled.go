//go:build gamepool && gamepool_gpu

package main

import (
	"fmt"

	"github.com/adams-shaun/gorge/internal/gamepool"
)

// newScorer resolves -scorer for a build with the GPU tag.
func newScorer(name, artifact string) (gamepool.Scorer, error) {
	switch name {
	case "", "local":
		return gamepool.LocalScorer{}, nil
	case "gpu":
		return gamepool.NewFlatGPUScorer(artifact)
	default:
		return nil, fmt.Errorf("gamepool: unknown scorer %q (want local or gpu)", name)
	}
}
