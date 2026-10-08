//go:build gamepool && gamepool_gpu

package gamepool

import "testing"

func synthBatch() *flatBatch {
	fb := &flatBatch{offsets: []int32{0, 3, 5}}
	fb.states = make([]float32, 2*flatStateDim)
	fb.actions = make([]float32, 5*flatActionDim)
	for i := range fb.actions {
		fb.actions[i] = float32(i%7) * 0.01
	}
	fb.actionOwner = []int32{0, 0, 0, 1, 1}
	return fb
}

// TestGPUForward runs one synthetic batch through the device forward, to
// isolate a crash in the driver plumbing from the pool scheduler.
func TestGPUForward(t *testing.T) {
	sc, err := NewFlatGPUScorer("/home/sadams/projects/gorge/scratch/gpupoc/artifacts/flat_train.cudafile")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	logits := sc.dev.forward(synthBatch())
	if len(logits) != 5 {
		t.Fatalf("logits = %d, want 5", len(logits))
	}
	t.Logf("main-thread logits: %v", logits)
}

// TestGPUForwardOtherGoroutine runs the forward from a fresh goroutine, the
// way the pool's batch goroutine does.
func TestGPUForwardOtherGoroutine(t *testing.T) {
	sc, err := NewFlatGPUScorer("/home/sadams/projects/gorge/scratch/gpupoc/artifacts/flat_train.cudafile")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	done := make(chan []float32, 1)
	go func() {
		done <- sc.dev.forward(synthBatch())
	}()
	logits := <-done
	if len(logits) != 5 {
		t.Fatalf("logits = %d, want 5", len(logits))
	}
	t.Logf("other-goroutine logits: %v", logits)
}
