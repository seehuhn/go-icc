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

package icc_test

import (
	"math"
	"testing"

	"seehuhn.de/go/icc"
	"seehuhn.de/go/icc/icctest"
)

// Y of L* = 20
const yL20 = 0.0298900

func TestIntentProfileBlackPoints(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version icc.Version
		intent  icc.RenderingIntent
		srcY    float64
		dstY    float64
		exact   *[3]float64
	}{
		// A2B0 raises black to L* = 20
		{"v2 perceptual", icc.Version2_1_0, icc.Perceptual, yL20, yL20, nil},
		// the B2A1/A2B1 round trip is straight, so the relative destination
		// black point is the source black point
		{"v2 relative", icc.Version2_1_0, icc.RelativeColorimetric, 0, 0, nil},
		{"v2 saturation", icc.Version2_1_0, icc.Saturation, 0, 0, nil},
		{"v4 perceptual", icc.Version4_3_0, icc.Perceptual, 0, 0, &icc.PerceptualBlackPoint},
		{"v4 relative", icc.Version4_3_0, icc.RelativeColorimetric, 0, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := icctest.IntentProfile(tc.version, icc.OutputDeviceProfile)
			tr, err := icc.NewTransform(p, tc.intent)
			if err != nil {
				t.Fatal(err)
			}
			src := tr.SourceBlackPoint()
			dst := tr.DestinationBlackPoint()
			if tc.exact != nil {
				if src != *tc.exact || dst != *tc.exact {
					t.Errorf("black points %v, %v, want %v", src, dst, *tc.exact)
				}
				return
			}
			if math.Abs(src[1]-tc.srcY) > 1e-4 {
				t.Errorf("source black Y = %g, want %g", src[1], tc.srcY)
			}
			if math.Abs(dst[1]-tc.dstY) > 1e-4 {
				t.Errorf("destination black Y = %g, want %g", dst[1], tc.dstY)
			}
			for i, w := range icc.PCSWhitePoint {
				if math.Abs(src[i]-w*src[1]) > 1e-6 || math.Abs(dst[i]-w*dst[1]) > 1e-6 {
					t.Errorf("black points %v, %v are not neutral", src, dst)
					break
				}
			}
		})
	}
}
