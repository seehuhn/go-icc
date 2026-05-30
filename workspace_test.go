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

import "testing"

// cmykTransforms decodes the embedded CMYK LUT profile and returns its
// forward (device->PCS, 4-input multilinear) and reverse (PCS->device,
// 3-input tetrahedral) transforms.
func cmykTransforms(t testing.TB) (fwd, rev *Transform) {
	t.Helper()
	p, err := Decode(CMYKProfile)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	fwd, err = NewTransform(p, DeviceToPCS, Perceptual)
	if err != nil {
		t.Fatalf("forward NewTransform failed: %v", err)
	}
	rev, err = NewTransform(p, PCSToDevice, Perceptual)
	if err != nil {
		t.Fatalf("reverse NewTransform failed: %v", err)
	}
	return fwd, rev
}

// TestWorkspaceToXYZEquivalence checks that ToXYZ produces identical results
// whether a Workspace is supplied or not (LUT/multilinear path).
func TestWorkspaceToXYZEquivalence(t *testing.T) {
	fwd, _ := cmykTransforms(t)
	ws := &Workspace{}
	steps := []float64{0, 0.25, 0.5, 0.75, 1}
	for _, c := range steps {
		for _, m := range steps {
			for _, y := range steps {
				for _, k := range steps {
					in := []float64{c, m, y, k}
					wantX, wantY, wantZ := fwd.ToXYZ(in, nil)
					gotX, gotY, gotZ := fwd.ToXYZ(in, ws)
					if gotX != wantX || gotY != wantY || gotZ != wantZ {
						t.Fatalf("ToXYZ(%v): with ws (%v,%v,%v) != without (%v,%v,%v)",
							in, gotX, gotY, gotZ, wantX, wantY, wantZ)
					}
				}
			}
		}
	}
}

// TestWorkspaceFromXYZIntoEquivalence checks that FromXYZInto produces identical
// results whether a Workspace is supplied or not (LUT/tetrahedral path).
func TestWorkspaceFromXYZIntoEquivalence(t *testing.T) {
	_, rev := cmykTransforms(t)
	n := rev.Channels()
	ws := &Workspace{}
	dstNil := make([]float64, n)
	dstWS := make([]float64, n)
	steps := []float64{0, 0.2, 0.4, 0.6, 0.8, 1}
	for _, X := range steps {
		for _, Y := range steps {
			for _, Z := range steps {
				rev.FromXYZInto(X, Y, Z, dstNil, nil)
				rev.FromXYZInto(X, Y, Z, dstWS, ws)
				for i := range n {
					if dstWS[i] != dstNil[i] {
						t.Fatalf("FromXYZInto(%v,%v,%v)[%d]: with ws %v != without %v",
							X, Y, Z, i, dstWS[i], dstNil[i])
					}
				}
			}
		}
	}
}

// TestWorkspaceZeroAlloc verifies that, with a supplied Workspace, the
// LUT-based conversion path performs no heap allocation per call.
func TestWorkspaceZeroAlloc(t *testing.T) {
	fwd, rev := cmykTransforms(t)
	ws := &Workspace{}
	in := []float64{0.1, 0.2, 0.3, 0.4}
	dst := make([]float64, rev.Channels())

	// prime the workspace buffers so growth does not count as allocation
	fwd.ToXYZ(in, ws)
	rev.FromXYZInto(0.3, 0.4, 0.5, dst, ws)

	if n := testing.AllocsPerRun(100, func() {
		fwd.ToXYZ(in, ws)
	}); n != 0 {
		t.Errorf("ToXYZ with workspace: got %v allocs/op, want 0", n)
	}

	if n := testing.AllocsPerRun(100, func() {
		rev.FromXYZInto(0.3, 0.4, 0.5, dst, ws)
	}); n != 0 {
		t.Errorf("FromXYZInto with workspace: got %v allocs/op, want 0", n)
	}
}

func BenchmarkToXYZ(b *testing.B) {
	fwd, _ := cmykTransforms(b)
	in := []float64{0.1, 0.2, 0.3, 0.4}

	b.Run("nil", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			fwd.ToXYZ(in, nil)
		}
	})
	b.Run("workspace", func(b *testing.B) {
		ws := &Workspace{}
		fwd.ToXYZ(in, ws)
		b.ReportAllocs()
		for range b.N {
			fwd.ToXYZ(in, ws)
		}
	})
}

func BenchmarkFromXYZInto(b *testing.B) {
	_, rev := cmykTransforms(b)
	dst := make([]float64, rev.Channels())

	b.Run("nil", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			rev.FromXYZInto(0.3, 0.4, 0.5, dst, nil)
		}
	})
	b.Run("workspace", func(b *testing.B) {
		ws := &Workspace{}
		rev.FromXYZInto(0.3, 0.4, 0.5, dst, ws)
		b.ReportAllocs()
		for range b.N {
			rev.FromXYZInto(0.3, 0.4, 0.5, dst, ws)
		}
	})
}
