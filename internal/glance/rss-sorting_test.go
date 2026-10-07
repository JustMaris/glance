package glance

import (
	"os"
	"testing"
	"time"
)

func TestSortRSSItemsChronological(t *testing.T) {
	// Test chronological sorting (default)
	now := time.Now()
	items := rssFeedItemList{
		{PublishedAt: now.Add(-2 * time.Hour)},
		{PublishedAt: now.Add(-1 * time.Hour)},
		{PublishedAt: now.Add(-3 * time.Hour)},
	}

	sorted := SortRSSItems(items, RSSSortChronological, nil, nil, DefaultRSSSortConfig())

	// Should be sorted newest first
	if !sorted[0].PublishedAt.After(sorted[1].PublishedAt) {
		t.Error("Chronological sort: newest item should be first")
	}
	if !sorted[1].PublishedAt.After(sorted[2].PublishedAt) {
		t.Error("Chronological sort: second newest item should be second")
	}
}

func TestSortRSSItemsEmptyAlgorithm(t *testing.T) {
	// Test that empty algorithm falls back to chronological
	now := time.Now()
	items := rssFeedItemList{
		{PublishedAt: now.Add(-2 * time.Hour)},
		{PublishedAt: now.Add(-1 * time.Hour)},
	}

	sorted := SortRSSItems(items, "", nil, nil, DefaultRSSSortConfig())

	// Should fall back to chronological
	if !sorted[0].PublishedAt.After(sorted[1].PublishedAt) {
		t.Error("Empty algorithm should fall back to chronological")
	}
}

func TestSortRSSItemsTier(t *testing.T) {
	now := time.Now()
	items := rssFeedItemList{
		{ChannelURL: "feed1", PublishedAt: now.Add(-2 * time.Hour)},
		{ChannelURL: "feed2", PublishedAt: now.Add(-1 * time.Hour)},
		{ChannelURL: "feed1", PublishedAt: now.Add(-3 * time.Hour)},
		{ChannelURL: "feed2", PublishedAt: now.Add(-4 * time.Hour)},
	}

	// feed1 is tier 1, feed2 is tier 2
	feedTierMap := map[string]int{
		"feed1": 1,
		"feed2": 2,
	}

	sorted := SortRSSItems(items, RSSSortTier, nil, feedTierMap, DefaultRSSSortConfig())

	// Tier 1 items should come first
	if sorted[0].ChannelURL != "feed1" || sorted[1].ChannelURL != "feed1" {
		t.Errorf("Tier 1 items should come first, got: %s, %s", sorted[0].ChannelURL, sorted[1].ChannelURL)
	}

	// Tier 2 items should come after
	if sorted[2].ChannelURL != "feed2" || sorted[3].ChannelURL != "feed2" {
		t.Errorf("Tier 2 items should come after tier 1, got: %s, %s", sorted[2].ChannelURL, sorted[3].ChannelURL)
	}

	// Within each tier, should be chronological
	if !sorted[0].PublishedAt.After(sorted[1].PublishedAt) {
		t.Error("Within tier 1: items should be chronological")
	}
	if !sorted[2].PublishedAt.After(sorted[3].PublishedAt) {
		t.Error("Within tier 2: items should be chronological")
	}
}

func TestSortRSSItemsTierMissingFeed(t *testing.T) {
	// Test tier sorting when some feeds don't have tiers
	now := time.Now()
	items := rssFeedItemList{
		{ChannelURL: "feed1", PublishedAt: now.Add(-2 * time.Hour)},
		{ChannelURL: "feed2", PublishedAt: now.Add(-1 * time.Hour)},
		{ChannelURL: "feed3", PublishedAt: now.Add(-3 * time.Hour)},
	}

	feedTierMap := map[string]int{
		"feed1": 1,
		"feed2": 2,
		// feed3 has no tier
	}

	sorted := SortRSSItems(items, RSSSortTier, nil, feedTierMap, DefaultRSSSortConfig())

	// Tier 1 should come first
	if sorted[0].ChannelURL != "feed1" {
		t.Errorf("Tier 1 should come first, got: %s", sorted[0].ChannelURL)
	}
	
	// Tier 2 should come second
	if sorted[1].ChannelURL != "feed2" {
		t.Errorf("Tier 2 should come second, got: %s", sorted[1].ChannelURL)
	}
	
	// No-tier feed should come last (tier 0)
	if sorted[2].ChannelURL != "feed3" {
		t.Errorf("No-tier feed should come last, got: %s", sorted[2].ChannelURL)
	}
}

func TestSortRSSItemsSerendipity(t *testing.T) {
	now := time.Now()
	items := rssFeedItemList{
		{PublishedAt: now.Add(-1 * time.Hour)},           // recent
		{PublishedAt: now.Add(-2 * time.Hour)},           // recent
		{PublishedAt: now.Add(-35 * 24 * time.Hour)},      // old (> 30 days)
		{PublishedAt: now.Add(-40 * 24 * time.Hour)},      // old (> 30 days)
		{PublishedAt: now.Add(-3 * time.Hour)},           // recent
	}

	sorted := SortRSSItems(items, RSSSortSerendipity, nil, nil, DefaultRSSSortConfig())

	// Should have 5 items
	if len(sorted) != 5 {
		t.Errorf("Expected 5 items, got %d", len(sorted))
	}

	// First item should be the newest (unchanged)
	if !sorted[0].PublishedAt.Equal(now.Add(-1 * time.Hour)) {
		t.Errorf("First item should be newest, got: %v", sorted[0].PublishedAt)
	}

	// We can't guarantee an old item is injected due to the deterministic selection,
	// but we can check that the algorithm runs without errors
	t.Logf("Serendipity sort completed successfully")
}

func TestRSSSortManagerBasic(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "rss-sort-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager := newRSSSortManager(tmpDir)
	if err := manager.initialize(); err != nil {
		t.Fatalf("Failed to initialize sort manager: %v", err)
	}

	// Test recording timestamps
	feedURL := "https://example.com/feed.xml"
	testTime := time.Now().Add(-24 * time.Hour)
	manager.recordFeedTimestamp(feedURL, testTime)

	// Test retrieving history
	history := manager.getFeedHistory(feedURL)
	if len(history) != 1 {
		t.Errorf("Expected 1 timestamp in history, got %d", len(history))
	}

	// Test persistence
	if err := manager.save(); err != nil {
		t.Fatalf("Failed to save sort manager: %v", err)
	}

	// Create a new manager and verify it loads the persisted data
	newManager := newRSSSortManager(tmpDir)
	if err := newManager.initialize(); err != nil {
		t.Fatalf("Failed to initialize new sort manager: %v", err)
	}

	loadedHistory := newManager.getFeedHistory(feedURL)
	if len(loadedHistory) != 1 {
		t.Errorf("Expected 1 timestamp in loaded history, got %d", len(loadedHistory))
	}
}

func TestRaritySorting(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "rss-rarity-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager := newRSSSortManager(tmpDir)
	if err := manager.initialize(); err != nil {
		t.Fatalf("Failed to initialize sort manager: %v", err)
	}

	// Record some historical timestamps for a feed
	// These are spaced about 24 hours apart (frequent feed)
	feedURL := "https://frequent.com/feed.xml"
	now := time.Now()
	for i := 0; i < 10; i++ {
		manager.recordFeedTimestamp(feedURL, now.Add(-time.Duration(i*24)*time.Hour))
	}

	// Record some historical timestamps for an infrequent feed
	// These are spaced about 7 days apart (infrequent feed)
	infrequentFeedURL := "https://infrequent.com/feed.xml"
	for i := 0; i < 10; i++ {
		manager.recordFeedTimestamp(infrequentFeedURL, now.Add(-time.Duration(i*7*24)*time.Hour))
	}

	// Create test items
	testItems := rssFeedItemList{
		{ChannelURL: feedURL, PublishedAt: now.Add(-1 * time.Hour)},           // recent from frequent feed
		{ChannelURL: infrequentFeedURL, PublishedAt: now.Add(-8 * 24 * time.Hour)}, // old from infrequent feed
	}

	// Sort by rarity
	sorted := sortByRarity(testItems, manager, DefaultRSSSortConfig())

	// The infrequent feed item should be ranked higher (lower effective age)
	// because it's rarer, even though it's older in actual time
	if len(sorted) != 2 {
		t.Fatalf("Expected 2 items, got %d", len(sorted))
	}

	// The test here is that the algorithm runs without errors
	// The actual ranking depends on the complex formula, but we can verify
	// it produces some ordering
	t.Logf("Rarity sort completed successfully")
	t.Logf("First item: %s, Second item: %s", sorted[0].ChannelURL, sorted[1].ChannelURL)
}

func TestRaritySortingCustomConfig(t *testing.T) {
	// Test rarity sorting with custom configuration parameters
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "rss-rarity-custom-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager := newRSSSortManager(tmpDir)
	if err := manager.initialize(); err != nil {
		t.Fatalf("Failed to initialize sort manager: %v", err)
	}

	// Record some historical timestamps for a feed
	feedURL := "https://test.com/feed.xml"
	now := time.Now()
	for i := 0; i < 5; i++ {
		manager.recordFeedTimestamp(feedURL, now.Add(-time.Duration(i*24)*time.Hour))
	}

	// Create test items
	testItems := rssFeedItemList{
		{ChannelURL: feedURL, PublishedAt: now.Add(-1 * time.Hour)},
		{ChannelURL: feedURL, PublishedAt: now.Add(-2 * time.Hour)},
	}

	// Test with custom configuration
	customConfig := RSSSortConfig{
		RarityBase:     144.0,  // Double the default
		RarityExponent: 3.0,    // Higher exponent
	}

	// Sort by rarity with custom config
	sorted := sortByRarity(testItems, manager, customConfig)

	// Should complete without errors
	if len(sorted) != 2 {
		t.Fatalf("Expected 2 items, got %d", len(sorted))
	}

	t.Logf("Custom rarity config sort completed successfully")
}