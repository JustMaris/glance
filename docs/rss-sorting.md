# RSS Feed Sorting Algorithms

Glance now supports flexible, multi-strategy sorting for RSS feeds. You can configure each RSS widget to use different sorting algorithms based on your needs.

## Overview

The default behavior remains **chronological** (newest first), but you can now specify alternative sorting strategies:

- `chronological` - Newest items first (default)
- `rarity` - Zach Manson's frequency-weighted algorithm for surfacing infrequent feeds
- `tier` - Source priority tiering (higher priority sources first)
- `serendipity` - Randomized discovery injection (breaks echo chambers)

## Configuration

### Basic Usage

```yaml
widgets:
  - type: rss
    sort: rarity  # Optional: chronological, rarity, tier, serendipity
    feeds:
      - url: https://example.com/rss
```

### Tier Sorting Configuration

For tier-based sorting, you can assign priority tiers to individual feeds:

```yaml
widgets:
  - type: rss
    sort: tier
    feeds:
      - url: https://important.com/rss
        tier: 1  # Highest priority
      - url: https://secondary.com/rss
        tier: 2  # Medium priority
      - url: https://background.com/rss
        # No tier specified - lowest priority
```

### Rarity Sorting with Custom Configuration

For fine-tuning the rarity algorithm, you can specify custom parameters:

```yaml
widgets:
  - type: rss
    sort: rarity
    rarity-base: 144        # Default: 72, higher = more aggressive boosting
    rarity-exponent: 3.0   # Default: 2.5, higher = more extreme weighting
    cache: 1h              # Recommended for better learning
    feeds:
      - url: https://infrequent-blog.com/rss
      - url: https://high-volume-news.com/rss
```

## Sorting Algorithms

### 1. Chronological (Default)

**Behavior:** Sorts items strictly by publication timestamp, newest first.

**Configuration:**
```yaml
sort: chronological  # or omit the sort field entirely
```

**Use Case:** Best for news feeds where you want the absolute latest content first.

### 2. Rarity (Zach Manson's Frequency-Weighted Algorithm)

**Behavior:** Boosts infrequent feeds and penalizes hyper-posting sites. Uses historical data to calculate posting frequency and apply a weighted ranking.

**Formula:**
- `actual_age` = hours since post was published
- `gap` = average hours between posts for that specific feed
- `multiplier` = clamp((rarity_base / max(0.1, gap))^rarity_exponent, 0.0001, 100)
- `effective_age` = actual_age × multiplier

Items are sorted ascending by `effective_age`, meaning feeds that post less frequently get a boost.

**Configurable Parameters:**
- `rarity-base` (default: 72): Controls the baseline frequency sensitivity. Higher values make infrequent feeds more likely to appear at the top.
- `rarity-exponent` (default: 2.5): Controls how aggressively frequency differences are amplified. Higher values create more extreme weighting.

**Configuration:**
```yaml
sort: rarity
cache: 1h  # More frequent updates help the algorithm learn posting patterns
feeds:
  - url: https://infrequent-blog.com/rss
  - url: https://high-volume-news.com/rss
```

**State Persistence:** The rarity algorithm maintains a local history file (`data/rss-feed-history.json`) to track posting patterns. This allows the algorithm to learn and adapt to each feed's frequency over time.

**Use Case:** Ideal for personal blogs and niche content where you want to ensure infrequent posters don't get buried by high-volume sites.

### 3. Tier (Source Priority Tiering)

**Behavior:** Sorts items primarily by source tier (Tier 1 first, then Tier 2, etc.), and chronologically within each tier.

**Configuration:**
```yaml
sort: tier
feeds:
  - url: https://must-read.com/rss
    tier: 1  # Highest priority - items from this feed appear first
  
  - url: https://sometimes-interesting.com/rss
    tier: 2  # Secondary priority - items appear after tier 1
  
  - url: https://background-noise.com/rss
    # No tier specified - lowest priority (after all tiered feeds)
```

**Tier Handling:**
- Lower tier numbers = higher priority
- Feeds without an explicit tier are assigned the lowest priority
- Within each tier, items are sorted chronologically (newest first)

**Use Case:** Perfect for curating a reading list where some sources are more important than others.

### 4. Serendipity (Randomized Discovery Injection)

**Behavior:** After applying chronological sorting, periodically selects a random older item (30+ days old) and injects it into the top display slots to break up echo chambers and surface content you might have missed.

**Configuration:**
```yaml
sort: serendipity
limit: 20  # Good to have a higher limit for this to work effectively
feeds:
  - url: https://example.com/rss
```

**Algorithm:**
1. First sorts all items chronologically
2. Identifies items older than 30 days
3. Uses deterministic selection (based on current timestamp) to pick one older item
4. Injects the selected item into a prominent position (after the newest item)

**Use Case:** Great for discovering older content you might have missed, breaking out of recency bias.

## Examples

See `rss-sorting-examples.yml` for comprehensive configuration examples.

### Mixed Strategies

You can use different sorting strategies for different widgets on the same page:

```yaml
pages:
  - name: Dashboard
    columns:
      - size: small
        widgets:
          - type: rss
            title: Priority Sources
            sort: tier
            feeds:
              - url: https://critical.com/rss
                tier: 1
              - url: https://important.com/rss
                tier: 2
                
      - size: full
        widgets:
          - type: rss
            title: Hidden Gems
            sort: rarity
            feeds:
              - url: https://personal-blog.com/rss
              - url: https://news-site.com/rss

      - size: small
        widgets:
          - type: rss
            title: Discovery
            sort: serendipity
            feeds:
              - url: https://archive.org/rss
```

## Implementation Details

### State Persistence

The **rarity** algorithm uses persistent storage to track historical post timestamps. This data is stored in `data/rss-feed-history.json` and includes:

- Feed URLs
- Historical post timestamps (up to 1000 per feed)
- Automatic cleanup of old data

The state file is:
- Created automatically if it doesn't exist
- Updated after each fetch cycle
- Loaded on widget initialization
- Compatible across restarts

### Performance

- **Chronological & Tier:** O(n log n) - simple sorting operations
- **Rarity:** O(n log n + m) where m is the number of feeds with historical data
- **Serendipity:** O(n log n) - chronological sort plus O(n) for identifying old items

All algorithms are designed to be efficient even with hundreds of RSS items.

### Backwards Compatibility

Existing RSS widgets without a `sort` field will continue to work exactly as before, using chronological sorting.

### Error Handling

- If the rarity state file cannot be read or written, the widget falls back to chronological sorting
- If an unknown sort algorithm is specified, the widget falls back to chronological sorting
- Invalid tier values are treated as having no tier (lowest priority)

## Data Directory

By default, the sorting state is stored in the `data/` directory relative to where Glance is run. Ensure this directory is writable by the Glance process.

You can also configure a different data directory by modifying the data path in the sort manager initialization (in `widget-rss.go`).

## Migration Guide

If you're upgrading an existing Glance installation:

1. **No action required** - existing configurations continue to work unchanged
2. **To use new sorting:** Add the `sort` field to your RSS widget configurations
3. **To use tiering:** Add `tier` fields to individual feeds in your configuration

## Troubleshooting

### Sorting not working as expected?

1. **Check configuration:** Ensure the `sort` field is spelled correctly
2. **Verify tiers:** For tier sorting, ensure feeds have valid `tier` values
3. **Check permissions:** For rarity sorting, ensure the `data/` directory is writable
4. **Review cache:** Rarity sorting works best with more frequent updates (`cache: 1h` recommended)

### Performance issues?

- Reduce the number of feeds per widget
- Use the `limit` field to restrict the number of items processed
- Consider splitting high-volume feeds into separate widgets

## Contributing

The sorting algorithms are implemented in:
- `internal/glance/rss-sorting.go` - Core sorting algorithms
- `internal/glance/widget-rss.go` - Widget integration
- `internal/glance/rss-sorting_test.go` - Tests

To add a new sorting algorithm:
1. Define the algorithm constant
2. Add the sorting function
3. Update the `SortRSSItems` switch statement
4. Add configuration support if needed
5. Add tests