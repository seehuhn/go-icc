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

import "math"

// PerceptualBlackPoint is the black point of the ICC perceptual reference
// medium, as media-relative PCS XYZ.  It is the source and destination black
// point of version 4 profiles under the perceptual and saturation intents,
// and corresponds to L* = 3.1373.
var PerceptualBlackPoint = [3]float64{0.00336, 0.0034731, 0.00287}

// SourceBlackPoint returns the black point of the profile when it is the
// source of a conversion under t.Intent (ISO 18619).
//
// The result is media-relative PCS XYZ, neutral, and has L* at most 50.  It
// is zero for device link, abstract and named colour profiles, under
// [AbsoluteColorimetric], and whenever the profile does not allow the black
// point to be determined.
//
// The lookup follows the usual AToB/BToA fallback chain when the tag for
// the intent is absent.
func (t *Transform) SourceBlackPoint() [3]float64 {
	t.bpOnce.Do(func() {
		t.bp = t.detectSourceBlackPoint()
	})
	return t.bp
}

// DestinationBlackPoint returns the black point of the profile when it is
// the destination of a conversion under t.Intent.
//
// For LUT-based gray, RGB and CMYK profiles, the black point is found by
// analysing the round trip from PCS to device and back, as described in ISO
// 18619.  This discounts ink limits in the device-to-PCS tables.  For all
// other profiles the result equals [Transform.SourceBlackPoint].  The result
// is media-relative PCS XYZ.
//
// The lookup follows the usual AToB/BToA fallback chain when the tag for
// the intent is absent.
func (t *Transform) DestinationBlackPoint() [3]float64 {
	t.dbpOnce.Do(func() {
		t.dbp = t.detectDestinationBlackPoint()
	})
	return t.dbp
}

func (t *Transform) detectSourceBlackPoint() [3]float64 {
	p := t.Profile
	if !hasBlackPoint(p, t.Intent) {
		return [3]float64{}
	}

	if p.Version >= Version4_0_0 && (t.Intent == Perceptual || t.Intent == Saturation) {
		if isMatrixShaper(p) {
			// matrix/TRC profiles share one table for all intents
			return darkerColorantBlack(t.withIntent(RelativeColorimetric))
		}
		return PerceptualBlackPoint
	}

	// discount ink limiting in output profiles
	if t.Intent == RelativeColorimetric && p.Class == OutputDeviceProfile && p.ColorSpace == CMYKSpace {
		return t.perceptualRoundTripBlack()
	}

	return darkerColorantBlack(t)
}

// hasBlackPoint reports whether black point detection applies to p under
// the given intent.
func hasBlackPoint(p *Profile, intent RenderingIntent) bool {
	switch p.Class {
	case DeviceLinkProfile, AbstractProfile, NamedColorProfile:
		return false
	}
	switch intent {
	case Perceptual, RelativeColorimetric, Saturation:
		return true
	default:
		return false
	}
}

// isMatrixShaper reports whether p carries the tags of a matrix/TRC or a gray
// TRC profile.  Unlike [Profile.ProfileType], this ignores any LUT tags.
func isMatrixShaper(p *Profile) bool {
	has := func(tags ...TagType) bool {
		for _, tag := range tags {
			if _, ok := p.TagData[tag]; !ok {
				return false
			}
		}
		return true
	}
	switch p.ColorSpace {
	case GraySpace:
		return has(GrayTRC)
	case RGBSpace:
		return has(RedMatrixColumn, GreenMatrixColumn, BlueMatrixColumn,
			RedTRC, GreenTRC, BlueTRC)
	default:
		return false
	}
}

// withIntent returns a transform for the same profile as t, using the given
// intent.  It returns nil if the transform cannot be created.
func (t *Transform) withIntent(intent RenderingIntent) *Transform {
	if intent == t.Intent {
		return t
	}
	tr, err := NewTransform(t.Profile, intent)
	if err != nil {
		return nil
	}
	return tr
}

// darkerColorantBlack converts the darkest colorant combination of the
// device space to PCS, and returns the neutral colour of the same lightness,
// clipped to L* <= 50.
func darkerColorantBlack(tr *Transform) [3]float64 {
	if tr == nil || !tr.CanToXYZ() {
		return [3]float64{}
	}

	var device []float64
	switch tr.Profile.ColorSpace {
	case GraySpace:
		device = []float64{0}
	case RGBSpace:
		device = []float64{0, 0, 0}
	case CMYSpace:
		device = []float64{1, 1, 1}
	case CMYKSpace:
		device = []float64{1, 1, 1, 1}
	case CIELabSpace:
		device = []float64{0, 128.0 / 255.0, 128.0 / 255.0}
	default:
		return [3]float64{}
	}

	return neutralBlack(tr.toPCS(device, &Workspace{}))
}

// perceptualRoundTripBlack maps PCS black to device values through the
// perceptual PCS-to-device table, and back to PCS through the relative
// colorimetric device-to-PCS table.
func (t *Transform) perceptualRoundTripBlack() [3]float64 {
	perc := t.withIntent(Perceptual)
	rel := t.withIntent(RelativeColorimetric)
	if perc == nil || rel == nil || !perc.CanFromXYZ() || !rel.CanToXYZ() {
		return [3]float64{}
	}

	ws := &Workspace{}
	device := make([]float64, t.Channels())
	perc.fromPCS(0, 0, 0, device, ws)
	return neutralBlack(rel.toPCS(device, ws))
}

// neutralBlack returns the neutral colour with the lightness of X, Y, Z,
// clipped to L* <= 50.
func neutralBlack(X, Y, Z float64) [3]float64 {
	L, _, _ := xyzToLab(X, Y, Z)
	X, Y, Z = labToXYZ([]float64{min(L, 50), 0, 0})
	return [3]float64{X, Y, Z}
}

func (t *Transform) detectDestinationBlackPoint() [3]float64 {
	p := t.Profile
	if !hasBlackPoint(p, t.Intent) {
		return [3]float64{}
	}

	if p.Version >= Version4_0_0 && (t.Intent == Perceptual || t.Intent == Saturation) {
		return t.SourceBlackPoint()
	}

	if t.profileType != ProfileTypeLUT || !t.CanFromXYZ() {
		return t.SourceBlackPoint()
	}
	switch p.ColorSpace {
	case GraySpace, RGBSpace, CMYKSpace:
		// use the round trip below
	default:
		return t.SourceBlackPoint()
	}
	t.buildReverse()
	if t.revLUT == nil {
		return t.SourceBlackPoint()
	}

	rel := t.withIntent(RelativeColorimetric)
	if rel == nil || !rel.CanToXYZ() {
		return [3]float64{}
	}

	// initial guess, which should work for well-behaved profiles
	var initial [3]float64
	if t.Intent == RelativeColorimetric {
		bp := t.SourceBlackPoint()
		initial[0], initial[1], initial[2] = xyzToLab(bp[0], bp[1], bp[2])
	}

	// lightness ramp through PCS -> device -> PCS
	var inRamp, outRamp [256]float64
	ws := &Workspace{}
	device := make([]float64, t.Channels())
	lab := make([]float64, 3)
	a := clamp(initial[1], -50, 50)
	b := clamp(initial[2], -50, 50)
	for l := range 256 {
		lab[0], lab[1], lab[2] = float64(l)*100/255, a, b
		X, Y, Z := labToXYZ(lab)
		t.fromPCS(X, Y, Z, device, ws)
		X, Y, Z = rel.toPCS(device, ws)
		outL, _, _ := xyzToLab(X, Y, Z)
		inRamp[l] = lab[0]
		outRamp[l] = outL
	}

	black, ok := blackPointFromRamps(t.Intent, initial, &inRamp, &outRamp)
	if !ok {
		return [3]float64{}
	}
	X, Y, Z := labToXYZ(black[:])
	return [3]float64{X, Y, Z}
}

// blackPointFromRamps implements the analysis step of ISO 18619 destination
// black point detection.  The input ramp holds equally spaced L* values from
// 0 to 100, the output ramp the L* values they map to after a round trip
// through the destination profile.  The result is the black point as Lab.  If
// ok is false, the black point is zero.  The output ramp is modified.
func blackPointFromRamps(intent RenderingIntent, initial [3]float64, inRamp, outRamp *[256]float64) (lab [3]float64, ok bool) {
	// make the output ramp monotonic, leaving out[0] alone
	for l := 254; l > 0; l-- {
		outRamp[l] = min(outRamp[l], outRamp[l+1])
	}
	if !(outRamp[0] < outRamp[255]) {
		return lab, false
	}

	minL, maxL := outRamp[0], outRamp[255]

	// for relative colorimetric, a straight mid-range means that the initial
	// guess is good
	if intent == RelativeColorimetric {
		straight := true
		for l := range 256 {
			if !(inRamp[l] <= minL+0.2*(maxL-minL) || math.Abs(inRamp[l]-outRamp[l]) < 4) {
				straight = false
				break
			}
		}
		if straight {
			return initial, true
		}
	}

	// The round trip curve normally has a nearly constant section near the
	// black point, a corner, and then a nearly straight line to the white
	// point.  Fit a quadratic to the shadow part and locate its root.
	lo, hi := 0.03, 0.25
	if intent == RelativeColorimetric {
		lo, hi = 0.1, 0.5
	}
	var x, y []float64
	for l := range 256 {
		ff := (outRamp[l] - minL) / (maxL - minL)
		if ff >= lo && ff < hi {
			x = append(x, inRamp[l])
			y = append(y, ff)
		}
	}
	if len(x) < 4 {
		return lab, false
	}

	return [3]float64{quadraticFitRoot(x, y), initial[1], initial[2]}, true
}

// quadraticFitRoot fits y = a·x² + b·x + c by least squares and returns the
// larger root, clamped to [0, 50].  It returns 0 if the fit fails or the
// fitted curve has no real root.
func quadraticFitRoot(x, y []float64) float64 {
	var sx, sx2, sx3, sx4, sy, syx, syx2 float64
	for i, xi := range x {
		yi := y[i]
		x2 := xi * xi
		sx += xi
		sx2 += x2
		sx3 += x2 * xi
		sx4 += x2 * x2
		sy += yi
		syx += yi * xi
		syx2 += yi * x2
	}
	n := float64(len(x))

	inv := invertMatrix3x3([]float64{
		n, sx, sx2,
		sx, sx2, sx3,
		sx2, sx3, sx4,
	})
	if inv == nil {
		return 0
	}
	c := inv[0]*sy + inv[1]*syx + inv[2]*syx2
	b := inv[3]*sy + inv[4]*syx + inv[5]*syx2
	a := inv[6]*sy + inv[7]*syx + inv[8]*syx2

	if math.Abs(a) < 1e-10 {
		// the fit is a straight line; use its root
		if math.Abs(b) < 1e-10 {
			return 0
		}
		return clamp(-c/b, 0, 50)
	}

	d := b*b - 4*a*c
	if d <= 0 {
		return 0
	}
	return clamp((-b+math.Sqrt(d))/(2*a), 0, 50)
}

// BlackPointScaling is the PCS mapping of ISO 18619 black point compensation:
// per component a·v + b with a = (dst − W)/(src − W), b = W·(1 − a),
// W = PCSWhitePoint.
//
// The mapping takes the source black point to the destination black point
// and leaves the PCS white point fixed.  Use [NewBlackPointScaling] to
// construct a BlackPointScaling.
type BlackPointScaling struct{ scale, offset [3]float64 }

// NewBlackPointScaling returns the mapping which takes the source black
// point src to the destination black point dst.  It returns ok == false when
// there is nothing to apply: the mapping is the identity (src == dst), it is
// degenerate (src_k >= W_k for some k), or a component of src or dst is not
// finite.
//
// The arguments are media-relative PCS XYZ black points, as returned by
// [Transform.SourceBlackPoint] and [Transform.DestinationBlackPoint].
func NewBlackPointScaling(src, dst [3]float64) (s BlackPointScaling, ok bool) {
	if src == dst {
		return s, false
	}
	for k, W := range PCSWhitePoint {
		if !isFinite(src[k]) || !isFinite(dst[k]) || src[k] >= W {
			return BlackPointScaling{}, false
		}
		a := (dst[k] - W) / (src[k] - W)
		s.scale[k] = a
		s.offset[k] = W * (1 - a)
	}
	return s, true
}

// Apply maps a media-relative PCS XYZ value.
func (s BlackPointScaling) Apply(X, Y, Z float64) (float64, float64, float64) {
	return s.scale[0]*X + s.offset[0],
		s.scale[1]*Y + s.offset[1],
		s.scale[2]*Z + s.offset[2]
}

func isFinite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
