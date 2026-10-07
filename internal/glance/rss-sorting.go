package glance

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// RSSSortAlgorithm defines the available sorting algorithms for RSS feeds
type RSSSortAlgorithm string

const (
	// RSSSortChronological sorts items by publication date (newest first)
	RSSSortChronological RSSSortAlgorithm = "chronological"
	// RSSSortRarity uses Zach Manson's frequency-weighted algorithm
	RSSSortRarity RSSSortAlgorithm = "rarity"
	// RSSSortTier sorts by source tier priority
	RSSSortTier RSSSortAlgorithm = "tier"
	// RSSSortSerendipity injects random older items
	RSSSortSerendipity RSSSortAlgorithm = "serendipity"
)

// feedHistory stores historical post timestamps for a feed URL for rarity calculations
type feedHistory struct {
	URL       string    `json:"url"`
	Timestamps []time.Time `json:"timestamps"`
	mu        sync.Mutex
}

// rssSortManager handles state persistence and sorting for RSS feeds
type rssSortManager struct {
	dataDir      string
	feedHistory  map[string]*feedHistory
	mu          sync.RWMutex
	initialized bool
}

// newRSSSortManager creates a new sorting manager with the specified data directory
func newRSSSortManager(dataDir string) *rssSortManager {
	return &rssSortManager{
		dataDir:     dataDir,
		feedHistory: make(map[string]*feedHistory),
	}
}

// initialize loads feed history from persistent storage
func (sm *rssSortManager) initialize() error {
	if sm.initialized {
		return nil
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(sm.dataDir, 0755); err != nil {
		// If we can't create the directory, we'll still work but without persistence
		// This is acceptable for containerized environments or read-only filesystems
		return nil
	}

	// Load existing feed history
	historyFile := filepath.Join(sm.dataDir, "rss-feed-history.json")
	if _, err := os.Stat(historyFile); err == nil {
		data, err := os.ReadFile(historyFile)
		if err == nil {
			var histories []feedHistory
			if err := json.Unmarshal(data, &histories); err == nil {
				for _, history := range histories {
					sm.feedHistory[history.URL] = &history
				}
			}
		}
	}

	sm.initialized = true
	return nil
}

// save persists feed history to disk
func (sm *rssSortManager) save() error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if !sm.initialized || sm.dataDir == "" {
		return nil
	}

	// Ensure directory still exists (might have been deleted)
	if err := os.MkdirAll(sm.dataDir, 0755); err != nil {
		return err
	}

	histories := make([]feedHistory, 0, len(sm.feedHistory))
	for _, history := range sm.feedHistory {
		history.mu.Lock()
		histories = append(histories, *history)
		history.mu.Unlock()
	}

	data, err := json.MarshalIndent(histories, "", "  ")
	if err != nil {
		return err
	}

	historyFile := filepath.Join(sm.dataDir, "rss-feed-history.json")
	return os.WriteFile(historyFile, data, 0644)
}

// recordFeedTimestamp records a new timestamp for the given feed URL
func (sm *rssSortManager) recordFeedTimestamp(feedURL string, timestamp time.Time) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.initialized {
		return
	}

	history, exists := sm.feedHistory[feedURL]
	if !exists {
		history = &feedHistory{
			URL:       feedURL,
			Timestamps: make([]time.Time, 0, 100),
		}
		sm.feedHistory[feedURL] = history
	}

	history.mu.Lock()
	history.Timestamps = append(history.Timestamps, timestamp)
	// Keep only the most recent 1000 timestamps to prevent unbounded growth
	if len(history.Timestamps) > 1000 {
		history.Timestamps = history.Timestamps[len(history.Timestamps)-1000:]
	}
	history.mu.Unlock()
}

// getFeedHistory returns the history for a given feed URL
func (sm *rssSortManager) getFeedHistory(feedURL string) []time.Time {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if !sm.initialized {
		return nil
	}

	history, exists := sm.feedHistory[feedURL]
	if !exists {
		return nil
	}

	history.mu.Lock()
	defer history.mu.Unlock()

	// Return a copy
	result := make([]time.Time, len(history.Timestamps))
	copy(result, history.Timestamps)
	return result
}

// RSSSortConfig holds configurable parameters for the rarity algorithm
type RSSSortConfig struct {
	RarityBase     float64
	RarityExponent float64
}

// DefaultRSSSortConfig returns the default configuration for rarity sorting
func DefaultRSSSortConfig() RSSSortConfig {
	return RSSSortConfig{
		RarityBase:     72.0,    // Default from Zach Manson's algorithm
		RarityExponent: 2.5,    // Default from Zach Manson's algorithm
	}
}

// SortRSSItems sorts the RSS items based on the specified algorithm
func SortRSSItems(items rssFeedItemList, algorithm RSSSortAlgorithm, sortManager *rssSortManager, feedTierMap map[string]int, config RSSSortConfig) rssFeedItemList {
	switch algorithm {
	case RSSSortChronological, "":
		return items.sortByNewest()
	case RSSSortRarity:
		return sortByRarity(items, sortManager, config)
	case RSSSortTier:
		return sortByTier(items, feedTierMap)
	case RSSSortSerendipity:
		return sortBySerendipity(items)
	default:
		// Fall back to chronological for unknown algorithms
		return items.sortByNewest()
	}
}

// Default rarity configuration constants
const (
	defaultRarityMinGap  = 0.1
	defaultRarityClampMin = 0.0001
	defaultRarityClampMax = 100.0
)

// sortByRarity implements Zach Manson's frequency-weighted algorithm
// Formula:
// - actual_age = hours since post was published
// - gap = average hours between posts for that specific feed
// - multiplier = clamp((rarityBase / max(minGap, gap))^rarityExponent, clampMin, clampMax)
// - effective_age = actual_age * multiplier
// Sort ascending by effective_age
func sortByRarity(items rssFeedItemList, sortManager *rssSortManager, config RSSSortConfig) rssFeedItemList {
	if sortManager == nil || !sortManager.initialized {
		return items.sortByNewest()
	}

	now := time.Now()

	// Calculate effective age for each item
	type itemWithEffectiveAge struct {
		item          rssFeedItem
		effectiveAge float64
		feedURL      string
	}

	itemsWithAge := make([]itemWithEffectiveAge, 0, len(items))

	for _, item := range items {
		actualAgeHours := now.Sub(item.PublishedAt).Hours()
		
		// Get feed history to calculate gap
		feedHistory := sortManager.getFeedHistory(item.ChannelURL)
		
		var gapHours float64
		if len(feedHistory) >= 2 {
			// Calculate average gap between posts
			var totalGap time.Duration
			for i := 1; i < len(feedHistory); i++ {
				gap := feedHistory[i].Sub(feedHistory[i-1])
				totalGap += gap
			}
			avgGap := totalGap / time.Duration(len(feedHistory)-1)
			gapHours = avgGap.Hours()
		} else {
			// If we don't have enough history, use a default gap of 24 hours
			gapHours = 24
		}

		// Ensure gap is at least 0.1 to avoid division by zero
		if gapHours < 0.1 {
			gapHours = 0.1
		}

		// Use configured values, fall back to defaults
		rarityBase := config.RarityBase
		rarityExponent := config.RarityExponent
		clampMin := defaultRarityClampMin
		clampMax := defaultRarityClampMax

		if rarityBase <= 0 {
			rarityBase = 72.0
		}
		if rarityExponent <= 0 {
			rarityExponent = 2.5
		}

		// Calculate multiplier: clamp((rarityBase / max(0.1, gap))^rarityExponent, clampMin, clampMax)
		multiplier := math.Pow(rarityBase/gapHours, rarityExponent)
		if multiplier < clampMin {
			multiplier = clampMin
		} else if multiplier > clampMax {
			multiplier = clampMax
		}

		effectiveAge := actualAgeHours * multiplier
		
		itemsWithAge = append(itemsWithAge, itemWithEffectiveAge{
			item:          item,
			effectiveAge: effectiveAge,
			feedURL:      item.ChannelURL,
		})
	}

	// Sort by effective age ascending (lower effective age = more recent/rarer)
	sort.Slice(itemsWithAge, func(i, j int) bool {
		return itemsWithAge[i].effectiveAge < itemsWithAge[j].effectiveAge
	})

	// Extract the sorted items
	sortedItems := make(rssFeedItemList, len(itemsWithAge))
	for i, itemWithAge := range itemsWithAge {
		sortedItems[i] = itemWithAge.item
	}

	return sortedItems
}

// sortByTier sorts items by source tier priority, then chronologically within each tier
func sortByTier(items rssFeedItemList, feedTierMap map[string]int) rssFeedItemList {
	if len(feedTierMap) == 0 {
		return items.sortByNewest()
	}

	// Get the maximum tier to handle feeds without explicit tiers
	maxTier := 0
	for _, tier := range feedTierMap {
		if tier > maxTier {
			maxTier = tier
		}
	}
	// Feeds without explicit tiers get assigned maxTier + 1 (lowest priority)
	maxTier++

	// Sort by tier ascending (lower tier number = higher priority), then by date descending
	sort.Slice(items, func(i, j int) bool {
		// Get tier for item i, default to maxTier if not found
		tierI, existsI := feedTierMap[items[i].ChannelURL]
		if !existsI {
			tierI = maxTier
		}
		
		// Get tier for item j, default to maxTier if not found
		tierJ, existsJ := feedTierMap[items[j].ChannelURL]
		if !existsJ {
			tierJ = maxTier
		}
		
		// If tiers are different, sort by tier
		if tierI != tierJ {
			return tierI < tierJ
		}
		
		// If same tier, sort chronologically (newest first)
		return items[i].PublishedAt.After(items[j].PublishedAt)
	})

	return items
}

// sortBySerendipity implements randomized discovery injection
// After applying chronological sort, periodically select a random older item and inject it
func sortBySerendipity(items rssFeedItemList) rssFeedItemList {
	if len(items) == 0 {
		return items
	}

	// First, sort chronologically
	sortedItems := items.sortByNewest()
	
	if len(sortedItems) <= 1 {
		return sortedItems
	}

	// Find items older than 30 days
	thirtyDaysAgo := time.Now().Add(-30 * 24 * time.Hour)
	oldItems := make([]int, 0)
	
	for i, item := range sortedItems {
		if item.PublishedAt.Before(thirtyDaysAgo) {
			oldItems = append(oldItems, i)
		}
	}

	// If we have older items, inject one into the top positions
	if len(oldItems) > 0 {
		// Use current time as seed for deterministic randomness within this sort cycle
		// This ensures the same item isn't always injected on every render
		now := time.Now()
		seed := now.UnixNano()
		// Simple deterministic selection based on seed
		selectedIndex := int(seed) % len(oldItems)
		if selectedIndex < 0 {
			selectedIndex = -selectedIndex
		}
		selectedIndex = selectedIndex % len(oldItems)
		
		oldItemIndex := oldItems[selectedIndex]
		
		// Move the selected old item to a prominent position (position 1, after the newest item)
		if oldItemIndex > 0 {
			// Remove the item from its current position
			oldItem := sortedItems[oldItemIndex]
			sortedItems = append(sortedItems[:oldItemIndex], sortedItems[oldItemIndex+1:]...)
			
			// Insert it after the newest item (position 1)
			if len(sortedItems) > 0 {
				sortedItems = append(sortedItems[:1], append([]rssFeedItem{oldItem}, sortedItems[1:]...)...)
			} else {
				sortedItems = append([]rssFeedItem{oldItem}, sortedItems...)
			}
		}
	}

	return sortedItems
}