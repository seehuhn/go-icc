// Copyright 2026 Jochen Voss <voss@seehuhn.de>
// SPDX-License-Identifier: BSD-3-Clause

package icc

import (
	"sync"
	"testing"
)

// TestSharedTransformConcurrent checks that a single bidirectional Transform may
// be used from several goroutines at once, each with its own Workspace, with
// both directions exercised concurrently. Run with -race to catch unsynchronised
// lazy state: the per-direction fwdOnce/revOnce builds.
func TestSharedTransformConcurrent(t *testing.T) {
	t.Run("grayTRC", func(t *testing.T) {
		tr, err := NewTransform(makeGrayProfile(2.2, PCSWhitePoint), Perceptual)
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
					tr.FromXYZ(0, f, 0, dst, ws) // first-use reverse
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
