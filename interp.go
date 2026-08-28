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

// tetrahedralInterp3DInto performs tetrahedral interpolation in a 3D CLUT,
// writing the result into out[:outChannels] supplied by the caller.
// The input r, g, b values are in [0, 1]. The clut contains flattened data with
// outChannels values per grid point; gridSize is the number of grid points per
// dimension (same for all three).
func tetrahedralInterp3DInto(clut []float32, gridSize int, outChannels int, r, g, b float64, out []float64) {
	if gridSize < 2 {
		for i := range outChannels {
			out[i] = 0
		}
		if len(clut) >= outChannels {
			for i := range outChannels {
				out[i] = float64(clut[i])
			}
		}
		return
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

	// tetrahedral interpolation - select tetrahedron based on which
	// fractional component is largest
	if fr > fg {
		if fg > fb {
			// fr > fg > fb: tetrahedron 1
			for i := range outChannels {
				out[i] = (1-fr)*float64(clut[c000+i]) +
					(fr-fg)*float64(clut[c100+i]) +
					(fg-fb)*float64(clut[c110+i]) +
					fb*float64(clut[c111+i])
			}
		} else if fr > fb {
			// fr > fb >= fg: tetrahedron 2
			for i := range outChannels {
				out[i] = (1-fr)*float64(clut[c000+i]) +
					(fr-fb)*float64(clut[c100+i]) +
					(fb-fg)*float64(clut[c101+i]) +
					fg*float64(clut[c111+i])
			}
		} else {
			// fb >= fr > fg: tetrahedron 3
			for i := range outChannels {
				out[i] = (1-fb)*float64(clut[c000+i]) +
					(fb-fr)*float64(clut[c001+i]) +
					(fr-fg)*float64(clut[c101+i]) +
					fg*float64(clut[c111+i])
			}
		}
	} else {
		if fr > fb {
			// fg >= fr > fb: tetrahedron 4
			for i := range outChannels {
				out[i] = (1-fg)*float64(clut[c000+i]) +
					(fg-fr)*float64(clut[c010+i]) +
					(fr-fb)*float64(clut[c110+i]) +
					fb*float64(clut[c111+i])
			}
		} else if fg > fb {
			// fg > fb >= fr: tetrahedron 5
			for i := range outChannels {
				out[i] = (1-fg)*float64(clut[c000+i]) +
					(fg-fb)*float64(clut[c010+i]) +
					(fb-fr)*float64(clut[c011+i]) +
					fr*float64(clut[c111+i])
			}
		} else {
			// fb >= fg >= fr: tetrahedron 6
			for i := range outChannels {
				out[i] = (1-fb)*float64(clut[c000+i]) +
					(fb-fg)*float64(clut[c001+i]) +
					(fg-fr)*float64(clut[c011+i]) +
					fr*float64(clut[c111+i])
			}
		}
	}
}

// simplexInterpInto performs n-dimensional simplex (Kuhn–Freudenthal)
// interpolation, writing the result into out[:outChannels]. The input values are
// in [0, 1] and gridPoints contains the grid size for each dimension. It uses the
// caller-supplied strides (see [computeStrides]) and the scratch slices indices,
// order (each of length >= len(gridPoints)) and fracs (length >= len(gridPoints)).
//
// Within the enclosing grid cell, the point's fractional coordinates are sorted in
// descending order; that order both selects the simplex and yields its barycentric
// weights as successive differences of the sorted fractions. Only the n+1 vertices
// of that simplex are read, versus the 2^n corners of multilinear interpolation.
// For n = 3 this is identical to [tetrahedralInterp3DInto].
func simplexInterpInto(clut []float32, gridPoints, strides []int, outChannels int, input, out []float64, indices, order []int, fracs []float64) {
	nDims := len(gridPoints)
	if nDims == 0 || len(input) != nDims {
		for i := range outChannels {
			out[i] = 0
		}
		return
	}

	// compute grid positions, fractions, and the identity dimension order
	for i := range nDims {
		scale := float64(gridPoints[i] - 1)
		pos := input[i] * scale
		idx := max(int(pos), 0)
		if idx >= gridPoints[i]-1 {
			idx = max(gridPoints[i]-2, 0)
		}
		indices[i] = idx
		fracs[i] = clamp(pos-float64(idx), 0, 1)
		order[i] = i
	}

	// base offset of the all-low corner of the cell
	baseOffset := 0
	for d := range nDims {
		baseOffset += indices[d] * strides[d]
	}

	// sort dimensions by fraction, descending (insertion sort; nDims is small)
	for a := 1; a < nDims; a++ {
		d := order[a]
		f := fracs[d]
		b := a
		for b > 0 && fracs[order[b-1]] < f {
			order[b] = order[b-1]
			b--
		}
		order[b] = d
	}

	for i := range outChannels {
		out[i] = 0
	}

	// walk the simplex from the all-low corner to the all-high corner, flipping
	// one coordinate high at each step in descending-fraction order; the weight of
	// each visited vertex is the drop in fraction across that step
	off := baseOffset
	w := 1 - fracs[order[0]]
	for i := range outChannels {
		if off+i < len(clut) {
			out[i] += w * float64(clut[off+i])
		}
	}
	for k := range nDims {
		off += strides[order[k]]
		if k < nDims-1 {
			w = fracs[order[k]] - fracs[order[k+1]]
		} else {
			w = fracs[order[k]]
		}
		for i := range outChannels {
			if off+i < len(clut) {
				out[i] += w * float64(clut[off+i])
			}
		}
	}
}

// computeStrides fills strides[i] with the element stride of dimension i of a
// flattened CLUT with the given per-dimension gridPoints and outChannels values
// per grid point. strides must have length >= len(gridPoints).
func computeStrides(strides, gridPoints []int, outChannels int) {
	stride := outChannels
	for i := len(gridPoints) - 1; i >= 0; i-- {
		strides[i] = stride
		stride *= gridPoints[i]
	}
}
