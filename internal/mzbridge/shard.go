package mzbridge

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
)

// A shard is one seat's training records for a batch of games, in the layout
// LabeledStateWriter.java writes and MageZero's dataset.H5Indexed reads:
//
//	/indices       int32   [nnz]     feature ids, ascending within a state
//	/offsets       int64   [N+1]     offsets[0] = 0, offsets[i+1] = end of state i
//	/row           float32 [N, A+4]  A visit counts, value target, root score,
//	                                 isPlayer (0/1), action type ordinal
//	/game_offsets  int64   [G+1]     row index where each game starts, then N
//
// gorge has no HDF5 writer and takes no dependency to get one: a Shard is
// written as four NumPy .npy files and scripts/mzrepro/npy2h5.py converts
// them to the one HDF5 file upstream expects.

// RowExtra is the number of scalar columns after the A policy columns.
const RowExtra = 4

// Record is one decision state.
type Record struct {
	// IDs is the state's feature ids. Append sorts and de-duplicates a copy.
	IDs []int32
	// Policy holds root visit counts by action index; indices outside
	// [0, A) are an error. Indices that repeat add up, as upstream adds the
	// visits of children sharing an index.
	Policy []PolicyEntry
	// Value is the training target (resultLabel) in [-1, 1].
	Value float32
	// Score is the root's mean value (stateScore); the trainer ignores it.
	Score float32
	// IsPlayer is true for the recording seat's own decisions.
	IsPlayer bool
	Type     ActionType
}

// PolicyEntry is one action index and its visit count.
type PolicyEntry struct {
	Index  int
	Visits float32
}

// Shard accumulates records in memory (4 KiB per record at A = 1024).
type Shard struct {
	dim         int
	indices     []int32
	offsets     []int64
	rows        []float32
	gameOffsets []int64
}

// NewShard returns an empty shard for an A-wide policy (Vocab.Dim).
func NewShard(actionDim int) *Shard {
	return &Shard{dim: actionDim, offsets: []int64{0}, gameOffsets: []int64{0}}
}

// Dim is A. Rows is N. Games is G.
func (s *Shard) Dim() int   { return s.dim }
func (s *Shard) Rows() int  { return len(s.offsets) - 1 }
func (s *Shard) Games() int { return len(s.gameOffsets) - 1 }

// Append adds one record to the current game.
func (s *Shard) Append(r Record) error {
	row := make([]float32, s.dim+RowExtra)
	for _, p := range r.Policy {
		if p.Index < 0 || p.Index >= s.dim {
			return fmt.Errorf("mzbridge: policy index %d outside [0, %d)", p.Index, s.dim)
		}
		if p.Visits < 0 || p.Visits != p.Visits {
			return fmt.Errorf("mzbridge: policy index %d has visit count %v", p.Index, p.Visits)
		}
		row[p.Index] += p.Visits
	}
	if !(r.Value >= -1 && r.Value <= 1) {
		return fmt.Errorf("mzbridge: value target %v outside [-1, 1]", r.Value)
	}
	if r.Score != r.Score {
		return errors.New("mzbridge: NaN root score")
	}
	row[s.dim] = r.Value
	row[s.dim+1] = r.Score
	if r.IsPlayer {
		row[s.dim+2] = 1
	}
	row[s.dim+3] = float32(r.Type)

	ids := slices.Clone(r.IDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	s.indices = append(s.indices, ids...)
	s.offsets = append(s.offsets, int64(len(s.indices)))
	s.rows = append(s.rows, row...)
	return nil
}

// SetValue overwrites the value target of row i: the game's labels are only
// known once it ends, after its records were appended.
func (s *Shard) SetValue(i int, v float32) error {
	if i < 0 || i >= s.Rows() {
		return fmt.Errorf("mzbridge: row %d of %d", i, s.Rows())
	}
	if !(v >= -1 && v <= 1) {
		return fmt.Errorf("mzbridge: value target %v outside [-1, 1]", v)
	}
	s.rows[i*(s.dim+RowExtra)+s.dim] = v
	return nil
}

// Score returns the root score stored for row i.
func (s *Shard) Score(i int) float32 { return s.rows[i*(s.dim+RowExtra)+s.dim+1] }

// EndGame closes the current game (LabeledStateWriter.endGame), also when it
// recorded nothing.
func (s *Shard) EndGame() { s.gameOffsets = append(s.gameOffsets, int64(s.Rows())) }

// The four files of a shard written under a common stem.
const (
	IndicesSuffix     = ".indices.npy"
	OffsetsSuffix     = ".offsets.npy"
	RowSuffix         = ".row.npy"
	GameOffsetsSuffix = ".game_offsets.npy"
)

// WriteFiles writes stem+".indices.npy", ".offsets.npy", ".row.npy" and
// ".game_offsets.npy". Records appended since the last EndGame are closed as
// a game first, so /game_offsets always ends at N.
func (s *Shard) WriteFiles(stem string) error {
	if g := s.gameOffsets[len(s.gameOffsets)-1]; g != int64(s.Rows()) {
		s.EndGame()
	}
	n := s.Rows()
	files := []struct {
		suffix string
		write  func(io.Writer) error
	}{
		{IndicesSuffix, func(w io.Writer) error { return WriteNPYInt32(w, s.indices) }},
		{OffsetsSuffix, func(w io.Writer) error { return WriteNPYInt64(w, s.offsets) }},
		{RowSuffix, func(w io.Writer) error { return WriteNPYFloat32(w, s.rows, n, s.dim+RowExtra) }},
		{GameOffsetsSuffix, func(w io.Writer) error { return WriteNPYInt64(w, s.gameOffsets) }},
	}
	for _, f := range files {
		if err := writeFile(stem+f.suffix, f.write); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(path string, write func(io.Writer) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	if err := write(w); err != nil {
		f.Close()
		return err
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// npyHeader is a NumPy format 1.0 header: the magic, version 1.0, a
// little-endian uint16 length, then a Python dict literal padded with spaces
// and a final newline so the data starts on a 64-byte boundary.
func npyHeader(descr string, shape ...int) []byte {
	dict := "{'descr': '" + descr + "', 'fortran_order': False, 'shape': ("
	for i, d := range shape {
		if i > 0 {
			dict += ", "
		}
		dict += strconv.Itoa(d)
	}
	if len(shape) == 1 {
		dict += "," // a one-element Python tuple
	}
	dict += "), }"
	const prefix = 10 // magic(6) + version(2) + length(2)
	pad := 64 - (prefix+len(dict)+1)%64
	if pad == 64 {
		pad = 0
	}
	h := make([]byte, 0, prefix+len(dict)+pad+1)
	h = append(h, 0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0)
	h = binary.LittleEndian.AppendUint16(h, uint16(len(dict)+pad+1))
	h = append(h, dict...)
	for i := 0; i < pad; i++ {
		h = append(h, ' ')
	}
	return append(h, '\n')
}

// WriteNPYInt32 writes a one-dimensional little-endian int32 array.
func WriteNPYInt32(w io.Writer, v []int32) error {
	if _, err := w.Write(npyHeader("<i4", len(v))); err != nil {
		return err
	}
	return writeChunks(w, len(v), 4, func(b []byte, i int) { binary.LittleEndian.PutUint32(b, uint32(v[i])) })
}

// WriteNPYInt64 writes a one-dimensional little-endian int64 array.
func WriteNPYInt64(w io.Writer, v []int64) error {
	if _, err := w.Write(npyHeader("<i8", len(v))); err != nil {
		return err
	}
	return writeChunks(w, len(v), 8, func(b []byte, i int) { binary.LittleEndian.PutUint64(b, uint64(v[i])) })
}

// WriteNPYFloat32 writes a rows x cols little-endian float32 matrix in C
// (row-major) order.
func WriteNPYFloat32(w io.Writer, v []float32, rows, cols int) error {
	if len(v) != rows*cols {
		return fmt.Errorf("mzbridge: %d values for a %d x %d matrix", len(v), rows, cols)
	}
	if _, err := w.Write(npyHeader("<f4", rows, cols)); err != nil {
		return err
	}
	return writeChunks(w, len(v), 4, func(b []byte, i int) { binary.LittleEndian.PutUint32(b, math.Float32bits(v[i])) })
}

func writeChunks(w io.Writer, n, size int, put func(b []byte, i int)) error {
	const chunk = 8192
	buf := make([]byte, chunk*size)
	for i := 0; i < n; {
		m := min(chunk, n-i)
		for j := 0; j < m; j++ {
			put(buf[j*size:], i+j)
		}
		if _, err := w.Write(buf[:m*size]); err != nil {
			return err
		}
		i += m
	}
	return nil
}
