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
	"time"
)

// Profile represents the data stored in an ICC profile.
type Profile struct {
	PreferedCMMType    uint32
	Version            Version
	Class              ProfileClass
	ColorSpace         uint32
	PCS                uint32
	CreationDate       time.Time
	PrimaryPlatform    uint32
	Flags              uint32
	DeviceManufacturer uint32
	DeviceModel        uint32
	DeviceAttributes   uint64
	RenderingIntent    RenderingIntent
	Creator            uint32

	CheckSum CheckSum

	TagData map[TagType][]byte
}

// Version is a version of the ICC profile format.
type Version uint32

// Some well-known versions of the ICC profile format.
const (
	Version2_1_0 Version = 0x0210_0000 // Version 3.3 (November 1996)
	Version2_2_0 Version = 0x0220_0000 // ICC.1:1998-09
	Version2_3_0 Version = 0x0230_0000 // ICC.1:1998-09 + ICC.1A:1999-04
	Version4_0_0 Version = 0x0400_0000 // ICC.1:2001-12
	Version4_1_0 Version = 0x0410_0000 // ICC.1:2003-09
	Version4_2_0 Version = 0x0420_0000 // ICC.1:2004-10
	Version4_3_0 Version = 0x0430_0000 // ICC.1:2010-12
	Version4_4_0 Version = 0x0440_0000 // ICC.1:2022-05

	currentVersion = Version4_4_0
)

func (v Version) String() string {
	major := int(v >> 24)
	minor := int(v >> 20 & 0xF)
	bugfix := int(v >> 16 & 0xF)
	other := int(v & 0xFFFF)

	suffix := ""
	if other != 0 {
		suffix = fmt.Sprintf(".%04X", other)
	}
	return fmt.Sprintf("%d.%d.%d%s", major, minor, bugfix, suffix)
}

// ProfileClass is the ICC profile or device class.
type ProfileClass uint32

func (c ProfileClass) String() string {
	switch c {
	case InputDeviceProfile:
		return "Input Device Profile"
	case DisplayDeviceProfile:
		return "Display Device Profile"
	case OutputDeviceProfile:
		return "Output Device Profile"
	case DeviceLinkProfile:
		return "DeviceLink Profile"
	case ColorSpaceProfile:
		return "ColorSpace Profile"
	case AbstractProfile:
		return "Abstract Profile"
	case NamedColorProfile:
		return "Named Color Profile"
	default:
		return fmt.Sprintf("ProfileClass(0x%08X)", uint32(c))
	}
}

// Profile classes defined in the ICC specification.
const (
	InputDeviceProfile   ProfileClass = 0x73636E72
	DisplayDeviceProfile ProfileClass = 0x6D6E7472
	OutputDeviceProfile  ProfileClass = 0x70727472

	ColorSpaceProfile ProfileClass = 0x73706163
	DeviceLinkProfile ProfileClass = 0x6C696E6B
	AbstractProfile   ProfileClass = 0x61627374
	NamedColorProfile ProfileClass = 0x6E6D636C
)

// RenderingIntent is the ICC rendering intent.
type RenderingIntent uint32

func (ri RenderingIntent) String() string {
	switch ri {
	case Perceptual:
		return "Perceptual"
	case RelativeColorimetric:
		return "Relative Colorimetric"
	case Saturation:
		return "Saturation"
	case AbsoluteColorimetric:
		return "Absolute Colorimetric"
	default:
		return fmt.Sprintf("RenderingIntent(%d)", ri)
	}
}

// The ICC rendering intents.
const (
	Perceptual           RenderingIntent = 0
	RelativeColorimetric RenderingIntent = 1
	Saturation           RenderingIntent = 2
	AbsoluteColorimetric RenderingIntent = 3
)

// CheckSum contains information about the Profile ID field.
type CheckSum int

func (c CheckSum) String() string {
	switch c {
	case CheckSumValid:
		return "Valid"
	case CheckSumInvalid:
		return "Invalid"
	default:
		return "Missing"
	}
}

// Possible values of the CheckSum field.
const (
	CheckSumMissing CheckSum = iota
	CheckSumValid
	CheckSumInvalid
)
