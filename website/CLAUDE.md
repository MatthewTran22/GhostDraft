# GhostDraft Website

Next.js companion website for browsing League of Legends champion statistics, builds, and matchup data.

## Tech Stack
- **Framework**: Next.js 15 with App Router
- **Styling**: Tailwind CSS 4 + Custom CSS (Hextech Arcane theme)
- **Database**: SQLite with better-sqlite3 (server-side only)
- **Fonts**: Cinzel (display), Rajdhani (body)

## Project Structure

```
website/
├── src/
│   ├── app/
│   │   ├── globals.css           # Hextech Arcane theme + Tailwind
│   │   ├── layout.tsx            # Root layout with fonts
│   │   ├── page.tsx              # Landing page
│   │   ├── privacy/page.tsx      # Privacy policy
│   │   ├── terms/page.tsx        # Terms of service
│   │   ├── stats/
│   │   │   ├── page.tsx          # Champion tier list by role
│   │   │   └── [championId]/
│   │   │       └── page.tsx      # Champion detail page
│   │   ├── admin/
│   │   │   └── page.tsx          # Admin panel for DB updates
│   │   └── api/
│   │       ├── stats/route.ts    # Stats info + update trigger
│   │       ├── meta/route.ts     # Top champions by role
│   │       ├── champions/[id]/route.ts  # Champion build data
│   │       └── matchups/[id]/route.ts   # Champion matchup data
│   ├── components/
│   │   ├── Header.tsx            # Navigation header
│   │   └── Footer.tsx            # Site footer
│   └── lib/
│       ├── db.ts                 # SQLite connection + remote sync
│       ├── stats.ts              # Stats query functions
│       └── champions.ts          # Champion ID→name mapping, utilities
├── data/
│   └── stats.db                  # SQLite database (auto-downloaded)
└── public/                       # Static assets
```

## Pages

### `/` - Landing Page
Hero section with download CTA, feature highlights, and links to stats.

### `/stats` - Champion Tier List
- Role tabs (Top, Jungle, Mid, ADC, Support)
- Full champion list sorted by win rate
- Tier badges (S+, S, A, B, C, D)
- Links to individual champion pages

### `/stats/[championId]` - Champion Detail
- Champion header with icon, tier, win rate, pick rate
- Role tabs for multi-role champions
- Recommended build: 2 core items + boots + situational options
- Counters section (worst matchups)
- Best matchups section

### `/admin` - Admin Panel
- Database status (patch, champion count, matchup count)
- Check for Updates button (smart update)
- Force Update button (re-download everything)

## Key Files

### `lib/db.ts`
- SQLite connection management
- Remote data sync from GitHub (manifest.json + data.json)
- Patch version tracking

### `lib/stats.ts`
Core query functions:
- `fetchChampionData()` - Build paths with item options
- `fetchAllMatchups()` - All matchup win rates
- `fetchCounterMatchups()` - Worst matchups (counters)
- `fetchBestMatchups()` - Best matchups
- `fetchAllChampionsByRole()` - Full tier list
- `fetchChampionStats()` - Single champion stats
- `fetchChampionRoles()` - Roles a champion plays

### `lib/champions.ts`
- Champion ID to name mapping
- Data Dragon icon URL generation
- Tier calculation (S+/S/A/B/C/D based on win rate + pick rate)
- Role display names

## Database Schema

Uses the same SQLite schema as the desktop app:

```sql
champion_stats        -- Win rates by champion/position
champion_items        -- Item stats (overall)
champion_item_slots   -- Item stats by build slot (1-6)
champion_matchups     -- Matchup win rates
data_version          -- Current patch version
```

## Build System

### Item Build Logic
Core build = 2 legendary items + best boots:
1. Query best boots across all slots
2. Get top item from slots 1-3 (excluding boots + duplicates)
3. Add boots to complete core
4. 4th/5th/6th options exclude core items

### Tier Calculation
```typescript
if (winRate >= 53 && pickRate >= 3) return "S+";
if (winRate >= 52 && pickRate >= 2) return "S";
if (winRate >= 51 && pickRate >= 1) return "A";
if (winRate >= 50) return "B";
if (winRate >= 48) return "C";
return "D";
```

## Data Flow

```
GitHub (LoLOverlay-Data repo)
  ├── manifest.json    # Patch version + data URL
  └── data.json        # All aggregated stats
         ↓
Website startup / Admin trigger
         ↓
Downloads if newer patch available
         ↓
Bulk inserts to local SQLite (data/stats.db)
         ↓
Server components query SQLite
         ↓
Rendered pages with revalidation
```

## Environment Variables

```
STATS_MANIFEST_URL=https://raw.githubusercontent.com/.../manifest.json
```

## Build Commands

```bash
npm run dev      # Development server
npm run build    # Production build
npm run start    # Start production server
```

## CSS Classes

### Theme Classes (globals.css)
- `.hex-card` - Card with gold border
- `.btn-hextech` - Gold gradient button
- `.btn-outline` - Outlined button
- `.text-glow` - Gold text shadow
- `.text-glow-cyan` - Cyan text shadow
- `.wr-high` / `.wr-mid` / `.wr-low` - Win rate colors
- `.hover-line` - Underline hover effect

### Tailwind Custom Colors
Use CSS variables: `text-[var(--hextech-gold)]`, `bg-[var(--abyss)]`, etc.
