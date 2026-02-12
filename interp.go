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

// tetrahedralInterp3D performs tetrahedral interpolation in a 3D CLUT.
// The input r, g, b values are in [0, 1].
// The clut contains flattened data with outChannels values per grid point.
// gridSize is the number of grid points per dimension (same for all three).
func tetrahedralInterp3D(clut []float64, gridSize int, outChannels int, r, g, b float64) []float64 {
	if gridSize < 2 {
		out := make([]float64, outChannels)
		if len(clut) >= outChannels {
			copy(out, clut[:outChannels])
		}
		return out
	}

	// scale to grid coordinates
	scale := float64(gridSize - 1)
	rPos := r * scale
	gPos := g * scale
	bPos := b * scale

	// grid indices
	ri := int(rPos)
	gi := int(gPos)
	bi := int(bPos)

	// clamp indices
	if ri < 0 {
		ri = 0
	}
	if ri >= gridSize-1 {
		ri = gridSize - 2
	}
	if gi < 0 {
		gi = 0
	}
	if gi >= gridSize-1 {
		gi = gridSize - 2
	}
	if bi < 0 {
		bi = 0
	}
	if bi >= gridSize-1 {
		bi = gridSize - 2
	}

	// fractional parts
	fr := rPos - float64(ri)
	fg := gPos - float64(gi)
	fb := bPos - float64(bi)

	// clamp fractions
	fr = clamp(fr, 0, 1)
	fg = clamp(fg, 0, 1)
	fb = clamp(fb, 0, 1)

	// compute base offset for cube corner (ri, gi, bi)
	stride := outChannels
	gStride := gridSize * stride
	rStride := gridSize * gStride

	base := ri*rStride + gi*gStride + bi*stride

	// get the 8 corners of the cube
	c000 := base
	c001 := base + stride
	c010 := base + gStride
	c011 := base + gStride + stride
	c100 := base + rStride
	c101 := base + rStride + stride
	c110 := base + rStride + gStride
	c111 := base + rStride + gStride + stride

	out := make([]float64, outChannels)

	// tetrahedral interpolation - select tetrahedron based on which
	// fractional component is largest
	if fr > fg {
		if fg > fb {
			// fr > fg > fb: tetrahedron 1
			for i := range outChannels {
				out[i] = (1-fr)*clut[c000+i] +
					(fr-fg)*clut[c100+i] +
					(fg-fb)*clut[c110+i] +
					fb*clut[c111+i]
			}
		} else if fr > fb {
			// fr > fb >= fg: tetrahedron 2
			for i := range outChannels {
				out[i] = (1-fr)*clut[c000+i] +
					(fr-fb)*clut[c100+i] +
					(fb-fg)*clut[c101+i] +
					fg*clut[c111+i]
			}
		} else {
			// fb >= fr > fg: tetrahedron 3
			for i := range outChannels {
				out[i] = (1-fb)*clut[c000+i] +
					(fb-fr)*clut[c001+i] +
					(fr-fg)*clut[c101+i] +
					fg*clut[c111+i]
			}
		}
	} else {
		if fr > fb {
			// fg >= fr > fb: tetrahedron 4
			for i := range outChannels {
				out[i] = (1-fg)*clut[c000+i] +
					(fg-fr)*clut[c010+i] +
					(fr-fb)*clut[c110+i] +
					fb*clut[c111+i]
			}
		} else if fg > fb {
			// fg > fb >= fr: tetrahedron 5
			for i := range outChannels {
				out[i] = (1-fg)*clut[c000+i] +
					(fg-fb)*clut[c010+i] +
					(fb-fr)*clut[c011+i] +
					fr*clut[c111+i]
			}
		} else {
			// fb >= fg >= fr: tetrahedron 6
			for i := range outChannels {
				out[i] = (1-fb)*clut[c000+i] +
					(fb-fg)*clut[c001+i] +
					(fg-fr)*clut[c011+i] +
					fr*clut[c111+i]
			}
		}
	}

	return out
}

// multilinearInterp performs n-dimensional linear interpolation.
// The input values are in [0, 1].
// gridPoints contains the grid size for each dimension.
func multilinearInterp(clut []float64, gridPoints []int, outChannels int, input []float64) []float64 {
	nDims := len(gridPoints)
	if nDims == 0 || len(input) != nDims {
		return make([]float64, outChannels)
	}

	// compute strides
	strides := make([]int, nDims)
	stride := outChannels
	for i := nDims - 1; i >= 0; i-- {
		strides[i] = stride
		stride *= gridPoints[i]
	}

	// compute grid positions and fractions
	indices := make([]int, nDims)
	fracs := make([]float64, nDims)
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

	// interpolate: iterate over 2^nDims corners
	numCorners := 1 << nDims
	out := make([]float64, outChannels)

	for corner := range numCorners {
		// compute offset and weight for this corner
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

		// base offset
		baseOffset := 0
		for d := range nDims {
			baseOffset += indices[d] * strides[d]
		}

		for i := range outChannels {
			idx := baseOffset + offset + i
			if idx < len(clut) {
				out[i] += weight * clut[idx]
			}
		}
	}

	return out
}
