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
	"seehuhn.de/go/membudget"
)

// ICC-specific budget sizing.  See [newBudget] for how the constants
// combine.
const (
	// iccBudgetBase is the minimum budget any decode starts with,
	// independent of input size, so tiny profiles can still parse.
	iccBudgetBase = 1 << 20 // 1 MiB

	// iccBudgetMultiplier is the maximum number of bytes a decoder may
	// allocate per byte of input data.  Tuned for the worst case here:
	// an mft1 CLUT expands one input byte into one float64 (×8).
	// Lower values will reject valid profiles.
	iccBudgetMultiplier = 8

	// iccBudgetHardCap caps the input-proportional part of the budget,
	// so a header claiming a huge profile cannot unlock unlimited memory.
	iccBudgetHardCap = 64 << 20 // 64 MiB
)

// newBudget returns a [membudget.Budget] sized for decoding an ICC
// profile blob of inputSize bytes.  The returned budget grows with
// inputSize up to a hard cap.
func newBudget(inputSize int64) *membudget.Budget {
	if inputSize < 0 {
		inputSize = 0
	}
	add := int64(iccBudgetMultiplier) * inputSize
	if add < 0 || add > iccBudgetHardCap {
		add = iccBudgetHardCap
	}
	return membudget.New(iccBudgetBase + add)
}
