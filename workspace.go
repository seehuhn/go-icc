// Copyright 2026 Jochen Voss <voss@seehuhn.de>
// SPDX-License-Identifier: BSD-3-Clause

package icc

// Workspace holds reusable scratch buffers so that colour conversions can run
// without per-call heap allocation.
//
// The zero value &Workspace{} is ready to use.
//
// A Workspace is not safe for concurrent use, use separate Workspaces for
// separate goroutines.
type Workspace struct {
	resultBuf  []float64 // transform-level input/output buffer
	valuesBuf  []float64 // LUT pipeline working buffer
	fracsBuf   []float64 // CLUT interpolation fractions
	indicesBuf []int     // CLUT interpolation grid indices
	gridBuf    []int     // per-dimension grid sizes
	stridesBuf []int     // CLUT strides
	orderBuf   []int     // simplex interpolation dimension order

	// caller-owned scratch buffers addressed by slot index; icc never reads or
	// writes these (see [Workspace.Scratch]).
	scratch [][]float64
}

// Scratch returns a reusable []float64 of length n bound to the given slot.
// The icc library never touches these buffers; they are for the callers' use
// as scratch space.  Requesting the same slot again returns the same buffer.
func (ws *Workspace) Scratch(slot, n int) []float64 {
	if slot >= len(ws.scratch) {
		ws.scratch = append(ws.scratch, make([][]float64, slot+1-len(ws.scratch))...)
	}
	ws.scratch[slot] = growFloats(ws.scratch[slot], n)
	return ws.scratch[slot]
}

func growFloats(buf []float64, n int) []float64 {
	if len(buf) < n {
		buf = append(buf, make([]float64, n-len(buf))...)
	}
	return buf[:n]
}

func growInts(buf []int, n int) []int {
	if len(buf) < n {
		buf = append(buf, make([]int, n-len(buf))...)
	}
	return buf[:n]
}

func (ws *Workspace) result(n int) []float64 {
	ws.resultBuf = growFloats(ws.resultBuf, n)
	return ws.resultBuf
}

func (ws *Workspace) values(n int) []float64 {
	ws.valuesBuf = growFloats(ws.valuesBuf, n)
	return ws.valuesBuf
}

func (ws *Workspace) fracs(n int) []float64 {
	ws.fracsBuf = growFloats(ws.fracsBuf, n)
	return ws.fracsBuf
}

func (ws *Workspace) indices(n int) []int {
	ws.indicesBuf = growInts(ws.indicesBuf, n)
	return ws.indicesBuf
}

func (ws *Workspace) grid(n int) []int {
	ws.gridBuf = growInts(ws.gridBuf, n)
	return ws.gridBuf
}

func (ws *Workspace) strides(n int) []int {
	ws.stridesBuf = growInts(ws.stridesBuf, n)
	return ws.stridesBuf
}

func (ws *Workspace) order(n int) []int {
	ws.orderBuf = growInts(ws.orderBuf, n)
	return ws.orderBuf
}
