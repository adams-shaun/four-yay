//go:build gamepool && gamepool_gpu

// The CUDA Driver API reached from pure Go, ported from scratch/gpupoc/cuda.go.
// libcuda.so.1 is dlopen'd at runtime and its entry points are resolved with
// dlsym (purego): no cgo, no libcudart, no nvcc at run time. The driver JITs
// the PTX shipped in the flat cudafile for the device's compute capability.
package gamepool

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

const cudaSuccess = 0

var (
	libcuda uintptr

	cuInit                   func(flags uint32) int
	cuDeviceGet              func(dev *int32, ordinal int32) int
	cuDeviceGetName          func(name *byte, ln int32, dev int32) int
	cuDeviceComputeCapability func(major, minor *int32, dev int32) int
	cuDevicePrimaryCtxRetain func(ctx *uintptr, dev int32) int
	cuCtxSetCurrent          func(ctx uintptr) int
	cuModuleLoadData         func(mod *uintptr, image uintptr) int
	cuModuleGetFunction      func(fn *uintptr, mod uintptr, name *byte) int
	cuMemAlloc               func(dptr *uint64, bytes uint64) int
	cuMemFree                func(dptr uint64) int
	cuMemcpyHtoD             func(dst uint64, src uintptr, bytes uint64) int
	cuMemcpyDtoH             func(dst uintptr, src uint64, bytes uint64) int
	cuLaunchKernel           func(fn uintptr,
		gx, gy, gz, bx, by, bz uint32,
		shmem uint32, stream uintptr,
		kparams uintptr, extra uintptr) int
	cuCtxSynchronize func() int
	cuGetErrorName   func(code int, name *uintptr) int
	cuMemsetD8       func(dst uint64, value uint8, count uint64) int
)

func mustSym(name string, fn any) {
	p, err := purego.Dlsym(libcuda, name)
	if err != nil || p == 0 {
		panic(fmt.Sprintf("gamepool: dlsym %s: %v", name, err))
	}
	purego.RegisterFunc(fn, p)
}

// gContext is the retained primary context; every driver entry point calls
// bindCtx first so it is current on the executing thread.
var gContext uintptr

func bindCtx() {
	if gContext != 0 {
		cuCtxSetCurrent(gContext)
	}
}

// cudaInit loads libcuda.so.1 and binds the Driver API, returning the first
// device ordinal, its name and compute capability. It pins the calling
// goroutine to its OS thread: a CUDA context is per-thread, and the scheduler
// could otherwise migrate between launch and synchronize.
func cudaInit() (dev int32, name string, ccMajor, ccMinor int32) {
	runtime.LockOSThread()
	var err error
	libcuda, err = purego.Dlopen("libcuda.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		libcuda, err = purego.Dlopen("libcuda.so", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	}
	if err != nil {
		panic(fmt.Sprintf("gamepool: dlopen libcuda: %v", err))
	}
	mustSym("cuInit", &cuInit)
	mustSym("cuDeviceGet", &cuDeviceGet)
	mustSym("cuDeviceGetName", &cuDeviceGetName)
	mustSym("cuDeviceComputeCapability", &cuDeviceComputeCapability)
	mustSym("cuDevicePrimaryCtxRetain", &cuDevicePrimaryCtxRetain)
	mustSym("cuCtxSetCurrent", &cuCtxSetCurrent)
	mustSym("cuModuleLoadData", &cuModuleLoadData)
	mustSym("cuModuleGetFunction", &cuModuleGetFunction)
	mustSym("cuMemAlloc_v2", &cuMemAlloc)
	mustSym("cuMemFree_v2", &cuMemFree)
	mustSym("cuMemcpyHtoD_v2", &cuMemcpyHtoD)
	mustSym("cuMemcpyDtoH_v2", &cuMemcpyDtoH)
	mustSym("cuLaunchKernel", &cuLaunchKernel)
	mustSym("cuCtxSynchronize", &cuCtxSynchronize)
	mustSym("cuGetErrorName", &cuGetErrorName)
	mustSym("cuMemsetD8_v2", &cuMemsetD8)

	if r := cuInit(0); r != cudaSuccess {
		panic(fmt.Sprintf("gamepool: cuInit: %s", cudaErr(r)))
	}
	dev = 0
	var cb [128]byte
	if r := cuDeviceGetName(&cb[0], int32(len(cb)), dev); r != cudaSuccess {
		panic("gamepool: cuDeviceGetName: " + cudaErr(r))
	}
	name = cstr(cb[:])
	if r := cuDeviceComputeCapability(&ccMajor, &ccMinor, dev); r != cudaSuccess {
		panic("gamepool: cuDeviceComputeCapability: " + cudaErr(r))
	}
	var ctx uintptr
	if r := cuDevicePrimaryCtxRetain(&ctx, dev); r != cudaSuccess {
		panic("gamepool: cuDevicePrimaryCtxRetain: " + cudaErr(r))
	}
	gContext = ctx
	if r := cuCtxSetCurrent(ctx); r != cudaSuccess {
		panic("gamepool: cuCtxSetCurrent: " + cudaErr(r))
	}
	return dev, name, ccMajor, ccMinor
}

func cudaErr(code int) string {
	var p uintptr
	if cuGetErrorName != nil && cuGetErrorName(code, &p) == cudaSuccess && p != 0 {
		return cstr(unsafe.Slice((*byte)(unsafe.Pointer(p)), 64))
	}
	return fmt.Sprintf("CUresult(%d)", code)
}

func cstr(b []byte) string {
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n])
}
