# Data Analyzer - Match History Collection & Aggregation

## Purpose
Collect match history data from Riot API and aggregate into static JSON files for distribution to the GhostDraft client app.

## Architecture

```
Riot API → Collector (spider) → JSONL files → Reducer → data.json + manifest.json
                                    ↓                            ↓
                              hot/ → warm/ → cold/         (hosted remotely)
                                                                 ↓
                                              Client App → Local SQLite
```

### Components
1. **Collector** - Spider that crawls match history from Riot API
2. **Reducer** - Processes JSONL files into aggregated stats, exports to JSON (and optionally PostgreSQL)
3. **Server** - Web UI for viewing collected data (optional)

## Quick Start

```bash
# One command to collect + reduce (recommended)
go run cmd/pipeline/main.go --riot-id="Player#NA1" --max-players=100

# Or run steps separately:
# 1. Collect match data (spider from a starting player)
go run cmd/collector/main.go --riot-id="Player#NA1"

# 2. Process collected data and export to JSON
go run cmd/reducer/main.go --output-dir=./export

# 3. Upload export/data.json to your CDN and update manifest.json
```

### Pipeline Options
```bash
go run cmd/pipeline/main.go \
  --riot-id="Player#NA1" \  # Starting player (required)
  --count=20 \              # Matches per player (default: 20)
  --max-players=100 \       # Max players to spider (default: 100)
  --output-dir=./export \   # Output directory (default: ./export)
  --reduce-only             # Skip collection, only run reducer
```

### Reducer Options
```bash
go run cmd/reducer/main.go \
  --output-dir=./export      # Directory for JSON output (default: ./export)
```

## Docker UI

Run the pipeline from a web UI:

```bash
# 1. Create .env file with your Riot API key
echo "RIOT_API_KEY=RGAPI-xxxxx" > .env

# 2. Build and start the UI
docker-compose up pipeline --build

# 3. Open http://localhost:8080
```

The UI provides:
- Form to input Riot ID and collection settings
- Real-time streaming output via SSE
- "Reduce only" mode to skip collection and just run the reducer
- Status display for API key and storage path

Data is persisted to local volumes:
- `./data/` - Raw JSONL match data (hot/warm/cold)
- `./export/` - Generated data.json output

## Environment Variables
Create `.env` file:
```
RIOT_API_KEY=RGAPI-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
BLOB_STORAGE_PATH=./data
DATABASE_URL=postgres://analyzer:analyzer123@localhost:5432/lol_matches?sslmode=disable
```

## Storage Lifecycle
- **hot/** - Active writes (current JSONL file being written)
- **warm/** - Closed files awaiting reducer processing
- **cold/** - Compressed archives (.jsonl.gz) after processing

### File Rotation Triggers
- 1,000 matches (10,000 participant records) per file
- 1 hour max file age
- Graceful shutdown (Ctrl+C flushes to warm/)

## Project Structure
```
data-analyzer/
├── cmd/
│   ├── collector/       # Spider crawler CLI
│   │   └── main.go
│   ├── reducer/         # JSONL → PostgreSQL aggregator
│   │   └── main.go
│   └── server/          # Web UI server
│       └── main.go
├── internal/
│   ├── riot/            # Riot API client
│   │   ├── client.go    # HTTP client with rate limiting
│   │   └── types.go     # API response structs
│   ├── storage/         # JSONL file rotation
│   │   ├── rotator.go   # FileRotator implementation
│   │   └── types.go     # RawMatch struct
│   └── db/              # PostgreSQL queries
│       ├── db.go        # Connection pool
│       └── queries*.go  # Query functions
├── web/                 # Static HTML/CSS for server
└── docker-compose.yml   # PostgreSQL container
```

## Database Schema

```sql
-- Champion overall stats
CREATE TABLE champion_stats (
    patch VARCHAR(10),
    champion_id INT,
    team_position VARCHAR(20),  -- TOP, JUNGLE, MIDDLE, BOTTOM, UTILITY
    wins INT,
    matches INT,
    PRIMARY KEY (patch, champion_id, team_position)
);

-- Item stats per champion (overall, regardless of build slot)
CREATE TABLE champion_items (
    patch VARCHAR(10),
    champion_id INT,
    team_position VARCHAR(20),
    item_id INT,
    wins INT,
    matches INT,
    PRIMARY KEY (patch, champion_id, team_position, item_id)
);

-- Item stats by build slot (1st, 2nd, 3rd, 4th, 5th, 6th completed item)
CREATE TABLE champion_item_slots (
    patch VARCHAR(10),
    champion_id INT,
    team_position VARCHAR(20),
    item_id INT,
    build_slot INT,  -- 1-6
    wins INT,
    matches INT,
    PRIMARY KEY (patch, champion_id, team_position, item_id, build_slot)
);

-- Matchup stats (champion vs enemy)
CREATE TABLE champion_matchups (
    patch VARCHAR(10),
    champion_id INT,
    team_position VARCHAR(20),
    enemy_champion_id INT,
    wins INT,
    matches INT,
    PRIMARY KEY (patch, champion_id, team_position, enemy_champion_id)
);
```

## Reducer Features

- **Patch normalization**: `14.24.448` → `14.24`
- **Build order tracking**: Uses `buildOrder` field to track item purchase order (slots 1-6)
- **Item deduplication**: Only counts unique items per player
- **Completed items only**: Filters out components using Data Dragon (items with no "into" field, cost >= 1000g)
- **Matchup calculation**: Groups participants by matchId to find lane opponents
- **JSON Export**: Aggregates ALL files together and exports to data.json + manifest.json
- **Archiving**: Compresses processed files to cold/ with gzip

## JSON Export Format

### manifest.json
```json
{
  "patch": "14.24",
  "dataUrl": "https://your-cdn.com/data/data.json",
  "generatedAt": "2025-01-15T10:30:00Z"
}
```

### data.json
```json
{
  "patch": "14.24",
  "generatedAt": "2025-01-15T10:30:00Z",
  "championStats": [
    {"patch": "14.24", "championId": 103, "teamPosition": "MIDDLE", "wins": 5234, "matches": 10000}
  ],
  "championItems": [
    {"patch": "14.24", "championId": 103, "teamPosition": "MIDDLE", "itemId": 3089, "wins": 3500, "matches": 6000}
  ],
  "championItemSlots": [
    {"patch": "14.24", "championId": 103, "teamPosition": "MIDDLE", "itemId": 3089, "buildSlot": 1, "wins": 2000, "matches": 4000},
    {"patch": "14.24", "championId": 103, "teamPosition": "MIDDLE", "itemId": 3157, "buildSlot": 2, "wins": 1800, "matches": 3500}
  ],
  "championMatchups": [
    {"patch": "14.24", "championId": 103, "teamPosition": "MIDDLE", "enemyChampionId": 238, "wins": 450, "matches": 1000}
  ]
}
```

## JSONL Record Format
```json
{
  "matchId": "NA1_12345678",
  "gameVersion": "14.24.1",
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
  "buildOrder": [3089, 3157, 3165, 3135, 3907]
}
```

## Collection Strategy

1. **Spider/Snowball approach**: Start with 1 player, discover others from their matches
2. **In-memory deduplication**: Track visitedMatchIDs to avoid re-fetching
3. **Rate limiting**: 15 req/sec, 90 req/2min (conservative under Riot's 20/100 limits)
4. **Graceful shutdown**: Ctrl+C flushes current file to warm/

## Collector Options
```bash
go run cmd/collector/main.go \
  --riot-id="Player#NA1" \  # Starting player
  --count=20 \              # Matches per player (default: 20)
  --max-players=100 \       # Max players to spider (default: 100)
  --data-dir=./data         # Storage directory
```

## Riot API Endpoints Used

1. **Account Lookup**: `/riot/account/v1/accounts/by-riot-id/{gameName}/{tagLine}`
2. **Match History**: `/lol/match/v5/matches/by-puuid/{puuid}/ids?queue=420&count=20`
3. **Match Details**: `/lol/match/v5/matches/{matchId}`

## Rate Limits (Dev Key)
- 20 requests/second
- 100 requests/2 minutes
- Collector waits 30 seconds on 429 responses

## Web Server API Endpoints

### Raw Data
- `GET /api/stats` - Match/participant counts
- `GET /api/matches` - Recent matches list
- `GET /api/match/{id}` - Match details
- `GET /api/champions` - Champion stats from raw data

### Aggregated Data
- `GET /api/aggregated/overview` - Summary stats
- `GET /api/aggregated/patches` - Available patches
- `GET /api/aggregated/champions?patch=X` - Champion stats by patch
- `GET /api/aggregated/items?patch=X&champion=Y&position=Z` - Item stats
