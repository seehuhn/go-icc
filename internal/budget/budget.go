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

// Package budget tracks the remaining memory budget for a single
// decode, so that malformed ICC data cannot drive the heap arbitrarily
// large.
package budget

import (
	"errors"
	"unsafe"
)

// Memory-budget tuning constants.  See [New] for how they combine.
const (
	// perAllocOverhead is the fixed cost added to every charge, to
	// account for slice-header and heap-block overhead.  Without this
	// surcharge an attacker can amplify input size by issuing many
	// tiny allocations, each costing far more than its payload.
	perAllocOverhead = 32

	// MapEntryOverhead is the budget cost a caller should charge when
	// inserting a new entry into a Go map: the key, the slice or
	// pointer value header, and the map's bucket bookkeeping.  Callers
	// should not charge this for overwrites.
	MapEntryOverhead = 48

	// budgetBase is the minimum budget any caller starts with,
	// independent of input size, so tiny profiles can still parse.
	budgetBase = 1 << 20 // 1 MiB

	// budgetMultiplier is the maximum number of bytes a decoder may
	// allocate per byte of input data.  Tuned for the worst case
	// in [seehuhn.de/go/icc]: an mft1 CLUT expands one input byte
	// into one float64 (×8).  Lower values will reject valid profiles.
	budgetMultiplier = 8

	// budgetHardCap caps the input-proportional part of the budget,
	// so a header claiming a huge profile cannot unlock unlimited memory.
	budgetHardCap = 64 << 20 // 64 MiB
)

// ErrBudgetExceeded is returned by [Budget.Charge] when an allocation
// would push the budget below zero.
var ErrBudgetExceeded = errors.New("icc: memory budget exceeded")

// Budget tracks the remaining memory budget for a single decode.
// Callers that do not opt in to budget tracking can pass a nil *Budget;
// in that case every charge succeeds.
type Budget struct {
	remaining int64
}

// New returns a Budget sized for an input of inputSize bytes.
// The returned budget grows with inputSize up to a hard cap.
func New(inputSize int64) *Budget {
	if inputSize < 0 {
		inputSize = 0
	}
	add := int64(budgetMultiplier) * inputSize
	if add < 0 || add > budgetHardCap {
		add = budgetHardCap
	}
	return &Budget{remaining: budgetBase + add}
}

// Remaining returns the number of bytes still available in the budget.
// A nil receiver returns 0.
func (b *Budget) Remaining() int64 {
	if b == nil {
		return 0
	}
	return b.remaining
}

// Charge subtracts (bytes + perAllocOverhead) from the budget.  A nil
// receiver is a no-op (callers without budget tracking pass through).
// On exhaustion the budget is left unchanged and [ErrBudgetExceeded] is
// returned.
func (b *Budget) Charge(bytes int) error {
	if b == nil {
		return nil
	}
	if bytes < 0 {
		return ErrBudgetExceeded
	}
	cost := int64(bytes) + perAllocOverhead
	if cost < int64(bytes) { // overflow guard
		return ErrBudgetExceeded
	}
	if b.remaining < cost {
		return ErrBudgetExceeded
	}
	b.remaining -= cost
	return nil
}

// AllocSlice charges b for a slice of n elements of T and, if the
// charge succeeds, returns make([]T, n).  A nil budget skips the check
// and always allocates.
//
// For slice, map, interface, or string element types, only the header
// size is charged here; the referenced elements (slice backing array,
// map buckets, interface body, or string bytes) must be charged
// separately when allocated.
func AllocSlice[T any](b *Budget, n int) ([]T, error) {
	var zero T
	size := int(unsafe.Sizeof(zero))
	if n < 0 {
		return nil, ErrBudgetExceeded
	}
	// overflow-safe multiplication for the byte count
	bytes := n * size
	if size != 0 && bytes/size != n {
		return nil, ErrBudgetExceeded
	}
	if err := b.Charge(bytes); err != nil {
		return nil, err
	}
	return make([]T, n), nil
}
