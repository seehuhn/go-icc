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

			tr, err := NewTransform(p, DeviceToPCS, Perceptual)
			if err != nil {
				t.Fatalf("NewTransform failed: %v", err)
			}

			// D50 white point
			X, Y, Z := tr.ToXYZ([]float64{1, 1, 1})
			if math.Abs(X-0.9642) > 0.02 || math.Abs(Y-1.0) > 0.02 || math.Abs(Z-0.8249) > 0.02 {
				t.Errorf("white -> XYZ = (%v, %v, %v), want D50 white point", X, Y, Z)
			}

			// black
			X, Y, Z = tr.ToXYZ([]float64{0, 0, 0})
			if math.Abs(X) > 0.01 || math.Abs(Y) > 0.01 || math.Abs(Z) > 0.01 {
				t.Errorf("black -> XYZ = (%v, %v, %v), want near zero", X, Y, Z)
			}

			// luminance of red < green (standard sRGB property)
			_, yR, _ := tr.ToXYZ([]float64{1, 0, 0})
			_, yG, _ := tr.ToXYZ([]float64{0, 1, 0})
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

			tr, err := NewTransform(p, DeviceToPCS, Perceptual)
			if err != nil {
				t.Fatalf("NewTransform failed: %v", err)
			}

			for _, pp := range primaries {
				t.Run(pp.name, func(t *testing.T) {
					X, Y, Z := tr.ToXYZ(pp.input)
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

			fwd, err := NewTransform(p, DeviceToPCS, Perceptual)
			if err != nil {
				t.Fatalf("NewTransform(DeviceToPCS) failed: %v", err)
			}

			inv, err := NewTransform(p, PCSToDevice, Perceptual)
			if err != nil {
				t.Fatalf("NewTransform(PCSToDevice) failed: %v", err)
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
				X, Y, Z := fwd.ToXYZ(rgb)
				back := inv.FromXYZ(X, Y, Z)

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
