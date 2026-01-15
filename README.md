# GhostDraft

A real-time League of Legends companion overlay that provides matchup statistics, item build recommendations, and team composition analysis during champion select.

## Download

**[Download for Windows](https://github.com/MatthewTran22/LoLOverlay/releases/latest/download/GhostDraft.zip)**

> Windows 10/11 required. Windows may show "Unknown Publisher" warning - click "More info" → "Run anyway" (the app isn't code-signed yet).

## Features

### During Champion Select
- **Matchup Win Rates** - See your win rate against the enemy laner
- **Counter Recommendations** - View your hardest counters for ban suggestions
- **Item Builds** - Core items (1st, 2nd, 3rd) with win rates, plus 4th/5th/6th item options
- **Team Composition Analysis** - Warnings when your team is too AD or AP heavy
- **Meta Tab** - Top 5 champions by win rate for each role

### In-Game
- **Tab HUD Overlay** - Hold Tab to see:
  - Team gold vs enemy gold (with +/- difference)
  - Your recommended item build
- **Automatic Detection** - Overlay appears during champ select, hides during game

### Hotkeys
- `Ctrl+O` - Toggle overlay visibility
- `Tab` (in-game) - Show gold/build HUD overlay

## Screenshots

The overlay displays:
- Your assigned role and champion
- Matchup win rate vs lane opponent (color-coded: green/red/orange)
- Recommended item builds with win rates
- Counter picks and team composition warnings

## Installation

### Option 1: Download Release (Recommended)
1. Download [GhostDraft.zip](https://github.com/MatthewTran22/LoLOverlay/releases/latest/download/GhostDraft.zip)
2. Extract and run `GhostDraft.exe`
3. Launch League of Legends

### Option 2: Build from Source

**Prerequisites:**
- [Go 1.21+](https://go.dev/dl/)
- [Node.js 18+](https://nodejs.org/)
- [Wails CLI](https://wails.io/docs/gettingstarted/installation)

```bash
# Clone the repo
git clone https://github.com/MatthewTran22/LoLOverlay.git
cd LoLOverlay

# Install frontend dependencies
cd frontend && npm install && cd ..

# Build the app
wails build
```

The executable will be in `build/bin/`.

### Development

```bash
wails dev
```

## Usage

1. Run GhostDraft
2. Launch League of Legends
3. GhostDraft automatically connects to the client
4. Enter champion select - the overlay appears with matchup data
5. During game, hold Tab to see gold difference and your build

## How It Works

- Connects to League Client via LCU API (reads lockfile)
- Listens for champion select events via WebSocket
- Queries local SQLite database for matchup/build statistics
- Uses Riot's Live Client API for in-game gold tracking
- Data sourced from aggregated Diamond+ match data

## Tech Stack

- **Backend**: Go + [Wails v2](https://wails.io/)
- **Frontend**: Vanilla JavaScript + CSS (Hextech Arcane theme)
- **Database**: SQLite (local stats cache)
- **APIs**: Riot LCU API, Live Client API, Data Dragon

## Companion Website

Browse champion statistics, builds, and matchups at the companion website:
- Champion tier lists by role
- Detailed build paths and item win rates
- Matchup data and counter picks

## Project Structure

```
├── app.go                 # Main app, LCU polling, startup/shutdown
├── app_champselect.go     # Champion select event handling
├── app_emitters.go        # Real-time event emitters
├── hotkey_windows.go      # Global hotkeys (Ctrl+O, Tab)
├── frontend/              # Wails frontend (HTML/CSS/JS)
├── internal/
│   ├── lcu/               # LCU client, WebSocket, Data Dragon
│   └── data/              # SQLite database, stats queries
├── data-analyzer/         # Match data collection pipeline
└── website/               # Next.js companion website
```

## Privacy

- **No account required** - Works without login or registration
- **No personal data collected** - We don't store summoner names or match history
- **Open source** - All code publicly available
- **Local processing** - Stats processed on your machine

## License

This project is open source software licensed under the [MIT License](LICENSE.md).

Copyright (c) 2026 M-Tran Software

## Contributing

Contributions welcome! Please open an issue or PR.

## Acknowledgments

- [Wails](https://wails.io/) - Go + Web frontend framework
- [Riot Games](https://developer.riotgames.com/) - LCU API and Data Dragon
- League of Legends community for feedback
