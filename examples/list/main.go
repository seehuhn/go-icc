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

package main

import (
	"flag"
	"fmt"
	"os"
	"slices"

	"golang.org/x/exp/maps"
	"seehuhn.de/go/icc"
)

var (
	verbose = flag.Bool("v", false, "verbose output")
)

func main() {
	flag.Parse()
	for _, fname := range flag.Args() {
		err := show(fname)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", fname, err)
		}
	}
}

func show(fname string) error {
	body, err := os.ReadFile(fname)
	if err != nil {
		return err
	}
	p, err := icc.Decode(body)
	if err != nil {
		return err
	}
	if !*verbose {
		fmt.Printf("%-8s %-25s %6d bytes  %s \n", p.Version, p.Class, len(body), fname)
		return nil
	}

	fmt.Printf("Profile: %s\n", fname)
	if p.PreferedCMMType != 0 {
		fmt.Printf("  PreferedCMMType: %s\n", tag(p.PreferedCMMType))
	}
	fmt.Printf("  Version: %s\n", p.Version)
	fmt.Printf("  Class: %s\n", p.Class)
	fmt.Printf("  ColorSpace: %s\n", tag(p.ColorSpace))
	fmt.Printf("  PCS: %s\n", tag(p.PCS))
	fmt.Printf("  CreationDate: %s\n", p.CreationDate)
	if p.PrimaryPlatform != 0 {
		fmt.Printf("  PrimaryPlatform: %s\n", tag(p.PrimaryPlatform))
	}
	if p.Flags != 0 {
		fmt.Printf("  Flags: %08X\n", p.Flags)
	}
	if p.DeviceManufacturer != 0 {
		fmt.Printf("  DeviceManufacturer: %s\n", tag(p.DeviceManufacturer))
	}
	if p.DeviceModel != 0 {
		fmt.Printf("  DeviceModel: %s\n", tag(p.DeviceModel))
	}
	if p.DeviceAttributes != 0 {
		fmt.Printf("  DeviceAttributes: %08X %08X\n",
			uint32(p.DeviceAttributes>>32), uint32(p.DeviceAttributes))
	}
	fmt.Printf("  RenderingIntent: %s\n", p.RenderingIntent)
	if p.Creator != 0 {
		fmt.Printf("  Creator: %s\n", tag(p.Creator))
	}
	if p.CheckSum != icc.CheckSumMissing {
		fmt.Printf("  CheckSum: %s\n", p.CheckSum)
	}

	fmt.Println()

	tags := maps.Keys(p.TagData)
	slices.Sort(tags)
	for _, t := range tags {
		data := p.TagData[t]
		switch t {
		case icc.Copyright:
			fmt.Printf("  %s: (%d bytes)\n", t, len(data))
			cprt, err := p.Copyright()
			if err != nil {
				return err
			}
			for _, lu := range cprt {
				fmt.Printf("    [%s_%s] %s\n", lu.Language, lu.Country, lu.Value)
			}
		default:
			sig := uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])
			fmt.Printf("  %s: %s (%d bytes)\n", t, tag(sig), len(data))
		}
	}

	fmt.Println()

	return nil
}

func tag(x uint32) string {
	a := fmt.Sprintf("%08X", x)

	b := ""
	bb := []byte{
		byte(x >> 24),
		byte(x >> 16),
		byte(x >> 8),
		byte(x),
	}
	isASCII := true
	for _, c := range bb {
		if c < 0x20 || c > 0x7E {
			isASCII = false
			break
		}
	}
	if isASCII {
		b = fmt.Sprintf(" \"%s\"", bb)
	}

	return a + b
}
