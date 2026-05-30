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

// tetrahedralInterp3D is an allocating wrapper around [tetrahedralInterp3DInto]
// used to test the interpolation in isolation.
func tetrahedralInterp3D(clut []float64, gridSize int, outChannels int, r, g, b float64) []float64 {
	out := make([]float64, outChannels)
	tetrahedralInterp3DInto(clut, gridSize, outChannels, r, g, b, out)
	return out
}

// multilinearInterp is an allocating wrapper around [multilinearInterpInto] used
// to test the interpolation in isolation.
func multilinearInterp(clut []float64, gridPoints []int, outChannels int, input []float64) []float64 {
	out := make([]float64, outChannels)
	nDims := len(gridPoints)
	if nDims == 0 || len(input) != nDims {
		return out
	}
	strides := make([]int, nDims)
	computeStrides(strides, gridPoints, outChannels)
	indices := make([]int, nDims)
	fracs := make([]float64, nDims)
	multilinearInterpInto(clut, gridPoints, strides, outChannels, input, out, indices, fracs)
	return out
}
