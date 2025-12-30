# GhostDraft - Project Overview

A Wails-based League of Legends overlay that provides real-time champion select assistance.

## Tech Stack
- **Backend**: Go with Wails v2
- **Frontend**: Vanilla JavaScript (no framework)
- **Data Sources**: Riot LCU API, U.GG API, Data Dragon

## Project Structure

```
├── app.go                 # Main application logic, event handlers
├── main.go                # Wails app entry point
├── frontend/
│   └── src/
│       ├── main.js        # UI logic, event listeners, DOM updates
│       └── style.css      # Styling
├── internal/
│   ├── lcu/
│   │   ├── client.go      # LCU HTTP client (connects to League Client)
│   │   ├── websocket.go   # LCU WebSocket (champ select events)
│   │   ├── champions.go   # ChampionRegistry - ID→name/icon from Data Dragon
│   │   ├── items.go       # ItemRegistry - ID→name/icon from Data Dragon
│   │   └── types.go       # LCU data structures
│   ├── ugg/
│   │   ├── fetcher.go     # U.GG API client, patch fetching
│   │   ├── champion.go    # FetchChampionData - builds/items from overview endpoint
│   │   ├── matchup.go     # FetchMatchups - win rates vs enemies
│   │   └── types.go       # BuildData, MatchupData structs
│   └── data/
│       └── champions.go   # SQLite DB for static champion data (damage types, tags)
```

## Key Data Flows

### 1. Champion Select Updates
```
LCU WebSocket → websocket.go:handleChampSelectEvent
             → app.go:onChampSelectUpdate
             → Emits: champselect:update, bans:update, items:update, etc.
             → Frontend listeners update DOM
```

### 2. U.GG API Endpoints
- **Overview** (`/overview/`): Champion builds, items, runes, win rates
  - Structure: `region → role → tier → [statsArray]`
  - statsData[2] = starting items `[wins, games, [itemIds]]`
  - statsData[3] = core items `[wins, games, [itemIds]]`
  - statsData[5] = situational items per slot:
    - Slot 0: 4th item options `[[itemId, wins, games], ...]`
    - Slot 1: 5th item options
    - Slot 2: 6th item options
    - Slot 3: consumables (ignored)

- **Matchups** (`/matchups/`): Win rates vs specific enemies
  - Structure: `region → tier → role → [[enemyId, wins, games, ...]]`
  - Note: Different nesting order than overview!

### 3. Frontend Events
```javascript
EventsOn('lcu:status', updateStatus);
EventsOn('champselect:update', updateChampSelect);
EventsOn('build:update', updateBuild);
EventsOn('bans:update', updateBans);
EventsOn('items:update', updateItems);
EventsOn('teamcomp:update', updateTeamComp);
EventsOn('fullcomp:update', updateFullComp);
```

## UI Tabs
1. **Matchup** - Recommended bans, win rate vs lane opponent
2. **Build** - Starting items, core items
3. **Team Comp** - Team archetype analysis (when all locked in)

## Important Implementation Notes

### U.GG API Parsing
- Overview and Matchups endpoints have DIFFERENT nesting structures
- Overview: `region → role → tier`
- Matchups: `region → tier → role`
- Don't change one to match the other - they're intentionally different

### Item Parsing
Items in statsData are structured as `[wins, games, [itemId1, itemId2, ...]]`
- Index 0: wins
- Index 1: games
- Index 2: array of item IDs

### Caching Keys
- `lastFetchedChamp` - prevents refetching same champion
- `lastBanFetchKey` - `"{champId}-{role}"` for bans
- `lastItemFetchKey` - `"{champId}-{role}"` for items

### Role IDs (U.GG)
```
"1" = jungle
"2" = support/utility
"3" = bottom/adc
"4" = top
"5" = middle
```

## SQLite Database
Located at `{UserConfigDir}/GhostDraft/champions.db`
- Auto-created on first run
- Stores: champion name, damage type (AP/AD/Mixed/Tank), role tags
- Used for team comp analysis

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
wails dev                # Run in dev mode
wails build              # Build production binary
```
