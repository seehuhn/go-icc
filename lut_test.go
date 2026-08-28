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
	"bytes"
	"math"
	"testing"
)

func TestDecodeLUT8(t *testing.T) {
	// build a minimal lut8Type (mft1) with identity mapping
	inputChannels := 3
	outputChannels := 3
	clutPoints := 2

	inputTableSize := 256 * inputChannels
	clutSize := clutPoints * clutPoints * clutPoints * outputChannels
	outputTableSize := 256 * outputChannels
	totalSize := 48 + inputTableSize + clutSize + outputTableSize

	data := make([]byte, totalSize)
	copy(data[0:4], "mft1")
	data[8] = byte(inputChannels)
	data[9] = byte(outputChannels)
	data[10] = byte(clutPoints)

	// identity matrix at offset 12
	putS15Fixed16(data, 12, 1.0)
	putS15Fixed16(data, 16, 0.0)
	putS15Fixed16(data, 20, 0.0)
	putS15Fixed16(data, 24, 0.0)
	putS15Fixed16(data, 28, 1.0)
	putS15Fixed16(data, 32, 0.0)
	putS15Fixed16(data, 36, 0.0)
	putS15Fixed16(data, 40, 0.0)
	putS15Fixed16(data, 44, 1.0)

	// identity input tables (256 entries per channel)
	offset := 48
	for ch := range inputChannels {
		for i := range 256 {
			data[offset+ch*256+i] = byte(i)
		}
	}
	offset += inputTableSize

	// identity CLUT (2x2x2 grid, output = input)
	for r := range clutPoints {
		for g := range clutPoints {
			for b := range clutPoints {
				idx := offset + (r*clutPoints*clutPoints+g*clutPoints+b)*outputChannels
				data[idx+0] = byte(r * 255)
				data[idx+1] = byte(g * 255)
				data[idx+2] = byte(b * 255)
			}
		}
	}
	offset += clutSize

	// identity output tables
	for ch := range outputChannels {
		for i := range 256 {
			data[offset+ch*256+i] = byte(i)
		}
	}

	lut, err := DecodeLUT(data)
	if err != nil {
		t.Fatalf("DecodeLUT failed: %v", err)
	}

	if lut.InputChannels() != inputChannels {
		t.Errorf("InputChannels = %d, want %d", lut.InputChannels(), inputChannels)
	}
	if lut.OutputChannels() != outputChannels {
		t.Errorf("OutputChannels = %d, want %d", lut.OutputChannels(), outputChannels)
	}

	lut8, ok := lut.(*LUT8)
	if !ok {
		t.Fatalf("expected *LUT8, got %T", lut)
	}
	if lut8.gridPoints != clutPoints {
		t.Errorf("gridPoints = %d, want %d", lut8.gridPoints, clutPoints)
	}

	// test that identity LUT produces identity output
	tests := [][]float64{
		{0, 0, 0},
		{1, 1, 1},
		{0.5, 0.5, 0.5},
		{0.25, 0.75, 0.5},
	}

	for _, input := range tests {
		output := make([]float64, lut.OutputChannels())
		lut.Apply(input, output, &Workspace{})
		for i := range 3 {
			if math.Abs(output[i]-input[i]) > 0.02 {
				t.Errorf("Apply(%v) = %v, want ~%v", input, output, input)
				break
			}
		}
	}
}

func TestDecodeLUT16(t *testing.T) {
	// build a minimal lut16Type (mft2)
	inputChannels := 3
	outputChannels := 3
	clutPoints := 2
	tableEntries := 4 // small tables for test

	inputTableSize := tableEntries * inputChannels * 2
	clutSize := clutPoints * clutPoints * clutPoints * outputChannels * 2
	outputTableSize := tableEntries * outputChannels * 2
	totalSize := 52 + inputTableSize + clutSize + outputTableSize

	data := make([]byte, totalSize)
	copy(data[0:4], "mft2")
	data[8] = byte(inputChannels)
	data[9] = byte(outputChannels)
	data[10] = byte(clutPoints)

	// identity matrix at offset 12
	putS15Fixed16(data, 12, 1.0)
	putS15Fixed16(data, 16, 0.0)
	putS15Fixed16(data, 20, 0.0)
	putS15Fixed16(data, 24, 0.0)
	putS15Fixed16(data, 28, 1.0)
	putS15Fixed16(data, 32, 0.0)
	putS15Fixed16(data, 36, 0.0)
	putS15Fixed16(data, 40, 0.0)
	putS15Fixed16(data, 44, 1.0)

	// table entry counts
	putUint16(data, 48, uint16(tableEntries))
	putUint16(data, 50, uint16(tableEntries))

	// linear input tables
	offset := 52
	for ch := range inputChannels {
		for i := range tableEntries {
			val := uint16(float64(i) / float64(tableEntries-1) * 65535)
			putUint16(data, offset+(ch*tableEntries+i)*2, val)
		}
	}
	offset += inputTableSize

	// identity CLUT
	for r := range clutPoints {
		for g := range clutPoints {
			for b := range clutPoints {
				idx := offset + (r*clutPoints*clutPoints+g*clutPoints+b)*outputChannels*2
				putUint16(data, idx+0, uint16(r*65535))
				putUint16(data, idx+2, uint16(g*65535))
				putUint16(data, idx+4, uint16(b*65535))
			}
		}
	}
	offset += clutSize

	// linear output tables
	for ch := range outputChannels {
		for i := range tableEntries {
			val := uint16(float64(i) / float64(tableEntries-1) * 65535)
			putUint16(data, offset+(ch*tableEntries+i)*2, val)
		}
	}

	lut, err := DecodeLUT(data)
	if err != nil {
		t.Fatalf("DecodeLUT failed: %v", err)
	}

	if lut.InputChannels() != inputChannels {
		t.Errorf("InputChannels = %d, want %d", lut.InputChannels(), inputChannels)
	}

	// test identity
	input := []float64{0.5, 0.5, 0.5}
	output := make([]float64, lut.OutputChannels())
	lut.Apply(input, output, &Workspace{})
	for i := range 3 {
		if math.Abs(output[i]-input[i]) > 0.02 {
			t.Errorf("Apply(%v) = %v, want ~%v", input, output, input)
			break
		}
	}
}

func TestLUTAToBApplyWithMCurves(t *testing.T) {
	// test that M curves are applied in the correct order for LUTAToB
	// order: ACurves → CLUT → MCurves → Matrix → BCurves
	lut := &LUTAToB{
		nInput:     3,
		nOutput:    3,
		gridPoints: []int{2, 2, 2},
		// identity input curves
		aCurves: []*Curve{
			{Gamma: 1.0},
			{Gamma: 1.0},
			{Gamma: 1.0},
		},
		// M curves with gamma 2.0 (applied after CLUT)
		mCurves: []*Curve{
			{Gamma: 2.0},
			{Gamma: 2.0},
			{Gamma: 2.0},
		},
		// identity output curves
		bCurves: []*Curve{
			{Gamma: 1.0},
			{Gamma: 1.0},
			{Gamma: 1.0},
		},
	}

	// build identity CLUT
	clutSize := 2 * 2 * 2 * 3
	lut.clut = make([]float32, clutSize)
	for r := range 2 {
		for g := range 2 {
			for b := range 2 {
				idx := (r*4 + g*2 + b) * 3
				lut.clut[idx+0] = float32(r)
				lut.clut[idx+1] = float32(g)
				lut.clut[idx+2] = float32(b)
			}
		}
	}

	// input 0.5 should go through CLUT unchanged, then M curves apply gamma 2.0
	input := []float64{0.5, 0.5, 0.5}
	output := make([]float64, lut.OutputChannels())
	lut.Apply(input, output, &Workspace{})

	// expected: 0.5^2.0 = 0.25
	expected := 0.25
	for i := range 3 {
		if math.Abs(output[i]-expected) > 0.02 {
			t.Errorf("Apply(%v)[%d] = %v, want ~%v", input, i, output[i], expected)
		}
	}
}

func TestDecodeLUTInvalidData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"too short", []byte{0, 0, 0, 0}},
		{"unknown type", []byte{'x', 'x', 'x', 'x', 0, 0, 0, 0}},
		{"mft1 too short", append([]byte("mft1"), make([]byte, 40)...)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeLUT(tt.data)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestComputeCLUTSizeOverflow(t *testing.T) {
	// test that overflow is detected
	gridPoints := []int{256, 256, 256, 256} // would overflow
	size := computeCLUTSize(gridPoints, 4)
	if size != 0 {
		t.Errorf("computeCLUTSize with overflow = %d, want 0", size)
	}

	// test normal case
	gridPoints = []int{17, 17, 17}
	size = computeCLUTSize(gridPoints, 3)
	expected := 17 * 17 * 17 * 3
	if size != expected {
		t.Errorf("computeCLUTSize = %d, want %d", size, expected)
	}
}

func TestMatrix3x4Layout(t *testing.T) {
	// test that 3x4 matrix layout matches ICC spec
	lut := &LUTAToB{
		nInput:  3,
		nOutput: 3,
		// matrix that scales by 2 and adds offset (0.1, 0.2, 0.3)
		matrix: []float64{
			2, 0, 0, // row 1
			0, 2, 0, // row 2
			0, 0, 2, // row 3
			0.1, 0.2, 0.3, // offsets
		},
	}

	input := []float64{0.1, 0.2, 0.3}
	output := make([]float64, lut.OutputChannels())
	lut.Apply(input, output, &Workspace{})

	// expected: 2*input + offset
	expected := []float64{0.1*2 + 0.1, 0.2*2 + 0.2, 0.3*2 + 0.3}
	for i := range 3 {
		if math.Abs(output[i]-expected[i]) > 1e-6 {
			t.Errorf("Apply(%v)[%d] = %v, want %v", input, i, output[i], expected[i])
		}
	}
}

func TestMatrix3x4Identity(t *testing.T) {
	// verify identity matrix produces identity output
	lut := &LUTAToB{
		nInput:  3,
		nOutput: 3,
		matrix: []float64{
			1, 0, 0,
			0, 1, 0,
			0, 0, 1,
			0, 0, 0,
		},
	}

	input := []float64{0.25, 0.5, 0.75}
	output := make([]float64, lut.OutputChannels())
	lut.Apply(input, output, &Workspace{})

	for i := range 3 {
		if math.Abs(output[i]-input[i]) > 1e-6 {
			t.Errorf("identity matrix: Apply(%v)[%d] = %v, want %v", input, i, output[i], input[i])
		}
	}
}

func TestLUTAToBOrder(t *testing.T) {
	// test LUTAToB processing order: ACurves → CLUT → MCurves → Matrix → BCurves
	buildIdentityCLUT := func() []float32 {
		clut := make([]float32, 2*2*2*3)
		for r := range 2 {
			for g := range 2 {
				for b := range 2 {
					idx := (r*4 + g*2 + b) * 3
					clut[idx+0] = float32(r)
					clut[idx+1] = float32(g)
					clut[idx+2] = float32(b)
				}
			}
		}
		return clut
	}

	// LUT with MCurves (gamma 0.5 = sqrt) and Matrix that doubles values
	lut := &LUTAToB{
		nInput:     3,
		nOutput:    3,
		gridPoints: []int{2, 2, 2},
		clut:       buildIdentityCLUT(),
		mCurves: []*Curve{
			{Gamma: 0.5}, // sqrt
			{Gamma: 0.5},
			{Gamma: 0.5},
		},
		matrix: []float64{
			2, 0, 0,
			0, 2, 0,
			0, 0, 2,
			0, 0, 0,
		},
	}

	input := []float64{0.25, 0.25, 0.25}

	// mAB order: 0.25 → CLUT(identity) → MCurves(sqrt=0.5) → Matrix(*2) = 1.0
	output := make([]float64, lut.OutputChannels())
	lut.Apply(input, output, &Workspace{})

	expectedAB := 1.0
	for i := range 3 {
		if math.Abs(output[i]-expectedAB) > 0.02 {
			t.Errorf("mAB Apply(%v)[%d] = %v, want ~%v", input, i, output[i], expectedAB)
		}
	}
}

func TestLUTBToAOrder(t *testing.T) {
	// test LUTBToA processing order: BCurves → Matrix → MCurves → CLUT → ACurves
	buildIdentityCLUT := func() []float32 {
		clut := make([]float32, 2*2*2*3)
		for r := range 2 {
			for g := range 2 {
				for b := range 2 {
					idx := (r*4 + g*2 + b) * 3
					clut[idx+0] = float32(r)
					clut[idx+1] = float32(g)
					clut[idx+2] = float32(b)
				}
			}
		}
		return clut
	}

	// LUT with MCurves (gamma 0.5 = sqrt) and Matrix that doubles values
	lut := &LUTBToA{
		nInput:     3,
		nOutput:    3,
		gridPoints: []int{2, 2, 2},
		clut:       buildIdentityCLUT(),
		mCurves: []*Curve{
			{Gamma: 0.5}, // sqrt
			{Gamma: 0.5},
			{Gamma: 0.5},
		},
		matrix: []float64{
			2, 0, 0,
			0, 2, 0,
			0, 0, 2,
			0, 0, 0,
		},
	}

	input := []float64{0.25, 0.25, 0.25}

	// mBA order: 0.25 → Matrix(*2=0.5) → MCurves(sqrt≈0.707) → CLUT(identity) ≈ 0.707
	output := make([]float64, lut.OutputChannels())
	lut.Apply(input, output, &Workspace{})

	expectedBA := math.Sqrt(0.5)
	for i := range 3 {
		if math.Abs(output[i]-expectedBA) > 0.02 {
			t.Errorf("mBA Apply(%v)[%d] = %v, want ~%v", input, i, output[i], expectedBA)
		}
	}
}

func TestLUTAToBVsLUTBToADifferentOrder(t *testing.T) {
	// verify that LUTAToB and LUTBToA produce different results
	// with the same components, proving different processing order
	buildIdentityCLUT := func() []float32 {
		clut := make([]float32, 2*2*2*3)
		for r := range 2 {
			for g := range 2 {
				for b := range 2 {
					idx := (r*4 + g*2 + b) * 3
					clut[idx+0] = float32(r)
					clut[idx+1] = float32(g)
					clut[idx+2] = float32(b)
				}
			}
		}
		return clut
	}

	mCurves := []*Curve{
		{Gamma: 0.5},
		{Gamma: 0.5},
		{Gamma: 0.5},
	}
	matrix := []float64{
		2, 0, 0,
		0, 2, 0,
		0, 0, 2,
		0, 0, 0,
	}

	lutAToB := &LUTAToB{
		nInput:     3,
		nOutput:    3,
		gridPoints: []int{2, 2, 2},
		clut:       buildIdentityCLUT(),
		mCurves:    mCurves,
		matrix:     matrix,
	}

	lutBToA := &LUTBToA{
		nInput:     3,
		nOutput:    3,
		gridPoints: []int{2, 2, 2},
		clut:       buildIdentityCLUT(),
		mCurves:    mCurves,
		matrix:     matrix,
	}

	input := []float64{0.25, 0.25, 0.25}

	outputAB := make([]float64, lutAToB.OutputChannels())
	lutAToB.Apply(input, outputAB, &Workspace{})
	outputBA := make([]float64, lutBToA.OutputChannels())
	lutBToA.Apply(input, outputBA, &Workspace{})

	// verify they produce different results
	if math.Abs(outputAB[0]-outputBA[0]) < 0.1 {
		t.Errorf("mAB and mBA should produce different results: AB=%v, BA=%v", outputAB, outputBA)
	}
}

// LUT round-trip tests

func buildIdentityCLUT3D(gridPoints int, outputChannels int) []float32 {
	size := gridPoints * gridPoints * gridPoints * outputChannels
	clut := make([]float32, size)
	for r := range gridPoints {
		for g := range gridPoints {
			for b := range gridPoints {
				idx := (r*gridPoints*gridPoints + g*gridPoints + b) * outputChannels
				clut[idx+0] = float32(r) / float32(gridPoints-1)
				clut[idx+1] = float32(g) / float32(gridPoints-1)
				clut[idx+2] = float32(b) / float32(gridPoints-1)
			}
		}
	}
	return clut
}

type lutTestCase struct {
	Name string
	LUT  LUT
}

var lutTestCases = []lutTestCase{
	{
		Name: "minimal-mAB",
		LUT: &LUTAToB{
			nInput:  3,
			nOutput: 3,
		},
	},
	{
		Name: "minimal-mBA",
		LUT: &LUTBToA{
			nInput:  3,
			nOutput: 3,
		},
	},
	{
		Name: "with-clut-mAB",
		LUT: &LUTAToB{
			nInput:     3,
			nOutput:    3,
			gridPoints: []int{2, 2, 2},
			clut:       buildIdentityCLUT3D(2, 3),
		},
	},
	{
		Name: "with-clut-mBA",
		LUT: &LUTBToA{
			nInput:     3,
			nOutput:    3,
			gridPoints: []int{2, 2, 2},
			clut:       buildIdentityCLUT3D(2, 3),
		},
	},
	{
		Name: "with-curves-mAB",
		LUT: &LUTAToB{
			nInput:  3,
			nOutput: 3,
			aCurves: []*Curve{
				{Gamma: 2.2},
				{Gamma: 2.2},
				{Gamma: 2.2},
			},
			bCurves: []*Curve{
				{Gamma: 1.0},
				{Gamma: 1.0},
				{Gamma: 1.0},
			},
		},
	},
	{
		Name: "with-curves-mBA",
		LUT: &LUTBToA{
			nInput:  3,
			nOutput: 3,
			bCurves: []*Curve{
				{Gamma: 2.2},
				{Gamma: 2.2},
				{Gamma: 2.2},
			},
			aCurves: []*Curve{
				{Gamma: 1.0},
				{Gamma: 1.0},
				{Gamma: 1.0},
			},
		},
	},
	{
		Name: "with-matrix-mAB",
		LUT: &LUTAToB{
			nInput:  3,
			nOutput: 3,
			matrix: []float64{
				1.0, 0.0, 0.0,
				0.0, 1.0, 0.0,
				0.0, 0.0, 1.0,
				0.1, 0.2, 0.3,
			},
		},
	},
	{
		Name: "with-mcurves-mAB",
		LUT: &LUTAToB{
			nInput:     3,
			nOutput:    3,
			gridPoints: []int{2, 2, 2},
			clut:       buildIdentityCLUT3D(2, 3),
			mCurves: []*Curve{
				{Gamma: 2.0},
				{Gamma: 2.0},
				{Gamma: 2.0},
			},
		},
	},
	{
		Name: "full-mAB",
		LUT: &LUTAToB{
			nInput:     3,
			nOutput:    3,
			gridPoints: []int{3, 3, 3},
			aCurves: []*Curve{
				{Gamma: 2.2},
				{Gamma: 2.2},
				{Gamma: 2.2},
			},
			clut: buildIdentityCLUT3D(3, 3),
			mCurves: []*Curve{
				{Gamma: 1.0},
				{Gamma: 1.0},
				{Gamma: 1.0},
			},
			matrix: []float64{
				1.0, 0.0, 0.0,
				0.0, 1.0, 0.0,
				0.0, 0.0, 1.0,
				0.0, 0.0, 0.0,
			},
			bCurves: []*Curve{
				{Gamma: 0.45},
				{Gamma: 0.45},
				{Gamma: 0.45},
			},
		},
	},
	{
		Name: "full-mBA",
		LUT: &LUTBToA{
			nInput:     3,
			nOutput:    3,
			gridPoints: []int{3, 3, 3},
			bCurves: []*Curve{
				{Gamma: 2.2},
				{Gamma: 2.2},
				{Gamma: 2.2},
			},
			clut: buildIdentityCLUT3D(3, 3),
			mCurves: []*Curve{
				{Gamma: 1.0},
				{Gamma: 1.0},
				{Gamma: 1.0},
			},
			matrix: []float64{
				1.0, 0.0, 0.0,
				0.0, 1.0, 0.0,
				0.0, 0.0, 1.0,
				0.0, 0.0, 0.0,
			},
			aCurves: []*Curve{
				{Gamma: 0.45},
				{Gamma: 0.45},
				{Gamma: 0.45},
			},
		},
	},
}

func testLUTRoundTrip(t *testing.T, lut LUT) {
	t.Helper()

	// encode
	data, err := lut.Encode()
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	// decode
	decoded, err := DecodeLUT(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	// compare structural fields
	if decoded.InputChannels() != lut.InputChannels() {
		t.Errorf("InputChannels: got %d, want %d", decoded.InputChannels(), lut.InputChannels())
	}
	if decoded.OutputChannels() != lut.OutputChannels() {
		t.Errorf("OutputChannels: got %d, want %d", decoded.OutputChannels(), lut.OutputChannels())
	}

	// compare by applying both LUTs and checking output
	testInputs := [][]float64{
		{0, 0, 0},
		{1, 1, 1},
		{0.5, 0.5, 0.5},
		{0.25, 0.5, 0.75},
	}

	for _, input := range testInputs {
		output1 := make([]float64, lut.OutputChannels())
		lut.Apply(input, output1, &Workspace{})
		output2 := make([]float64, decoded.OutputChannels())
		decoded.Apply(input, output2, &Workspace{})

		for i := range output1 {
			// allow small tolerance for fixed-point encoding precision
			if math.Abs(output1[i]-output2[i]) > 0.001 {
				t.Errorf("Apply(%v): got %v, want %v", input, output2, output1)
				break
			}
		}
	}
}

func TestLUTRoundTrip(t *testing.T) {
	for _, tc := range lutTestCases {
		t.Run(tc.Name, func(t *testing.T) {
			testLUTRoundTrip(t, tc.LUT)
		})
	}
}

func FuzzLUTRoundTrip(f *testing.F) {
	// seed corpus with test cases
	for _, tc := range lutTestCases {
		data, err := tc.LUT.Encode()
		if err != nil {
			continue
		}
		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		lut, err := DecodeLUT(data)
		if err != nil {
			t.Skip("invalid LUT data")
		}

		// encode and decode again
		encoded, err := lut.Encode()
		if err != nil {
			t.Fatalf("encode failed: %v", err)
		}

		decoded, err := DecodeLUT(encoded)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}

		// compare by applying both LUTs
		testInputs := [][]float64{
			{0.5, 0.5, 0.5},
		}
		if lut.InputChannels() == 1 {
			testInputs = [][]float64{{0.5}}
		} else if lut.InputChannels() == 4 {
			testInputs = [][]float64{{0.5, 0.5, 0.5, 0.5}}
		}

		for _, input := range testInputs {
			if len(input) != lut.InputChannels() {
				continue
			}
			output1 := make([]float64, lut.OutputChannels())
			lut.Apply(input, output1, &Workspace{})
			output2 := make([]float64, decoded.OutputChannels())
			decoded.Apply(input, output2, &Workspace{})

			for i := range output1 {
				if math.Abs(output1[i]-output2[i]) > 0.01 {
					t.Errorf("round-trip mismatch: Apply(%v) got %v, want %v", input, output2, output1)
					break
				}
			}
		}
	})
}

// wantSameBytes fails the test unless got and want hold the same bytes,
// reporting the first difference.
func wantSameBytes(t *testing.T, got, want []byte) {
	t.Helper()
	if bytes.Equal(got, want) {
		return
	}
	if len(got) != len(want) {
		t.Fatalf("encoded %d bytes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d: got %d, want %d", i, got[i], want[i])
		}
	}
}

// TestCLUTByteRoundTrip checks that decoding a CLUT and encoding it again
// reproduces the original bytes exactly, for every 8-bit sample value and
// every 16-bit one.  The table is held as float32, which represents every
// n/255 and n/65535 a profile can hold, so storage loses nothing; the encoder
// must round rather than truncate for that to survive the return trip.
func TestCLUTByteRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name       string
		precision  int
		gridPoints []int
		values     int
	}{
		{"8-bit", 1, []int{16, 16}, 256},
		{"16-bit", 2, []int{16, 16, 16, 16}, 65536},
	} {
		t.Run(tc.name, func(t *testing.T) {
			block := make([]byte, 20+tc.values*tc.precision)
			for d, g := range tc.gridPoints {
				block[d] = byte(g)
			}
			block[16] = byte(tc.precision)
			for i := range tc.values {
				if tc.precision == 1 {
					block[20+i] = byte(i)
				} else {
					putUint16(block, 20+i*2, uint16(i))
				}
			}

			gridPoints, clut, precision, err := decodeCLUT(newBudget(0), block, 0,
				len(tc.gridPoints), 1)
			if err != nil {
				t.Fatal(err)
			}
			if precision != tc.precision {
				t.Fatalf("precision = %d, want %d", precision, tc.precision)
			}

			got := encodeCLUT(gridPoints, 1, clut, precision)
			wantSameBytes(t, got, block)
		})
	}
}

// TestLUTTagByteRoundTrip decodes a whole mft1/mft2 tag and encodes it again,
// demanding the tag come back byte for byte.  This covers the input and output
// tables as well as the CLUT: all three quantise a [0,1] value to an integer on
// the way out, and all three need round-to-nearest rather than truncation to
// reproduce the value they were decoded from.
func TestLUTTagByteRoundTrip(t *testing.T) {
	t.Run("mft1", func(t *testing.T) {
		const nIn, nOut, grid = 3, 3, 5
		data := makeMft1(nIn, nOut, grid)
		// fill every table and CLUT byte with a varying pattern
		for i := 48; i < len(data); i++ {
			data[i] = byte(i * 7)
		}

		lut, err := decodeLUT8(newBudget(0), data)
		if err != nil {
			t.Fatal(err)
		}
		got, err := lut.Encode()
		if err != nil {
			t.Fatal(err)
		}
		wantSameBytes(t, got, data)
	})

	t.Run("mft2", func(t *testing.T) {
		const nIn, nOut, grid, entries = 3, 3, 5, 256
		data := makeMft2(nIn, nOut, grid, entries, entries)
		for i := 52; i < len(data); i++ {
			data[i] = byte(i * 7)
		}

		lut, err := decodeLUT16(newBudget(0), data)
		if err != nil {
			t.Fatal(err)
		}
		got, err := lut.Encode()
		if err != nil {
			t.Fatal(err)
		}
		wantSameBytes(t, got, data)
	})
}
