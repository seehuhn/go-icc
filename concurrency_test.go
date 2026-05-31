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
	"sync"
	"testing"
)

// graySampledProfile builds an in-memory gray profile whose tone curve is a
// sampled (table) curve, so that PCSToDevice conversion goes through
// Curve.invertSampled (which caches an inverse table on first use).
func graySampledProfile() *Profile {
	tbl := make([]uint16, 512)
	for i := range tbl {
		tbl[i] = uint16(i * 128)
	}
	return &Profile{
		ColorSpace: GraySpace,
		PCS:        PCSXYZSpace,
		TagData: map[TagType][]byte{
			GrayTRC: (&Curve{Table: tbl}).Encode(),
		},
	}
}

// TestSharedTransformConcurrent checks that a single bidirectional Transform may
// be used from several goroutines at once, each with its own Workspace, with
// both directions exercised concurrently. Run with -race to catch unsynchronised
// lazy state: the per-direction fwdOnce/revOnce builds and a Curve's cached
// inverse table.
func TestSharedTransformConcurrent(t *testing.T) {
	t.Run("grayTRC", func(t *testing.T) {
		tr, err := NewTransform(graySampledProfile(), Perceptual)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for g := range 8 {
			wg.Go(func() {
				dst := make([]float64, tr.Channels())
				ws := &Workspace{}
				for i := range 20000 {
					f := float64((i+g)%101) / 100
					tr.ToXYZ([]float64{f}, ws)   // first-use forward
					tr.FromXYZ(0, f, 0, dst, ws) // first-use reverse (prepareInverse)
				}
			})
		}
		wg.Wait()
	})

	t.Run("lut", func(t *testing.T) {
		p, err := Decode(CMYKProfile)
		if err != nil {
			t.Fatal(err)
		}
		tr, err := NewTransform(p, Perceptual)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for g := range 8 {
			wg.Go(func() {
				dst := make([]float64, tr.Channels())
				ws := &Workspace{}
				for i := range 5000 {
					f := float64((i+g)%101) / 100
					tr.ToXYZ([]float64{f, 1 - f, f, 1 - f}, ws) // first-use forward LUT
					tr.FromXYZ(0.3+0.1*f, 0.4, 0.5, dst, ws)    // first-use reverse LUT
				}
			})
		}
		wg.Wait()
	})
}
