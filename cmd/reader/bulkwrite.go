// Copyright 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/polling"
	"github.com/ZaparooProject/go-pn532/tagops"
)

type bulkWriteEntry struct {
	name string
	url  string
}

func parseBulkWriteFile(path string) ([]bulkWriteEntry, error) {
	file, err := os.Open(path) //nolint:gosec // Path is user-provided CLI argument
	if err != nil {
		return nil, fmt.Errorf("failed to open bulk write file: %w", err)
	}
	defer func() { _ = file.Close() }()

	var entries []bulkWriteEntry
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ",", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("line %d: expected name,url format: %q", lineNum, line)
		}

		name := strings.TrimSpace(parts[0])
		url := strings.TrimSpace(parts[1])

		if name == "" {
			return nil, fmt.Errorf("line %d: name cannot be empty", lineNum)
		}

		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return nil, fmt.Errorf("line %d: URL must start with http:// or https://: %q", lineNum, url)
		}

		entries = append(entries, bulkWriteEntry{name: name, url: url})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read bulk write file: %w", err)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no valid entries found in %s", path)
	}

	return entries, nil
}

func printBulkWritePrompt(entries []bulkWriteEntry, index int) {
	entry := entries[index]
	_, _ = fmt.Printf("\nWaiting for card... [%d/%d] %q -> %s\n",
		index+1, len(entries), entry.name, entry.url)
}

func runBulkWriteMode(ctx context.Context, device *pn532.Device, cfg *config) error {
	entries, err := parseBulkWriteFile(cfg.bulkWriteFile)
	if err != nil {
		return err
	}

	_, _ = fmt.Println("\n========================================")
	_, _ = fmt.Println("         PN532 Bulk Write Mode")
	_, _ = fmt.Println("========================================")
	_, _ = fmt.Printf("Loaded %d entries from %s\n", len(entries), cfg.bulkWriteFile)
	_, _ = fmt.Println("WARNING: Cards will be permanently locked after writing!")
	_, _ = fmt.Println()

	for entryIndex, entry := range entries {
		_, _ = fmt.Printf("  [%d/%d] %q -> %s\n", entryIndex+1, len(entries), entry.name, entry.url)
	}

	sessionConfig := polling.DefaultConfig()
	session := polling.NewSession(device, sessionConfig)

	defer func() {
		if closeErr := session.Close(); closeErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Failed to close session: %v\n", closeErr)
		}
	}()

	ops := tagops.New(device)
	currentIndex := 0

	session.SetOnCardDetected(func(callbackCtx context.Context, detectedTag *pn532.DetectedTag) error {
		currentIndex = handleBulkWriteCard(callbackCtx, ops, detectedTag, entries, currentIndex)
		return nil
	})

	session.SetOnCardRemoved(func() {
		printBulkWritePrompt(entries, currentIndex)
	})

	printBulkWritePrompt(entries, currentIndex)

	done := make(chan error, 1)
	go func() {
		done <- session.Start(ctx)
	}()

	select {
	case startErr := <-done:
		if startErr != nil {
			return fmt.Errorf("failed to start session: %w", startErr)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func handleBulkWriteCard(
	ctx context.Context,
	ops *tagops.TagOperations,
	detectedTag *pn532.DetectedTag,
	entries []bulkWriteEntry,
	currentIndex int,
) int {
	entry := entries[currentIndex]
	typeName := tagops.TagTypeDisplayName(detectedTag.Type)

	_, _ = fmt.Printf("\n--- Card detected: UID=%s Type=%s ---\n",
		detectedTag.UID, typeName)
	_, _ = fmt.Printf("  [%d/%d] %q\n", currentIndex+1, len(entries), entry.name)

	if initErr := ops.InitFromDetectedTag(ctx, detectedTag); initErr != nil {
		_, _ = fmt.Printf("  Init: FAILED (%v)\n", initErr)
		return currentIndex
	}

	// Write URI NDEF record
	_, _ = fmt.Printf("  Writing URI: %s\n", entry.url)
	message := &pn532.NDEFMessage{
		Records: []pn532.NDEFRecord{
			{
				Type: pn532.NDEFTypeURI,
				URI:  entry.url,
			},
		},
	}
	if writeErr := ops.WriteNDEF(ctx, message); writeErr != nil {
		_, _ = fmt.Printf("  Write: FAILED (%v)\n", writeErr)
		return currentIndex
	}
	_, _ = fmt.Println("  Write: OK")

	// Verify by reading back
	if verifyErr := verifyWrittenURI(ctx, ops, entry.url); verifyErr != nil {
		_, _ = fmt.Printf("  Verify: FAILED (%v)\n", verifyErr)
		return currentIndex
	}
	_, _ = fmt.Println("  Verify: OK")

	// Lock the card
	lockErr := ops.MakeReadOnly(ctx)
	switch {
	case lockErr == nil:
		_, _ = fmt.Println("  Lock: OK (permanently read-only)")
	case errors.Is(lockErr, tagops.ErrLockNotSupported):
		_, _ = fmt.Printf("  Lock: SKIPPED (not supported for %s)\n", typeName)
	default:
		_, _ = fmt.Printf("  Lock: FAILED (%v) — tap card again to retry\n", lockErr)
		return currentIndex
	}

	_, _ = fmt.Printf("  [%d/%d] DONE - %q\n", currentIndex+1, len(entries), entry.name)

	// Advance to next entry
	nextIndex := currentIndex + 1
	if nextIndex >= len(entries) {
		nextIndex = 0
		_, _ = fmt.Printf("\n  All %d entries written! Looping back to start...\n", len(entries))
	}

	return nextIndex
}

func verifyWrittenURI(ctx context.Context, ops *tagops.TagOperations, expectedURI string) error {
	ndefMsg, err := ops.ReadNDEF(ctx)
	if err != nil {
		return fmt.Errorf("read-back failed: %w", err)
	}

	if ndefMsg == nil || len(ndefMsg.Records) == 0 {
		return errors.New("read-back returned empty NDEF")
	}

	if ndefMsg.Records[0].URI != expectedURI {
		return fmt.Errorf("URI mismatch: got %q, expected %q", ndefMsg.Records[0].URI, expectedURI)
	}

	return nil
}
