# GhostDraft Desktop App Documentation

This document details how every page and feature of the GhostDraft desktop overlay application works.

## Table of Contents

1. [Overview](#overview)
2. [Application States](#application-states)
3. [Champion Select Mode](#champion-select-mode)
   - [Stats Tab](#stats-tab)
   - [Matchup Tab](#matchup-tab)
   - [Build Tab](#build-tab)
   - [Team Comp Tab](#team-comp-tab)
   - [Meta Tab](#meta-tab)
4. [In-Game Mode](#in-game-mode)
   - [Build Tab (In-Game)](#build-tab-in-game)
   - [Scouting Tab](#scouting-tab)
5. [Tab HUD Mode](#tab-hud-mode)
6. [Hotkeys](#hotkeys)
7. [Data Sources](#data-sources)

---

## Overview

GhostDraft is a real-time overlay that provides champion select assistance and in-game information. The application automatically detects the League Client connection and game state, switching between different modes:

- **Idle Mode**: Shows Stats and Meta tabs while waiting for champion select
- **Champion Select Mode**: Full tab navigation with matchup data, builds, and team composition
- **In-Game Mode**: Simplified overlay with build info and player scouting
- **Tab HUD Mode**: Minimal overlay triggered by holding Tab during a game

---

## Application States

### Connection Status

Located at the top of the overlay, the status indicator shows:

| Status | Indicator | Description |
|--------|-----------|-------------|
| **Connected** | Green pulsing dot | Successfully connected to League Client |
| **Waiting** | Gold pulsing dot | Attempting to connect to League Client |

The app polls for the League Client on startup and automatically connects when detected.

### Window Controls

- **Drag**: Click and drag the header to reposition the overlay
- **Toggle Visibility**: `Ctrl+O` hides/shows the overlay globally
- **Auto-hide**: The overlay automatically hides when entering a game and shows when leaving

---

## Champion Select Mode

When you enter champion select, the app switches to show champion-select-specific tabs (Matchup, Build, Team Comp) while hiding the Stats tab. The Meta tab remains available throughout.

### Stats Tab

**When Visible**: Only outside of champion select (in lobby, queue, post-game)

**Purpose**: Shows your personal ranked performance statistics

**Data Displayed**:

1. **Overall Stats Strip** (top bar):
   - Win/Loss record (e.g., "45W 32L")
   - Win Rate percentage (color-coded: green >55%, red <45%, gold 45-55%)
   - Average KDA
   - CS per minute

2. **Most Played Champion Banner**:
   - Champion splash art as background
   - Role icon and champion name
   - Number of games played
   - Win rate, KDA, and CS/min for that champion

3. **Also Played Section**:
   - List of other recently played champions
   - Shows icon, name, games played, and win rate for each

**Data Source**: Fetches last 20 ranked games from the League Client API (LCU) when the tab is clicked or on initial load.

**How It Works**:
1. Calls `GetPersonalStats()` which fetches match history from LCU
2. `lcu.CalculatePersonalStats()` aggregates data from the matches
3. Filters to ranked games only, calculates averages
4. Groups by champion to find most played

---

### Matchup Tab

**When Visible**: During champion select only

**Purpose**: Provides real-time matchup information to help with banning and picking

**Components**:

#### 1. Team Comp Warning Card (Conditional)
Shows when your team's damage type is imbalanced:
- **Warning** (yellow): 75%+ of one damage type
- **Critical** (red): 90%+ of one damage type
- Message suggests picking the opposite damage type

**Logic** (from `app_teamcomp.go:23-136`):
- Counts AP, AD, and mixed damage champions on your team
- Excludes your own hover (only counts locked-in teammates)
- Calculates ratio and displays warning if heavily skewed

#### 2. Recommended Bans Card
**When Shown**: During ban phase (before all bans are completed)

**Data Displayed**:
- Subheader showing your champion and role (e.g., "Counters for Yasuo (Mid)")
- List of up to 5 champions that counter your pick
- Each row shows: Rank, Icon, Name, Damage Type (AP/AD/Mixed), Win Rate

**How It Works**:
1. When you hover/lock a champion, `fetchAndEmitRecommendedBans()` is called
2. Calls `FetchCounterMatchups(championID, role, 5)` from stats provider
3. Returns champions with <49% win rate against your champion
4. Adds damage type info from the local champion database
5. Caching: Uses `lastBanFetchKey` to prevent refetching for same champion+role

#### 3. Counter Picks Card
**When Shown**: After ban phase, when an enemy laner is visible

**Data Displayed**:
- Subheader showing enemy laner name (e.g., "vs Zed")
- List of champions that beat the enemy laner (>51% win rate)
- Each row shows: Icon, Name, Win Rate, Game count

**How It Works**:
1. After ban phase, finds enemy in your lane position
2. `fetchAndEmitCounterPicks()` calls `FetchCounterPicks(enemyChampID, role, 6)`
3. Returns champions with win rate >51% against that enemy
4. Caching: Uses `lastCounterFetchKey`

#### 4. Build Card (Matchup Win Rate)
**When Shown**: When both you and your lane opponent have champions selected

**Data Displayed**:
- Your role (Top, Jungle, Mid, ADC, Support)
- Win rate label (e.g., "vs Zed")
- Large win rate percentage, color-coded:
  - **Green (winning)**: >51%
  - **Red (losing)**: <49%
  - **Gold (even)**: 49-51%

**How It Works**:
1. `fetchAndEmitBuild()` called when champion changes or enemies appear
2. Fetches all matchups for your champion via `FetchAllMatchups()`
3. Finds enemy with highest game count in matchup data (likely lane opponent)
4. Displays that specific matchup win rate

---

### Build Tab

**When Visible**: During champion select only

**Purpose**: Shows optimal item builds for your champion and role

**Data Displayed**:

1. **Core Items** (3 items):
   - First 3 items to build in order
   - No win rate shown (these are the standard core)

2. **4th Item Options**:
   - Multiple item choices with individual win rates
   - Win rate color-coded (green >51%, red <49%)
   - Shows game count on hover

3. **5th Item Options**:
   - Same format as 4th items

4. **6th Item Options**:
   - Same format as 4th/5th items

**How It Works**:
1. `fetchAndEmitItems()` called when champion+role changes
2. Calls `FetchChampionData(championID, name, role)` from stats provider
3. Core items come from `champion_item_slots` table (1st, 2nd, 3rd slots)
4. Late game options come from 4th, 5th, 6th slot data
5. All items filtered to "completed" items only (no components)

**Caching**: Uses `lastItemFetchKey` to avoid refetching same champion+role

---

### Team Comp Tab

**When Visible**: During champion select only

**Purpose**: Analyzes both teams' compositions once all champions are locked in

**States**:

#### Waiting State
- Shows "Waiting for all players to lock in..."
- Active until all 10 players have locked champions

#### Analysis State
Shows two sections (Your Team / Enemy Team) with:

1. **Archetype** (e.g., "Hard Engage", "Poke/Siege", "Pick Comp"):
   - Determined by analyzing team's role tags
   - Possible archetypes:
     - **Hard Engage**: 3+ Engage, has Tank/Bruiser
     - **Poke/Siege**: 3+ Poke, lacks hard engage
     - **Pick Comp**: 3+ Burst with single-target CC
     - **Teamfight**: 2+ Engage and 2+ Burst
     - **Skirmish**: 3+ Bruisers
     - **Front-to-Back**: 2+ Tanks
     - **Mixed**: Default when no clear archetype

2. **Tags** (e.g., "Engage (3)", "Burst (2)"):
   - Shows dominant role tags (count >= 2)
   - Priority order: Engage, Burst, Poke, Tank, Bruiser, Disengage

3. **Damage Distribution Bar**:
   - Visual bar showing AP% vs AD% split
   - Purple for AP, Orange for AD

**How It Works** (`app_teamcomp.go:138-323`):
1. Checks if all 10 players have locked champions
2. For each team, calls `analyzeTeamTags()`
3. Looks up each champion in local `champions.db` for damage type and role tags
4. Counts tags and calculates damage split
5. `determineArchetype()` uses tag counts to classify team style

---

### Meta Tab

**When Visible**: Always available

**Purpose**: Browse top champions by win rate for each role

**Components**:

#### 1. Role Tabs
- Top, Jungle, Mid, ADC, Support buttons
- Click to filter champions by role

#### 2. Tier List View
Table showing top 5 champions for selected role:
- **Rank**: 1-5, top 3 highlighted in gold
- **Champion**: Icon and name (clickable)
- **Pick Rate**: How often the champion is picked
- **Win Rate**: Overall win rate (always green for meta picks)

#### 3. Champion Details View (on click)
When you click a champion, shows:

1. **Banner with Splash Art**:
   - Champion name and role
   - Full item build (same format as Build tab)

2. **Counters Row**:
   - Shows up to 6 champions that counter the selected champion
   - Displays their win rate and game count

**How It Works**:
1. `GetMetaChampions()` called when Meta tab clicked
2. Calls `FetchAllRolesTopChampions(5)` - gets top 5 for all roles
3. On champion click, calls both:
   - `GetChampionDetails()` for matchup data
   - `GetChampionBuild()` for item builds
4. Data cached in `currentMetaData` to avoid refetching

---

## In-Game Mode

When a game starts (gameflow phase = "InProgress"), the app switches to in-game mode:
- Main overlay is hidden automatically
- In-game overlay shows with Build and Scouting tabs
- Press `Ctrl+O` to show/hide manually

### Build Tab (In-Game)

**Purpose**: Quick reference for your build during the game

**Data Displayed**:
- Champion icon, name, and role
- Core Items (3 items)
- 4th Item Options (multiple with win rates)
- 5th Item Options (multiple with win rates)
- 6th Item Options (multiple with win rates)

**How It Works** (`app_champselect.go:fetchAndEmitInGameBuild`):

1. **Primary**: Uses saved champ select data
   - During champ select, when user locks in, saves `lockedChampionID`, `lockedChampionName`, `lockedPosition`
   - On game start, uses this saved data directly (no re-fetching)

2. **Fallback**: Uses stored PUUID (if champ select data missing)
   - On LCU connection, user's PUUID is stored in `currentPUUID`
   - Finds player in game session by matching PUUID
   - Gets champion ID from game session
   - Infers role via `GetMostPlayedRole(championID)` - queries stats DB for most common position

3. Fetches build using `FetchChampionData()` with champion + role
4. Emits `ingame:build` event to frontend

**Data Flow**:
```
LCU Connect → Store currentPUUID
Champ Select Lock → Save lockedChampionID, lockedChampionName, lockedPosition
Game Start → Use saved data OR find via PUUID + infer role
           → Fetch build → Emit to frontend
Game End → Clear saved data
```

---

### Scouting Tab

**Purpose**: Shows recent performance stats for all players in the game

**Data Displayed**:

For each player (separated into Your Team / Enemy Team):

1. **Player Card**:
   - Champion icon
   - Summoner name (with "YOU" tag if you)
   - Champion name
   - Win rate badge (colored by performance)

2. **Stats**:
   - KDA with average breakdown (e.g., "2.54 (6.2/4.8/6.1)")
   - Game count

3. **Tilt Indicator** (conditional):
   - Fire emoji for tilted (3 losses in a row)
   - Star emoji for on fire (3 wins in a row)
   - Sweat emoji for warming up (2+ recent losses)

4. **Fun Fact** (conditional observations):
   - "Lost 3 in a row - probably tilted"
   - "60% WR smurf alert!"
   - "Dies 7.5 times per game on average"
   - "3.2 KDA - this one's dangerous"
   - "Recent int game: 0/8/2 on Yasuo"

**How It Works** (`app_champselect.go:383-551`):
1. `fetchAndEmitScouting()` called on game start
2. Gets all players via `lcuClient.GetGamePlayers()`
3. For each player, fetches last 20 matches via `GetMatchHistoryByPUUID()`
4. `calculatePlayerStats()` aggregates:
   - Filters to CLASSIC game mode only
   - Calculates averages, win rate, KDA
   - Tracks recent 3 game results for tilt detection
   - Tracks worst game (5+ deaths with lowest KDA)
5. `detectTiltAndFunFact()` generates observations based on stats

---

## Tab HUD Mode

**When Available**: During an active game only

**How to Activate**: Hold the Tab key

**Purpose**: Shows build reference overlay alongside the game scoreboard

**Components**:

### 1. Gold Box (Top Center)
- Large gold difference display (e.g., "+1,250g" or "-800g")
- Color-coded:
  - **Green**: Your team ahead
  - **Red**: Your team behind
  - **Gold**: Even (within small margin)

### 2. Build Box (Right Side)
- Champion name header
- **Core Items** (3 icons)
- **4th Item** options (up to 4 with win rates)
- **5th Item** options (up to 4 with win rates)
- **6th Item** options (up to 4 with win rates)

Win rates are color-coded:
- **Green**: >51% (winning)
- **Red**: <49% (losing)
- **Gold**: 49-51% (even)

**How It Works** (`hotkey_windows.go`):
1. Low-level keyboard hook monitors Tab key globally
2. On Tab press (`onTabPressed()`):
   - Saves current window position/size
   - Expands window to fullscreen (transparent)
   - Emits `goldbox:show` event to frontend
   - Starts polling gold data every 500ms
3. While held:
   - `emitGoldUpdate()` calls `GetGoldDiff()`
   - Live Client API provides player gold/items
   - Calculates team gold totals from item values
4. On Tab release (`onTabReleased()`):
   - Stops gold polling
   - Hides overlay
   - Restores original window size/position

**Gold Calculation** (`app_champselect.go:554-650`):
- Uses Live Client API (`liveClient.GetAllPlayers()`)
- For each player, sums gold value of all items
- Calculates team totals and difference
- Also tracks individual lane matchup gold differences

---

## Hotkeys

| Hotkey | Action | Context |
|--------|--------|---------|
| `Ctrl+O` | Toggle overlay visibility | Global (any time) |
| `Tab` (hold) | Show Tab HUD mode | In-game only |

**Implementation**: Uses Windows low-level keyboard hook (`WH_KEYBOARD_LL`) to capture keys globally, even when League is focused.

---

## App State

The app maintains state for tracking user identity and champ select data:

### User Identity (stored on LCU connection)
```go
currentPUUID string  // User's PUUID, fetched on connection
```

### Champ Select State (passed to in-game)
```go
lockedChampionID   int     // Champion ID when locked in
lockedChampionName string  // Champion name when locked in
lockedPosition     string  // Role/position when locked in
```

### State Lifecycle
1. **On LCU Connect**: `currentPUUID` is fetched and stored
2. **On Champion Lock**: `lockedChampionID`, `lockedChampionName`, `lockedPosition` are saved
3. **On Game Start**: Saved data is used for in-game build (or PUUID fallback)
4. **On Game End**: `locked*` fields are cleared for next game

---

## Data Sources

### Local SQLite Databases

Located at `{UserConfigDir}/GhostDraft/`:

1. **champions.db** - Static champion metadata
   - Damage types (AP, AD, Mixed, Tank)
   - Role tags (Engage, Burst, Poke, etc.)
   - Used for team comp analysis

2. **stats.db** - Match statistics (downloaded from remote)
   - `champion_stats` - Win rates by patch/position
   - `champion_items` - Overall item stats
   - `champion_item_slots` - Item stats by slot (1-6)
   - `champion_matchups` - Win rates between champions
   - Updated from remote manifest on startup

### Stats Provider Queries (`internal/data/stats_queries.go`)

| Function | Purpose |
|----------|---------|
| `FetchChampionData()` | Get item builds for champion+role |
| `FetchAllMatchups()` | Get all matchup win rates for a champion |
| `FetchCounterMatchups()` | Get champions that counter you (<49% WR) |
| `FetchCounterPicks()` | Get champions that beat an enemy (>51% WR) |
| `FetchAllRolesTopChampions()` | Get top 5 meta champions per role |
| `GetMostPlayedRole()` | Get most common role for a champion (by game count) |

### Remote APIs

1. **League Client API (LCU)**:
   - Champion select session data
   - Match history
   - Current game info
   - Player data

2. **Live Client API** (in-game):
   - All player data (items, gold, scores)
   - Game state

3. **Data Dragon**:
   - Champion icons and splash art
   - Item icons and gold values
   - Loaded on startup

### Stats Update Flow

```
Startup → Check STATS_MANIFEST_URL
        → Compare remote version with local
        → If newer: Download data.json
        → Bulk insert into SQLite
        → Stats provider queries local DB
```

---

## Event System

The app uses Wails' event system to communicate between Go backend and JavaScript frontend:

| Event | Direction | Description |
|-------|-----------|-------------|
| `lcu:status` | Go→JS | Connection status updates |
| `champselect:update` | Go→JS | Champion select state changes |
| `build:update` | Go→JS | Matchup win rate data |
| `bans:update` | Go→JS | Recommended bans list |
| `items:update` | Go→JS | Item build data |
| `counterpicks:update` | Go→JS | Counter pick suggestions |
| `teamcomp:update` | Go→JS | Team damage balance warning |
| `fullcomp:update` | Go→JS | Full team composition analysis |
| `gameflow:update` | Go→JS | Game phase changes |
| `ingame:build` | Go→JS | In-game build data |
| `ingame:scouting` | Go→JS | Player scouting data |
| `gold:update` | Go→JS | Gold difference (Tab HUD) |
| `goldbox:show` | Go→JS | Toggle Tab HUD visibility |
