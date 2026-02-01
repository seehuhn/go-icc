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

// Package icc reads and writes ICC colour profiles.
//
// ICC profiles describe how to convert colours between device colour spaces
// (such as RGB or CMYK) and a device-independent Profile Connection Space (PCS).
// The PCS is either CIEXYZ or CIELAB, both based on the D50 illuminant.
//
// # Reading and Writing Profiles
//
// Use [Decode] to read an ICC profile from binary data, and [Profile.Encode]
// to convert a profile back to binary form:
//
//	p, err := icc.Decode(data)
//	if err != nil {
//	    // handle error
//	}
//	// inspect p.Class, p.ColorSpace, p.Version, p.TagData, etc.
//
//	encoded, err := p.Encode()
//
// # Colour Transformations
//
// To convert colours using a profile, create a [Transform] with [NewTransform]:
//
//	t, err := icc.NewTransform(p, icc.DeviceToPCS, icc.Perceptual)
//	if err != nil {
//	    // handle error
//	}
//	X, Y, Z := t.ToXYZ([]float64{r, g, b})  // device RGB to PCS XYZ
//
// For the reverse direction:
//
//	t, err := icc.NewTransform(p, icc.PCSToDevice, icc.Perceptual)
//	rgb := t.FromXYZ(X, Y, Z)  // PCS XYZ to device RGB
package icc

import (
	"fmt"
	"time"
)

// Profile represents an ICC colour profile.
//
// The header fields (Version, Class, ColorSpace, etc.) describe the profile's
// characteristics. The TagData map contains the raw binary data for each tag
// in the profile; use [DecodeCurve], [DecodeLUT], or [NewTransform] to
// interpret tag data for colour transformations.
type Profile struct {
	PreferredCMMType   uint32
	Version            Version
	Class              ProfileClass
	ColorSpace         ColorSpace // device colour space (e.g. RGBSpace, CMYKSpace)
	PCS                ColorSpace // Profile Connection Space (PCSXYZSpace or PCSLabSpace)
	CreationDate       time.Time
	PrimaryPlatform    uint32
	Flags              uint32
	DeviceManufacturer uint32
	DeviceModel        uint32
	DeviceAttributes   uint64
	RenderingIntent    RenderingIntent
	Creator            uint32

	// CheckSum indicates whether the profile's embedded checksum is valid.
	// This is only meaningful for profiles read using Decode.
	CheckSum CheckSum

	// TagData maps tag signatures to their raw binary data.
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
	InputDeviceProfile   ProfileClass = 0x73636E72 // "scnr"
	DisplayDeviceProfile ProfileClass = 0x6D6E7472 // "mntr"
	OutputDeviceProfile  ProfileClass = 0x70727472 // "prtr"

	ColorSpaceProfile ProfileClass = 0x73706163 // "spac"
	DeviceLinkProfile ProfileClass = 0x6C696E6B // "link"
	AbstractProfile   ProfileClass = 0x61627374 // "abst"
	NamedColorProfile ProfileClass = 0x6E6D636C // "nmcl"
)

// RenderingIntent specifies how colours outside the destination gamut are handled.
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

// Standard ICC rendering intents.
const (
	Perceptual           RenderingIntent = 0 // preserves visual relationships between colours
	RelativeColorimetric RenderingIntent = 1 // maps white point, preserves in-gamut colours
	Saturation           RenderingIntent = 2 // preserves saturation, may shift hue
	AbsoluteColorimetric RenderingIntent = 3 // preserves exact colorimetric values
)

// ColorSpace identifies a colour space in an ICC profile.
type ColorSpace uint32

func (s ColorSpace) String() string {
	switch s {
	case CIEXYZSpace:
		return "CIEXYZ"
	case CIELabSpace:
		return "CIELAB"
	case CIELuvSpace:
		return "CIELUV"
	case YCbCrSpace:
		return "YCbCr"
	case CIEYxySpace:
		return "CIEYxy"
	case RGBSpace:
		return "RGB"
	case GraySpace:
		return "Gray"
	case HSVSpace:
		return "HSV"
	case HLSSpace:
		return "HLS"
	case CMYKSpace:
		return "CMYK"
	case CMYSpace:
		return "CMY"
	case Color2Space:
		return "2CLR"
	case Color3Space:
		return "3CLR"
	case Color4Space:
		return "4CLR"
	case Color5Space:
		return "5CLR"
	case Color6Space:
		return "6CLR"
	case Color7Space:
		return "7CLR"
	case Color8Space:
		return "8CLR"
	case Color9Space:
		return "9CLR"
	case Color10Space:
		return "10CLR"
	case Color11Space:
		return "11CLR"
	case Color12Space:
		return "12CLR"
	case Color13Space:
		return "13CLR"
	case Color14Space:
		return "14CLR"
	case Color15Space:
		return "15CLR"
	default:
		return fmt.Sprintf("ColorSpace(0x%08X)", uint32(s))
	}
}

// NumComponents returns the number of color components in the color space.
func (s ColorSpace) NumComponents() int {
	switch s {
	case CIEXYZSpace:
		return 3
	case CIELabSpace:
		return 3
	case CIELuvSpace:
		return 3
	case YCbCrSpace:
		return 3
	case CIEYxySpace:
		return 3
	case RGBSpace:
		return 3
	case GraySpace:
		return 1
	case HSVSpace:
		return 3
	case HLSSpace:
		return 3
	case CMYKSpace:
		return 4
	case CMYSpace:
		return 3
	case Color2Space:
		return 2
	case Color3Space:
		return 3
	case Color4Space:
		return 4
	case Color5Space:
		return 5
	case Color6Space:
		return 6
	case Color7Space:
		return 7
	case Color8Space:
		return 8
	case Color9Space:
		return 9
	case Color10Space:
		return 10
	case Color11Space:
		return 11
	case Color12Space:
		return 12
	case Color13Space:
		return 13
	case Color14Space:
		return 14
	case Color15Space:
		return 15
	default:
		return 0 // unknown
	}
}

// Color spaces defined in the ICC specification.
const (
	CIEXYZSpace  ColorSpace = 0x58595A20 // "XYZ "
	CIELabSpace  ColorSpace = 0x4C616220 // "Lab "
	CIELuvSpace  ColorSpace = 0x4C757620 // "Luv "
	YCbCrSpace   ColorSpace = 0x59436272 // "YCbr"
	CIEYxySpace  ColorSpace = 0x59787920 // "Yxy "
	RGBSpace     ColorSpace = 0x52474220 // "RGB "
	GraySpace    ColorSpace = 0x47524159 // "GRAY"
	HSVSpace     ColorSpace = 0x48535620 // "HSV "
	HLSSpace     ColorSpace = 0x484C5320 // "HLS "
	CMYKSpace    ColorSpace = 0x434D594B // "CMYK"
	CMYSpace     ColorSpace = 0x434D5920 // "CMY "
	Color2Space  ColorSpace = 0x32434C52 // "2CLR"
	Color3Space  ColorSpace = 0x33434C52 // "3CLR"
	Color4Space  ColorSpace = 0x34434C52 // "4CLR"
	Color5Space  ColorSpace = 0x35434C52 // "5CLR"
	Color6Space  ColorSpace = 0x36434C52 // "6CLR"
	Color7Space  ColorSpace = 0x37434C52 // "7CLR"
	Color8Space  ColorSpace = 0x38434C52 // "8CLR"
	Color9Space  ColorSpace = 0x39434C52 // "9CLR"
	Color10Space ColorSpace = 0x41434C52 // "ACLR"
	Color11Space ColorSpace = 0x42434C52 // "BCLR"
	Color12Space ColorSpace = 0x43434C52 // "CCLR"
	Color13Space ColorSpace = 0x44434C52 // "DCLR"
	Color14Space ColorSpace = 0x45434C52 // "ECLR"
	Color15Space ColorSpace = 0x46434C52 // "FCLR"

	PCSXYZSpace = CIEXYZSpace
	PCSLabSpace = CIELabSpace
)

// PCSName returns the name of the PCS color space.
func (p *Profile) PCSName() string {
	switch p.PCS {
	case PCSXYZSpace:
		return "PCSXYZ"
	case PCSLabSpace:
		return "PCSLab"
	default:
		return p.PCS.String()
	}
}

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

// d50WhitePoint is the CIE standard illuminant D50 white point in XYZ coordinates.
// This is the reference illuminant for the ICC Profile Connection Space.
var d50WhitePoint = [3]float64{0.9642, 1.0, 0.8249}
