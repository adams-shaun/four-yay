//go:build gamepool && gamepool_gpu

package gamepool

import (
	"context"
	"runtime"

	"github.com/adams-shaun/gorge/decision"
)

// FlatGPUScorer serves a batch of decisions with ONE flat-net forward on the
// device. It is the point of the pool: many decisions that arrived at
// different times are packed into one ragged batch and scored in a single
// launch per phase.
//
// ALL driver use happens on one dedicated, OS-thread-locked goroutine: it
// loads the artifact, JITs the kernels and runs every forward. Serve hands it
// a batch over a channel and waits for the logits. This mirrors the scratch
// POC, where one thread owns cudaInit and every launch. An earlier design
// initialised the context on the caller's thread and then locked a second
// goroutine to call the driver; it segfaulted nondeterministically (no Go
// trace) mid-run and at process exit.
//
// The state vector is the mzenc MageZero feature-id set folded multi-hot into
// the flat net's 2048-wide surface (encodeStateMZ). The 128-wide action vectors
// are still a deterministic placeholder keyed on option identity (encodeAction),
// so the GPU result remains a throughput harness rather than a trained policy:
// a chosen option the engine would refuse falls back to the request's wrapped
// bot, so games still play legally.
type FlatGPUScorer struct {
	jobs  chan gpuJob
	ready chan error
	dev   *flatDevice // set by worker before ready; read only in tests
}

type gpuJob struct {
	fb  *flatBatch
	out chan []float32
}

// NewFlatGPUScorer loads a flat_train cudafile (PTX + weights) and JITs its
// forward for the device, on a dedicated goroutine.
func NewFlatGPUScorer(path string) (*FlatGPUScorer, error) {
	s := &FlatGPUScorer{jobs: make(chan gpuJob), ready: make(chan error, 1)}
	go s.worker(path)
	if err := <-s.ready; err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FlatGPUScorer) worker(path string) {
	runtime.LockOSThread()
	a, err := loadFlatArtifact(path)
	if err != nil {
		s.ready <- err
		return
	}
	dev, err := newFlatDevice(a)
	if err != nil {
		s.ready <- err
		return
	}
	s.dev = dev
	s.ready <- nil
	for j := range s.jobs {
		j.out <- dev.forward(j.fb)
	}
}

func (*FlatGPUScorer) Name() string { return "flatgpu" }

// Serve encodes every request's options as actions, runs the forward on the
// GPU goroutine, picks each decision's highest-logit option, and validates the
// resulting intent.
func (s *FlatGPUScorer) Serve(batch []*Request) error {
	fb := &flatBatch{offsets: make([]int32, len(batch)+1)}
	fb.states = make([]float32, len(batch)*flatStateDim)
	total := 0
	for i, r := range batch {
		fb.offsets[i] = int32(total)
		encodeStateMZ(r, fb.states[i*flatStateDim:(i+1)*flatStateDim])
		for _, o := range r.Decision.Options {
			fb.actionOwner = append(fb.actionOwner, int32(i))
			start := len(fb.actions)
			fb.actions = append(fb.actions, make([]float32, flatActionDim)...)
			encodeAction(&r.Decision, o, fb.actions[start:start+flatActionDim])
			total++
		}
	}
	fb.offsets[len(batch)] = int32(total)

	if total == 0 {
		s.fallbackAll(batch)
		return nil
	}
	out := make(chan []float32, 1)
	s.jobs <- gpuJob{fb: fb, out: out}
	logits := <-out

	for i, r := range batch {
		begin, end := int(fb.offsets[i]), int(fb.offsets[i+1])
		if end <= begin {
			s.fallback(r)
			continue
		}
		best := -1
		for j := begin; j < end; j++ {
			if r.Decision.Options[j-begin].Kind == "concede" {
				continue // the host owns concession; a random net must never pick it
			}
			if best < 0 || logits[j] > logits[best] {
				best = j
			}
		}
		if best < 0 {
			s.fallback(r)
			continue
		}
		opt := r.Decision.Options[best-begin]
		in := decision.Intent{Seq: r.Decision.Seq, Player: r.Decision.Player, Choices: []int{opt.Index}}
		if r.Decision.Validate(in) != nil {
			s.fallback(r)
			continue
		}
		r.Intent = in
	}
	return nil
}

func (s *FlatGPUScorer) fallbackAll(batch []*Request) {
	for _, r := range batch {
		s.fallback(r)
	}
}

func (s *FlatGPUScorer) fallback(r *Request) {
	if r.Inner == nil {
		return
	}
	if in, err := r.Inner.Decide(context.Background(), r.View, r.Decision); err == nil {
		r.Intent = in
	}
}

var _ Scorer = (*FlatGPUScorer)(nil)

// splitmix64 is a tiny deterministic PRNG (the flat net's own initializer
// family), used only to fill the encoder's placeholder vectors.
type splitmix64 struct{ s uint64 }

func (r *splitmix64) next() uint64 {
	r.s += 0x9e3779b97f4a7c15
	z := r.s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func (r *splitmix64) unit() float32 {
	return float32((r.next()>>40)&0xffffff)/16777216.0*2.0 - 1.0
}

// mix walks the decision's observable identity into a seed: seat, sequence,
// player, kind, turn, option count. Deterministic and map-free.
func mix(seed uint64, vals ...uint64) uint64 {
	for _, v := range vals {
		seed ^= v + 0x9e3779b97f4a7c15 + (seed << 6) + (seed >> 2)
	}
	return seed
}

func kindSeed(k decision.Kind) uint64 {
	h := uint64(1469598103934665603) // FNV offset
	for i := 0; i < len(k); i++ {
		h ^= uint64(k[i])
		h *= 1099511628211
	}
	return h
}

func encodeState(d *decision.Decision, seatIdx int, out []float32) {
	rng := splitmix64{s: mix(0x51ed, uint64(seatIdx), d.Seq, uint64(d.Player), kindSeed(d.Kind), uint64(len(d.Options)))}
	for i := range out {
		out[i] = rng.unit()
	}
}

func encodeAction(d *decision.Decision, o decision.Option, out []float32) {
	rng := splitmix64{s: mix(0xa0c7, kindSeed(d.Kind), uint64(o.Index), kindSeed(decision.Kind(o.Kind)), uint64(o.Obj), uint64(len(o.Label)))}
	for i := range out {
		out[i] = rng.unit()
	}
}
