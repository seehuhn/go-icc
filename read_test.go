// seehuhn.de/go/icc - read and write ICC profiles
// Copyright (C) 2024  Jochen Voss <voss@seehuhn.de>
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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestDateTime(t *testing.T) {
	in := []byte{
		byte(2020 >> 8), byte(2020 & 0xFF),
		0, 1,
		0, 2,
		0, 4,
		0, 5,
		0, 6,
	}
	want := "2020-01-02 04:05:06 +0000 UTC"
	got := getDateTime(in, 0).String()
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

type testCase struct {
	Name    string
	Profile *Profile
}

var testCases = []testCase{
	{
		Name: "minimal",
		Profile: &Profile{
			Version:      Version4_4_0,
			TagData:      make(map[TagType][]byte),
			CreationDate: time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC),
		},
	},
	{
		Name: "with-tag",
		Profile: &Profile{
			Version: Version4_4_0,
			TagData: map[TagType][]byte{
				0x100: {0, 0, 0, 0},
			},
			CreationDate: time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC),
		},
	},
	{
		Name: "with-chrm-tag",
		Profile: &Profile{
			Version: Version4_4_0,
			TagData: map[TagType][]byte{
				0x100:      {0, 0, 0, 0},
				0x6368726D: {0, 0, 0, 0}, // "chrm"
			},
			CreationDate: time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC),
		},
	},
	{
		Name: "display-sRGB",
		Profile: &Profile{
			Version:         Version4_4_0,
			Class:           DisplayDeviceProfile,
			ColorSpace:      RGBSpace,
			PCS:             PCSXYZSpace,
			RenderingIntent: Perceptual,
			TagData:         make(map[TagType][]byte),
			CreationDate:    time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
		},
	},
	{
		Name: "output-CMYK",
		Profile: &Profile{
			Version:         Version4_3_0,
			Class:           OutputDeviceProfile,
			ColorSpace:      CMYKSpace,
			PCS:             PCSLabSpace,
			RenderingIntent: RelativeColorimetric,
			TagData:         make(map[TagType][]byte),
			CreationDate:    time.Date(2023, 3, 10, 8, 30, 0, 0, time.UTC),
		},
	},
}

func testRoundTrip(t *testing.T, p *Profile) {
	t.Helper()

	// encode
	data, err := p.Encode()
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	// decode
	q, err := Decode(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	// normalise checksum before comparison (encoding recalculates it)
	p.CheckSum = CheckSumMissing
	q.CheckSum = CheckSumMissing

	if diff := cmp.Diff(p, q); diff != "" {
		t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
	}
}

func TestRoundTrip(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			testRoundTrip(t, tc.Profile)
		})
	}
}

func FuzzRoundTrip(f *testing.F) {
	// seed corpus with test cases
	for _, tc := range testCases {
		data, err := tc.Profile.Encode()
		if err != nil {
			continue
		}
		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Decode(data)
		if err != nil {
			t.Skip("invalid ICC profile")
		}

		testRoundTrip(t, p)
	})
}

// Curve round-trip tests

type curveTestCase struct {
	Name  string
	Curve *Curve
}

var curveTestCases = []curveTestCase{
	{
		Name:  "identity",
		Curve: &Curve{Gamma: 1.0},
	},
	{
		// gamma encoded as u8Fixed8: 2.25 = 576/256
		Name:  "gamma-2.25",
		Curve: &Curve{Gamma: 2.25},
	},
	{
		// gamma encoded as u8Fixed8: 1.75 = 448/256
		Name:  "gamma-1.75",
		Curve: &Curve{Gamma: 1.75},
	},
	{
		Name:  "sampled-linear",
		Curve: &Curve{Table: []uint16{0, 32768, 65535}},
	},
	{
		Name:  "sampled-256",
		Curve: &Curve{Table: makeLinearTable(256)},
	},
	{
		// params encoded as s15Fixed16: 2.5 = 163840/65536
		Name:  "parametric-type0",
		Curve: &Curve{FuncType: 0, Params: []float64{2.5}},
	},
	{
		Name:  "parametric-type1",
		Curve: &Curve{FuncType: 1, Params: []float64{2.5, 1.0, 0.0}},
	},
	{
		Name:  "parametric-type2",
		Curve: &Curve{FuncType: 2, Params: []float64{2.5, 1.0, 0.0, 0.0}},
	},
	{
		Name:  "parametric-type3",
		Curve: &Curve{FuncType: 3, Params: []float64{2.5, 1.0, 0.0, 0.5, 0.125}},
	},
	{
		Name:  "parametric-type4",
		Curve: &Curve{FuncType: 4, Params: []float64{2.5, 1.0, 0.0, 0.5, 0.125, 0.0, 0.0}},
	},
}

func makeLinearTable(n int) []uint16 {
	table := make([]uint16, n)
	for i := range table {
		table[i] = uint16(float64(i) / float64(n-1) * 65535)
	}
	return table
}

func testCurveRoundTrip(t *testing.T, c *Curve) {
	t.Helper()

	// encode
	data := c.Encode()

	// decode
	d, err := DecodeCurve(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	// compare (ignoring cached inverse table)
	opt := cmp.FilterPath(func(p cmp.Path) bool {
		return p.Last().String() == ".inverseTable"
	}, cmp.Ignore())

	if diff := cmp.Diff(c, d, opt); diff != "" {
		t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
	}
}

func TestCurveRoundTrip(t *testing.T) {
	for _, tc := range curveTestCases {
		t.Run(tc.Name, func(t *testing.T) {
			testCurveRoundTrip(t, tc.Curve)
		})
	}
}

func FuzzCurveRoundTrip(f *testing.F) {
	// seed corpus with test cases
	for _, tc := range curveTestCases {
		f.Add(tc.Curve.Encode())
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := DecodeCurve(data)
		if err != nil {
			t.Skip("invalid curve data")
		}

		testCurveRoundTrip(t, c)
	})
}
