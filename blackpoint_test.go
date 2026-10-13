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
)

// constLabLUT returns an encoded lut16Type table which maps every input to
// the neutral colour with lightness L.
func constLabLUT(t *testing.T, nIn int, L float64) []byte {
	t.Helper()
	clut := make([]float32, computeCLUTSizeUniform(2, nIn, 3))
	for i := 0; i < len(clut); i += 3 {
		clut[i] = float32(L / 100 / legacyLabScale)
		clut[i+1] = float32(0x8000 / 65535.0)
		clut[i+2] = float32(0x8000 / 65535.0)
	}
	lut := &LUT16{nInput: nIn, nOutput: 3, gridPoints: 2, clut: clut}
	data, err := lut.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// constLabProfile returns a v2 output profile in the given colour space,
// whose A2B0 table maps everything to the neutral colour with lightness L.
func constLabProfile(t *testing.T, space ColorSpace, L float64) *Profile {
	t.Helper()
	nIn := space.NumComponents()
	if nIn == 0 {
		nIn = 3
	}
	return &Profile{
		Version:    Version2_1_0,
		Class:      OutputDeviceProfile,
		ColorSpace: space,
		PCS:        PCSLabSpace,
		TagData: map[TagType][]byte{
			AToB0: constLabLUT(t, nIn, L),
		},
	}
}

func lightness(xyz [3]float64) float64 {
	L, _, _ := xyzToLab(xyz[0], xyz[1], xyz[2])
	return L
}

func mustTransform(t *testing.T, p *Profile, intent RenderingIntent) *Transform {
	t.Helper()
	tr, err := NewTransform(p, intent)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestSourceBlackPointSRGB(t *testing.T) {
	for _, data := range [][]byte{SRGBv2Profile, SRGBv4Profile} {
		p, err := Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, intent := range []RenderingIntent{Perceptual, RelativeColorimetric, Saturation, AbsoluteColorimetric} {
			tr := mustTransform(t, p, intent)
			if bp := tr.SourceBlackPoint(); bp != [3]float64{} {
				t.Errorf("%v %v: source black point %v, want 0", p.Version, intent, bp)
			}
			if bp := tr.DestinationBlackPoint(); bp != [3]float64{} {
				t.Errorf("%v %v: destination black point %v, want 0", p.Version, intent, bp)
			}
		}
	}
}

// TestSourceBlackPointCMYK checks that the relative colorimetric black point
// of an output CMYK profile comes from the perceptual round trip, which
// discounts ink limits.
func TestSourceBlackPointCMYK(t *testing.T) {
	p, err := Decode(CMYKProfile)
	if err != nil {
		t.Fatal(err)
	}
	tr := mustTransform(t, p, RelativeColorimetric)
	got := tr.SourceBlackPoint()

	perc := mustTransform(t, p, Perceptual)
	ws := &Workspace{}
	device := make([]float64, 4)
	perc.FromXYZ(0, 0, 0, device, ws)
	want := neutralBlack(tr.ToXYZ(device, ws))
	if got != want {
		t.Errorf("black point %v, want %v", got, want)
	}

	// littleCMS 2.19 (transicc) gives L* = 11.3526 for this round trip; the
	// difference comes from the different CLUT interpolation schemes.
	pinned := [3]float64{0.01266823, 0.01313859, 0.01083802}
	for i := range got {
		if math.Abs(got[i]-pinned[i]) > 1e-7 {
			t.Errorf("black point %v, want %v", got, pinned)
			break
		}
	}

	// for perceptual and saturation, the darkest colorant is used
	for _, intent := range []RenderingIntent{Perceptual, Saturation} {
		tr := mustTransform(t, p, intent)
		want := neutralBlack(tr.ToXYZ([]float64{1, 1, 1, 1}, ws))
		if got := tr.SourceBlackPoint(); got != want {
			t.Errorf("%v: black point %v, want %v", intent, got, want)
		}
	}
}

func TestSourceBlackPointNoBlack(t *testing.T) {
	cmyk, err := Decode(CMYKProfile)
	if err != nil {
		t.Fatal(err)
	}
	for _, class := range []ProfileClass{DeviceLinkProfile, AbstractProfile, NamedColorProfile} {
		p := *cmyk
		p.Class = class
		tr := mustTransform(t, &p, Perceptual)
		if bp := tr.SourceBlackPoint(); bp != [3]float64{} {
			t.Errorf("%v: source black point %v, want 0", class, bp)
		}
		if bp := tr.DestinationBlackPoint(); bp != [3]float64{} {
			t.Errorf("%v: destination black point %v, want 0", class, bp)
		}
	}

	tr := mustTransform(t, cmyk, AbsoluteColorimetric)
	if bp := tr.SourceBlackPoint(); bp != [3]float64{} {
		t.Errorf("absolute: source black point %v, want 0", bp)
	}
	if bp := tr.DestinationBlackPoint(); bp != [3]float64{} {
		t.Errorf("absolute: destination black point %v, want 0", bp)
	}

	// relative colorimetric CMYK output profiles without the tables for the
	// perceptual round trip
	for _, tags := range [][]TagType{
		{BToA0, BToA1, BToA2},
		{AToB0, AToB1, AToB2},
	} {
		p := &Profile{
			Version:    cmyk.Version,
			Class:      OutputDeviceProfile,
			ColorSpace: CMYKSpace,
			PCS:        cmyk.PCS,
			TagData:    map[TagType][]byte{},
		}
		for _, tag := range tags {
			p.TagData[tag] = cmyk.TagData[tag]
		}
		tr := mustTransform(t, p, RelativeColorimetric)
		if bp := tr.SourceBlackPoint(); bp != [3]float64{} {
			t.Errorf("tags %v: source black point %v, want 0", tags, bp)
		}
	}
}

func TestSourceBlackPointDarkerColorant(t *testing.T) {
	for _, tc := range []struct {
		space ColorSpace
		L     float64
		want  float64
	}{
		{CMYSpace, 30, 30},
		{CMYKSpace, 60, 50}, // clipped to L* = 50
		{CIELabSpace, 20, 20},
		{GraySpace, 10, 10},
		{RGBSpace, 15, 15},
		{HSVSpace, 30, 0}, // no known darkest colorant
	} {
		p := constLabProfile(t, tc.space, tc.L)
		tr := mustTransform(t, p, Perceptual)
		bp := tr.SourceBlackPoint()
		if L := lightness(bp); math.Abs(L-tc.want) > 0.01 {
			t.Errorf("%v: black point L* = %g, want %g", tc.space, L, tc.want)
		}
		if tc.want > 0 {
			_, a, b := xyzToLab(bp[0], bp[1], bp[2])
			if math.Abs(a) > 1e-9 || math.Abs(b) > 1e-9 {
				t.Errorf("%v: black point not neutral: a* = %g, b* = %g", tc.space, a, b)
			}
		}
	}

	// a LUT profile which cannot convert to PCS
	p := constLabProfile(t, GraySpace, 10)
	p.TagData = map[TagType][]byte{BToA0: p.TagData[AToB0]}
	tr := mustTransform(t, p, Perceptual)
	if bp := tr.SourceBlackPoint(); bp != [3]float64{} {
		t.Errorf("no A2B: black point %v, want 0", bp)
	}
}

// TestSourceBlackPointV4 checks the version 4 rule for the perceptual and
// saturation intents.
func TestSourceBlackPointV4(t *testing.T) {
	// a LUT profile uses the perceptual reference medium black
	p := constLabProfile(t, GraySpace, 10)
	p.Version = Version4_3_0
	for _, intent := range []RenderingIntent{Perceptual, Saturation} {
		tr := mustTransform(t, p, intent)
		if bp := tr.SourceBlackPoint(); bp != PerceptualBlackPoint {
			t.Errorf("%v: black point %v, want %v", intent, bp, PerceptualBlackPoint)
		}
	}
	tr := mustTransform(t, p, RelativeColorimetric)
	if L := lightness(tr.SourceBlackPoint()); math.Abs(L-10) > 0.01 {
		t.Errorf("relative: black point L* = %g, want 10", L)
	}

	// a gray TRC profile uses the darkest colorant
	g := makeGrayProfile(1, PCSWhitePoint)
	g.Version = Version4_3_0
	g.TagData[GrayTRC] = mustEncodeCurve(&Curve{Params: []float64{1, 0.9, 0.1}})
	tr = mustTransform(t, g, Perceptual)
	want := neutralBlack(tr.ToXYZ([]float64{0}, &Workspace{}))
	if bp := tr.SourceBlackPoint(); bp != want || math.Abs(bp[1]-0.1) > 1e-4 {
		t.Errorf("gray: black point %v, want %v", bp, want)
	}
}

func TestIsMatrixShaper(t *testing.T) {
	srgb, err := Decode(SRGBv4Profile)
	if err != nil {
		t.Fatal(err)
	}
	if !isMatrixShaper(srgb) {
		t.Error("sRGB: not a matrix shaper")
	}
	p := *srgb
	p.TagData = map[TagType][]byte{RedTRC: srgb.TagData[RedTRC]}
	if isMatrixShaper(&p) {
		t.Error("RGB with missing tags: matrix shaper")
	}
	if !isMatrixShaper(makeGrayProfile(1, PCSWhitePoint)) {
		t.Error("gray: not a matrix shaper")
	}
	if isMatrixShaper(constLabProfile(t, CMYKSpace, 10)) {
		t.Error("CMYK: matrix shaper")
	}
}

func TestDestinationBlackPointFallback(t *testing.T) {
	// not gray, RGB or CMYK
	p := constLabProfile(t, CMYSpace, 30)
	p.TagData[BToA0] = constLabLUT(t, 3, 30)
	tr := mustTransform(t, p, Perceptual)
	if got, want := tr.DestinationBlackPoint(), tr.SourceBlackPoint(); got != want {
		t.Errorf("CMY: destination %v, want %v", got, want)
	}

	// no usable B2A table
	p = constLabProfile(t, GraySpace, 30)
	tr = mustTransform(t, p, Perceptual)
	if got, want := tr.DestinationBlackPoint(), tr.SourceBlackPoint(); got != want {
		t.Errorf("no B2A: destination %v, want %v", got, want)
	}
	p.TagData[BToA0] = []byte("mft2")
	tr = mustTransform(t, p, Perceptual)
	if got, want := tr.DestinationBlackPoint(), tr.SourceBlackPoint(); got != want {
		t.Errorf("broken B2A: destination %v, want %v", got, want)
	}

	// B2A only: the round trip cannot return to PCS
	p.TagData = map[TagType][]byte{BToA0: constLabLUT(t, 3, 30)}
	tr = mustTransform(t, p, Perceptual)
	if bp := tr.DestinationBlackPoint(); bp != [3]float64{} {
		t.Errorf("B2A only: destination %v, want 0", bp)
	}
}

// TestDestinationBlackPointCMYK runs the round-trip analysis on the built-in
// CMYK profile.
func TestDestinationBlackPointCMYK(t *testing.T) {
	p, err := Decode(CMYKProfile)
	if err != nil {
		t.Fatal(err)
	}
	for _, intent := range []RenderingIntent{Perceptual, RelativeColorimetric, Saturation} {
		tr := mustTransform(t, p, intent)
		L := lightness(tr.DestinationBlackPoint())
		if L < 5 || L > 20 {
			t.Errorf("%v: destination black point L* = %g", intent, L)
		}
	}
}

func TestBlackPointFromRamps(t *testing.T) {
	newRamps := func(f func(L float64) float64) (*[256]float64, *[256]float64) {
		var in, out [256]float64
		for l := range in {
			in[l] = float64(l) * 100 / 255
			out[l] = f(in[l])
		}
		return &in, &out
	}
	initial := [3]float64{5, 1, -1}

	// a flat ramp has no black point
	in, out := newRamps(func(float64) float64 { return 50 })
	if _, ok := blackPointFromRamps(Perceptual, initial, in, out); ok {
		t.Error("flat ramp: black point found")
	}

	// a straight ramp keeps the initial guess under relative colorimetric
	in, out = newRamps(func(L float64) float64 { return max(L, 5) })
	lab, ok := blackPointFromRamps(RelativeColorimetric, initial, in, out)
	if !ok || lab != initial {
		t.Errorf("straight ramp: %v, %t, want %v", lab, ok, initial)
	}

	// a clipped ramp, fitted by a straight line
	in, out = newRamps(func(L float64) float64 { return max(0, 1.25*(L-20)) })
	lab, ok = blackPointFromRamps(Perceptual, initial, in, out)
	if !ok || math.Abs(lab[0]-20) > 1e-6 || lab[1] != initial[1] || lab[2] != initial[2] {
		t.Errorf("clipped ramp: %v, %t, want L* = 20", lab, ok)
	}

	// a ramp which bends away from the straight line, fitted by a quadratic
	in, out = newRamps(func(L float64) float64 {
		if L < 20 {
			return 10
		}
		return 10 + 90*(L-20)*(L+20)/9600
	})
	lab, ok = blackPointFromRamps(RelativeColorimetric, initial, in, out)
	if !ok || math.Abs(lab[0]-20) > 1e-6 {
		t.Errorf("curved ramp: %v, %t, want L* = 20", lab, ok)
	}

	// a step leaves too few points for the fit
	in, out = newRamps(func(L float64) float64 {
		if L < 50 {
			return 0
		}
		return 100
	})
	if _, ok := blackPointFromRamps(Perceptual, initial, in, out); ok {
		t.Error("step: black point found")
	}
}

func TestQuadraticFitRoot(t *testing.T) {
	xs := []float64{10, 20, 30, 40, 50}
	ys := func(f func(x float64) float64) []float64 {
		y := make([]float64, len(xs))
		for i, x := range xs {
			y[i] = f(x)
		}
		return y
	}
	for _, tc := range []struct {
		name string
		x, y []float64
		want float64
	}{
		{"quadratic", xs, ys(func(x float64) float64 { return (x - 5) * (x + 3) / 1000 }), 5},
		{"line", xs, ys(func(x float64) float64 { return 0.02 * (x - 8) }), 8},
		{"line beyond 50", xs, ys(func(x float64) float64 { return 0.02 * (x - 80) }), 50},
		{"line below 0", xs, ys(func(x float64) float64 { return 0.02 * (x + 8) }), 0},
		{"constant", xs, ys(func(float64) float64 { return 0.2 }), 0},
		{"no real root", xs, ys(func(x float64) float64 { return 1 + x*x/1000 }), 0},
		{"singular", []float64{1, 1, 1, 1}, []float64{0.1, 0.2, 0.3, 0.4}, 0},
	} {
		if got := quadraticFitRoot(tc.x, tc.y); math.Abs(got-tc.want) > 1e-6 {
			t.Errorf("%s: root %g, want %g", tc.name, got, tc.want)
		}
	}
}

func TestBlackPointScaling(t *testing.T) {
	src := [3]float64{0.03, 0.031, 0.025}
	dst := [3]float64{0.003, 0.0031, 0.0025}
	s, ok := NewBlackPointScaling(src, dst)
	if !ok {
		t.Fatal("scaling not created")
	}
	check := func(name string, in, want [3]float64) {
		t.Helper()
		X, Y, Z := s.Apply(in[0], in[1], in[2])
		got := [3]float64{X, Y, Z}
		for i := range got {
			if math.Abs(got[i]-want[i]) > 1e-12 {
				t.Errorf("%s: %v -> %v, want %v", name, in, got, want)
				break
			}
		}
	}
	check("black", src, dst)
	check("white", PCSWhitePoint, PCSWhitePoint)

	if _, ok := NewBlackPointScaling(src, src); ok {
		t.Error("identity reported as non-trivial")
	}
	if _, ok := NewBlackPointScaling([3]float64{0.03, 1.0, 0.025}, dst); ok {
		t.Error("degenerate scaling reported as usable")
	}
}

func TestBlackPointScalingNonFinite(t *testing.T) {
	good := [3]float64{0.01, 0.01, 0.01}
	nan := math.NaN()
	for _, bad := range [][3]float64{{nan, 0, 0}, {0, nan, 0}, {0, 0, math.Inf(1)}, {0, 0, math.Inf(-1)}} {
		if _, ok := NewBlackPointScaling(bad, good); ok {
			t.Errorf("source %v accepted", bad)
		}
		if _, ok := NewBlackPointScaling(good, bad); ok {
			t.Errorf("destination %v accepted", bad)
		}
	}
}
