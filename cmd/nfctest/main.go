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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Import detection packages to register detectors
	_ "github.com/ZaparooProject/go-pn532/detection/i2c"
	_ "github.com/ZaparooProject/go-pn532/detection/spi"
	_ "github.com/ZaparooProject/go-pn532/detection/uart"
)

func main() {
	if run() != 0 {
		os.Exit(1)
	}
}

func run() int {
	// Parse command line flags
	quick := flag.Bool("quick", false, "Quick mode - lighter, faster testing")
	vendorTest := flag.Bool("vendor-test", false, "Vendor test mode - continuous operation for testing readers")
	connectTimeoutFlag := flag.Duration("connect-timeout", 10*time.Second, "Reader connection timeout")
	detectTimeoutFlag := flag.Duration("detect-timeout", 30*time.Second, "Card/tag detection timeout")
	verboseFlag := flag.Bool("verbose", false, "Enable verbose output")

	flag.Parse()

	// Create configuration
	config := DefaultConfig()

	// Determine operating mode
	switch {
	case *quick:
		config.Mode = ModeQuick
	case *vendorTest:
		config.Mode = ModeVendorTest
	default:
		config.Mode = ModeComprehensive
	}

	config.ConnectTimeout = *connectTimeoutFlag
	config.DetectTimeout = *detectTimeoutFlag
	config.Verbose = *verboseFlag

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		_, _ = fmt.Print("\nShutting down gracefully...\n")
		cancel()
	}()

	// Initialize components
	output := NewOutput(config.Verbose)
	discovery := NewDiscovery(config, output)
	testing := NewTesting(config, output, discovery)
	monitoring := NewMonitoring(config, output, discovery, testing)
	modes := NewModes(config, output, discovery, monitoring, testing)

	// Run the appropriate mode
	var err error
	switch config.Mode {
	case ModeComprehensive:
		err = modes.RunComprehensive(ctx)
	case ModeQuick:
		err = modes.RunQuick(ctx)
	case ModeVendorTest:
		err = modes.RunVendorTest(ctx)
	}

	if err != nil {
		output.Error("%v", err)
		return 1
	}
	return 0
}
