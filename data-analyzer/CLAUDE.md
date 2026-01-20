# Data Analyzer - Match History Collection & Aggregation

## Purpose
Collect match history data from Riot API and aggregate stats, pushing directly to Turso for consumption by both the desktop app and website.

## Architecture

```
Riot API → Collector (spider) → JSONL files → Reducer → Turso DB
                                    ↓                       ↓
                              hot/ → warm/ → cold/    ┌─────┴─────┐
                                                      ↓           ↓
                                                 Desktop App   Website
```

### Components
1. **Collector** - Spider that crawls match history from Riot API
2. **Reducer** - Processes JSONL files into aggregated stats and pushes to Turso
3. **ContinuousCollector** - Orchestrator for 24/7 collection with state machine
4. **Server** - Web UI for viewing collected data (optional)

## Quick Start

```bash
# One command to collect + reduce (recommended)
# Auto-seeds from top Challenger player - no riot-id needed!
go run cmd/pipeline/main.go --max-players=100

# Or specify a starting player manually
go run cmd/pipeline/main.go --riot-id="Player#NA1" --max-players=100

# Or run steps separately:
# 1. Collect match data (auto-seeds from Challenger if no --riot-id)
go run cmd/collector/main.go --max-players=100

# 2. Process collected data and push to Turso
go run cmd/reducer/main.go
```

### Pipeline Options
```bash
go run cmd/pipeline/main.go \
  --riot-id="Player#NA1" \  # Starting player (optional - auto-seeds from Challenger if omitted)
  --count=20 \              # Matches per player (default: 20)
  --max-players=100 \       # Max active players to collect (default: 100)
  --reduce-only             # Skip collection, only run reducer
```

### Reducer Options
```bash
go run cmd/reducer/main.go \
  --skip-turso               # Skip Turso push (for testing)
```

## Environment Variables
Create `.env` file:
```
RIOT_API_KEY=RGAPI-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
BLOB_STORAGE_PATH=./data

# Turso (required)
TURSO_DATABASE_URL=libsql://your-db.turso.io
TURSO_AUTH_TOKEN=your-token
```

## Collection Strategy

```
┌─────────────────────────────────────────────────────────────────┐
│                         COLLECTOR                                │
└─────────────────────────────────────────────────────────────────┘

1. STARTUP
   ├── Load .env (RIOT_API_KEY, BLOB_STORAGE_PATH)
   ├── Fetch current patch from Data Dragon
   ├── Create file rotator (writes to hot/)
   └── Resolve starting player:
       ├── If --riot-id provided → lookup PUUID
       ├── If --puuid provided → use directly
       └── If neither → auto-seed from top Challenger player

2. SPIDER LOOP (worker pool with producer-consumer pattern)
   ├── Producer: Pop player from queue
   ├── Check rank via Riot API (must be Emerald 4+)
   │   ├── Skip if no solo queue rank
   │   └── Skip if below Emerald 4
   ├── Fetch match history for qualified players
   ├── Dispatch match IDs to worker pool (default 10 workers)
   │
   └── Workers process each match:
       ├── Skip if already visited (bloom filter)
       ├── Fetch match details (1 API call) - ALWAYS
       ├── If old patch → skip (don't collect players)
       ├── Statistical sampling: fetch timeline for ~20% of matches
       ├── Write 10 participants to JSONL
       └── Queue new players from current patch matches

3. FILE ROTATION → hot/*.jsonl → warm/*.jsonl (at 1000 matches)

4. SHUTDOWN → Flush to warm/
```

### Auto-Seeding from Challenger
When no `--riot-id` is provided, the collector automatically fetches the top LP Challenger player as the seed:
- Calls `/lol/league/v4/challengerleagues/by-queue/RANKED_SOLO_5x5`
- Finds the player with highest LP
- Uses their PUUID as the starting point
- Guarantees a high-quality, active player to start the spider

### Rank Filtering
Only players Emerald 4 or higher are collected:
- **Emerald IV, III, II, I** - minimum qualifying ranks
- **Diamond, Master, Grandmaster, Challenger** - all included
- **Below Emerald 4** - skipped (Iron through Platinum, Emerald unranked)

This ensures data quality by focusing on higher-skill games where builds and matchups are more refined.

**API call for rank check** (1 call per player):
- `/lol/league/v4/entries/by-puuid/{puuid}` - get ranked entries directly

### Statistical Sampling Strategy
To reproduce U.GG-style build order stats without fetching expensive timeline for every match:
- **Match Details**: Fetched for 100% of games (accurate win rates)
- **Match Timeline**: Fetched for ~20% of games (build order data)
- **Configurable**: `TimelineSamplingRate` in SpiderConfig (0.0-1.0, default 0.20)

This reduces API calls by ~40% while maintaining statistically representative build path data.

### Key Optimizations
- **Worker pool**: 10 concurrent workers (configurable) for parallel match fetching
- **Bloom filters**: Memory-efficient deduplication of matches and players
- **Timeline sampling**: Only fetch heavy timeline endpoint for 20% of matches
- **Early break**: Once we hit an old patch match, skip remaining (they're older)
- **goccy/go-json**: Fast JSON parsing (~2x faster than encoding/json)

### API Calls Per Player (with rank check + 20% timeline sampling)
| Scenario | Calls Breakdown |
|----------|----------------|
| Rank check | 1 (league entries by PUUID) |
| Match history | 1 |
| 20 current patch games | 1 + 1 + 24 = 26 (20 details + ~4 timelines) |
| 10 current patch games | 1 + 1 + 12 = 14 (10 details + ~2 timelines) |
| Player below Emerald 4 | 1 (only rank check, then skip) |

## Reducer Workflow

```
┌─────────────────────────────────────────────────────────────────┐
│                          REDUCER                                 │
└─────────────────────────────────────────────────────────────────┘

1. LOAD
   ├── Fetch completed items from Data Dragon
   └── Scan warm/*.jsonl files

2. AGGREGATE (per file)
   ├── Parse JSONL records (using goccy/go-json)
   ├── Normalize patch (15.24.734 → 15.24)
   ├── Champion stats: ALL matches (100%)
   ├── Item stats: ALL matches using item0-5 (100%)
   ├── Item slot stats: ONLY matches with buildOrder (~20%)
   └── Calculate matchups (group by matchId, find lane opponents)

3. PUSH TO TURSO (bulk load optimized)
   ├── Create tables (without indexes)
   ├── Drop existing indexes
   ├── Calculate next version (15.24.2 → 15.24.3, or 15.25.1 for new patch)
   ├── Upsert all tables (ON CONFLICT DO UPDATE, 500 rows/batch)
   ├── Recreate indexes
   └── Delete old patches (WHERE patch < min_patch)

4. ARCHIVE → warm/*.jsonl → cold/*.jsonl.gz
```

### Refactored Reducer Components (internal/collector/)

The reducer logic has been refactored into composable functions for the continuous collector:

#### AggregateWarmFiles (reducer.go)
```go
// Reads all JSONL files from warm/ and returns aggregated stats in memory
func AggregateWarmFiles(warmDir string, itemFilter ItemFilter) (*AggData, error)

// AggData contains all aggregated statistics
type AggData struct {
    ChampionStats  map[ChampionStatsKey]*ChampionStats
    ItemStats      map[ItemStatsKey]*ItemStats
    ItemSlotStats  map[ItemSlotStatsKey]*ItemSlotStats
    MatchupStats   map[MatchupStatsKey]*MatchupStats
    DetectedPatch  string
    FilesProcessed int
    TotalRecords   int
}
```

#### ArchiveWarmToCold (reducer.go)
```go
// Moves all .jsonl files from warm/ to cold/ with gzip compression
func ArchiveWarmToCold(warmDir, coldDir string) (int, error)
```

#### TursoPusher (turso_pusher.go)
```go
// Async push queue - processes pushes sequentially in background
type TursoPusher struct { ... }

func NewTursoPusher(pusher DataPusher) *TursoPusher
func (t *TursoPusher) Start(ctx context.Context)      // Start background goroutine
func (t *TursoPusher) Push(ctx context.Context, data *AggData) error  // Queue push (non-blocking)
func (t *TursoPusher) Wait()                          // Wait for pending pushes

// Interface for mocking in tests
type DataPusher interface {
    PushAggData(ctx context.Context, data *AggData) error
}
```

#### WarmLock (warmlock.go)
```go
// RWMutex wrapper to prevent hot→warm rotation during reducer processing
type WarmLock struct { ... }

func (w *WarmLock) RLock()    // Collector acquires for rotation (allows concurrent)
func (w *WarmLock) RUnlock()
func (w *WarmLock) Lock()     // Reducer acquires exclusive access
func (w *WarmLock) Unlock()
```

#### WarmFileCounter (warmcounter.go)
```go
// Atomic counter that triggers callback when threshold is reached
// Used to trigger reduce cycle after N warm file rotations
type WarmFileCounter struct { ... }

func NewWarmFileCounter(threshold int64, callback func()) *WarmFileCounter
func (c *WarmFileCounter) Increment()   // Thread-safe increment, fires callback at threshold
func (c *WarmFileCounter) Count() int64 // Get current count
func (c *WarmFileCounter) Reset()       // Reset to 0, allows callback to fire again
```

### Reduce Cycle (Continuous Mode)
```
1. Acquire WarmLock (exclusive)
2. AggregateWarmFiles() → AggData (in memory)
3. ArchiveWarmToCold() → compress to cold/
4. Release WarmLock
5. TursoPusher.Push(AggData) → async, non-blocking
6. Collector resumes immediately while Turso push runs in background
```

### Reducer Features
- **Patch normalization**: `14.24.448` → `14.24`
- **Sampling-aware aggregation**:
  - Item stats (champion_items): Uses `item0-5` from ALL matches (100% sample)
  - Item slot stats (champion_item_slots): Uses `buildOrder` from sampled matches (~20%)
- **Upsert pattern**: Data accumulates into existing patch buckets via `ON CONFLICT DO UPDATE`
- **Item deduplication**: Only counts unique items per player
- **Completed items only**: Filters out components using Data Dragon (items with no "into" field, cost >= 1000g)
- **Matchup calculation**: Groups participants by matchId to find lane opponents
- **Old patch cleanup**: Deletes data older than current patch - 3 (e.g., if 15.24, deletes 15.21 and older)
- **Archiving**: Compresses processed files to cold/ with gzip

### Turso Bulk Loading
- **Drop indexes before insert**: Faster bulk inserts without index maintenance
- **Multi-value INSERT**: 500 rows per SQL statement
- **Upsert pattern**: `ON CONFLICT DO UPDATE SET wins = wins + excluded.wins`
- **Recreate indexes after insert**: Indexes built once on final data
- **Single transaction per table**: All inserts for a table in one transaction

For 40k rows: ~80 SQL statements instead of ~40,000.

## Storage Lifecycle
- **hot/** - Active writes (current JSONL file being written)
- **warm/** - Closed files awaiting reducer processing
- **cold/** - Compressed archives (.jsonl.gz) after processing

### File Rotation Triggers
- 1,000 matches (10,000 participant records) per file
- Graceful shutdown (Ctrl+C flushes to warm/)

## Database Schema

```sql
-- No foreign keys - all tables independent for parallel operations

CREATE TABLE champion_stats (
    patch TEXT NOT NULL,
    champion_id INTEGER NOT NULL,
    team_position TEXT NOT NULL,
    wins INTEGER NOT NULL DEFAULT 0,
    matches INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (patch, champion_id, team_position)
);

CREATE TABLE champion_items (
    patch TEXT NOT NULL,
    champion_id INTEGER NOT NULL,
    team_position TEXT NOT NULL,
    item_id INTEGER NOT NULL,
    wins INTEGER NOT NULL DEFAULT 0,
    matches INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (patch, champion_id, team_position, item_id)
);

CREATE TABLE champion_item_slots (
    patch TEXT NOT NULL,
    champion_id INTEGER NOT NULL,
    team_position TEXT NOT NULL,
    item_id INTEGER NOT NULL,
    build_slot INTEGER NOT NULL,
    wins INTEGER NOT NULL DEFAULT 0,
    matches INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (patch, champion_id, team_position, item_id, build_slot)
);

CREATE TABLE champion_matchups (
    patch TEXT NOT NULL,
    champion_id INTEGER NOT NULL,
    team_position TEXT NOT NULL,
    enemy_champion_id INTEGER NOT NULL,
    wins INTEGER NOT NULL DEFAULT 0,
    matches INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (patch, champion_id, team_position, enemy_champion_id)
);

-- Indexes for query performance
CREATE INDEX idx_champion_stats_champ_pos ON champion_stats(champion_id, team_position);
CREATE INDEX idx_champion_items_champ_pos ON champion_items(champion_id, team_position);
CREATE INDEX idx_champion_item_slots_champ_pos ON champion_item_slots(champion_id, team_position);
CREATE INDEX idx_champion_matchups_champ_pos ON champion_matchups(champion_id, team_position);
```

## JSONL Record Format
```json
{
  "matchId": "NA1_12345678",
  "gameVersion": "15.24.734",
  "gameDuration": 1847,
  "gameCreation": 1703001234567,
  "puuid": "...",
  "gameName": "Player",
  "tagLine": "NA1",
  "championId": 103,
  "championName": "Ahri",
  "teamPosition": "MIDDLE",
  "win": true,
  "item0": 3089, "item1": 3157, "item2": 3020, "item3": 3165, "item4": 3135, "item5": 3907,
  "buildOrder": [3089, 3157, 3165, 3135, 3907]  // Optional: only present for ~20% of matches (sampled)
}
```

**Note**: `buildOrder` is only populated when timeline was fetched (statistical sampling).
When empty/missing, reducer uses `item0-5` for item stats but skips item slot stats.

## Riot API Endpoints Used

### Americas API (americas.api.riotgames.com)
1. **Account by Riot ID**: `/riot/account/v1/accounts/by-riot-id/{gameName}/{tagLine}`
2. **Account by PUUID**: `/riot/account/v1/accounts/by-puuid/{puuid}` (get Riot ID from PUUID)
3. **Match History**: `/lol/match/v5/matches/by-puuid/{puuid}/ids?queue=420&count=20`
4. **Match Details**: `/lol/match/v5/matches/{matchId}`
5. **Match Timeline**: `/lol/match/v5/matches/{matchId}/timeline` (for build order)

### Regional API (na1.api.riotgames.com)
6. **Ranked Entries by PUUID**: `/lol/league/v4/entries/by-puuid/{puuid}` (get rank for filtering)
7. **Challenger League**: `/lol/league/v4/challengerleagues/by-queue/RANKED_SOLO_5x5` (for auto-seeding)

## Rate Limits (Dev Key)
- 20 requests/second
- 100 requests/2 minutes
- Collector uses conservative 90 req/2min limit
- Waits 30 seconds on 429 responses

## Project Structure
```
data-analyzer/
├── cmd/
│   ├── collector/       # Spider crawler CLI
│   ├── reducer/         # JSONL → Turso aggregator (legacy, standalone)
│   ├── pipeline/        # Combined collector + reducer
│   ├── server/          # Web UI server (optional)
│   └── ui/              # Web UI for pipeline control
│       ├── main.go      # HTTP server with SSE streaming
│       └── templates/   # HTML templates
├── internal/
│   ├── collector/       # Spider + Reducer components
│   │   ├── spider.go        # Producer-consumer pattern, bloom filters, timeline sampling
│   │   ├── reducer.go       # In-memory aggregation + archive (AggregateWarmFiles, ArchiveWarmToCold)
│   │   ├── turso_pusher.go  # Async Turso push queue (TursoPusher, DataPusher interface)
│   │   ├── warmlock.go      # RWMutex wrapper for warm directory synchronization
│   │   └── warmcounter.go   # Atomic counter to trigger reduce at N file rotations
│   ├── riot/            # Riot API client + Data Dragon
│   │   ├── client.go    # HTTP client with rate limiting
│   │   └── types.go     # API response structs
│   ├── storage/         # JSONL file rotation
│   │   ├── rotator.go   # FileRotator with FlushAndRotate for reducer coordination
│   │   └── types.go     # RawMatch struct
│   └── db/
│       └── turso.go     # Turso client with bulk loading, upserts
├── Dockerfile           # Multi-stage build for pipeline
└── docker-compose.yml   # Single service (pipeline + Turso)
```

## Testing

### Unit Tests
```bash
# Run all unit tests (no API key needed)
go test ./... -run "^Test[^_]*$" -v

# Run collector/reducer unit tests
go test ./internal/collector/... -v
```

### Integration Tests
```bash
# Requires valid RIOT_API_KEY in .env
go test ./internal/riot -run TestGetTopChallengerPUUID_Integration -v
go test ./internal/riot -run TestGetSoloQueueRank_Integration -v

# Reducer integration tests (uses in-memory SQLite, no external deps)
go test ./internal/collector/... -run "^Test(ReduceCycle|TursoPush|FullPipeline)" -v
```

### Test Files (internal/collector/)
| File | Description |
|------|-------------|
| `reducer_test.go` | Unit tests for aggregation and archive |
| `turso_pusher_test.go` | Unit tests for async push queue |
| `reducer_integration_test.go` | Integration tests with real files and in-memory DB |
| `warmlock_test.go` | Unit tests for RWMutex wrapper |
| `warmlock_integration_test.go` | Stress tests for lock contention |
| `warmcounter_test.go` | Unit tests for atomic file counter |
| `warmcounter_integration_test.go` | Integration tests with rotator, concurrency stress tests |
| `spider_test.go` | Data collection tests (requires API key) |

### Test Naming Convention
- **Unit tests**: `TestFunctionName` (e.g., `TestIsEmerald4OrHigher`)
- **Integration tests**: `TestFunctionName_Integration` (e.g., `TestGetTopChallengerPUUID_Integration`)

This convention allows Docker builds to run unit tests without requiring API keys.

## Docker

### Build Process
The Dockerfile runs unit tests during build. If tests fail, the build fails and no container is created.

```dockerfile
# Tests run automatically during build
RUN go test ./... -run "^Test[^_]*$" -v
```

### docker-compose.yml
```yaml
services:
  pipeline:
    build: .
    ports:
      - "8080:8080"
    environment:
      - RIOT_API_KEY=${RIOT_API_KEY}
      - BLOB_STORAGE_PATH=/app/data
      - TURSO_DATABASE_URL=${TURSO_DATABASE_URL}
      - TURSO_AUTH_TOKEN=${TURSO_AUTH_TOKEN}
    volumes:
      - ./data:/app/data
```

### Running with Docker
```bash
# Build (runs tests) and start
docker-compose up --build

# Access Web UI at http://localhost:8080
```

## Web UI

The pipeline includes a web UI (`cmd/ui/`) for controlling collection and viewing output.

### Features
- **Environment display**: Shows Riot API Key, Storage path, and Turso database status
- **Pipeline settings**: Configure Riot ID (optional), matches per player, max players
- **Auto-seed**: Leave Riot ID empty to auto-seed from top Challenger player
- **Reduce-only mode**: Skip collection, just run reducer on existing data
- **Live output streaming**: SSE-based real-time log output
- **Auto-push to Turso**: Reducer automatically pushes to Turso when complete
