package mzbridge

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestNPYBytes(t *testing.T) {
	var b bytes.Buffer
	if err := WriteNPYInt32(&b, []int32{1, -2, 2147483646}); err != nil {
		t.Fatal(err)
	}
	// the exact bytes numpy.save writes for np.array([1,-2,2147483646], '<i4')
	// in format 1.0: a 118-byte padded dict, data at offset 128
	head := "\x93NUMPY\x01\x00\x76\x00{'descr': '<i4', 'fortran_order': False, 'shape': (3,), }"
	want := []byte(head + strings.Repeat(" ", 127-len(head)) + "\n" +
		"\x01\x00\x00\x00\xfe\xff\xff\xff\xfe\xff\xff\x7f")
	if !bytes.Equal(b.Bytes(), want) {
		t.Fatalf("npy bytes:\n got %q\nwant %q", b.Bytes(), want)
	}

	b.Reset()
	if err := WriteNPYInt64(&b, []int64{0, 1 << 40}); err != nil {
		t.Fatal(err)
	}
	descr, shape, data := parseNPY(t, b.Bytes())
	if descr != "<i8" || !slices.Equal(shape, []string{"2"}) || len(data) != 16 ||
		binary.LittleEndian.Uint64(data[8:]) != 1<<40 {
		t.Fatalf("int64: %q %v % x", descr, shape, data)
	}

	b.Reset()
	if err := WriteNPYFloat32(&b, []float32{0, 1.5, -1, 3, 4, 5}, 2, 3); err != nil {
		t.Fatal(err)
	}
	descr, shape, data = parseNPY(t, b.Bytes())
	if descr != "<f4" || !slices.Equal(shape, []string{"2", "3"}) || len(data) != 24 ||
		math.Float32frombits(binary.LittleEndian.Uint32(data[4:])) != 1.5 {
		t.Fatalf("float32: %q %v % x", descr, shape, data)
	}
	if err := WriteNPYFloat32(&b, []float32{0}, 2, 3); err == nil {
		t.Fatal("a short matrix was accepted")
	}

	// an empty matrix keeps its column count, and every header is 64-aligned
	b.Reset()
	if err := WriteNPYFloat32(&b, nil, 0, 1028); err != nil {
		t.Fatal(err)
	}
	if descr, shape, data = parseNPY(t, b.Bytes()); !slices.Equal(shape, []string{"0", "1028"}) || len(data) != 0 {
		t.Fatalf("empty matrix: %v, %d data bytes", shape, len(data))
	}
	if !bytes.HasPrefix(b.Bytes(), []byte("\x93NUMPY\x01\x00\x76\x00{'descr': '<f4', 'fortran_order': False, 'shape': (0, 1028), }  ")) {
		t.Fatalf("2-d header differs from numpy's: %q", b.Bytes()[:80])
	}
}

// parseNPY checks the format-1.0 framing and returns descr, the shape's
// entries and the payload.
func parseNPY(t *testing.T, b []byte) (string, []string, []byte) {
	t.Helper()
	if len(b) < 10 || string(b[:6]) != "\x93NUMPY" || b[6] != 1 || b[7] != 0 {
		t.Fatalf("bad magic/version % x", b[:min(len(b), 10)])
	}
	hl := int(binary.LittleEndian.Uint16(b[8:]))
	if (10+hl)%64 != 0 {
		t.Fatalf("data starts at %d, not 64-aligned", 10+hl)
	}
	h := string(b[10 : 10+hl])
	if !strings.HasSuffix(h, "\n") || strings.Contains(strings.TrimRight(h[:len(h)-1], " "), "\n") {
		t.Fatalf("header padding: %q", h)
	}
	var descr, shape string
	if _, after, ok := strings.Cut(h, "'descr': '"); ok {
		descr, _, _ = strings.Cut(after, "'")
	}
	if !strings.Contains(h, "'fortran_order': False") {
		t.Fatalf("no fortran_order: %q", h)
	}
	if _, after, ok := strings.Cut(h, "'shape': ("); ok {
		shape, _, _ = strings.Cut(after, ")")
	}
	var dims []string
	for _, d := range strings.Split(shape, ",") {
		if d = strings.TrimSpace(d); d != "" {
			dims = append(dims, d)
		}
	}
	return descr, dims, b[10+hl:]
}

func testShard(t *testing.T) *Shard {
	t.Helper()
	s := NewShard(8)
	add := func(r Record) {
		t.Helper()
		if err := s.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	add(Record{IDs: []int32{30, 10, 20, 10}, Policy: []PolicyEntry{{0, 5}, {3, 2}, {3, 1}}, Value: 0.5, Score: 0.25, IsPlayer: true, Type: Priority})
	add(Record{IDs: nil, Policy: []PolicyEntry{{7, 4}}, Value: -1, Score: -0.5, IsPlayer: true, Type: ChooseTarget})
	s.EndGame()
	add(Record{IDs: []int32{2147483646, 0}, Policy: []PolicyEntry{{1, 9}, {0, 1}}, Value: 1, Score: 0, IsPlayer: false, Type: ChooseUse})
	return s
}

func TestShardLayout(t *testing.T) {
	s := testShard(t)
	dir := t.TempDir()
	stem := filepath.Join(dir, "s")
	if err := s.WriteFiles(stem); err != nil {
		t.Fatal(err)
	}
	if s.Rows() != 3 || s.Games() != 2 || s.Dim() != 8 {
		t.Fatalf("rows %d games %d", s.Rows(), s.Games())
	}
	read := func(suffix string) (string, []string, []byte) {
		b, err := os.ReadFile(stem + suffix)
		if err != nil {
			t.Fatal(err)
		}
		return parseNPY(t, b)
	}
	descr, shape, data := read(IndicesSuffix)
	var ids []int32
	for i := 0; i < len(data); i += 4 {
		ids = append(ids, int32(binary.LittleEndian.Uint32(data[i:])))
	}
	if descr != "<i4" || !slices.Equal(shape, []string{"5"}) || !slices.Equal(ids, []int32{10, 20, 30, 0, 2147483646}) {
		t.Fatalf("indices %q %v %v", descr, shape, ids)
	}
	i64 := func(data []byte) (out []int64) {
		for i := 0; i < len(data); i += 8 {
			out = append(out, int64(binary.LittleEndian.Uint64(data[i:])))
		}
		return out
	}
	if descr, _, data = read(OffsetsSuffix); descr != "<i8" || !slices.Equal(i64(data), []int64{0, 3, 3, 5}) {
		t.Fatalf("offsets %q %v", descr, i64(data))
	}
	if descr, _, data = read(GameOffsetsSuffix); descr != "<i8" || !slices.Equal(i64(data), []int64{0, 2, 3}) {
		t.Fatalf("game_offsets %q %v", descr, i64(data))
	}
	descr, shape, data = read(RowSuffix)
	var rows []float32
	for i := 0; i < len(data); i += 4 {
		rows = append(rows, math.Float32frombits(binary.LittleEndian.Uint32(data[i:])))
	}
	want := []float32{
		5, 0, 0, 3, 0, 0, 0, 0, 0.5, 0.25, 1, 0,
		0, 0, 0, 0, 0, 0, 0, 4, -1, -0.5, 1, 3,
		1, 9, 0, 0, 0, 0, 0, 0, 1, 0, 0, 5,
	}
	if descr != "<f4" || !slices.Equal(shape, []string{"3", "12"}) || !slices.Equal(rows, want) {
		t.Fatalf("row %q %v\n got %v\nwant %v", descr, shape, rows, want)
	}
}

func TestShardRejectsAndRelabels(t *testing.T) {
	s := NewShard(4)
	for name, r := range map[string]Record{
		"index past A":   {Policy: []PolicyEntry{{4, 1}}},
		"negative index": {Policy: []PolicyEntry{{-1, 1}}},
		"negative count": {Policy: []PolicyEntry{{0, -1}}},
		"NaN count":      {Policy: []PolicyEntry{{0, float32(math.NaN())}}},
		"value past 1":   {Value: 1.5},
		"NaN value":      {Value: float32(math.NaN())},
		"NaN score":      {Score: float32(math.NaN())},
	} {
		if err := s.Append(r); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if s.Rows() != 0 {
		t.Fatalf("a rejected record left %d rows", s.Rows())
	}
	if err := s.Append(Record{Score: 0.75, Policy: []PolicyEntry{{1, 1}}}); err != nil {
		t.Fatal(err)
	}
	if s.Score(0) != 0.75 {
		t.Fatalf("Score = %v", s.Score(0))
	}
	if err := s.SetValue(0, -0.25); err != nil || s.rows[4] != -0.25 {
		t.Fatalf("SetValue: %v %v", err, s.rows)
	}
	if s.SetValue(1, 0) == nil || s.SetValue(0, 2) == nil {
		t.Fatal("SetValue accepted a bad row or value")
	}
	// an empty shard still writes four well-formed files
	e := NewShard(1024)
	if err := e.WriteFiles(filepath.Join(t.TempDir(), "e")); err != nil || e.Games() != 0 {
		t.Fatalf("empty shard: %v, %d games", err, e.Games())
	}
}

// TestShardLoadsInMageZero converts a shard with scripts/mzrepro/npy2h5.py
// and loads the result with MageZero's own dataset.H5Indexed. It needs the
// upstream Python environment, so it runs only when pointed at one:
//
//	MZBRIDGE_PYTHON=<venv>/bin/python MZBRIDGE_MAGEZERO=<MageZero>/src/magezero
func TestShardLoadsInMageZero(t *testing.T) {
	py, mz := os.Getenv("MZBRIDGE_PYTHON"), os.Getenv("MZBRIDGE_MAGEZERO")
	if py == "" || mz == "" {
		t.Skip("MZBRIDGE_PYTHON / MZBRIDGE_MAGEZERO unset: the MageZero reader check did not run")
	}
	s := testShard(t)
	dir := t.TempDir()
	stem := filepath.Join(dir, "session1_A_cur")
	if err := s.WriteFiles(stem); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(py, "../../scripts/mzrepro/npy2h5.py", "--dim", "8", "--remove", stem, stem+".hdf5").CombinedOutput()
	if err != nil {
		t.Fatalf("npy2h5: %v\n%s", err, out)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.npy")); len(m) != 0 {
		t.Fatalf("--remove left %v", m)
	}
	const script = `
import json, sys
sys.path.insert(0, sys.argv[1])
import h5py
from dataset import H5Indexed, collate_batch
ds = H5Indexed(sys.argv[2])
rec = []
for k in range(len(ds)):
    idx, pol, val, isp, typ = ds[k]
    rec.append({"ids": idx.tolist(), "policy": pol.tolist(), "value": val.item(), "is_player": isp.item(), "type": typ.item()})
with h5py.File(ds.files[0], "r") as f:
    games = f["/game_offsets"][...].tolist()
    dt = [str(f[n].dtype) for n in ("indices", "offsets", "row", "game_offsets")]
idxs, offs, pols, vals, isps, typs = collate_batch([ds[k] for k in range(len(ds))])
print(json.dumps({"N": len(ds), "A": ds.A, "rec": rec, "games": games, "dtypes": dt, "batch_offsets": offs.tolist(), "batch_ids": idxs.tolist()}))
`
	cmd := exec.Command(py, "-c", script, mz, dir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("H5Indexed: %v\n%s", err, stderr.String())
	}
	var got struct {
		N, A int
		Rec  []struct {
			IDs      []int32   `json:"ids"`
			Policy   []float32 `json:"policy"`
			Value    float32   `json:"value"`
			IsPlayer float32   `json:"is_player"`
			Type     int       `json:"type"`
		} `json:"rec"`
		Games        []int64  `json:"games"`
		Dtypes       []string `json:"dtypes"`
		BatchOffsets []int64  `json:"batch_offsets"`
		BatchIDs     []int64  `json:"batch_ids"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	if got.N != 3 || got.A != 8 || !slices.Equal(got.Games, []int64{0, 2, 3}) ||
		!slices.Equal(got.Dtypes, []string{"int32", "int64", "float32", "int64"}) {
		t.Fatalf("N %d A %d games %v dtypes %v", got.N, got.A, got.Games, got.Dtypes)
	}
	wantIDs := [][]int32{{10, 20, 30}, {}, {0, 2147483646}}
	wantPol := [][]float32{{5, 0, 0, 3, 0, 0, 0, 0}, {0, 0, 0, 0, 0, 0, 0, 4}, {1, 9, 0, 0, 0, 0, 0, 0}}
	wantVal := []float32{0.5, -1, 1}
	wantIsP := []float32{1, 1, 0}
	wantTyp := []int{0, 3, 5}
	for i, r := range got.Rec {
		if !slices.Equal(append([]int32{}, r.IDs...), wantIDs[i]) || !slices.Equal(r.Policy, wantPol[i]) ||
			r.Value != wantVal[i] || r.IsPlayer != wantIsP[i] || r.Type != wantTyp[i] {
			t.Errorf("record %d as MageZero reads it: %+v", i, r)
		}
	}
	if !slices.Equal(got.BatchOffsets, []int64{0, 3, 3}) || !slices.Equal(got.BatchIDs, []int64{10, 20, 30, 0, 2147483646}) {
		t.Fatalf("collate_batch: offsets %v ids %v", got.BatchOffsets, got.BatchIDs)
	}
}

// TestConverterRefusesBadShard: npy2h5.py exits non-zero and writes nothing
// for a shard whose arrays disagree. Gated like TestShardLoadsInMageZero.
func TestConverterRefusesBadShard(t *testing.T) {
	py := os.Getenv("MZBRIDGE_PYTHON")
	if py == "" {
		t.Skip("MZBRIDGE_PYTHON unset: the converter check did not run")
	}
	corrupt := map[string]func(stem string) error{
		"offsets end early": func(stem string) error {
			return writeFile(stem+OffsetsSuffix, func(w io.Writer) error { return WriteNPYInt64(w, []int64{0, 3, 3, 4}) })
		},
		"unsorted state": func(stem string) error {
			return writeFile(stem+IndicesSuffix, func(w io.Writer) error { return WriteNPYInt32(w, []int32{10, 30, 20, 0, 5}) })
		},
		"repeated id": func(stem string) error {
			return writeFile(stem+IndicesSuffix, func(w io.Writer) error { return WriteNPYInt32(w, []int32{10, 20, 20, 0, 5}) })
		},
		"games past N": func(stem string) error {
			return writeFile(stem+GameOffsetsSuffix, func(w io.Writer) error { return WriteNPYInt64(w, []int64{0, 2, 4}) })
		},
		"wrong dtype": func(stem string) error {
			return writeFile(stem+OffsetsSuffix, func(w io.Writer) error { return WriteNPYInt32(w, []int32{0, 3, 3, 5}) })
		},
		"missing part": func(stem string) error { return os.Remove(stem + RowSuffix) },
	}
	for name, breakIt := range corrupt {
		dir := t.TempDir()
		stem := filepath.Join(dir, "s")
		if err := testShard(t).WriteFiles(stem); err != nil {
			t.Fatal(err)
		}
		if err := breakIt(stem); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(py, "../../scripts/mzrepro/npy2h5.py", stem, stem+".hdf5").CombinedOutput()
		if err == nil {
			t.Errorf("%s: converter accepted it\n%s", name, out)
		}
		if _, err := os.Stat(stem + ".hdf5"); err == nil {
			t.Errorf("%s: an HDF5 file was written", name)
		}
	}
	// and a wrong --dim
	dir := t.TempDir()
	stem := filepath.Join(dir, "s")
	if err := testShard(t).WriteFiles(stem); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(py, "../../scripts/mzrepro/npy2h5.py", "--dim", "1024", stem, stem+".hdf5").CombinedOutput(); err == nil {
		t.Errorf("--dim mismatch accepted\n%s", out)
	}
}

// TestWriteSyntheticDataset writes a deterministic synthetic run directory's
// worth of .npy shards (a training and a testing shard, A = 1024) for the
// trainer / server smoke described in docs: it runs only when
// MZBRIDGE_SYNTH_OUT names a directory, and writes
// <dir>/training/session1_A_cur.* and <dir>/testing/session2_A_cur.*.
func TestWriteSyntheticDataset(t *testing.T) {
	out := os.Getenv("MZBRIDGE_SYNTH_OUT")
	if out == "" {
		t.Skip("MZBRIDGE_SYNTH_OUT unset")
	}
	// a pool of real-shaped ids; each state draws ~150 of 1500, so most ids
	// clear the trainer's "active in more than 10 states" vocabulary rule
	pool := make([]int32, 1500)
	for i := range pool {
		pool[i] = FeatureID("synthetic"+strconv.Itoa(i)+"#1", GlobalSeed)
	}
	rng := rand.New(rand.NewPCG(20261002, 1))
	for _, part := range []struct {
		dir, stem string
		states    int
	}{{"training", "session1_A_cur", 640}, {"testing", "session2_A_cur", 128}} {
		s := NewShard(1024)
		for i := 0; i < part.states; i++ {
			r := Record{IsPlayer: true, Value: float32(rng.Float64()*2 - 1), Score: float32(rng.Float64()*2 - 1)}
			for j := 0; j < 150; j++ {
				r.IDs = append(r.IDs, pool[rng.IntN(len(pool))])
			}
			switch i % 4 {
			case 0, 1:
				r.Type = Priority
				r.Policy = []PolicyEntry{{0, float32(1 + rng.IntN(200))}, {22 + rng.IntN(483), float32(1 + rng.IntN(200))}, {644 + rng.IntN(380), float32(rng.IntN(50))}}
			case 2:
				r.Type = ChooseTarget
				r.Policy = []PolicyEntry{{1 + rng.IntN(2), float32(1 + rng.IntN(200))}, {3 + rng.IntN(527), float32(1 + rng.IntN(200))}}
			case 3:
				r.Type = ChooseUse
				r.Policy = []PolicyEntry{{0, float32(rng.IntN(300))}, {1, float32(1 + rng.IntN(300))}}
			}
			if err := s.Append(r); err != nil {
				t.Fatal(err)
			}
			if i%40 == 39 {
				s.EndGame()
			}
		}
		dir := filepath.Join(out, part.dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := s.WriteFiles(filepath.Join(dir, part.stem)); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: %d states, %d games", filepath.Join(dir, part.stem), s.Rows(), s.Games())
	}
}
