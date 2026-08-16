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
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestSRGBProfilesDecode(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		version Version
	}{
		{"v2", SRGBv2Profile, Version2_1_0},
		{"v4", SRGBv4Profile, Version4_2_0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Decode(tt.data)
			if err != nil {
				t.Fatalf("decode failed: %v", err)
			}

			if p.Class != DisplayDeviceProfile {
				t.Errorf("class = %v, want DisplayDeviceProfile", p.Class)
			}
			if p.ColorSpace != RGBSpace {
				t.Errorf("color space = %v, want RGB", p.ColorSpace)
			}
			if p.PCS != PCSXYZSpace {
				t.Errorf("PCS = %v, want PCSXYZ", p.PCS)
			}
			if p.Version < tt.version {
				t.Errorf("version = %v, want >= %v", p.Version, tt.version)
			}
		})
	}
}

func TestSRGBProfilesRoundTrip(t *testing.T) {
	for _, tt := range []struct {
		name string
		data []byte
	}{
		{"v2", SRGBv2Profile},
		{"v4", SRGBv4Profile},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Decode(tt.data)
			if err != nil {
				t.Fatalf("decode failed: %v", err)
			}

			encoded, err := p.Encode()
			if err != nil {
				t.Fatalf("encode failed: %v", err)
			}

			q, err := Decode(encoded)
			if err != nil {
				t.Fatalf("re-decode failed: %v", err)
			}

			p.CheckSum = CheckSumMissing
			q.CheckSum = CheckSumMissing

			if diff := cmp.Diff(p, q); diff != "" {
				t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSRGBProfilesTransform(t *testing.T) {
	for _, tt := range []struct {
		name string
		data []byte
	}{
		{"v2", SRGBv2Profile},
		{"v4", SRGBv4Profile},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Decode(tt.data)
			if err != nil {
				t.Fatalf("decode failed: %v", err)
			}

			tr, err := NewTransform(p, Perceptual)
			if err != nil {
				t.Fatalf("NewTransform failed: %v", err)
			}

			// D50 white point
			X, Y, Z := tr.ToXYZ([]float64{1, 1, 1}, &Workspace{})
			if math.Abs(X-0.9642) > 0.02 || math.Abs(Y-1.0) > 0.02 || math.Abs(Z-0.8249) > 0.02 {
				t.Errorf("white -> XYZ = (%v, %v, %v), want D50 white point", X, Y, Z)
			}

			// black
			X, Y, Z = tr.ToXYZ([]float64{0, 0, 0}, &Workspace{})
			if math.Abs(X) > 0.01 || math.Abs(Y) > 0.01 || math.Abs(Z) > 0.01 {
				t.Errorf("black -> XYZ = (%v, %v, %v), want near zero", X, Y, Z)
			}

			// luminance of red < green (standard sRGB property)
			_, yR, _ := tr.ToXYZ([]float64{1, 0, 0}, &Workspace{})
			_, yG, _ := tr.ToXYZ([]float64{0, 1, 0}, &Workspace{})
			if yR >= yG {
				t.Errorf("red luminance (%v) >= green luminance (%v)", yR, yG)
			}
		})
	}
}

// TestSRGBProfilesPrimaries checks that the sRGB primaries map to the
// expected XYZ coordinates in the D50 profile connection space.
// The reference values are the sRGB-to-XYZ(D65) matrix columns,
// adapted to D50 using the Bradford transform.
func TestSRGBProfilesPrimaries(t *testing.T) {
	// sRGB primaries in XYZ (D50), from Bradford adaptation of the
	// IEC 61966-2-1 matrix
	type xyz struct{ X, Y, Z float64 }
	primaries := []struct {
		name  string
		input []float64
		want  xyz
	}{
		{"red", []float64{1, 0, 0}, xyz{0.4361, 0.2225, 0.0139}},
		{"green", []float64{0, 1, 0}, xyz{0.3851, 0.7169, 0.0971}},
		{"blue", []float64{0, 0, 1}, xyz{0.1431, 0.0606, 0.7141}},
	}

	for _, tt := range []struct {
		name string
		data []byte
	}{
		{"v2", SRGBv2Profile},
		{"v4", SRGBv4Profile},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Decode(tt.data)
			if err != nil {
				t.Fatalf("decode failed: %v", err)
			}

			tr, err := NewTransform(p, Perceptual)
			if err != nil {
				t.Fatalf("NewTransform failed: %v", err)
			}

			for _, pp := range primaries {
				t.Run(pp.name, func(t *testing.T) {
					X, Y, Z := tr.ToXYZ(pp.input, &Workspace{})
					const eps = 0.005
					if math.Abs(X-pp.want.X) > eps ||
						math.Abs(Y-pp.want.Y) > eps ||
						math.Abs(Z-pp.want.Z) > eps {
						t.Errorf("XYZ = (%.4f, %.4f, %.4f), want (%.4f, %.4f, %.4f)",
							X, Y, Z, pp.want.X, pp.want.Y, pp.want.Z)
					}
				})
			}
		})
	}
}

func TestCMYKProfileDecode(t *testing.T) {
	p, err := Decode(CMYKProfile)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if p.Class != OutputDeviceProfile {
		t.Errorf("class = %v, want OutputDeviceProfile", p.Class)
	}
	if p.ColorSpace != CMYKSpace {
		t.Errorf("color space = %v, want CMYK", p.ColorSpace)
	}
	if p.PCS != PCSLabSpace {
		t.Errorf("PCS = %v, want PCSLab", p.PCS)
	}
	if p.Version < Version2_2_0 {
		t.Errorf("version = %v, want >= %v", p.Version, Version2_2_0)
	}
}

func TestCMYKProfileRoundTrip(t *testing.T) {
	p, err := Decode(CMYKProfile)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	encoded, err := p.Encode()
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	q, err := Decode(encoded)
	if err != nil {
		t.Fatalf("re-decode failed: %v", err)
	}

	p.CheckSum = CheckSumMissing
	q.CheckSum = CheckSumMissing

	if diff := cmp.Diff(p, q); diff != "" {
		t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
	}
}

func TestCMYKProfileTransform(t *testing.T) {
	p, err := Decode(CMYKProfile)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	tr, err := NewTransform(p, Perceptual)
	if err != nil {
		t.Fatalf("NewTransform failed: %v", err)
	}

	// CMYK white (no ink) should have high luminance
	X, Y, Z := tr.ToXYZ([]float64{0, 0, 0, 0}, &Workspace{})
	if Y < 0.5 {
		t.Errorf("white (0,0,0,0) -> XYZ = (%v, %v, %v), want high luminance", X, Y, Z)
	}

	// CMYK black (full K) should have low luminance
	X, Y, Z = tr.ToXYZ([]float64{0, 0, 0, 1}, &Workspace{})
	if Y > 0.1 {
		t.Errorf("black (0,0,0,1) -> XYZ = (%v, %v, %v), want low luminance", X, Y, Z)
	}

	// yellow should have higher luminance than cyan or magenta
	_, yC, _ := tr.ToXYZ([]float64{1, 0, 0, 0}, &Workspace{})
	_, yM, _ := tr.ToXYZ([]float64{0, 1, 0, 0}, &Workspace{})
	_, yY, _ := tr.ToXYZ([]float64{0, 0, 1, 0}, &Workspace{})
	if yY <= yC {
		t.Errorf("yellow luminance (%v) <= cyan luminance (%v)", yY, yC)
	}
	if yY <= yM {
		t.Errorf("yellow luminance (%v) <= magenta luminance (%v)", yY, yM)
	}
}

// TestCMYKProfilePaperIsPCSWhite checks that unprinted paper lands on the PCS
// white point.  The PCS is defined relative to D50 whatever the medium is, so
// the media white point of the profile must not be used as the reference white
// of the Lab decode; doing so leaves paper at about three quarters of the PCS
// luminance.
func TestCMYKProfilePaperIsPCSWhite(t *testing.T) {
	p, err := Decode(CMYKProfile)
	if err != nil {
		t.Fatal(err)
	}

	// each intent selects a different AToB table, and all of them are
	// media-relative
	for _, intent := range []RenderingIntent{Perceptual, RelativeColorimetric, Saturation} {
		t.Run(intent.String(), func(t *testing.T) {
			tr, err := NewTransform(p, intent)
			if err != nil {
				t.Fatal(err)
			}

			X, Y, Z := tr.ToXYZ([]float64{0, 0, 0, 0}, &Workspace{})

			// The tolerance covers the quantisation of the profile's own
			// tables, which account for about 2e-5 here; the bug this guards
			// against left paper a quarter of the PCS luminance short.
			const tol = 1e-3
			if math.Abs(X-PCSWhitePoint[0]) > tol ||
				math.Abs(Y-PCSWhitePoint[1]) > tol ||
				math.Abs(Z-PCSWhitePoint[2]) > tol {
				t.Errorf("paper white -> XYZ (%g, %g, %g), want %v", X, Y, Z, PCSWhitePoint)
			}
		})
	}
}

// TestCMYKProfileLegacyLabEncoding checks that PCS Lab values from a lut16Type
// tag are read in the legacy 16-bit encoding.  The reference values come from
// littleCMS 2.19 (transicc -t1) converting the same profile.
//
// Bare paper is the sharpest case: relative colorimetric rendering is defined
// against the media white, so no ink must land on the PCS white point, L* =
// 100.  Read in the current 16-bit encoding instead, the same table gives L* =
// 99.61, and every other colour is off by the same 0.39%.
func TestCMYKProfileLegacyLabEncoding(t *testing.T) {
	p, err := Decode(CMYKProfile)
	if err != nil {
		t.Fatal(err)
	}
	if p.PCS != PCSLabSpace {
		t.Fatalf("PCS = %v, want CIELAB", p.PCS)
	}
	tr, err := NewTransform(p, RelativeColorimetric)
	if err != nil {
		t.Fatal(err)
	}
	ws := &Workspace{}

	// The grid corners of the CLUT are hit exactly, so these need no
	// tolerance for interpolation, only for the 16-bit table quantisation.
	const tol = 5e-3
	for _, tc := range []struct {
		name    string
		cmyk    []float64
		L, a, b float64
	}{
		{"bare paper", []float64{0, 0, 0, 0}, 100, 0, 0},
		{"solid key", []float64{0, 0, 0, 1}, 22.3667, 1.0430, -0.1992},
		{"solid cyan", []float64{1, 0, 0, 0}, 63.6351, -39.8633, -48.8086},
		{"solid magenta", []float64{0, 1, 0, 0}, 53.6137, 76.8242, -6.9609},
		{"solid yellow", []float64{0, 0, 1, 0}, 94.9678, -7.3789, 90.6758},
	} {
		t.Run(tc.name, func(t *testing.T) {
			X, Y, Z := tr.ToXYZ(tc.cmyk, ws)
			L, a, b := xyzToLab(X, Y, Z)
			if math.Abs(L-tc.L) > tol || math.Abs(a-tc.a) > tol || math.Abs(b-tc.b) > tol {
				t.Errorf("Lab = (%.4f, %.4f, %.4f), want (%.4f, %.4f, %.4f)",
					L, a, b, tc.L, tc.a, tc.b)
			}
		})
	}

	// The reverse table carries the same encoding: asked for the PCS white
	// point, the profile must call for bare paper again.
	dst := make([]float64, 4)
	tr.FromXYZ(PCSWhitePoint[0], PCSWhitePoint[1], PCSWhitePoint[2], dst, ws)
	for i, v := range dst {
		if math.Abs(v) > 1e-4 {
			t.Errorf("PCS white asks for %g in channel %d, want no ink", v, i)
		}
	}
}

// TestCMYKProfileAbsoluteIntent checks that the absolute colorimetric intent
// reports paper as the media white point, and that the reverse direction
// undoes the scaling.
func TestCMYKProfileAbsoluteIntent(t *testing.T) {
	p, err := Decode(CMYKProfile)
	if err != nil {
		t.Fatal(err)
	}
	white, err := parseXYZ(p.TagData[MediaWhitePoint])
	if err != nil {
		t.Fatal(err)
	}
	tr, err := NewTransform(p, AbsoluteColorimetric)
	if err != nil {
		t.Fatal(err)
	}

	paper := []float64{0, 0, 0, 0}
	X, Y, Z := tr.ToXYZ(paper, &Workspace{})

	const tol = 0.02
	if math.Abs(X-white[0]) > tol || math.Abs(Y-white[1]) > tol || math.Abs(Z-white[2]) > tol {
		t.Errorf("paper white -> XYZ (%g, %g, %g), want %v", X, Y, Z, white)
	}

	back := make([]float64, 4)
	tr.FromXYZ(X, Y, Z, back, &Workspace{})
	for i, v := range back {
		if math.Abs(v-paper[i]) > 1e-3 {
			t.Errorf("round trip: channel %d = %g, want %g", i, v, paper[i])
		}
	}
}

// TestSRGBAbsoluteIntent checks the ICC-absolute scaling on the built-in
// matrix/TRC profiles.  White reports the media white point recorded in the
// profile: D50 for the version 4 profile, which adapts the tag as ICC v4
// requires, and the unadapted D65 measurement for the version 2 one.
func TestSRGBAbsoluteIntent(t *testing.T) {
	for _, tt := range []struct {
		name string
		data []byte
	}{
		{"v2", SRGBv2Profile},
		{"v4", SRGBv4Profile},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Decode(tt.data)
			if err != nil {
				t.Fatal(err)
			}
			white, err := parseXYZ(p.TagData[MediaWhitePoint])
			if err != nil {
				t.Fatal(err)
			}
			tr, err := NewTransform(p, AbsoluteColorimetric)
			if err != nil {
				t.Fatal(err)
			}

			X, Y, Z := tr.ToXYZ([]float64{1, 1, 1}, &Workspace{})
			const tol = 1e-3
			if math.Abs(X-white[0]) > tol ||
				math.Abs(Y-white[1]) > tol ||
				math.Abs(Z-white[2]) > tol {
				t.Errorf("white -> XYZ (%g, %g, %g), want %v", X, Y, Z, white)
			}

			rgb := []float64{0.2, 0.5, 0.8}
			X, Y, Z = tr.ToXYZ(rgb, &Workspace{})
			back := make([]float64, 3)
			tr.FromXYZ(X, Y, Z, back, &Workspace{})
			for i, v := range back {
				if math.Abs(v-rgb[i]) > 1e-4 {
					t.Errorf("round trip: channel %d = %g, want %g", i, v, rgb[i])
				}
			}
		})
	}
}

func TestSRGBProfilesDeviceRoundTrip(t *testing.T) {
	for _, tt := range []struct {
		name string
		data []byte
	}{
		{"v2", SRGBv2Profile},
		{"v4", SRGBv4Profile},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Decode(tt.data)
			if err != nil {
				t.Fatalf("decode failed: %v", err)
			}

			tr, err := NewTransform(p, Perceptual)
			if err != nil {
				t.Fatalf("NewTransform failed: %v", err)
			}

			inputs := [][]float64{
				{0, 0, 0},
				{1, 1, 1},
				{1, 0, 0},
				{0, 1, 0},
				{0, 0, 1},
				{0.5, 0.5, 0.5},
				{0.2, 0.4, 0.8},
			}

			for _, rgb := range inputs {
				X, Y, Z := tr.ToXYZ(rgb, &Workspace{})
				back := make([]float64, tr.Channels())
				tr.FromXYZ(X, Y, Z, back, &Workspace{})

				for i := range rgb {
					if math.Abs(back[i]-rgb[i]) > 0.02 {
						t.Errorf("round-trip %v -> XYZ(%v,%v,%v) -> %v",
							rgb, X, Y, Z, back)
						break
					}
				}
			}
		})
	}
}
