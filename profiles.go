// Copyright 2026 Jochen Voss <voss@seehuhn.de>
// SPDX-License-Identifier: BSD-3-Clause

package icc

import _ "embed"

// Built-in ICC profiles.
var (
	// SRGBv2Profile contains a compact sRGB ICC profile using ICC version 2.
	// From https://github.com/saucecontrol/Compact-ICC-Profiles (CC0 1.0).
	//
	//go:embed profiles/sRGB-v2-micro.icc
	SRGBv2Profile []byte

	// SRGBv4Profile contains a compact sRGB ICC profile using ICC version 4.
	// From https://github.com/saucecontrol/Compact-ICC-Profiles (CC0 1.0).
	//
	//go:embed profiles/sRGB-v4.icc
	SRGBv4Profile []byte

	// ROMMv2Profile contains a compact ROMM RGB (ProPhoto RGB) ICC profile
	// using ICC version 2.  The tone curve is sampled at 62 points.
	// From https://github.com/saucecontrol/Compact-ICC-Profiles (CC0 1.0).
	//
	//go:embed profiles/ProPhoto-v2-micro.icc
	ROMMv2Profile []byte

	// ROMMv4Profile contains a compact ROMM RGB (ProPhoto RGB) ICC profile
	// using ICC version 4.  The tone curve is exact, including the linear
	// segment near black.
	// From https://github.com/saucecontrol/Compact-ICC-Profiles (CC0 1.0).
	//
	//go:embed profiles/ProPhoto-v4.icc
	ROMMv4Profile []byte

	// CMYKProfile contains a CMYK ICC profile for US web-offset printing
	// on coated paper (CGATS TR 005 / SWOP). The profile supports both
	// device-to-PCS and PCS-to-device transforms.
	// Generated from the characterization data of ANSI CGATS/SWOP TR 005-2007,
	// as distributed in the Debian icc-profiles-free package.
	// See profiles/Licenses.txt for details.
	//
	//go:embed profiles/TR005.icc
	CMYKProfile []byte
)
