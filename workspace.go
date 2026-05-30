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

// Workspace holds reusable scratch buffers so that colour conversions can run
// without per-call heap allocation.
//
// A nil *Workspace is valid: its accessors then allocate fresh buffers,
// reproducing the behaviour of the plain allocating conversion path. To avoid
// allocation in a hot loop, allocate one Workspace and pass it to every
// conversion call.
//
// A Workspace is not safe for concurrent use. Give each goroutine its own;
// never share one across goroutines, and never store one on a [Transform]
// (which may itself be shared).
type Workspace struct {
	resultBuf  []float64 // transform-level input/output buffer
	valuesBuf  []float64 // LUT pipeline working buffer
	fracsBuf   []float64 // multilinear interpolation fractions
	indicesBuf []int     // multilinear interpolation grid indices
	gridBuf    []int     // per-dimension grid sizes
	stridesBuf []int     // CLUT strides

	// caller-owned scratch buffers addressed by slot index; icc never reads or
	// writes these (see [Workspace.Scratch]).
	scratch [][]float64
}

// Scratch returns a reusable []float64 of length n bound to the given slot.
// Buffers for distinct slots are independent and stay valid across nested
// conversion calls, so a caller may hold one slot's buffer while requesting
// another.
//
// icc never touches these buffers; they exist purely for callers layering on
// top of this package (for example, threading colour values through nested
// colour-space conversions). The contents are not zeroed — overwrite the
// returned buffer fully. Requesting the same slot again returns the same
// (possibly grown) buffer, so consume a slot's contents before requesting it
// again. Slots are small dense indices starting at 0. A nil Workspace allocates.
func (ws *Workspace) Scratch(slot, n int) []float64 {
	if ws == nil {
		return make([]float64, n)
	}
	if slot >= len(ws.scratch) {
		ws.scratch = append(ws.scratch, make([][]float64, slot+1-len(ws.scratch))...)
	}
	ws.scratch[slot] = growFloats(ws.scratch[slot], n)
	return ws.scratch[slot]
}

func growFloats(buf []float64, n int) []float64 {
	if cap(buf) < n {
		return make([]float64, n)
	}
	return buf[:n]
}

func growInts(buf []int, n int) []int {
	if cap(buf) < n {
		return make([]int, n)
	}
	return buf[:n]
}

func (ws *Workspace) result(n int) []float64 {
	if ws == nil {
		return make([]float64, n)
	}
	ws.resultBuf = growFloats(ws.resultBuf, n)
	return ws.resultBuf
}

func (ws *Workspace) values(n int) []float64 {
	if ws == nil {
		return make([]float64, n)
	}
	ws.valuesBuf = growFloats(ws.valuesBuf, n)
	return ws.valuesBuf
}

func (ws *Workspace) fracs(n int) []float64 {
	if ws == nil {
		return make([]float64, n)
	}
	ws.fracsBuf = growFloats(ws.fracsBuf, n)
	return ws.fracsBuf
}

func (ws *Workspace) indices(n int) []int {
	if ws == nil {
		return make([]int, n)
	}
	ws.indicesBuf = growInts(ws.indicesBuf, n)
	return ws.indicesBuf
}

func (ws *Workspace) grid(n int) []int {
	if ws == nil {
		return make([]int, n)
	}
	ws.gridBuf = growInts(ws.gridBuf, n)
	return ws.gridBuf
}

func (ws *Workspace) strides(n int) []int {
	if ws == nil {
		return make([]int, n)
	}
	ws.stridesBuf = growInts(ws.stridesBuf, n)
	return ws.stridesBuf
}
