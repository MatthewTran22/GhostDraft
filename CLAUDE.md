# GhostDraft - Project Overview

A League of Legends companion suite consisting of a desktop overlay app and a companion website, both sharing a unified Hextech Arcane visual theme.

## Projects

### 1. Desktop App (Wails)
Real-time champion select overlay that connects to the League Client.
- **Backend**: Go with Wails v2
- **Frontend**: Vanilla JavaScript + CSS
- **Data Sources**: Riot LCU API, Local SQLite, Data Dragon

### 2. Website (Next.js)
Companion website for browsing champion stats, builds, and matchups.
- **Framework**: Next.js 15 with App Router
- **Styling**: Tailwind CSS + Custom Hextech theme
- **Database**: Turso (cloud SQLite) with @libsql/client
- **Caching**: Next.js `unstable_cache` (1 hour TTL)
- See `website/CLAUDE.md` for details

### 3. Data Analyzer
Match history collection and aggregation pipeline.
- See `data-analyzer/CLAUDE.md` for details

## Shared Design System: Hextech Arcane Theme

Both the desktop app and website use a unified visual theme:

### Color Palette
```css
--void-black: #0a0a0f        /* Deepest background */
--abyss: #0d0d14             /* Card backgrounds */
--deep-navy: #12121a         /* Secondary backgrounds */
--hextech-gold: #c9a227      /* Primary accent */
--pale-gold: #f0e6d2         /* Light gold for headers */
--arcane-cyan: #00d4ff       /* Secondary accent */
--text-primary: #e8e6e3      /* Main text */
--text-secondary: #a09b8c    /* Muted text */
```

### Fonts
- **Display/Headers**: Cinzel (serif, elegant)
- **Body Text**: Rajdhani (sans-serif, technical)

### Design Elements
- Gold borders and glowing effects
- Subtle hex pattern backgrounds
- Rounded corners (8-12px)
- Gold gradient hover states

## Desktop App Structure

```
├── app.go                 # Main application struct, startup/shutdown, LCU polling
├── app_champselect.go     # Champion select event handling
├── app_emitters.go        # Real-time event emitters (fetchAndEmit* functions)
├── app_meta.go            # Meta tab types and API functions
├── app_stats.go           # Personal stats and force update
├── main.go                # Wails app entry point
├── frontend/
│   └── src/
│       ├── main.js        # UI logic, event listeners, DOM updates
│       └── style.css      # Hextech Arcane theme styling
├── internal/
│   ├── lcu/
│   │   ├── client.go      # LCU HTTP client (connects to League Client)
│   │   ├── websocket.go   # LCU WebSocket (champ select events)
│   │   ├── champions.go   # ChampionRegistry - ID→name/icon from Data Dragon
│   │   ├── items.go       # ItemRegistry - ID→name/icon from Data Dragon
│   │   └── types.go       # LCU data structures
│   └── data/
│       ├── champions.go     # SQLite DB for static champion data (damage types, tags)
│       ├── stats.go         # SQLite DB for match stats with remote update mechanism
│       └── stats_queries.go # StatsProvider - SQLite queries for builds, matchups, counters
├── data-analyzer/         # Separate module for collecting match data
├── website/               # Next.js companion website
```

## Key Data Flows

### 1. Champion Select Updates
```
LCU WebSocket → websocket.go:handleChampSelectEvent
             → app.go:onChampSelectUpdate
             → Emits: champselect:update, bans:update, items:update, etc.
             → Frontend listeners update DOM
```

### 2. Stats Data Distribution
```
Reducer (JSONL files) → Aggregates stats → Pushes to Turso
                                                    ↓
                                                 Turso DB
                                                    ↓
              ┌─────────────────────────────────────┴─────────────────────────────────────┐
              ↓                                                                           ↓
       Website (Next.js)                                                          Desktop App
       Queries Turso for web UI                                                   Queries Turso directly
                                                                                  (embedded read-only credentials)
                                                                                  + In-memory caching
```

**Note**: The desktop app embeds Turso credentials (read-only) at build time.
Credentials are not publicly exposed but can be extracted from binary with effort.
Data is public stats anyway - worst case someone reads your aggregated data.

### 3. Stats Provider (Turso + Cache)
The `internal/data/stats_queries.go` queries Turso directly with in-memory caching:
- **FetchChampionData**: Item builds by slot position with win rates
- **FetchAllMatchups**: All matchup win rates for a champion
- **FetchCounterMatchups**: Top N counters (lowest win rate matchups <49%, min 10 games)
- **FetchCounterPicks**: Champions that counter a specific enemy (win rate >51%)
- **FetchMatchup**: Specific champion vs enemy win rate
- **FetchTopChampionsByRole**: Meta champions by win rate per role

**Caching**: Results are cached in-memory per session. First query hits Turso (~100-200ms),
subsequent identical queries return instantly from cache. Cache clears on app restart.

### Database Tables (Turso)
```sql
champion_stats      -- Champion win rates by patch/position
champion_items      -- Item stats per champion/position (overall)
champion_item_slots -- Item stats by build slot (1st, 2nd, 3rd, 4th, 5th, 6th item)
champion_matchups   -- Matchup win rates between champions
data_version        -- Tracks current patch version
```

### 4. Frontend Events
```javascript
EventsOn('lcu:status', updateStatus);
EventsOn('champselect:update', updateChampSelect);
EventsOn('build:update', updateBuild);
EventsOn('bans:update', updateBans);
EventsOn('items:update', updateItems);
EventsOn('counterpicks:update', updateCounterPicks);  // Counter picks vs enemy laner (post-ban phase)
EventsOn('teamcomp:update', updateTeamComp);
EventsOn('fullcomp:update', updateFullComp);
```

## UI Tabs
1. **Matchup** - Counter matchups (champions with lowest WR against you), live matchup WR vs lane opponent
2. **Build** - Core items (slots 1-3), 4th/5th/6th item options with win rates
3. **Team Comp** - Team archetype analysis (when all locked in)
4. **Meta** - Top 5 champions by win rate for each role

## Important Implementation Notes

### Stats Database (internal/data/stats.go)
- Located at `{UserConfigDir}/GhostDraft/stats.db`
- On startup, checks `STATS_MANIFEST_URL` for updates
- Compares remote patch version with local `data_version` table
- Downloads and bulk-inserts new data in a single transaction (for performance)
- Falls back to cached data if network unavailable

### Stats Provider (internal/data/stats_queries.go)
- Queries local SQLite instead of remote PostgreSQL
- Uses `?` placeholders (SQLite) instead of `$1, $2` (PostgreSQL)
- Uses `CAST(... AS REAL)` for floating-point division
- Falls back to aggregated data across patches if current patch has no data
- Counter matchups filter: WR < 49% (true counters only)
- Counter picks filter: WR > 51% (champions that beat the enemy)
- Uses window functions for pick rate calculation from sampled data:
  ```sql
  CAST(SUM(matches) AS REAL) / SUM(SUM(matches)) OVER () * 100 as pick_rate
  ```
  This avoids the "denominator trap" when calculating percentages from sampled data.

### Item Filtering
- Only shows "completed" items (not components)
- Uses Data Dragon to identify items with no "into" field and cost >= 1000g

### Caching Keys
- `lastFetchedChamp` - prevents refetching same champion
- `lastBanFetchKey` - `"{champId}-{role}"` for bans
- `lastItemFetchKey` - `"{champId}-{role}"` for items
- `lastCounterFetchKey` - `"{enemyChampId}-{role}"` for counter picks

## Databases

### Local SQLite (Desktop App)
Located at `{UserConfigDir}/GhostDraft/`:
- `champions.db` - Static champion metadata (damage types, tags)

### Turso (Cloud)
Stats data is stored in Turso and queried directly by both website and desktop app:
- `champion_stats`, `champion_items`, `champion_item_slots`, `champion_matchups`

## Environment Variables

### Desktop App (.env in root - for development)
```bash
# Development only - overrides build-time values
TURSO_DATABASE_URL=libsql://your-db.turso.io
TURSO_AUTH_TOKEN=your-token
```

### Production Build Variables
Set these before running `build.ps1` or `build.sh`:
```bash
TURSO_DATABASE_URL=libsql://your-db.turso.io  # Turso database URL
TURSO_AUTH_TOKEN=your-read-only-token         # Read-only Turso auth token
```

### Website (.env in website/)
```bash
TURSO_DATABASE_URL=libsql://your-db.turso.io
TURSO_AUTH_TOKEN=your-token
```

## Common Tasks

### Adding a new event
1. Create emit in `app.go`: `runtime.EventsEmit(a.ctx, "event:name", data)`
2. Add listener in `main.js`: `EventsOn('event:name', handlerFunction)`
3. Create handler function to update DOM

### Adding a new tab
1. Add button in `main.js` HTML: `<button class="tab-btn" data-tab="tabname">Label</button>`
2. Add content div: `<div class="tab-content" id="tab-tabname">...</div>`
3. Tab switching is automatic via existing JS

## Build Commands
```bash
go build ./...           # Check Go compiles
wails dev                # Run in dev mode (uses env vars for Turso)
wails build              # Build dev binary (no embedded credentials)

# Production build (embeds Turso credentials)
./build.ps1              # Windows PowerShell
./build.sh               # Linux/Mac/Git Bash
```

### Manual Production Build
```bash
wails build -ldflags "\
-X 'ghostdraft/internal/data.TursoURL=libsql://your-db.turso.io' \
-X 'ghostdraft/internal/data.TursoAuthToken=your-read-only-token'"
```

## Data Pipeline
See `data-analyzer/CLAUDE.md` for the match collection and aggregation pipeline.

### Key Concepts
- **Statistical Sampling**: Timeline fetched for ~20% of matches (build order data), match details for 100% (win rates)
- **Versioned Data**: Patch versions include build numbers (e.g., 15.24.1, 15.24.2) for incremental updates
- **Upsert Pattern**: New data accumulates into existing patch buckets via `ON CONFLICT DO UPDATE`

### Data Sources by Table
| Table | Sample Rate | Data Source |
|-------|-------------|-------------|
| `champion_stats` | 100% | Match details |
| `champion_items` | 100% | Final inventory (item0-5) |
| `champion_item_slots` | ~20% | Timeline build order |
| `champion_matchups` | 100% | Match details |

### Running the Pipeline
```bash
cd data-analyzer

# Full pipeline: collect + reduce (auto-seeds from top Challenger)
go run cmd/pipeline/main.go --max-players=100

# Or with a specific starting player
go run cmd/pipeline/main.go --riot-id="Player#NA1" --max-players=100

# Reducer only (process existing data)
go run cmd/reducer/main.go --output-dir=./export
```

This generates:
- `export/manifest.json` - Version info (e.g., "15.24.3") and data URL
- `export/data.json` - All aggregated stats
