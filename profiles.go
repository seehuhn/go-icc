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

import _ "embed"

// Built-in ICC profiles.
//
// All profile data is from https://github.com/saucecontrol/Compact-ICC-Profiles
// and is in the public domain (CC0 1.0).
var (
	// SRGBv2Profile contains a compact sRGB ICC profile using ICC version 2.
	//
	//go:embed profiles/sRGB-v2-micro.icc
	SRGBv2Profile []byte

	// SRGBv4Profile contains a compact sRGB ICC profile using ICC version 4.
	//
	//go:embed profiles/sRGB-v4.icc
	SRGBv4Profile []byte

	// CGATS001Profile contains a compact CMYK ICC profile using ICC version 2.
	// The profile is compatible with the CGATS TR 001 (SWOP) specification.
	//
	//go:embed profiles/CGATS001Compat-v2-micro.icc
	CGATS001Profile []byte
)
