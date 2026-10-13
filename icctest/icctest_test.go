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

package icctest

import (
	"math"
	"testing"

	"seehuhn.de/go/icc"
)

// lab converts PCS XYZ to CIELAB relative to the PCS white point.
func lab(X, Y, Z float64) (L, a, b float64) {
	f := func(t float64) float64 {
		if t > 216.0/24389.0 {
			return math.Cbrt(t)
		}
		return t*841.0/108.0 + 16.0/116.0
	}
	w := icc.PCSWhitePoint
	fx, fy, fz := f(X/w[0]), f(Y/w[1]), f(Z/w[2])
	return 116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)
}

func TestIntentProfileRoundTrip(t *testing.T) {
	for _, v := range []icc.Version{icc.Version2_1_0, icc.Version4_3_0} {
		p := IntentProfile(v, icc.OutputDeviceProfile)
		data, err := p.Encode()
		if err != nil {
			t.Fatal(err)
		}
		q, err := icc.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if !p.Equal(q) {
			t.Errorf("%v: round trip changed the profile", v)
		}
		for tag, data := range q.TagData {
			if tag == icc.MediaWhitePoint {
				continue
			}
			if _, err := icc.DecodeLUT(data); err != nil {
				t.Errorf("%v %v: %v", v, tag, err)
			}
		}
		if q.ProfileType() != icc.ProfileTypeLUT {
			t.Errorf("%v: profile type %v", v, q.ProfileType())
		}
	}
}

func TestIntentProfileTables(t *testing.T) {
	p := IntentProfile(icc.Version2_1_0, icc.DisplayDeviceProfile)
	ws := &icc.Workspace{}

	for _, tc := range []struct {
		intent icc.RenderingIntent
		L, a   func(g float64) float64
		g      func(L float64) float64
	}{
		{
			intent: icc.Perceptual,
			L:      func(g float64) float64 { return 20 + 80*g },
			a:      func(float64) float64 { return 0 },
			g:      func(L float64) float64 { return min(max((L-20)/80, 0), 1) },
		},
		{
			intent: icc.RelativeColorimetric,
			L:      func(g float64) float64 { return 100 * g },
			a:      func(float64) float64 { return 0 },
			g:      func(L float64) float64 { return L / 100 },
		},
		{
			intent: icc.Saturation,
			L:      func(g float64) float64 { return 100 * g },
			a:      func(g float64) float64 { return 100 * g * (1 - g) },
			g:      func(L float64) float64 { return L / 100 },
		},
	} {
		tr, err := icc.NewTransform(p, tc.intent)
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range []float64{0, 0.1, 0.25, 0.5, 0.9, 1} {
			L, a, b := lab(tr.ToXYZ([]float64{g}, ws))
			if math.Abs(L-tc.L(g)) > 0.05 || math.Abs(a-tc.a(g)) > 0.05 || math.Abs(b) > 0.05 {
				t.Errorf("%v: g = %g gives Lab (%g, %g, %g), want (%g, %g, 0)",
					tc.intent, g, L, a, b, tc.L(g), tc.a(g))
			}
		}

		out := make([]float64, 1)
		for _, L := range []float64{0, 10, 30, 50, 90, 100} {
			fy := (L + 16) / 116
			var Y float64
			if fy > 6.0/29.0 {
				Y = fy * fy * fy
			} else {
				Y = (fy - 16.0/116.0) * 108.0 / 841.0
			}
			w := icc.PCSWhitePoint
			tr.FromXYZ(w[0]*Y, Y, w[2]*Y, out, ws)
			if math.Abs(out[0]-tc.g(L)) > 1e-3 {
				t.Errorf("%v: L* = %g gives g = %g, want %g", tc.intent, L, out[0], tc.g(L))
			}
		}
	}

	// the three intents give distinct colours
	var Ys, Xs []float64
	for _, intent := range []icc.RenderingIntent{icc.Perceptual, icc.RelativeColorimetric, icc.Saturation} {
		tr, err := icc.NewTransform(p, intent)
		if err != nil {
			t.Fatal(err)
		}
		X, Y, _ := tr.ToXYZ([]float64{0.5}, ws)
		Xs = append(Xs, X)
		Ys = append(Ys, Y)
	}
	if Ys[0] == Ys[1] || Xs[1] == Xs[2] || Ys[1] != Ys[2] {
		t.Errorf("intents not distinct: X = %v, Y = %v", Xs, Ys)
	}
}
