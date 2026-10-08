//go:build gamepool && gamepool_gpu

package gamepool

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"unsafe"
)

// The flat training net's geometry (contract constants, same as the scratch
// POC and native_flat_cpu_reference_v1.rs). Only the forward is used here.
const (
	flatStateDim  = 2048
	flatActionDim = 128
	flatHidden    = 64
	flatNumTensors = 14
)

// flatShapes are the 14 tensors' element counts, in cudafile blob order.
var flatShapes = [flatNumTensors]int{
	flatStateDim * flatHidden, flatHidden, // state_w1, state_b1
	flatHidden * flatHidden, flatHidden, // state_w2, state_b2
	flatActionDim * flatHidden, flatHidden, // action_w, action_b
	flatHidden * flatHidden, flatHidden * flatHidden, flatHidden, flatHidden, // scorer_state_w, scorer_action_w, scorer_b, scorer_out_w
	flatHidden * flatHidden, flatHidden, flatHidden, 1, // value_w1, value_b1, value_out_w, value_out_b
}

// flatArtifact is a parsed flat cudafile: the training-kernel PTX, the optional
// multi-block forward PTX, and the host tensor blob.
type flatArtifact struct {
	ptx, ptx2 string
	blob      []byte
}

func looksLikePTX(b []byte) bool {
	return len(b) >= 16 && bytes.HasPrefix(b, []byte("//\n// Gene"))
}

// loadFlatArtifact parses a flat_train cudafile. Layout (see the POC's
// cudafile.go): magic "CUDF", version, d, rows, nameLen, ptxLen, blobLen, name,
// ptx, blob -- where the blob may lead with a length-prefixed second PTX.
func loadFlatArtifact(path string) (*flatArtifact, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 24 || string(b[:4]) != "CUDF" {
		return nil, fmt.Errorf("gamepool: %s: bad cudafile magic", path)
	}
	off := 4
	rd := func() uint32 { v := binary.LittleEndian.Uint32(b[off:]); off += 4; return v }
	ver := rd()
	if ver != 1 {
		return nil, fmt.Errorf("gamepool: %s: cudafile version %d", path, ver)
	}
	rd() // d
	rd() // rows
	nameLen, ptxLen, blobLen := int(rd()), int(rd()), int(rd())
	if off+nameLen+ptxLen+blobLen > len(b) {
		return nil, fmt.Errorf("gamepool: %s: truncated", path)
	}
	name := string(b[off : off+nameLen])
	ptx := string(b[off+nameLen : off+nameLen+ptxLen])
	off += nameLen + ptxLen
	blob := b[off : off+blobLen]
	if name != "flat_train" {
		return nil, fmt.Errorf("gamepool: %s: not a flat_train artifact (name %q)", path, name)
	}
	a := &flatArtifact{ptx: ptx}
	if len(blob) >= 4 {
		if n := int(binary.LittleEndian.Uint32(blob[:4])); n > 0 && 4+n <= len(blob) && looksLikePTX(blob[4:4+n]) {
			a.ptx2 = string(blob[4 : 4+n])
			blob = blob[4+n:]
		}
	}
	a.blob = append([]byte(nil), blob...)
	return a, nil
}

// flatDevice holds the JIT'd forward kernels and the device weight buffers.
type flatDevice struct {
	fnH1, fnH2, fnValueH, fnValueOut, fnAction, fnGather, fnScorer, fnLogits uintptr
	dVal                                                                    [flatNumTensors]uint64

	dStates, dActions, dOffsets, dOwner                             uint64
	dStateH1, dStateH2, dActionH, dStateForActions, dScorerH, dLogits uint64
	dValueH, dValues                                                uint64

	capBatch, capActions int
}

func kernel(module uintptr, name string) uintptr {
	bindCtx()
	var fn uintptr
	nb := append([]byte(name), 0)
	if r := cuModuleGetFunction(&fn, module, &nb[0]); r != cudaSuccess {
		panic("gamepool: cuModuleGetFunction " + name + ": " + cudaErr(r))
	}
	return fn
}

func loadModule(ptx string) uintptr {
	bindCtx()
	var mod uintptr
	p := append([]byte(ptx), 0)
	if r := cuModuleLoadData(&mod, uintptr(unsafe.Pointer(&p[0]))); r != cudaSuccess {
		panic("gamepool: cuModuleLoadData: " + cudaErr(r))
	}
	return mod
}

// newFlatDevice loads both PTX modules, resolves the seven forward phases and
// uploads the 14 tensors.
func newFlatDevice(a *flatArtifact) (*flatDevice, error) {
	if a.ptx2 == "" {
		return nil, fmt.Errorf("gamepool: artifact has no multi-block forward PTX")
	}
	cudaInit()
	// The ff_* phases live in the SECOND PTX module (flatfwd.ptx); the
	// training PTX (a.ptx) has only flat_forward/backward/adam, unused here.
	mod := loadModule(a.ptx2)
	g := &flatDevice{
		fnH1:       kernel(mod, "ff_state_h1"),
		fnH2:       kernel(mod, "ff_state_h2"),
		fnValueH:   kernel(mod, "ff_value_h"),
		fnValueOut: kernel(mod, "ff_valueout"),
		fnAction:   kernel(mod, "ff_action"),
		fnGather:   kernel(mod, "ff_gather"),
		fnScorer:   kernel(mod, "ff_scorer"),
		fnLogits:   kernel(mod, "ff_logits"),
	}
	if err := g.uploadWeights(a.blob); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *flatDevice) uploadWeights(blob []byte) error {
	if len(blob) != flatTotalFloats()*4 {
		return fmt.Errorf("gamepool: blob %d bytes, want %d", len(blob), flatTotalFloats()*4)
	}
	off := 0
	for i, n := range flatShapes {
		floats := make([]float32, n)
		for j := range floats {
			floats[j] = math.Float32frombits(binary.LittleEndian.Uint32(blob[(off+j)*4:]))
		}
		off += n
		bindCtx()
		if r := cuMemAlloc(&g.dVal[i], uint64(n*4)); r != cudaSuccess {
			return fmt.Errorf("gamepool: cuMemAlloc tensor %d: %s", i, cudaErr(r))
		}
		if n > 0 {
			if r := cuMemcpyHtoD(g.dVal[i], uintptr(unsafe.Pointer(&floats[0])), uint64(n*4)); r != cudaSuccess {
				return fmt.Errorf("gamepool: HtoD tensor %d: %s", i, cudaErr(r))
			}
		}
	}
	return nil
}

func flatTotalFloats() int {
	n := 0
	for _, s := range flatShapes {
		n += s
	}
	return n
}

func (g *flatDevice) alloc(bytes uint64) uint64 {
	bindCtx()
	var d uint64
	if r := cuMemAlloc(&d, bytes); r != cudaSuccess {
		panic("gamepool: cuMemAlloc: " + cudaErr(r))
	}
	return d
}

func (g *flatDevice) ensureBatch(batch, actions int) {
	if batch <= g.capBatch && actions <= g.capActions {
		return
	}
	free := func(p *uint64) {
		if *p != 0 {
			cuMemFree(*p)
			*p = 0
		}
	}
	if batch > g.capBatch {
		for _, p := range []*uint64{&g.dStates, &g.dOffsets, &g.dStateH1, &g.dStateH2,
			&g.dValueH, &g.dValues} {
			free(p)
		}
		g.dStates = g.alloc(uint64(batch * flatStateDim * 4))
		g.dOffsets = g.alloc(uint64((batch + 1) * 4))
		g.dStateH1 = g.alloc(uint64(batch * flatHidden * 4))
		g.dStateH2 = g.alloc(uint64(batch * flatHidden * 4))
		g.dValueH = g.alloc(uint64(batch * flatHidden * 4))
		g.dValues = g.alloc(uint64(batch * 4))
		g.capBatch = batch
	}
	if actions > g.capActions {
		for _, p := range []*uint64{&g.dActions, &g.dOwner, &g.dActionH, &g.dStateForActions,
			&g.dScorerH, &g.dLogits} {
			free(p)
		}
		g.dActions = g.alloc(uint64(actions * flatActionDim * 4))
		g.dOwner = g.alloc(uint64(actions * 4))
		g.dActionH = g.alloc(uint64(actions * flatHidden * 4))
		g.dStateForActions = g.alloc(uint64(actions * flatHidden * 4))
		g.dScorerH = g.alloc(uint64(actions * flatHidden * 4))
		g.dLogits = g.alloc(uint64(actions * 4))
		g.capActions = actions
	}
}

// flatBatch is the ragged decision batch: one row per decision, one action per
// option across all decisions.
type flatBatch struct {
	states      []float32
	actions     []float32
	offsets     []int32
	actionOwner []int32
}

func (b *flatBatch) batch() int { return len(b.offsets) - 1 }
func (b *flatBatch) totalActions() int { return len(b.actionOwner) }

// forward launches the seven-phase multi-block forward and returns the per
// action logits.
func (g *flatDevice) forward(b *flatBatch) []float32 {
	nb, na := b.batch(), b.totalActions()
	g.ensureBatch(nb, na)
	g.htodF(g.dStates, b.states)
	g.htodF(g.dActions, b.actions)
	g.htodI32(g.dOffsets, b.offsets)
	g.htodI32(g.dOwner, b.actionOwner)

	blocks := func(work int) uint32 {
		n := uint32((work + 255) / 256)
		if n == 0 {
			n = 1
		}
		if n > 8192 {
			n = 8192
		}
		return n
	}
	nb32, na32 := int32(nb), int32(na)
	launch(g.fnH1, blocks(nb*flatHidden), 256, []uint64{g.dStates, g.dVal[0], g.dVal[1], g.dStateH1}, []int32{nb32})
	launch(g.fnH2, blocks(nb*flatHidden), 256, []uint64{g.dStateH1, g.dVal[2], g.dVal[3], g.dStateH2}, []int32{nb32})
	launch(g.fnValueH, blocks(nb*flatHidden), 256, []uint64{g.dStateH2, g.dVal[10], g.dVal[11], g.dValueH}, []int32{nb32})
	launch(g.fnValueOut, blocks(nb), 256, []uint64{g.dValueH, g.dVal[12], g.dVal[13], g.dValues}, []int32{nb32})
	launch(g.fnAction, blocks(na*flatHidden), 256, []uint64{g.dActions, g.dVal[4], g.dVal[5], g.dActionH}, []int32{na32})
	launch(g.fnGather, blocks(na), 256, []uint64{g.dStateH2, g.dOwner, g.dStateForActions}, []int32{na32})
	launch(g.fnScorer, blocks(na*flatHidden), 256, []uint64{g.dStateForActions, g.dActionH, g.dVal[6], g.dVal[7], g.dVal[8], g.dScorerH}, []int32{na32})
	launch(g.fnLogits, blocks(na), 256, []uint64{g.dScorerH, g.dVal[9], g.dLogits}, []int32{na32})

	logits := make([]float32, na)
	g.dtoh(logits, g.dLogits)
	return logits
}

func (g *flatDevice) htodF(d uint64, f []float32) {
	bindCtx()
	if len(f) == 0 {
		return
	}
	if r := cuMemcpyHtoD(d, uintptr(unsafe.Pointer(&f[0])), uint64(len(f)*4)); r != cudaSuccess {
		panic("gamepool: HtoD: " + cudaErr(r))
	}
}

func (g *flatDevice) htodI32(d uint64, f []int32) {
	bindCtx()
	if len(f) == 0 {
		return
	}
	if r := cuMemcpyHtoD(d, uintptr(unsafe.Pointer(&f[0])), uint64(len(f)*4)); r != cudaSuccess {
		panic("gamepool: HtoD i32: " + cudaErr(r))
	}
}

func (g *flatDevice) dtoh(f []float32, d uint64) {
	bindCtx()
	if len(f) == 0 {
		return
	}
	if r := cuMemcpyDtoH(uintptr(unsafe.Pointer(&f[0])), d, uint64(len(f)*4)); r != cudaSuccess {
		panic("gamepool: DtoH: " + cudaErr(r))
	}
}

// launch packs pointers-to-args (cuLaunchKernel wants an array of pointers).
func launch(fn uintptr, blocks, threads uint32, ptrVals []uint64, ints []int32) {
	bindCtx()
	ptrs := make([]uintptr, 0, len(ptrVals)+len(ints))
	for i := range ptrVals {
		ptrs = append(ptrs, uintptr(unsafe.Pointer(&ptrVals[i])))
	}
	for i := range ints {
		ptrs = append(ptrs, uintptr(unsafe.Pointer(&ints[i])))
	}
	if r := cuLaunchKernel(fn, blocks, 1, 1, threads, 1, 1, 0, 0, uintptr(unsafe.Pointer(&ptrs[0])), 0); r != cudaSuccess {
		panic("gamepool: cuLaunchKernel: " + cudaErr(r))
	}
	bindCtx()
	if r := cuCtxSynchronize(); r != cudaSuccess {
		panic("gamepool: cuCtxSynchronize: " + cudaErr(r))
	}
}
