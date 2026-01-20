# Continuous Collector Architecture Plan

A redesign of the data-analyzer pipeline to run continuously 24/7 until API key expiration, with efficient memory management and minimal downtime.

## Goals

- Single continuous runtime (no restart loops)
- Reduce every 10 warm file rotations (~10k matches)
- Minimal collector downtime during reduce
- Graceful handling of API key expiration
- Memory-safe for multi-day runs

---

## Architecture Overview

```
┌────────────────────────────────────────────────────────────────┐
│                     CONTINUOUS COLLECTOR                        │
├────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────────────┐ │
│  │   Spider    │───▶│  Rotator    │───▶│  Reduce Orchestrator│ │
│  │  (workers)  │    │  (hot/warm) │    │                     │ │
│  └─────────────┘    └─────────────┘    └─────────────────────┘ │
│        │                   │                     │              │
│        │                   │                     │              │
│        ▼                   ▼                     ▼              │
│   ┌─────────┐        ┌──────────┐         ┌───────────┐        │
│   │ API Key │        │ Rotation │         │  Reducer  │        │
│   │ Watcher │        │   Lock   │         │  + Turso  │        │
│   └─────────┘        └──────────┘         └───────────┘        │
│                                                                 │
└────────────────────────────────────────────────────────────────┘
```

---

## State Machine

```
                         ┌──────────────┐
                         │   STARTUP    │
                         │              │
                         │ Seed from    │
                         │ Challenger #1│
                         └──────┬───────┘
                                │
                                ▼
           ┌────────────────────────────────────┐
           │                                    │
       ┌──▶│           COLLECTING               │
       │   │                                    │
       │   │  - Spider fetches matches          │
       │   │  - Writes to hot/                  │
       │   │  - Rotates to warm/ (RLock)        │
       │   │  - Increments warm file counter    │
       │   │                                    │
       │   └──────────┬──────────┬──────────────┘
       │              │          │
       │    [10 warm files]  [401/403]
       │              │          │
       │              ▼          ▼
       │   ┌────────────────────────────────────┐
       │   │                                    │
       │   │           REDUCING                 │
       │   │                                    │
       │   │  1. Acquire rotation Lock          │
       │   │  2. Flush hot/ → warm/             │
       │   │  3. Aggregate warm/ → memory       │
       │   │  4. Archive warm/ → cold/ (gzip)   │
       │   │  5. Release Lock                   │
       │   │  6. Spawn Turso push (parallel)    │
       │   │  7. Reset warm file counter        │
       │   │                                    │
       │   └──────────┬──────────┬──────────────┘
       │              │          │
       │        [key valid]  [key expired]
       │              │          │
       └──────────────┘          ▼
                      ┌─────────────────────────┐
                      │                         │
                      │  WAITING FOR KEY        │
                      │                         │
                      │  - POST Discord webhook │
                      │  - Poll Discord channel │
                      │    every 5 min          │
                      │  - Turso push completes │
                      │                         │
                      └──────────┬──────────────┘
                                 │
                           [new key found]
                                 │
                                 ▼
                      ┌─────────────────────────┐
                      │                         │
                      │  FRESH RESTART          │
                      │                         │
                      │  - Clear all state      │
                      │  - Reset bloom filters  │
                      │  - Empty player queue   │
                      │  - Seed Challenger #1   │
                      │                         │
                      └──────────┬──────────────┘
                                 │
                                 └───────▶ STARTUP
```

**New key = New session.** All in-memory state is cleared and we start fresh from the top Challenger player.

---

## Reducer Workflow

```
REDUCER TRIGGERED (10 warm files or key expired)
         │
         ▼
┌─────────────────────────────────┐
│  1. ACQUIRE LOCK                │
│                                 │
│  2. AGGREGATE                   │  ← Lock held
│     - Read warm/*.jsonl         │
│     - Build stats in memory     │
│                                 │
│  3. ARCHIVE                     │
│     - Move warm/ → cold/        │
│     - Compress to .jsonl.gz     │
│                                 │
│  4. RELEASE LOCK                │
└────────────────┬────────────────┘
                 │
                 │  (lock released, collector resumes immediately)
                 │
    ┌────────────┴────────────┐
    │                         │
    ▼                         ▼
┌─────────┐           ┌──────────────┐
│COLLECTOR│           │ TURSO PUSH   │
│ resumes │           │              │
│         │           │ - Bulk insert│
│(parallel)           │ - Upserts    │
│         │           │              │
└─────────┘           └──────┬───────┘
                             │
                             ▼
                      ┌──────────────┐
                      │   COMPLETE   │
                      │              │
                      │ Reducer done │
                      │ until next   │
                      │ trigger      │
                      └──────────────┘
```

### Timeline View

```
TIME ──────────────────────────────────────────────────────────────────►

COLLECTOR   ████████████████░░░░████████████████████████████████████████
                            │   │
                         [lock] [unlock]
                            │   │
REDUCER                     █████
                            │   │
                            │   └─► archive done, lock released
                            │
TURSO PUSH                  ░░░░░████████████████
                                 │              │
                                 │         [push complete]
                                 │              │
                            [runs parallel     [reducer runtime ends]
                             with collector]
```

### Lock Duration

| Step | Duration | Lock Held? |
|------|----------|------------|
| Aggregate 10 files | ~5-10 sec | Yes |
| Compress & archive | ~5-10 sec | Yes |
| Turso push | ~2-3 min | No |

**Total lock time: ~10-20 seconds** - Collector downtime is minimal.

---

## Trigger Conditions

| Trigger | Action |
|---------|--------|
| 10 warm file rotations | Reduce → resume collecting |
| 401/403 from Riot API | Reduce → wait for new key |
| SIGTERM/SIGINT | Reduce → exit cleanly |

---

## Memory Management

### What to Keep After Reduce (Same Session)

| Resource | After Reduce | Reasoning |
|----------|--------------|-----------|
| Player queue | **Keep** | Continue crawling from where we left off |
| Match bloom filter | **Keep** | Don't re-fetch same matches |
| PUUID bloom filter | **Keep** | Don't re-process same players |
| Warm file counter | Reset to 0 | Fresh count for next cycle |
| Aggregated stats | Clear | After Turso push completes |

### What to Clear on New Key (New Session)

| Resource | On New Key | Reasoning |
|----------|------------|-----------|
| Player queue | **Clear** | Start fresh from Challenger #1 |
| Match bloom filter | **Clear** | New session, OK to re-encounter matches |
| PUUID bloom filter | **Clear** | New session, OK to re-process players |
| Warm file counter | Reset to 0 | Fresh count for new session |
| Aggregated stats | Already cleared | Pushed to Turso before key expired |

**Rationale:** Each API key represents a ~24h collection window. Starting fresh ensures:
- No stale player queue (meta may have shifted)
- Clean bloom filters (no false positive buildup)
- Always seeding from current top Challenger
- Simpler state management (no cross-session persistence)

### Bloom Filter Lifecycle (Within Session)

Reset bloom filters periodically to prevent unbounded false positive growth:

```
Reduce #1:  Keep filters
Reduce #2:  Keep filters
Reduce #3:  Keep filters
Reduce #4:  Keep filters
Reduce #5:  Reset filters → fresh start (~50k matches)
```

On key expiration, filters are cleared anyway (new session).

---

## Race Condition Handling

### 1. Rotation vs Reducer Reading Warm

**Problem:** Collector rotates hot → warm while reducer is reading warm files.

**Solution:** RWMutex
```
Collector rotation: RLock()  ─┬─ Multiple rotations can happen
Collector rotation: RLock()  ─┘  simultaneously

Reducer processing:  Lock()  ─── Exclusive, blocks all rotations
```

### 2. Double Reduce Trigger

**Problem:** 10th file rotates AND key expires at same moment → two reduce calls.

**Solution:** Atomic state flag
```
IDLE ──► REDUCING ──► PUSHING ──► IDLE
           │
           └── if already REDUCING or PUSHING, ignore trigger
```

Only one reducer runs at a time. Second trigger is dropped.

### 3. Turso Push Still Running When Next Reduce Triggers

**Problem:** Collector hits 10 more files while previous Turso push is still going.

**Solution:** Queue pushes sequentially
```
REDUCE #1:  [aggregate+archive] ──► [turso push #1]
REDUCE #2:        [aggregate+archive] ──► [wait] ──► [turso push #2]
                                              │
                                    (waits for push #1)
```

- Reducer can still free up warm files quickly
- Turso pushes are sequential (no concurrent writes)
- Collector never blocked waiting for Turso

### 4. Warm File Counter Race

**Problem:** Collector increments counter while reducer resets it.

**Solution:** Atomic int64
```
Collector: atomic.AddInt64(&warmFileCount, 1)
Reducer:   atomic.StoreInt64(&warmFileCount, 0)
```

### 5. Hot File Mid-Write During Flush

**Problem:** Reducer flushes hot → warm while collector is mid-write to hot file.

**Solution:** Flush through the rotator's internal synchronization
```
Reducer calls: rotator.FlushAndRotate()
                    │
                    ├── Acquires rotator's internal mutex
                    ├── Finishes current write
                    ├── Closes hot file
                    ├── Moves to warm/
                    └── Opens new hot file
```

### 6. Key Expires Mid-Reduce

**Problem:** Key expires, triggers reduce, but reduce is already running.

**Solution:** Flag and check after completion
```
if keyExpired && state == IDLE:
    trigger reduce
elif keyExpired && state == REDUCING:
    set pendingKeyExpiry = true
    after reduce: enter WAITING_FOR_KEY state
```

---

## Synchronization Summary

| Resource | Protection |
|----------|------------|
| Warm directory | RWMutex |
| Reducer state | Atomic state enum (IDLE/REDUCING/PUSHING) |
| Warm file counter | Atomic int64 |
| Hot file writes | Rotator internal mutex |
| Turso push queue | Buffered channel (size 1) |
| Key expired flag | Atomic bool |

### Shared State Structure

```go
type ContinuousCollector struct {
    // Synchronization
    warmLock        sync.RWMutex    // warm directory access
    reducerState    atomic.Int32    // IDLE/REDUCING/PUSHING
    warmFileCount   atomic.Int64    // rotation counter
    keyExpired      atomic.Bool     // API key status
    pushQueue       chan AggData    // capacity 1, sequential pushes
}
```

---

## Lock Behavior Matrix

| Actor | Operation | Lock Type | Blocks? |
|-------|-----------|-----------|---------|
| Spider workers | Write to hot/ | None | Never |
| Rotator | Rotate hot/ → warm/ | RLock | Only during reduce |
| Reducer | Process warm/ | Lock (exclusive) | Blocks rotation only |
| Turso push | Push to database | None | Never |

### Lock Purpose: Preventing Hot→Warm Rotation During Reduce

The `warmLock` RWMutex has **one specific purpose**: prevent new files from appearing in `warm/` while the reducer is processing it.

```
WITHOUT LOCK (race condition):
  Reducer reads warm/file1.jsonl, warm/file2.jsonl
  Rotator moves hot/current.jsonl → warm/file3.jsonl  ← appears mid-reduce!
  Reducer finishes, archives file1 + file2
  file3 is LOST (sits in warm/ until next reduce, or worse, gets double-counted)

WITH LOCK (safe):
  Reducer acquires Lock()
  Reducer reads warm/file1.jsonl, warm/file2.jsonl
  Rotator tries RLock() → BLOCKED
  Reducer archives, releases Lock()
  Rotator acquires RLock(), moves hot/current.jsonl → warm/file3.jsonl
  file3 safely waits for next reduce cycle
```

**What the lock does NOT protect:**
- Spider writes to hot/ (always allowed, hot file has its own internal mutex)
- Turso pushes (run in parallel after lock released)
- Bloom filter access (separate atomic/mutex protection)

This narrow scope keeps lock contention minimal (~10-20 sec per reduce cycle).

---

## API Key Management

### Key Expiration → Discord Notification Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        KEY EXPIRATION FLOW                               │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  401/403 from Riot API                                                   │
│         │                                                                │
│         ▼                                                                │
│  ┌──────────────┐                                                        │
│  │ Trigger      │                                                        │
│  │ Final Reduce │                                                        │
│  └──────┬───────┘                                                        │
│         │                                                                │
│         ▼                                                                │
│  ┌──────────────────────────────────────┐                                │
│  │ POST to Discord Webhook              │                                │
│  │                                      │                                │
│  │ "🔑 API Key Expired!                 │                                │
│  │  Collected: 47,832 matches           │                                │
│  │  Runtime: 18h 32m                    │                                │
│  │                                      │                                │
│  │  Reply with new key to continue"     │                                │
│  └──────────────────┬───────────────────┘                                │
│                     │                                                    │
│                     ▼                                                    │
│  ┌──────────────────────────────────────┐      ┌───────────────────────┐ │
│  │ WAITING_FOR_KEY                      │      │ Private Discord       │ │
│  │                                      │◄────▶│                       │ │
│  │ Poll Discord channel every 5 min    │      │ You post: RGAPI-xxx   │ │
│  │ for messages after webhook timestamp │      │                       │ │
│  └──────────────────┬───────────────────┘      └───────────────────────┘ │
│                     │                                                    │
│              [new key found]                                             │
│                     │                                                    │
│                     ▼                                                    │
│  ┌──────────────────────────────────────┐                                │
│  │ Validate Key                         │                                │
│  │                                      │                                │
│  │ Test call to Riot API                │                                │
│  │ If valid → COLLECTING                │                                │
│  │ If invalid → keep waiting            │                                │
│  └──────────────────────────────────────┘                                │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### Discord Integration Details

**Webhook (Outbound Notification)**
```go
type KeyExpiredPayload struct {
    Content string `json:"content"`
    Embeds  []Embed `json:"embeds"`
}

// POST to DISCORD_WEBHOOK_URL
{
    "content": "@here API Key Expired!",
    "embeds": [{
        "title": "🔑 Riot API Key Expired",
        "color": 15158332,  // red
        "fields": [
            {"name": "Matches Collected", "value": "47,832", "inline": true},
            {"name": "Runtime", "value": "18h 32m", "inline": true},
            {"name": "Last Reduce", "value": "2 min ago", "inline": true}
        ],
        "footer": {"text": "Reply with new RGAPI-xxx key to start fresh session"}
    }]
}

// Success message after new key validated
{
    "embeds": [{
        "title": "✅ New Session Started",
        "color": 5763719,  // green
        "fields": [
            {"name": "New Key", "value": "RGAPI-...xxxx (validated)", "inline": true},
            {"name": "Seed Player", "value": "Challenger #1", "inline": true}
        ],
        "footer": {"text": "Fresh crawl beginning from top of ladder"}
    }]
}
```

**Bot (Inbound Key Listener)**

Option A: **Discord Bot** (recommended)
- Bot listens in private channel for messages starting with `RGAPI-`
- Validates key format before attempting use
- Responds with confirmation or error

Option B: **Poll via Discord API**
- Use bot token to GET `/channels/{id}/messages?after={last_message_id}`
- Parse messages for `RGAPI-` pattern
- Less responsive but simpler (no websocket)

### Environment Variables

```bash
# Discord webhook for notifications (outbound)
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/xxx/yyy

# Discord bot for key input (inbound)
DISCORD_BOT_TOKEN=your-bot-token
DISCORD_CHANNEL_ID=123456789  # private channel ID
```

### Key Refresh Flow

1. Detect key expiry (401/403 from Riot API)
2. Trigger final reduce (flush all data to Turso)
3. POST notification to Discord webhook
4. Enter WAITING_FOR_KEY state
5. Poll Discord channel every 5 minutes for new messages
6. When `RGAPI-xxx` message found:
   - Validate with test API call (`/lol/status/v4/platform-data`)
   - If invalid: reply with error, keep waiting
   - If valid: proceed to step 7
7. **Fresh restart** (new key = new session):
   - Clear player queue
   - Clear match bloom filter
   - Clear PUUID bloom filter
   - Reset warm file counter
   - Fetch Challenger ladder
   - Seed queue with #1 Challenger player
8. Post success message to Discord with new session info
9. Transition to COLLECTING (fresh crawl begins)

---

## Deployment

### Docker Image

Build once, pull anywhere:
```bash
# Build and push
docker build -t ghcr.io/USERNAME/ghostdraft-collector:latest .
docker push ghcr.io/USERNAME/ghostdraft-collector:latest

# Run on any machine
docker run -d \
  --name collector \
  --restart unless-stopped \
  -e RIOT_API_KEY=your-key \
  -e TURSO_DATABASE_URL=libsql://your-db.turso.io \
  -e TURSO_AUTH_TOKEN=your-token \
  -v ~/collector-data:/app/data \
  ghcr.io/USERNAME/ghostdraft-collector:latest
```

### Recommended Hardware

| Device | Monthly Electricity | Notes |
|--------|---------------------|-------|
| MacBook Pro (2019) | ~$1.50 | Good option, use AlDente for battery |
| Raspberry Pi 5 | ~$0.50 | Cheapest, plenty capable |
| Used Mini PC | ~$3.50 | Best performance for price |

---

## Expected Usage

### Turso (Free Tier: 500M writes/month)

| Metric | Per Reduce | Per Day (~3) | Per Month |
|--------|------------|--------------|-----------|
| Rows written | ~330k | ~1M | ~30M |
| **Usage** | | | **~6%** |

### Network

| Destination | Monthly Egress |
|-------------|----------------|
| Riot API | ~750MB |
| Turso | ~1.5GB |
| **Total** | ~2.25GB |

---

## Files to Modify

- `cmd/pipeline/main.go` → Add continuous mode entry point
- `internal/collector/spider.go` → Add lock awareness for rotation
- `internal/collector/continuous.go` → New orchestrator (state machine)
- `internal/collector/reducer.go` → Refactor for async Turso push
- `internal/storage/rotator.go` → Add FlushAndRotate method

---

## Implementation Plan (TDD)

### Feature 1: Rotator Enhancement ✅

**Goal:** Add `FlushAndRotate()` method to safely flush hot file to warm directory.

#### Unit Tests

- [x] **1.1 Unit: FlushAndRotate basic behavior**
  - Write test that creates rotator, writes data, calls FlushAndRotate
  - Assert hot file is closed and moved to warm/
  - Assert new hot file is created
  - File: `internal/storage/rotator_test.go`

- [x] **1.2 Unit: FlushAndRotate with empty hot file**
  - Write test that calls FlushAndRotate when hot file has no data
  - Assert no empty file created in warm/
  - File: `internal/storage/rotator_test.go`

- [x] **1.3 Unit: Concurrent writes during FlushAndRotate**
  - Write test with goroutine writing while another calls FlushAndRotate
  - Assert no data loss, no panic
  - File: `internal/storage/rotator_test.go`

#### Integration Tests

- [x] **1.4 Integration: Rotator with real filesystem**
  - Create temp directory structure (hot/, warm/, cold/)
  - Write 1000+ records, trigger multiple rotations
  - Verify all data preserved across rotations
  - Verify file naming conventions and timestamps
  - File: `internal/storage/rotator_integration_test.go`

- [x] **1.5 Integration: Rotator recovery after crash**
  - Simulate crash mid-rotation (partial file in warm/)
  - Restart rotator, verify it handles orphaned files
  - File: `internal/storage/rotator_integration_test.go`

#### Implementation

- [x] **1.6 Implement FlushAndRotate**
  - Add method to `internal/storage/rotator.go`
  - Use internal mutex to synchronize with writes
  - File: `internal/storage/rotator.go`

#### Additional Changes
- Fixed `CompressToCold` to close files before deleting (Windows compatibility)

---

### Feature 2: Warm Directory Lock ✅

**Goal:** RWMutex to prevent hot→warm rotation while reducer processes warm files.

#### Unit Tests

- [x] **2.1 Unit: RLock allows concurrent rotations**
  - Write test with multiple goroutines acquiring RLock
  - Assert all succeed without blocking
  - File: `internal/collector/warmlock_test.go`

- [x] **2.2 Unit: Lock blocks RLock**
  - Write test where one goroutine holds Lock
  - Assert other goroutines block on RLock
  - File: `internal/collector/warmlock_test.go`

- [x] **2.3 Unit: RLock blocks Lock**
  - Write test where goroutine holds RLock
  - Assert Lock waits until RLock released
  - File: `internal/collector/warmlock_test.go`

#### Integration Tests

- [x] **2.4 Integration: Lock with Rotator under load**
  - Spin up 10 goroutines continuously rotating files
  - Periodically acquire exclusive Lock (simulating reducer)
  - Assert no files lost, no race conditions
  - Run with `-race` flag
  - File: `internal/collector/warmlock_integration_test.go`

- [x] **2.5 Integration: Lock contention metrics**
  - Simulate high contention scenario
  - Verify metrics captured (wait time, hold time)
  - File: `internal/collector/warmlock_integration_test.go`

#### Implementation

- [x] **2.6 Implement WarmLock wrapper**
  - Create thin wrapper around sync.RWMutex with logging
  - Add metrics for lock contention time
  - File: `internal/collector/warmlock.go`

#### Additional Tests
- Added `TestWarmLock_Integration_NoDeadlock` stress test (50 readers, 5 writers, no deadlock)
- Added metrics tracking tests (`TestWarmLock_MetricsTracking`, `TestWarmLock_ReadMetricsTracking`, `TestWarmLock_WaitTimeMetrics`)

---

### Feature 3: Reducer Refactor ✅

**Goal:** Refactor reducer to aggregate in-memory, archive to cold, and push to Turso asynchronously.

#### Unit Tests

- [x] **3.1 Unit: Aggregate warm files to memory**
  - Write test with sample JSONL files in warm/
  - Call aggregator, assert correct stats in memory
  - File: `internal/collector/reducer_test.go`

- [x] **3.2 Unit: Archive warm to cold with gzip**
  - Write test that archives warm/*.jsonl
  - Assert files moved to cold/*.jsonl.gz
  - Assert warm/ is empty after
  - File: `internal/collector/reducer_test.go`

- [x] **3.3 Unit: Turso push channel**
  - Write test that sends AggData to push channel
  - Assert channel receives data
  - Mock Turso client to verify upserts called
  - File: `internal/collector/turso_pusher_test.go`

- [x] **3.4 Unit: Sequential push queue**
  - Write test sending two AggData while first is "processing"
  - Assert second waits for first to complete
  - File: `internal/collector/turso_pusher_test.go`

#### Integration Tests

- [x] **3.5 Integration: Full reduce cycle with real files**
  - Create realistic warm/ directory with 10 JSONL files (~1k matches each)
  - Run full reduce: aggregate → archive → verify cold/ contents
  - Decompress cold/ files, verify data integrity
  - File: `internal/collector/reducer_integration_test.go`

- [x] **3.6 Integration: Reducer with WarmLock**
  - Simulate collector writing to warm/ while reducer runs
  - Verify lock prevents race condition
  - Verify new files not processed until next cycle
  - File: `internal/collector/reducer_integration_test.go`

- [x] **3.7 Integration: Turso push with in-memory SQLite**
  - Use in-memory SQLite (libsql compatible)
  - Push aggregated data, verify rows inserted
  - Test upsert behavior (run twice with overlapping data)
  - File: `internal/collector/reducer_integration_test.go`

- [x] **3.8 Integration: Async push doesn't block reducer**
  - Start reduce cycle, measure time to release lock
  - Verify Turso push runs in background
  - Verify collector can resume while push ongoing
  - File: `internal/collector/reducer_integration_test.go`

#### Implementation

- [x] **3.9 Implement in-memory aggregator**
  - Refactor existing reducer to build stats in memory
  - Return AggData struct instead of writing files
  - File: `internal/collector/reducer.go`

- [x] **3.10 Implement archive function**
  - Move warm/*.jsonl → cold/*.jsonl.gz
  - Use gzip compression
  - File: `internal/collector/reducer.go`

- [x] **3.11 Implement async Turso pusher**
  - Goroutine that reads from channel and pushes to Turso
  - Sequential processing (one at a time)
  - File: `internal/collector/turso_pusher.go`

#### Additional Tests
- Added `TestFullPipeline_Integration` - end-to-end test of aggregate → archive → async push

---

### Feature 4: State Machine ✅

**Goal:** Implement state transitions: STARTUP → COLLECTING → REDUCING → WAITING_FOR_KEY → FRESH_RESTART → STARTUP

#### Unit Tests

- [x] **4.1 Unit: State enum and transitions**
  - Write test for valid state transitions
  - Assert invalid transitions return error
  - File: `internal/collector/state_test.go`

- [x] **4.2 Unit: Atomic state changes**
  - Write test with concurrent state change attempts
  - Assert only one succeeds (no double-reduce)
  - File: `internal/collector/state_test.go`

- [x] **4.3 Unit: COLLECTING → REDUCING on warm file count**
  - Write test that increments warm file counter to 10
  - Assert state transitions to REDUCING
  - File: `internal/collector/continuous_test.go`

- [x] **4.4 Unit: COLLECTING → REDUCING on 401/403**
  - Write test that simulates API error
  - Assert state transitions to REDUCING, then WAITING_FOR_KEY
  - File: `internal/collector/continuous_test.go`

- [x] **4.5 Unit: WAITING_FOR_KEY → FRESH_RESTART on new key**
  - Write test that provides new valid key
  - Assert all state cleared (bloom filters, queue)
  - Assert seeds from Challenger #1
  - File: `internal/collector/continuous_test.go`

#### Integration Tests

- [x] **4.6 Integration: Full state cycle with mocked externals**
  - Mock Riot API, Discord, Turso
  - Run through complete cycle: STARTUP → COLLECTING → REDUCING → back to COLLECTING
  - Verify all components called in correct order
  - File: `internal/collector/continuous_integration_test.go`

- [x] **4.7 Integration: Key expiration cycle**
  - Mock Riot API to return 401 after N requests
  - Mock Discord to return new key after polling
  - Verify full cycle: COLLECTING → REDUCING → WAITING → FRESH_RESTART → COLLECTING
  - Verify state properly cleared on fresh restart
  - File: `internal/collector/continuous_integration_test.go`

- [x] **4.8 Integration: Double-trigger prevention**
  - Trigger reduce from warm file count AND simulated 401 simultaneously
  - Assert only one reduce runs
  - Assert state machine handles gracefully
  - File: `internal/collector/continuous_integration_test.go`

- [x] **4.9 Integration: State persistence across components**
  - Verify spider respects state (stops on REDUCING)
  - Verify reducer only runs in REDUCING state
  - Verify key watcher only active in WAITING_FOR_KEY
  - File: `internal/collector/continuous_integration_test.go`

#### Implementation

- [x] **4.10 Implement state machine**
  - Create State type with atomic operations
  - Implement transition validation
  - File: `internal/collector/state.go`

- [x] **4.11 Implement ContinuousCollector orchestrator**
  - Main loop that handles state transitions
  - Coordinates spider, reducer, key watcher
  - File: `internal/collector/continuous.go`

#### Additional Tests
- Added `TestStateMachine_WaitForState` and `TestStateMachine_WaitForState_Timeout` for blocking wait functionality
- Added `TestContinuousCollector_StateTransitionCallbacksInOrder` for verifying transition callback order
- Added `TestContinuousCollector_KeyExpirationFullCycle` for full key expiration flow
- Added `TestContinuousCollector_Integration_MultipleReduceCycles` for stress testing multiple reduce cycles
- Added `TestContinuousCollector_Integration_ReducerWithWarmLock` for lock coordination testing

---

### Feature 5: Discord Integration

**Goal:** Webhook notifications on key expiry, poll Discord for new key input.

#### Unit Tests

- [ ] **5.1 Unit: Webhook payload format**
  - Write test that builds KeyExpiredPayload
  - Assert JSON matches expected Discord embed format
  - File: `internal/discord/webhook_test.go`

- [ ] **5.2 Unit: Webhook HTTP call**
  - Write test with httptest server
  - Call SendKeyExpiredNotification
  - Assert correct HTTP method, headers, body
  - File: `internal/discord/webhook_test.go`

- [ ] **5.3 Unit: Parse RGAPI key from message**
  - Write test with sample Discord messages
  - Assert correctly extracts RGAPI-xxx pattern
  - Assert ignores messages without pattern
  - File: `internal/discord/keyfinder_test.go`

- [ ] **5.4 Unit: Poll channel for messages**
  - Write test with mock Discord API responses
  - Assert fetches messages after timestamp
  - Assert finds key in messages
  - File: `internal/discord/keyfinder_test.go`

- [ ] **5.5 Unit: Success notification**
  - Write test that builds NewSessionPayload
  - Assert JSON matches expected format
  - File: `internal/discord/webhook_test.go`

#### Integration Tests

- [ ] **5.6 Integration: Real Discord webhook (manual/CI skip)**
  - POST to real test webhook URL
  - Verify message appears in Discord
  - Mark as `// +build integration` to skip in CI
  - File: `internal/discord/webhook_integration_test.go`

- [ ] **5.7 Integration: Real Discord API polling (manual/CI skip)**
  - Poll real test channel for messages
  - Post a test key, verify finder detects it
  - Mark as `// +build integration` to skip in CI
  - File: `internal/discord/keyfinder_integration_test.go`

- [ ] **5.8 Integration: Discord rate limiting**
  - Simulate rapid polling (exceed Discord rate limits)
  - Verify client backs off appropriately
  - Verify no crashes or data loss
  - File: `internal/discord/keyfinder_integration_test.go`

- [ ] **5.9 Integration: End-to-end notification flow**
  - Trigger key expiration in test harness
  - Verify webhook sent
  - Simulate key reply in channel
  - Verify key finder detects and returns it
  - File: `internal/discord/discord_integration_test.go`

#### Implementation

- [ ] **5.10 Implement Discord webhook client**
  - POST embed payloads to webhook URL
  - KeyExpiredNotification, NewSessionNotification
  - File: `internal/discord/webhook.go`

- [ ] **5.11 Implement Discord key finder**
  - Poll channel via Discord API
  - Parse messages for RGAPI-xxx
  - Return first valid key found
  - File: `internal/discord/keyfinder.go`

---

### Feature 6: API Key Validation

**Goal:** Validate new API keys before using them.

#### Unit Tests

- [ ] **6.1 Unit: Valid key passes validation**
  - Write test with mock Riot API returning 200
  - Assert ValidateKey returns true
  - File: `internal/riot/keyvalidator_test.go`

- [ ] **6.2 Unit: Invalid key fails validation**
  - Write test with mock Riot API returning 403
  - Assert ValidateKey returns false
  - File: `internal/riot/keyvalidator_test.go`

- [ ] **6.3 Unit: Network error during validation**
  - Write test with mock that times out
  - Assert ValidateKey returns error, not false
  - File: `internal/riot/keyvalidator_test.go`

#### Integration Tests

- [ ] **6.4 Integration: Real Riot API validation (manual/CI skip)**
  - Use real (expired) API key
  - Verify returns invalid (not error)
  - Mark as `// +build integration`
  - File: `internal/riot/keyvalidator_integration_test.go`

- [ ] **6.5 Integration: Key validation with hot-swap**
  - Start collector with valid key
  - Simulate key expiration (mock 401)
  - Provide new key, verify validation runs
  - Verify collector resumes with new key
  - File: `internal/riot/keyvalidator_integration_test.go`

#### Implementation

- [ ] **6.6 Implement key validator**
  - Call `/lol/status/v4/platform-data` endpoint
  - Return valid/invalid/error
  - File: `internal/riot/keyvalidator.go`

---

### Feature 7: Fresh Restart

**Goal:** Clear all state and reseed from Challenger #1 on new key.

#### Unit Tests

- [ ] **7.1 Unit: Clear bloom filters**
  - Write test that populates bloom filters
  - Call FreshRestart
  - Assert filters are empty (test with known values)
  - File: `internal/collector/continuous_test.go`

- [ ] **7.2 Unit: Clear player queue**
  - Write test that populates player queue
  - Call FreshRestart
  - Assert queue is empty
  - File: `internal/collector/continuous_test.go`

- [ ] **7.3 Unit: Seed from Challenger**
  - Write test with mock Riot API returning Challenger ladder
  - Call FreshRestart
  - Assert queue contains #1 Challenger PUUID
  - File: `internal/collector/continuous_test.go`

#### Integration Tests

- [ ] **7.4 Integration: Full fresh restart cycle**
  - Populate collector with 10k matches worth of state
  - Trigger fresh restart
  - Verify all bloom filters cleared
  - Verify player queue cleared
  - Verify new Challenger seed fetched
  - File: `internal/collector/freshrestart_integration_test.go`

- [ ] **7.5 Integration: Fresh restart with real Riot API (manual/CI skip)**
  - Call FreshRestart with real API
  - Verify Challenger #1 PUUID is seeded
  - Verify PUUID is real (not empty/invalid)
  - Mark as `// +build integration`
  - File: `internal/collector/freshrestart_integration_test.go`

- [ ] **7.6 Integration: Fresh restart memory cleanup**
  - Monitor memory before/after fresh restart
  - Verify bloom filters actually freed (not just reset)
  - Verify no memory leaks across multiple restarts
  - File: `internal/collector/freshrestart_integration_test.go`

#### Implementation

- [ ] **7.7 Implement FreshRestart**
  - Clear matchBloom, puuidBloom
  - Clear playerQueue
  - Fetch Challenger ladder, seed #1
  - File: `internal/collector/continuous.go`

---

### Feature 8: Warm File Counter ✅

**Goal:** Atomic counter that triggers reduce at 10 files.

#### Unit Tests

- [x] **8.1 Unit: Increment counter atomically**
  - Write test with concurrent increments
  - Assert final count is correct
  - File: `internal/collector/warmcounter_test.go`

- [x] **8.2 Unit: Trigger callback at threshold**
  - Write test that increments to 10
  - Assert callback invoked exactly once
  - File: `internal/collector/warmcounter_test.go`

- [x] **8.3 Unit: Reset counter**
  - Write test that increments, resets, increments again
  - Assert counter starts from 0 after reset
  - File: `internal/collector/warmcounter_test.go`

#### Integration Tests

- [x] **8.4 Integration: Counter with real rotator**
  - Connect counter to rotator rotation events
  - Rotate 10 files, verify callback fired
  - Verify callback fired exactly once (not on each rotation)
  - File: `internal/collector/warmcounter_integration_test.go`

- [x] **8.5 Integration: Counter under high concurrency**
  - Spin up 100 goroutines incrementing counter
  - Verify final count accurate
  - Verify threshold callback fired correct number of times
  - Run with `-race` flag
  - File: `internal/collector/warmcounter_integration_test.go`

- [x] **8.6 Integration: Counter reset during active rotation**
  - Increment to 9, start reset, increment 10th concurrently
  - Verify no missed triggers or double-triggers
  - File: `internal/collector/warmcounter_integration_test.go`

#### Implementation

- [x] **8.7 Implement WarmFileCounter**
  - Atomic int64 with increment, reset, onThreshold callback
  - File: `internal/collector/warmcounter.go`

#### Additional Tests
- Added `TestWarmFileCounter_CallbackAtExactThreshold` - verifies callback fires at exact threshold, not before
- Added `TestWarmFileCounter_ResetAllowsNewCallback` - verifies reset enables callback for next cycle
- Added `TestWarmFileCounter_NilCallback` - verifies nil callback doesn't panic
- Added `TestWarmFileCounter_ThresholdOne` - verifies threshold of 1 works
- Added `TestWarmFileCounter_Integration_MultipleResetCycles` - stress test with multiple reset cycles
- Added `TestWarmFileCounter_Integration_StressConcurrentResets` - concurrent reset/increment stress test
- Added `TestWarmFileCounter_Integration_RapidFireIncrements` - rapid fire single-goroutine test

---

### Feature 9: Graceful Shutdown

**Goal:** Handle SIGTERM/SIGINT by reducing and exiting cleanly.

#### Unit Tests

- [ ] **9.1 Unit: Signal triggers reduce**
  - Write test that sends SIGTERM to process
  - Assert reducer is called
  - Assert process exits after reduce completes
  - File: `internal/collector/shutdown_test.go`

- [ ] **9.2 Unit: Wait for Turso push before exit**
  - Write test with pending Turso push
  - Send SIGTERM
  - Assert waits for push to complete
  - File: `internal/collector/shutdown_test.go`

#### Integration Tests

- [ ] **9.3 Integration: Full shutdown sequence**
  - Start collector with real components (mocked APIs)
  - Accumulate some data in hot/ and warm/
  - Send SIGTERM
  - Verify reduce triggered
  - Verify all data flushed to cold/
  - Verify Turso push completes
  - Verify clean exit (exit code 0)
  - File: `internal/collector/shutdown_integration_test.go`

- [ ] **9.4 Integration: Shutdown during active reduce**
  - Start reduce cycle
  - Send SIGTERM mid-reduce
  - Verify reduce completes (not interrupted)
  - Verify no double-reduce
  - File: `internal/collector/shutdown_integration_test.go`

- [ ] **9.5 Integration: Shutdown timeout**
  - Mock slow Turso push (30+ seconds)
  - Send SIGTERM
  - Verify shutdown waits reasonable time
  - Verify forced exit after timeout (configurable)
  - File: `internal/collector/shutdown_integration_test.go`

- [ ] **9.6 Integration: Multiple signals**
  - Send SIGTERM, then SIGINT quickly after
  - Verify graceful handling (not crash)
  - Verify only one shutdown sequence runs
  - File: `internal/collector/shutdown_integration_test.go`

#### Implementation

- [ ] **9.7 Implement signal handler**
  - Listen for SIGTERM, SIGINT
  - Trigger final reduce
  - Wait for Turso push
  - Exit cleanly
  - File: `internal/collector/shutdown.go`

---

### Feature 10: Entry Point

**Goal:** Add `--continuous` flag to pipeline command.

#### Unit Tests

- [ ] **10.1 Unit: Parse continuous flag**
  - Write test for CLI flag parsing
  - Assert continuous mode detected
  - File: `cmd/pipeline/main_test.go`

- [ ] **10.2 Unit: Parse all environment variables**
  - Write test for DISCORD_WEBHOOK_URL, DISCORD_BOT_TOKEN, etc.
  - Assert missing required vars return clear error
  - File: `cmd/pipeline/main_test.go`

#### Integration Tests

- [ ] **10.3 Integration: Full startup sequence**
  - Run `go run cmd/pipeline/main.go --continuous` with mocked deps
  - Verify ContinuousCollector instantiated
  - Verify state machine starts in STARTUP
  - Verify transitions to COLLECTING
  - File: `cmd/pipeline/main_integration_test.go`

- [ ] **10.4 Integration: Missing env vars**
  - Run without required env vars
  - Verify clear error message
  - Verify non-zero exit code
  - File: `cmd/pipeline/main_integration_test.go`

- [ ] **10.5 Integration: Backward compatibility**
  - Run without `--continuous` flag
  - Verify old pipeline behavior still works
  - File: `cmd/pipeline/main_integration_test.go`

#### Implementation

- [ ] **10.6 Implement continuous mode entry**
  - Add `--continuous` flag
  - Instantiate ContinuousCollector
  - Start orchestrator loop
  - File: `cmd/pipeline/main.go`

---

---

### Feature 11: End-to-End Integration Tests

**Goal:** Test the entire system working together with realistic scenarios.

#### E2E Tests

- [ ] **11.1 E2E: Happy path - full collection cycle**
  - Start continuous collector with mocked Riot API
  - Collect matches until 10 warm files
  - Verify reduce triggers
  - Verify data pushed to test Turso
  - Verify collector resumes
  - Run for 3 reduce cycles
  - File: `test/e2e/happy_path_test.go`

- [ ] **11.2 E2E: Key expiration and renewal**
  - Start collector with mock API
  - After 5 warm files, mock returns 401
  - Verify reduce triggers
  - Verify Discord webhook called
  - Mock Discord returns new key
  - Verify fresh restart (state cleared)
  - Verify collection resumes from Challenger #1
  - File: `test/e2e/key_renewal_test.go`

- [ ] **11.3 E2E: Graceful shutdown mid-collection**
  - Start collector, accumulate 5 warm files
  - Send SIGTERM
  - Verify final reduce runs
  - Verify all data in cold/
  - Verify Turso push completes
  - Verify exit code 0
  - File: `test/e2e/graceful_shutdown_test.go`

- [ ] **11.4 E2E: Multiple reduce cycles with real timing**
  - Run collector for simulated 1 hour (accelerated time)
  - Verify multiple reduce cycles complete
  - Verify no memory leaks (monitor RSS)
  - Verify no goroutine leaks
  - File: `test/e2e/long_running_test.go`

- [ ] **11.5 E2E: Recovery from Turso failure**
  - Start collector, trigger reduce
  - Mock Turso to fail during push
  - Verify data preserved in cold/
  - Verify retry mechanism
  - Verify eventual success
  - File: `test/e2e/turso_failure_test.go`

- [ ] **11.6 E2E: Discord webhook failure**
  - Trigger key expiration
  - Mock Discord webhook to fail (500)
  - Verify collector handles gracefully
  - Verify retries webhook
  - Verify still polls for new key
  - File: `test/e2e/discord_failure_test.go`

- [ ] **11.7 E2E: Concurrent stress test**
  - Configure for 50 spider workers
  - Mock API with realistic rate limits
  - Run for 5 reduce cycles
  - Verify no race conditions (`-race`)
  - Verify no deadlocks (timeout detection)
  - Verify data integrity (counts match)
  - File: `test/e2e/stress_test.go`

- [ ] **11.8 E2E: Docker container lifecycle**
  - Build Docker image
  - Start container with test config
  - Verify collector starts
  - Send SIGTERM to container
  - Verify graceful shutdown
  - Verify data persisted to volume
  - File: `test/e2e/docker_test.go`

---

## Implementation Order

Recommended order based on dependencies:

```
Phase 1: Foundation ✅
├── Feature 8: Warm File Counter (no dependencies) ✅
├── Feature 1: Rotator Enhancement (no dependencies) ✅
└── Feature 2: Warm Directory Lock (no dependencies) ✅

Phase 2: Core Pipeline ✅
├── Feature 3: Reducer Refactor (depends on 1, 2) ✅
└── Feature 4: State Machine (depends on 8) ✅

Phase 3: External Integration
├── Feature 6: API Key Validation (no dependencies)
├── Feature 5: Discord Integration (no dependencies)
└── Feature 7: Fresh Restart (depends on 6)

Phase 4: Orchestration
├── Feature 9: Graceful Shutdown (depends on 3, 4)
└── Feature 10: Entry Point (depends on all)

Phase 5: End-to-End Validation
└── Feature 11: E2E Integration Tests (depends on all)
```

### TDD Workflow Per Feature

```
┌─────────────────────────────────────────────────────────────────┐
│                     TDD CYCLE PER FEATURE                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. UNIT TESTS (Red)                                             │
│     │                                                            │
│     ├── Write failing unit tests first                           │
│     ├── Test individual functions/methods in isolation           │
│     └── Mock all dependencies                                    │
│                                                                  │
│  2. IMPLEMENTATION (Green)                                       │
│     │                                                            │
│     ├── Write minimal code to pass unit tests                    │
│     └── Run `go test ./...` until green                          │
│                                                                  │
│  3. INTEGRATION TESTS (Red → Green)                              │
│     │                                                            │
│     ├── Write integration tests with real/realistic deps         │
│     ├── Test component interactions                              │
│     └── May require additional implementation                    │
│                                                                  │
│  4. REFACTOR                                                     │
│     │                                                            │
│     ├── Clean up code while keeping tests green                  │
│     ├── Run `go test -race ./...` for race detection             │
│     └── Check coverage targets met                               │
│                                                                  │
│  5. E2E TESTS (after Phase 4 complete)                           │
│     │                                                            │
│     ├── Run full system integration tests                        │
│     └── Validate real-world scenarios                            │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Test Coverage Goals

| Package | Unit Test Coverage | Integration Test Coverage |
|---------|-------------------|---------------------------|
| `internal/storage` | 90% | Key paths covered |
| `internal/collector` | 85% | All state transitions |
| `internal/discord` | 80% | Webhook + polling |
| `internal/riot` | 80% | Validation flow |
| `cmd/pipeline` | 70% | Flag parsing |
| `test/e2e` | N/A | All critical paths |

### Test Tags

```bash
# Run only unit tests (fast, no external deps)
go test ./... -short

# Run unit + integration tests (may need test DB)
go test ./...

# Run with race detection
go test -race ./...

# Run E2E tests (requires full setup)
go test ./test/e2e/... -tags=e2e

# Run manual integration tests (real Discord, real Riot API)
go test ./... -tags=integration
```

---

## Definition of Done (Per Feature)

- [ ] All unit tests pass (`go test -short ./...`)
- [ ] All integration tests pass (`go test ./...`)
- [ ] No race conditions (`go test -race ./...`)
- [ ] Coverage targets met (`go test -cover ./...`)
- [ ] Code reviewed
- [ ] E2E tests updated if needed
- [ ] Documentation updated if needed

## Definition of Done (Full Project)

- [ ] All features complete (1-10)
- [ ] All E2E tests pass (Feature 11)
- [ ] Docker image builds and runs
- [ ] Manual test: run for 1+ hour on real hardware
- [ ] Manual test: key expiration and renewal flow
- [ ] Documentation complete (README, CLAUDE.md updated)
