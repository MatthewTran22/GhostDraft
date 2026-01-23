package collector

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"data-analyzer/internal/riot"
	"data-analyzer/internal/storage"

	"github.com/bits-and-blooms/bloom/v3"
)

const (
	// Worker pool configuration
	DefaultWorkerCount          = 10
	MatchChannelBuffer          = 100
	DefaultTimelineSamplingRate = 0.20 // 20% of matches get timeline data
)

// MatchJob represents a match to be fetched by workers
type MatchJob struct {
	MatchID string
	PUUID   string // Source player PUUID for tracking
}

// Spider crawls match data using a producer-consumer pattern
type Spider struct {
	client       *riot.Client
	rotator      *storage.FileRotator
	currentPatch string

	// Configuration
	matchesPerPlayer     int
	maxPlayers           int
	workerCount          int
	timelineSamplingRate float64 // Probability of fetching timeline (0.0-1.0)

	// Random number generator (thread-safe via per-worker seeding)
	rngMu sync.Mutex
	rng   *rand.Rand

	// Deduplication (bloom filters for memory efficiency)
	visitedMatches *bloom.BloomFilter
	visitedPUUIDs  *bloom.BloomFilter
	matchesMu      sync.Mutex
	puuidsMu       sync.Mutex

	// Queue of players to process
	playerQueue   []string
	playerQueueMu sync.Mutex

	// Channels for producer-consumer
	matchJobs chan MatchJob
	results   chan *MatchResult

	// Stats (atomic for thread safety)
	activePlayerCount  int64
	totalMatches       int64
	timelinesCollected int64 // Track how many timelines we fetched
	playersSkippedRank int64 // Players skipped due to low rank
	startTime          time.Time

	// Shutdown
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// MatchResult holds the result of fetching a match
type MatchResult struct {
	Match        *riot.MatchResponse
	MatchID      string
	NewPUUIDs    []string
	CurrentPatch bool
	BuildOrders  map[int][]int // participantID -> build order (nil if timeline not fetched)
	Error        error
}

// SpiderConfig holds configuration for the spider
type SpiderConfig struct {
	MatchesPerPlayer     int
	MaxPlayers           int
	WorkerCount          int
	TimelineSamplingRate float64 // 0.0-1.0, default 0.20 (20%)
}

// NewSpider creates a new spider with worker pool
func NewSpider(client *riot.Client, rotator *storage.FileRotator, currentPatch string, cfg SpiderConfig) *Spider {
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = DefaultWorkerCount
	}

	// Default timeline sampling rate to 20% if not set
	samplingRate := cfg.TimelineSamplingRate
	if samplingRate <= 0 {
		samplingRate = DefaultTimelineSamplingRate
	}
	if samplingRate > 1.0 {
		samplingRate = 1.0
	}

	return &Spider{
		client:               client,
		rotator:              rotator,
		currentPatch:         currentPatch,
		matchesPerPlayer:     cfg.MatchesPerPlayer,
		maxPlayers:           cfg.MaxPlayers,
		workerCount:          cfg.WorkerCount,
		timelineSamplingRate: samplingRate,
		rng:                  rand.New(rand.NewSource(time.Now().UnixNano())),
		visitedMatches:       bloom.NewWithEstimates(500000, 0.001),
		visitedPUUIDs:        bloom.NewWithEstimates(1000000, 0.001),
		playerQueue:          make([]string, 0, 1000),
		matchJobs:            make(chan MatchJob, MatchChannelBuffer),
		results:              make(chan *MatchResult, MatchChannelBuffer),
	}
}

// Run starts the spider with the given starting PUUID
func (s *Spider) Run(ctx context.Context, startingPUUID string) error {
	ctx, s.cancel = context.WithCancel(ctx)
	s.startTime = time.Now()

	// Add starting player to queue
	s.addPlayer(startingPUUID)

	// Start worker pool (consumers)
	for i := 0; i < s.workerCount; i++ {
		s.wg.Add(1)
		go s.worker(ctx, i)
	}

	// Start result processor
	s.wg.Add(1)
	go s.processResults(ctx)

	// Producer loop - manages queue and dispatches match IDs
	s.producerLoop(ctx)

	// Wait for all workers to finish
	close(s.matchJobs)
	s.wg.Wait()

	s.printSummary()
	return nil
}

// producerLoop is the main producer that traverses players and dispatches match IDs
func (s *Spider) producerLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Check if we've hit max players
		if atomic.LoadInt64(&s.activePlayerCount) >= int64(s.maxPlayers) {
			// Wait a bit for results to come in, then check again
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Get next player from queue
		puuid := s.popPlayer()
		if puuid == "" {
			// Queue empty, wait for more players from results
			time.Sleep(100 * time.Millisecond)

			// If queue is still empty and no jobs in flight, we're done
			if s.isQueueEmpty() && len(s.matchJobs) == 0 {
				return
			}
			continue
		}

		// Check player rank - skip if below Emerald 4
		tier, division, hasRank, err := s.client.GetSoloQueueRank(ctx, puuid)
		if err != nil {
			log.Printf("[Producer] Failed to get rank for %s: %v (skipping)", puuid[:16], err)
			atomic.AddInt64(&s.playersSkippedRank, 1)
			continue
		}
		if !hasRank {
			log.Printf("[Producer] Player %s has no solo queue rank (skipping)", puuid[:16])
			atomic.AddInt64(&s.playersSkippedRank, 1)
			continue
		}
		if !riot.IsEmerald4OrHigher(tier, division) {
			log.Printf("[Producer] Player %s is %s %s - below Emerald 4 (skipping)", puuid[:16], tier, division)
			atomic.AddInt64(&s.playersSkippedRank, 1)
			continue
		}

		// Fetch match history for this player
		matchIDs, err := s.client.GetMatchHistory(ctx, puuid, s.matchesPerPlayer)
		if err != nil {
			log.Printf("[Producer] Failed to fetch match history for %s: %v", puuid[:16], err)
			continue
		}

		elapsed := time.Since(s.startTime)
		fmt.Printf("\n[Player %d/%d] [%s] Processing: %s... (%s %s, %d matches)\n",
			atomic.LoadInt64(&s.activePlayerCount), s.maxPlayers,
			formatDuration(elapsed), puuid[:16], tier, division, len(matchIDs))

		// Track if this player has current patch matches
		currentPatchCount := 0
		dispatchedCount := 0

		// Dispatch unique matches to workers
		for _, matchID := range matchIDs {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Check bloom filter for deduplication
			if s.hasVisitedMatch(matchID) {
				continue
			}
			s.markMatchVisited(matchID)

			// Dispatch to workers
			select {
			case s.matchJobs <- MatchJob{MatchID: matchID, PUUID: puuid}:
				dispatchedCount++
			case <-ctx.Done():
				return
			}
		}

		// Count this player if they had matches dispatched
		// (actual current patch check happens in results processor)
		if dispatchedCount > 0 {
			_ = currentPatchCount // Will be updated by results
		}
	}
}

// worker is a consumer that fetches match details
func (s *Spider) worker(ctx context.Context, id int) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-s.matchJobs:
			if !ok {
				return
			}

			result := s.fetchMatch(ctx, job)

			select {
			case s.results <- result:
			case <-ctx.Done():
				return
			}
		}
	}
}

// shouldFetchTimeline returns true if we should fetch the timeline for this match
// Uses the configured sampling rate with thread-safe random number generation
func (s *Spider) shouldFetchTimeline() bool {
	s.rngMu.Lock()
	defer s.rngMu.Unlock()
	return s.rng.Float64() < s.timelineSamplingRate
}

// fetchMatch fetches match details and optionally timeline based on sampling rate
func (s *Spider) fetchMatch(ctx context.Context, job MatchJob) *MatchResult {
	result := &MatchResult{
		MatchID:   job.MatchID,
		NewPUUIDs: make([]string, 0),
	}

	// Always fetch match details (for accurate win rates)
	match, err := s.client.GetMatch(ctx, job.MatchID)
	if err != nil {
		result.Error = err
		return result
	}

	result.Match = match

	// Check if current patch
	matchPatch := riot.NormalizePatch(match.Info.GameVersion)
	result.CurrentPatch = (matchPatch == s.currentPatch)

	// If not current patch, don't collect new players or timeline
	if !result.CurrentPatch {
		return result
	}

	// Collect new PUUIDs from participants
	for _, p := range match.Info.Participants {
		if !s.hasVisitedPUUID(p.PUUID) {
			result.NewPUUIDs = append(result.NewPUUIDs, p.PUUID)
		}
	}

	// Statistical sampling: only fetch timeline for a percentage of matches
	if s.shouldFetchTimeline() {
		timeline, err := s.client.GetTimeline(ctx, job.MatchID)
		if err != nil {
			// Log but don't fail - timeline is optional for sampling
			log.Printf("    [Timeline] Failed to fetch for %s: %v", job.MatchID, err)
		} else {
			// Extract build orders for all participants
			result.BuildOrders = make(map[int][]int)
			for _, p := range match.Info.Participants {
				buildOrder := riot.ExtractBuildOrder(timeline, p.ParticipantID)
				if len(buildOrder) > 0 {
					result.BuildOrders[p.ParticipantID] = buildOrder
				}
			}
			atomic.AddInt64(&s.timelinesCollected, 1)
		}
	}

	return result
}

// processResults processes results from workers and writes to storage
func (s *Spider) processResults(ctx context.Context) {
	defer s.wg.Done()

	// Track current patch matches per source player
	playerCurrentPatchCounts := make(map[string]int)
	playerTotalCounts := make(map[string]int)

	for {
		select {
		case <-ctx.Done():
			return
		case result, ok := <-s.results:
			if !ok {
				return
			}

			if result.Error != nil {
				log.Printf("  [Worker] Failed to fetch %s: %v", result.MatchID, result.Error)
				continue
			}

			if result.Match == nil {
				continue
			}

			// Track counts for source player
			// (Note: we don't have source PUUID in result, tracking by match for simplicity)

			// If not current patch, skip writing but still process
			if !result.CurrentPatch {
				continue
			}

			// Write participants to storage
			for _, p := range result.Match.Info.Participants {
				rawMatch := &storage.RawMatch{
					MatchID:      result.MatchID,
					GameVersion:  result.Match.Info.GameVersion,
					GameDuration: result.Match.Info.GameDuration,
					GameCreation: result.Match.Info.GameCreation,
					PUUID:        p.PUUID,
					GameName:     p.RiotIdGameName,
					TagLine:      p.RiotIdTagline,
					ChampionID:   p.ChampionID,
					ChampionName: p.ChampionName,
					TeamPosition: p.TeamPosition,
					Win:          p.Win,
					Item0:        p.Item0,
					Item1:        p.Item1,
					Item2:        p.Item2,
					Item3:        p.Item3,
					Item4:        p.Item4,
					Item5:        p.Item5,
					BuildOrder:   []int{}, // Default to empty (will be omitted in JSON)
				}

				// Include build order if timeline was sampled for this match
				if result.BuildOrders != nil {
					if buildOrder, ok := result.BuildOrders[p.ParticipantID]; ok {
						rawMatch.BuildOrder = buildOrder
					}
				}

				if err := s.rotator.WriteLine(rawMatch); err != nil {
					log.Printf("  [Writer] Failed to write: %v", err)
				}
			}

			// Signal match complete
			if err := s.rotator.MatchComplete(); err != nil {
				log.Printf("  [Writer] Failed to complete match: %v", err)
			}

			atomic.AddInt64(&s.totalMatches, 1)

			// Add new players to queue
			for _, puuid := range result.NewPUUIDs {
				s.addPlayer(puuid)
			}

			// Increment active player count (simplified: count per match for now)
			// In a more sophisticated version, track per-player stats
			_ = playerCurrentPatchCounts
			_ = playerTotalCounts
		}
	}
}

// Bloom filter helpers with mutex protection
func (s *Spider) hasVisitedMatch(matchID string) bool {
	s.matchesMu.Lock()
	defer s.matchesMu.Unlock()
	return s.visitedMatches.TestString(matchID)
}

func (s *Spider) markMatchVisited(matchID string) {
	s.matchesMu.Lock()
	defer s.matchesMu.Unlock()
	s.visitedMatches.AddString(matchID)
}

func (s *Spider) hasVisitedPUUID(puuid string) bool {
	s.puuidsMu.Lock()
	defer s.puuidsMu.Unlock()
	return s.visitedPUUIDs.TestString(puuid)
}

func (s *Spider) markPUUIDVisited(puuid string) {
	s.puuidsMu.Lock()
	defer s.puuidsMu.Unlock()
	s.visitedPUUIDs.AddString(puuid)
}

// Queue helpers
func (s *Spider) addPlayer(puuid string) {
	s.puuidsMu.Lock()
	if s.visitedPUUIDs.TestString(puuid) {
		s.puuidsMu.Unlock()
		return
	}
	s.visitedPUUIDs.AddString(puuid)
	s.puuidsMu.Unlock()

	s.playerQueueMu.Lock()
	s.playerQueue = append(s.playerQueue, puuid)
	s.playerQueueMu.Unlock()

	atomic.AddInt64(&s.activePlayerCount, 1)
}

func (s *Spider) popPlayer() string {
	s.playerQueueMu.Lock()
	defer s.playerQueueMu.Unlock()

	if len(s.playerQueue) == 0 {
		return ""
	}

	puuid := s.playerQueue[0]
	s.playerQueue = s.playerQueue[1:]
	return puuid
}

func (s *Spider) isQueueEmpty() bool {
	s.playerQueueMu.Lock()
	defer s.playerQueueMu.Unlock()
	return len(s.playerQueue) == 0
}

// Stop gracefully stops the spider
func (s *Spider) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

// RunContinuous implements SpiderRunner interface.
// It runs a single batch of match collection and returns.
// Call this repeatedly in a loop for continuous collection.
func (s *Spider) RunContinuous(ctx context.Context) error {
	// If not initialized, seed first
	if s.startTime.IsZero() {
		s.startTime = time.Now()
	}

	// Check if we've hit max players
	if atomic.LoadInt64(&s.activePlayerCount) >= int64(s.maxPlayers) {
		log.Printf("[Spider] Max players reached (%d/%d), signaling collection complete", s.activePlayerCount, s.maxPlayers)
		return ErrCollectionComplete
	}

	// Get next player from queue
	puuid := s.popPlayer()
	if puuid == "" {
		// Queue empty, caller should wait for more
		return nil
	}

	// Check player rank - skip if below Emerald 4
	tier, division, hasRank, err := s.client.GetSoloQueueRank(ctx, puuid)
	if err != nil {
		// Check if this is an API key error
		if isHTTPError(err, 401) || isHTTPError(err, 403) {
			return fmt.Errorf("rank check failed: %w", WrapHTTPError(getHTTPStatus(err), "API key error"))
		}
		log.Printf("[Spider] Failed to get rank for %s: %v (skipping)", puuid[:min(16, len(puuid))], err)
		atomic.AddInt64(&s.playersSkippedRank, 1)
		return nil
	}
	if !hasRank {
		log.Printf("[Spider] Player %s has no solo queue rank (skipping)", puuid[:min(16, len(puuid))])
		atomic.AddInt64(&s.playersSkippedRank, 1)
		return nil
	}
	if !riot.IsEmerald4OrHigher(tier, division) {
		log.Printf("[Spider] Player %s is %s %s - below Emerald 4 (skipping)", puuid[:min(16, len(puuid))], tier, division)
		atomic.AddInt64(&s.playersSkippedRank, 1)
		return nil
	}

	// Fetch match history for this player
	matchIDs, err := s.client.GetMatchHistory(ctx, puuid, s.matchesPerPlayer)
	if err != nil {
		if isHTTPError(err, 401) || isHTTPError(err, 403) {
			return fmt.Errorf("match history failed: %w", WrapHTTPError(getHTTPStatus(err), "API key error"))
		}
		log.Printf("[Spider] Failed to fetch match history for %s: %v", puuid[:min(16, len(puuid))], err)
		return nil
	}

	elapsed := time.Since(s.startTime)
	fmt.Printf("\n[Player %d/%d] [%s] Processing: %s... (%s %s, %d matches)\n",
		atomic.LoadInt64(&s.activePlayerCount), s.maxPlayers,
		formatDuration(elapsed), puuid[:min(16, len(puuid))], tier, division, len(matchIDs))

	// Process matches synchronously in continuous mode
	for _, matchID := range matchIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check bloom filter for deduplication
		if s.hasVisitedMatch(matchID) {
			continue
		}
		s.markMatchVisited(matchID)

		// Fetch and process the match
		result := s.fetchMatch(ctx, MatchJob{MatchID: matchID, PUUID: puuid})
		if result.Error != nil {
			if isHTTPError(result.Error, 401) || isHTTPError(result.Error, 403) {
				return fmt.Errorf("match fetch failed: %w", WrapHTTPError(getHTTPStatus(result.Error), "API key error"))
			}
			log.Printf("  [Spider] Failed to fetch %s: %v", matchID, result.Error)
			continue
		}

		if result.Match == nil || !result.CurrentPatch {
			continue
		}

		// Write participants to storage
		for _, p := range result.Match.Info.Participants {
			rawMatch := &storage.RawMatch{
				MatchID:      result.MatchID,
				GameVersion:  result.Match.Info.GameVersion,
				GameDuration: result.Match.Info.GameDuration,
				GameCreation: result.Match.Info.GameCreation,
				PUUID:        p.PUUID,
				GameName:     p.RiotIdGameName,
				TagLine:      p.RiotIdTagline,
				ChampionID:   p.ChampionID,
				ChampionName: p.ChampionName,
				TeamPosition: p.TeamPosition,
				Win:          p.Win,
				Item0:        p.Item0,
				Item1:        p.Item1,
				Item2:        p.Item2,
				Item3:        p.Item3,
				Item4:        p.Item4,
				Item5:        p.Item5,
				BuildOrder:   []int{},
			}

			if result.BuildOrders != nil {
				if buildOrder, ok := result.BuildOrders[p.ParticipantID]; ok {
					rawMatch.BuildOrder = buildOrder
				}
			}

			if err := s.rotator.WriteLine(rawMatch); err != nil {
				log.Printf("  [Spider] Failed to write: %v", err)
			}
		}

		if err := s.rotator.MatchComplete(); err != nil {
			log.Printf("  [Spider] Failed to complete match: %v", err)
		}

		atomic.AddInt64(&s.totalMatches, 1)

		// Add new players to queue
		for _, newPUUID := range result.NewPUUIDs {
			s.addPlayer(newPUUID)
		}
	}

	return nil
}

// Reset clears all internal state (bloom filters, player queue).
// Implements SpiderRunner interface.
func (s *Spider) Reset() {
	log.Println("[Spider] Resetting internal state...")

	// Clear bloom filters
	s.matchesMu.Lock()
	s.visitedMatches = bloom.NewWithEstimates(500000, 0.001)
	s.matchesMu.Unlock()

	s.puuidsMu.Lock()
	s.visitedPUUIDs = bloom.NewWithEstimates(1000000, 0.001)
	s.puuidsMu.Unlock()

	// Clear player queue
	s.playerQueueMu.Lock()
	s.playerQueue = make([]string, 0, 1000)
	s.playerQueueMu.Unlock()

	// Reset counters
	atomic.StoreInt64(&s.activePlayerCount, 0)
	atomic.StoreInt64(&s.totalMatches, 0)
	atomic.StoreInt64(&s.timelinesCollected, 0)
	atomic.StoreInt64(&s.playersSkippedRank, 0)

	// Reset start time
	s.startTime = time.Time{}

	log.Println("[Spider] Reset complete")
}

// SeedFromChallenger seeds the spider with the top Challenger player.
// Implements SpiderRunner interface.
func (s *Spider) SeedFromChallenger(ctx context.Context) error {
	log.Println("[Spider] Seeding from top Challenger player...")

	puuid, err := s.client.GetTopChallengerPUUID(ctx)
	if err != nil {
		if isHTTPError(err, 401) || isHTTPError(err, 403) {
			return fmt.Errorf("seed from challenger failed: %w", WrapHTTPError(getHTTPStatus(err), "API key error"))
		}
		return fmt.Errorf("failed to get top Challenger: %w", err)
	}

	s.addPlayer(puuid)
	log.Printf("[Spider] Seeded with Challenger player: %s...", puuid[:min(16, len(puuid))])

	return nil
}

// isHTTPError checks if an error contains a specific HTTP status code
func isHTTPError(err error, statusCode int) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	statusStr := fmt.Sprintf("%d", statusCode)
	return contains(errStr, statusStr)
}

// getHTTPStatus extracts HTTP status code from error (best effort)
func getHTTPStatus(err error) int {
	if err == nil {
		return 0
	}
	errStr := err.Error()
	if contains(errStr, "401") {
		return 401
	}
	if contains(errStr, "403") {
		return 403
	}
	return 0
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Spider) printSummary() {
	elapsed := time.Since(s.startTime)
	totalMatches := atomic.LoadInt64(&s.totalMatches)
	playerCount := atomic.LoadInt64(&s.activePlayerCount)
	timelinesCollected := atomic.LoadInt64(&s.timelinesCollected)
	playersSkipped := atomic.LoadInt64(&s.playersSkippedRank)

	fmt.Printf("\n=== Spider Complete ===\n")
	fmt.Printf("Total time: %s\n", formatDuration(elapsed))
	fmt.Printf("Players processed: %d (Emerald 4+)\n", playerCount)
	fmt.Printf("Players skipped (below Emerald 4 / no rank): %d\n", playersSkipped)
	fmt.Printf("Matches written: %d\n", totalMatches)
	fmt.Printf("Total records (participants): %d\n", totalMatches*10)

	// Timeline sampling stats
	if totalMatches > 0 {
		actualRate := float64(timelinesCollected) / float64(totalMatches) * 100
		fmt.Printf("Timelines collected: %d (%.1f%% actual, %.0f%% target)\n",
			timelinesCollected, actualRate, s.timelineSamplingRate*100)
	}

	if totalMatches > 0 {
		avgPerMatch := elapsed / time.Duration(totalMatches)
		fmt.Printf("Avg time per match: %s\n", formatDuration(avgPerMatch))
		matchesPerMin := float64(totalMatches) / elapsed.Minutes()
		fmt.Printf("Throughput: %.1f matches/min\n", matchesPerMin)
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	} else if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%02ds", mins, secs)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	secs := int(d.Seconds()) % 60
	return fmt.Sprintf("%dh%02dm%02ds", hours, mins, secs)
}
