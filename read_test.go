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
	"fmt"
	"reflect"
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

func FuzzDecode(f *testing.F) {
	p := &Profile{
		TagData:      make(map[TagType][]byte),
		CreationDate: time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	f.Add(p.Encode())
	p.TagData[0x100] = []byte{0, 0, 0, 0}
	f.Add(p.Encode())
	p.TagData[0x6368726D] = []byte{0, 0, 0, 0}
	f.Add(p.Encode())
	f.Fuzz(func(t *testing.T, a []byte) {
		p, err := Decode(a)
		if err != nil {
			return
		}
		b := p.Encode()
		q, err := Decode(b)
		if err != nil {
			t.Fatalf("re-decoding failed: %v", err)
		}

		p.CheckSum = CheckSumMissing
		q.CheckSum = CheckSumMissing
		if !reflect.DeepEqual(p, q) {
			d := cmp.Diff(p, q)
			fmt.Println(d)
			t.Fatalf("profiles differ")
		}
	})
}
