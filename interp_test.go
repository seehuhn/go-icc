// seehuhn.de/go/icc - read and write ICC profiles
// Copyright (C) 2026  Jochen Voss <voss@seehuhn.de>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package icc

import (
	"math"
	"math/rand"
	"testing"
)

// tetrahedralInterp3D is an allocating wrapper around [tetrahedralInterp3DInto]
// used to test the interpolation in isolation.
func tetrahedralInterp3D(clut []float32, gridSize int, outChannels int, r, g, b float64) []float64 {
	out := make([]float64, outChannels)
	tetrahedralInterp3DInto(clut, gridSize, outChannels, r, g, b, out)
	return out
}

// multilinearInterpReferenceInto is a brute-force O(nDims·2^nDims) multilinear
// interpolation. Production code interpolates CLUTs with [simplexInterpInto]
// (and [tetrahedralInterp3DInto] for 3-D); this routine is the baseline that
// [BenchmarkMultilinearInterp4D] measures simplex interpolation against.
func multilinearInterpReferenceInto(clut []float32, gridPoints, strides []int, outChannels int, input, out []float64, indices []int, fracs []float64) {
	nDims := len(gridPoints)
	if nDims == 0 || len(input) != nDims {
		for i := range outChannels {
			out[i] = 0
		}
		return
	}

	for i := range nDims {
		scale := float64(gridPoints[i] - 1)
		pos := input[i] * scale
		idx := max(int(pos), 0)
		if idx >= gridPoints[i]-1 {
			idx = max(gridPoints[i]-2, 0)
		}
		indices[i] = idx
		fracs[i] = clamp(pos-float64(idx), 0, 1)
	}

	baseOffset := 0
	for d := range nDims {
		baseOffset += indices[d] * strides[d]
	}

	for i := range outChannels {
		out[i] = 0
	}

	numCorners := 1 << nDims
	for corner := range numCorners {
		offset := 0
		weight := 1.0
		for d := range nDims {
			if corner&(1<<d) != 0 {
				offset += strides[d]
				weight *= fracs[d]
			} else {
				weight *= 1 - fracs[d]
			}
		}
		for i := range outChannels {
			idx := baseOffset + offset + i
			if idx < len(clut) {
				out[i] += weight * float64(clut[idx])
			}
		}
	}
}

// simplexInterp is an allocating wrapper around [simplexInterpInto].
func simplexInterp(clut []float32, gridPoints []int, outChannels int, input []float64) []float64 {
	out := make([]float64, outChannels)
	nDims := len(gridPoints)
	if nDims == 0 || len(input) != nDims {
		return out
	}
	strides := make([]int, nDims)
	computeStrides(strides, gridPoints, outChannels)
	indices := make([]int, nDims)
	order := make([]int, nDims)
	fracs := make([]float64, nDims)
	simplexInterpInto(clut, gridPoints, strides, outChannels, input, out, indices, order, fracs)
	return out
}

// randomCLUT fills a flattened CLUT of the given shape with random values.
func randomCLUT(rng *rand.Rand, gridPoints []int, outChannels int) []float32 {
	total := outChannels
	for _, g := range gridPoints {
		total *= g
	}
	clut := make([]float32, total)
	for i := range clut {
		clut[i] = rng.Float32()
	}
	return clut
}

// affineCLUT fills a CLUT so that grid node j has value
// b[c] + Σ_d a[d][c]·(j_d/(gridPoints[d]-1)) for each output channel c, i.e. the
// table samples an affine function of the normalised input coordinates.
func affineCLUT(gridPoints []int, outChannels int, a [][]float64, b []float64) []float32 {
	n := len(gridPoints)
	total := outChannels
	for _, g := range gridPoints {
		total *= g
	}
	clut := make([]float32, total)
	strides := make([]int, n)
	computeStrides(strides, gridPoints, outChannels)

	idx := make([]int, n)
	for {
		off := 0
		for d := range n {
			off += idx[d] * strides[d]
		}
		for c := range outChannels {
			v := b[c]
			for d := range n {
				coord := float64(idx[d]) / float64(gridPoints[d]-1)
				v += a[d][c] * coord
			}
			clut[off+c] = float32(v)
		}
		k := n - 1
		for k >= 0 {
			idx[k]++
			if idx[k] < gridPoints[k] {
				break
			}
			idx[k] = 0
			k--
		}
		if k < 0 {
			break
		}
	}
	return clut
}

// cellCornerRange returns the per-channel min and max over the 2^nDims corners
// of the grid cell that contains input, mirroring the index clamping used by the
// interpolation routines.
func cellCornerRange(clut []float32, gridPoints []int, outChannels int, input []float64) (lo, hi []float64) {
	n := len(gridPoints)
	strides := make([]int, n)
	computeStrides(strides, gridPoints, outChannels)
	indices := make([]int, n)
	for d := range n {
		pos := input[d] * float64(gridPoints[d]-1)
		idx := max(int(pos), 0)
		if idx >= gridPoints[d]-1 {
			idx = max(gridPoints[d]-2, 0)
		}
		indices[d] = idx
	}
	base := 0
	for d := range n {
		base += indices[d] * strides[d]
	}
	lo = make([]float64, outChannels)
	hi = make([]float64, outChannels)
	for c := range outChannels {
		lo[c] = math.Inf(1)
		hi[c] = math.Inf(-1)
	}
	for corner := range 1 << n {
		off := base
		for d := range n {
			if corner&(1<<d) != 0 {
				off += strides[d]
			}
		}
		for c := range outChannels {
			v := float64(clut[off+c])
			lo[c] = min(lo[c], v)
			hi[c] = max(hi[c], v)
		}
	}
	return lo, hi
}

// TestSimplexInterpAffineExact checks that simplex interpolation reproduces an
// affine function sampled on the grid.  The tolerance is set by the CLUT's
// float32 storage, not by the interpolation: each node value carries up to one
// float32 ulp, and interpolation forms a convex combination of them, so the
// result cannot be closer to the float64 affine function than the table is.
func TestSimplexInterpAffineExact(t *testing.T) {
	// a few float32 ulps at the magnitudes these affine functions reach
	const tol = 1e-6

	rng := rand.New(rand.NewSource(1))
	grids := [][]int{
		{2, 2},
		{3, 3, 3},
		{4, 5, 3},       // non-uniform 3-D
		{5, 5, 5, 5},    // 4-D (CMYK-like)
		{6, 4, 3, 5},    // 4-D non-uniform
		{3, 3, 3, 3, 3}, // 5-D
	}
	const outChannels = 3
	for _, gridPoints := range grids {
		n := len(gridPoints)
		a := make([][]float64, n)
		for d := range n {
			a[d] = []float64{rng.Float64(), rng.Float64(), rng.Float64()}
		}
		b := []float64{rng.Float64(), rng.Float64(), rng.Float64()}
		clut := affineCLUT(gridPoints, outChannels, a, b)

		for range 300 {
			input := make([]float64, n)
			for d := range n {
				input[d] = rng.Float64()
			}
			got := simplexInterp(clut, gridPoints, outChannels, input)
			for c := range outChannels {
				want := b[c]
				for d := range n {
					want += a[d][c] * input[d]
				}
				if math.Abs(got[c]-want) > tol {
					t.Fatalf("grid %v input %v: channel %d = %v, want %v (affine)",
						gridPoints, input, c, got[c], want)
				}
			}
		}
	}
}

func TestSimplexInterpExactAtNodes(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	// grids whose (gridPoints-1) is a power of two, so node coordinates
	// j/(gridPoints-1) are exactly representable in float64
	grids := [][]int{
		{2, 2, 2},
		{3, 3, 3},
		{5, 5, 5, 5},
		{9, 3, 5, 2},
	}
	const outChannels = 3
	for _, gridPoints := range grids {
		n := len(gridPoints)
		clut := randomCLUT(rng, gridPoints, outChannels)
		strides := make([]int, n)
		computeStrides(strides, gridPoints, outChannels)

		idx := make([]int, n)
		for {
			input := make([]float64, n)
			off := 0
			for d := range n {
				input[d] = float64(idx[d]) / float64(gridPoints[d]-1)
				off += idx[d] * strides[d]
			}
			got := simplexInterp(clut, gridPoints, outChannels, input)
			for c := range outChannels {
				if math.Abs(got[c]-float64(clut[off+c])) > 1e-9 {
					t.Fatalf("grid %v node %v: channel %d = %v, want %v",
						gridPoints, idx, c, got[c], clut[off+c])
				}
			}
			k := n - 1
			for k >= 0 {
				idx[k]++
				if idx[k] < gridPoints[k] {
					break
				}
				idx[k] = 0
				k--
			}
			if k < 0 {
				break
			}
		}
	}
}

func TestSimplexInterpBounded(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	grids := [][]int{
		{2, 2},
		{4, 4, 4},
		{4, 5, 3},
		{5, 5, 5, 5},
		{6, 4, 3, 5},
	}
	const outChannels = 3
	for _, gridPoints := range grids {
		n := len(gridPoints)
		clut := randomCLUT(rng, gridPoints, outChannels)
		for range 300 {
			input := make([]float64, n)
			for d := range n {
				input[d] = rng.Float64()
			}
			got := simplexInterp(clut, gridPoints, outChannels, input)
			lo, hi := cellCornerRange(clut, gridPoints, outChannels, input)
			for c := range outChannels {
				if got[c] < lo[c]-1e-12 || got[c] > hi[c]+1e-12 {
					t.Fatalf("grid %v input %v: channel %d = %v, outside cell range [%v, %v]",
						gridPoints, input, c, got[c], lo[c], hi[c])
				}
			}
		}
	}
}

// TestSimplexMatchesTetrahedral checks that the general n-D routine reproduces
// the trusted hand-unrolled 3-D tetrahedral routine on a uniform 3-D grid, where
// they implement the same Kuhn simplex algorithm.
func TestSimplexMatchesTetrahedral(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	const outChannels = 3
	for _, gridSize := range []int{2, 3, 5, 9} {
		gridPoints := []int{gridSize, gridSize, gridSize}
		clut := randomCLUT(rng, gridPoints, outChannels)
		for range 500 {
			r, g, b := rng.Float64(), rng.Float64(), rng.Float64()
			gotS := simplexInterp(clut, gridPoints, outChannels, []float64{r, g, b})
			gotT := tetrahedralInterp3D(clut, gridSize, outChannels, r, g, b)
			for c := range outChannels {
				if math.Abs(gotS[c]-gotT[c]) > 1e-12 {
					t.Fatalf("gridSize %d input (%v,%v,%v): channel %d simplex=%v tetra=%v",
						gridSize, r, g, b, c, gotS[c], gotT[c])
				}
			}
		}
	}
}

// TestSimplexInterpTruncatedCLUT verifies that a malformed (short) CLUT, as may
// come from an untrusted profile, neither panics nor produces non-finite output.
func TestSimplexInterpTruncatedCLUT(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	gridPoints := []int{5, 5, 5, 5}
	const outChannels = 3
	full := randomCLUT(rng, gridPoints, outChannels)
	clut := full[:len(full)/2] // truncated
	for range 200 {
		input := []float64{rng.Float64(), rng.Float64(), rng.Float64(), rng.Float64()}
		got := simplexInterp(clut, gridPoints, outChannels, input)
		for c := range outChannels {
			if math.IsNaN(got[c]) || math.IsInf(got[c], 0) {
				t.Fatalf("truncated CLUT input %v: channel %d = %v (non-finite)", input, c, got[c])
			}
		}
	}
}

func benchmarkInterpInputs(rng *rand.Rand, n, count int) [][]float64 {
	inputs := make([][]float64, count)
	for i := range inputs {
		v := make([]float64, n)
		for d := range n {
			v[d] = rng.Float64()
		}
		inputs[i] = v
	}
	return inputs
}

func BenchmarkSimplexInterp4D(b *testing.B) {
	rng := rand.New(rand.NewSource(6))
	gridPoints := []int{9, 9, 9, 9}
	const outChannels = 3
	clut := randomCLUT(rng, gridPoints, outChannels)
	inputs := benchmarkInterpInputs(rng, 4, 256)

	n := len(gridPoints)
	strides := make([]int, n)
	computeStrides(strides, gridPoints, outChannels)
	indices := make([]int, n)
	order := make([]int, n)
	fracs := make([]float64, n)
	out := make([]float64, outChannels)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in := inputs[i%len(inputs)]
		simplexInterpInto(clut, gridPoints, strides, outChannels, in, out, indices, order, fracs)
	}
}

func BenchmarkMultilinearInterp4D(b *testing.B) {
	rng := rand.New(rand.NewSource(6))
	gridPoints := []int{9, 9, 9, 9}
	const outChannels = 3
	clut := randomCLUT(rng, gridPoints, outChannels)
	inputs := benchmarkInterpInputs(rng, 4, 256)

	n := len(gridPoints)
	strides := make([]int, n)
	computeStrides(strides, gridPoints, outChannels)
	indices := make([]int, n)
	fracs := make([]float64, n)
	out := make([]float64, outChannels)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in := inputs[i%len(inputs)]
		multilinearInterpReferenceInto(clut, gridPoints, strides, outChannels, in, out, indices, fracs)
	}
}
