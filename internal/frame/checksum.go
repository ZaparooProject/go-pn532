// go-pn532
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of go-pn532.
//
// go-pn532 is free software; you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public
// License as published by the Free Software Foundation; either
// version 3 of the License, or (at your option) any later version.
//
// go-pn532 is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
// Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with go-pn532; if not, write to the Free Software Foundation,
// Inc., 51 Franklin Street, Fifth Floor, Boston, MA  02110-1301, USA.

package frame

// CalculateChecksum computes the checksum for a data buffer
// This is a simple sum of all bytes in the provided data
func CalculateChecksum(data []byte) byte {
	chk := byte(0)
	for _, b := range data {
		chk += b
	}
	return chk
}

// ValidateChecksum verifies that the provided data has a valid checksum
// Returns true if checksum is invalid (requiring NACK), false if valid
func ValidateChecksum(data []byte) bool {
	return CalculateChecksum(data) != 0
}

// CalculateDataChecksum computes the checksum for frame data (TFI + data bytes)
// and returns the two's complement (for inclusion in frame)
func CalculateDataChecksum(tfi byte, data []byte) byte {
	chk := tfi
	for _, b := range data {
		chk += b
	}
	return (^chk) + 1 // Two's complement
}

// CalculateLengthChecksum computes the length checksum (LCS)
// The LCS is the two's complement of the length
func CalculateLengthChecksum(length byte) byte {
	return (^length) + 1
}
