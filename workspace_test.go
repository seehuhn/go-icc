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

// cmykTransform decodes the embedded CMYK LUT profile and returns its
// bidirectional transform (forward: device->PCS, 4-input simplex; reverse:
// PCS->device, 3-input tetrahedral).
func cmykTransform(t testing.TB) *Transform {
	t.Helper()
	p, err := Decode(CMYKProfile)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	tr, err := NewTransform(p, Perceptual)
	if err != nil {
		t.Fatalf("NewTransform failed: %v", err)
	}
	return tr
}

// TestWorkspaceToXYZEquivalence checks that a reused (dirtied) Workspace yields
// the same results as a fresh one, so buffer reuse never leaks stale data into
// a conversion (LUT/simplex path). A fresh Workspace zeroes its buffers via
// make, whereas a reused one carries non-zero stale contents; both must agree.
func TestWorkspaceToXYZEquivalence(t *testing.T) {
	tr := cmykTransform(t)
	// dirty carries stale buffer contents from a prior, different conversion.
	dirty := &Workspace{}
	tr.ToXYZ([]float64{1, 0.3, 0.7, 0.2}, dirty)
	steps := []float64{0, 0.25, 0.5, 0.75, 1}
	for _, c := range steps {
		for _, m := range steps {
			for _, y := range steps {
				for _, k := range steps {
					in := []float64{c, m, y, k}
					wantX, wantY, wantZ := tr.ToXYZ(in, &Workspace{})
					gotX, gotY, gotZ := tr.ToXYZ(in, dirty)
					if gotX != wantX || gotY != wantY || gotZ != wantZ {
						t.Fatalf("ToXYZ(%v): reused ws (%v,%v,%v) != fresh (%v,%v,%v)",
							in, gotX, gotY, gotZ, wantX, wantY, wantZ)
					}
				}
			}
		}
	}
}

// TestWorkspaceFromXYZEquivalence checks that a reused (dirtied) Workspace
// yields the same results as a fresh one (LUT/tetrahedral path), so buffer
// reuse never leaks stale data into a conversion.
func TestWorkspaceFromXYZEquivalence(t *testing.T) {
	tr := cmykTransform(t)
	n := tr.Channels()
	// dirty carries stale buffer contents from a prior, different conversion.
	dirty := &Workspace{}
	tr.FromXYZ(0.1, 0.9, 0.4, make([]float64, n), dirty)
	dstFresh := make([]float64, n)
	dstDirty := make([]float64, n)
	steps := []float64{0, 0.2, 0.4, 0.6, 0.8, 1}
	for _, X := range steps {
		for _, Y := range steps {
			for _, Z := range steps {
				tr.FromXYZ(X, Y, Z, dstFresh, &Workspace{})
				tr.FromXYZ(X, Y, Z, dstDirty, dirty)
				for i := range n {
					if dstDirty[i] != dstFresh[i] {
						t.Fatalf("FromXYZ(%v,%v,%v)[%d]: reused ws %v != fresh %v",
							X, Y, Z, i, dstDirty[i], dstFresh[i])
					}
				}
			}
		}
	}
}

// TestWorkspaceZeroAlloc verifies that, with a supplied Workspace, the
// LUT-based conversion path performs no heap allocation per call.
func TestWorkspaceZeroAlloc(t *testing.T) {
	tr := cmykTransform(t)
	ws := &Workspace{}
	in := []float64{0.1, 0.2, 0.3, 0.4}
	dst := make([]float64, tr.Channels())

	// prime the workspace buffers and lazy direction builds so neither counts
	// as allocation
	tr.ToXYZ(in, ws)
	tr.FromXYZ(0.3, 0.4, 0.5, dst, ws)

	if n := testing.AllocsPerRun(100, func() {
		tr.ToXYZ(in, ws)
	}); n != 0 {
		t.Errorf("ToXYZ with workspace: got %v allocs/op, want 0", n)
	}

	if n := testing.AllocsPerRun(100, func() {
		tr.FromXYZ(0.3, 0.4, 0.5, dst, ws)
	}); n != 0 {
		t.Errorf("FromXYZ with workspace: got %v allocs/op, want 0", n)
	}
}

func BenchmarkToXYZ(b *testing.B) {
	tr := cmykTransform(b)
	in := []float64{0.1, 0.2, 0.3, 0.4}

	b.Run("fresh", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			tr.ToXYZ(in, &Workspace{})
		}
	})
	b.Run("workspace", func(b *testing.B) {
		ws := &Workspace{}
		tr.ToXYZ(in, ws)
		b.ReportAllocs()
		for range b.N {
			tr.ToXYZ(in, ws)
		}
	})
}

func BenchmarkFromXYZ(b *testing.B) {
	tr := cmykTransform(b)
	dst := make([]float64, tr.Channels())

	b.Run("fresh", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			tr.FromXYZ(0.3, 0.4, 0.5, dst, &Workspace{})
		}
	})
	b.Run("workspace", func(b *testing.B) {
		ws := &Workspace{}
		tr.FromXYZ(0.3, 0.4, 0.5, dst, ws)
		b.ReportAllocs()
		for range b.N {
			tr.FromXYZ(0.3, 0.4, 0.5, dst, ws)
		}
	})
}
