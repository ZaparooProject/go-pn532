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

package detection

import (
	"sync"
	"time"
)

// cacheEntry holds cached detection results.
type cacheEntry struct {
	timestamp time.Time
	devices   []DeviceInfo
}

// detectionCache provides thread-safe caching of detection results.
type detectionCache struct {
	entries map[string]cacheEntry
	mu      sync.RWMutex
}

// global cache instance.
var cache = &detectionCache{
	entries: make(map[string]cacheEntry),
}

// getCached returns cached devices if available and not expired
func getCached(transport string, ttl time.Duration) ([]DeviceInfo, bool) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	entry, exists := cache.entries[transport]
	if !exists {
		return nil, false
	}

	if time.Since(entry.timestamp) > ttl {
		return nil, false
	}

	// Return a copy to prevent modification
	devices := make([]DeviceInfo, len(entry.devices))
	copy(devices, entry.devices)
	return devices, true
}

// setCached stores detection results in cache
func setCached(transport string, devices []DeviceInfo) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// Store a copy to prevent external modification
	devicesCopy := make([]DeviceInfo, len(devices))
	copy(devicesCopy, devices)

	cache.entries[transport] = cacheEntry{
		devices:   devicesCopy,
		timestamp: time.Now(),
	}
}

// clearCache removes all cached entries
func clearCache() {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.entries = make(map[string]cacheEntry)
}

// clearCacheForTransport removes cached entries for a specific transport
func clearCacheForTransport(transport string) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	delete(cache.entries, transport)
}
