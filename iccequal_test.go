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
	"testing"
	"time"
)

func TestProfileEqual(t *testing.T) {
	base := func() *Profile {
		return &Profile{
			PreferredCMMType:   1,
			Version:            Version4_4_0,
			Class:              DisplayDeviceProfile,
			ColorSpace:         RGBSpace,
			PCS:                PCSXYZSpace,
			CreationDate:       time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			PrimaryPlatform:    2,
			Flags:              3,
			DeviceManufacturer: 4,
			DeviceModel:        5,
			DeviceAttributes:   6,
			RenderingIntent:    Perceptual,
			Creator:            7,
			CheckSum:           CheckSumValid,
			TagData: map[TagType][]byte{
				TagType(0x41): {1, 2, 3},
				TagType(0x42): {4, 5, 6},
			},
		}
	}

	t.Run("identical", func(t *testing.T) {
		a := base()
		b := base()
		if !a.Equal(b) {
			t.Error("identical profiles should be equal")
		}
	})

	t.Run("checksum ignored", func(t *testing.T) {
		a := base()
		b := base()
		b.CheckSum = CheckSumInvalid
		if !a.Equal(b) {
			t.Error("profiles differing only in CheckSum should be equal")
		}
	})

	t.Run("different header field", func(t *testing.T) {
		a := base()
		b := base()
		b.Version = Version2_1_0
		if a.Equal(b) {
			t.Error("profiles with different Version should not be equal")
		}
	})

	t.Run("different tag data", func(t *testing.T) {
		a := base()
		b := base()
		b.TagData[TagType(0x41)] = []byte{9, 9, 9}
		if a.Equal(b) {
			t.Error("profiles with different tag data should not be equal")
		}
	})

	t.Run("extra tag", func(t *testing.T) {
		a := base()
		b := base()
		b.TagData[TagType(0x43)] = []byte{7, 8}
		if a.Equal(b) {
			t.Error("profiles with different tag count should not be equal")
		}
	})

	t.Run("missing tag", func(t *testing.T) {
		a := base()
		b := base()
		delete(b.TagData, TagType(0x42))
		if a.Equal(b) {
			t.Error("profiles with missing tag should not be equal")
		}
	})

	t.Run("both nil TagData", func(t *testing.T) {
		a := &Profile{}
		b := &Profile{}
		if !a.Equal(b) {
			t.Error("empty profiles should be equal")
		}
	})

	t.Run("one nil", func(t *testing.T) {
		a := base()
		if a.Equal(nil) {
			t.Error("non-nil should not equal nil")
		}
	})

	t.Run("both nil", func(t *testing.T) {
		var a, b *Profile
		if !a.Equal(b) {
			t.Error("two nil profiles should be equal")
		}
	})
}
