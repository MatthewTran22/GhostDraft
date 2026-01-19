package riot

import (
	"context"
	"os"
	"testing"

	"github.com/joho/godotenv"
)

func TestIsEmerald4OrHigher(t *testing.T) {
	tests := []struct {
		name     string
		tier     string
		division string
		want     bool
	}{
		// Should pass - Emerald 4 and above
		{"Emerald IV", "EMERALD", "IV", true},
		{"Emerald III", "EMERALD", "III", true},
		{"Emerald II", "EMERALD", "II", true},
		{"Emerald I", "EMERALD", "I", true},
		{"Diamond IV", "DIAMOND", "IV", true},
		{"Diamond III", "DIAMOND", "III", true},
		{"Diamond II", "DIAMOND", "II", true},
		{"Diamond I", "DIAMOND", "I", true},
		{"Master", "MASTER", "", true},
		{"Grandmaster", "GRANDMASTER", "", true},
		{"Challenger", "CHALLENGER", "", true},

		// Should fail - below Emerald 4
		{"Iron IV", "IRON", "IV", false},
		{"Iron I", "IRON", "I", false},
		{"Bronze IV", "BRONZE", "IV", false},
		{"Bronze I", "BRONZE", "I", false},
		{"Silver IV", "SILVER", "IV", false},
		{"Silver I", "SILVER", "I", false},
		{"Gold IV", "GOLD", "IV", false},
		{"Gold I", "GOLD", "I", false},
		{"Platinum IV", "PLATINUM", "IV", false},
		{"Platinum I", "PLATINUM", "I", false},

		// Edge cases
		{"Invalid tier", "INVALID", "IV", false},
		{"Empty tier", "", "IV", false},
		{"Empty division for Emerald", "EMERALD", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsEmerald4OrHigher(tt.tier, tt.division)
			if got != tt.want {
				t.Errorf("IsEmerald4OrHigher(%q, %q) = %v, want %v",
					tt.tier, tt.division, got, tt.want)
			}
		})
	}
}

func TestTierOrder(t *testing.T) {
	// Verify tier ordering is correct
	expectedOrder := []string{
		"IRON", "BRONZE", "SILVER", "GOLD", "PLATINUM",
		"EMERALD", "DIAMOND", "MASTER", "GRANDMASTER", "CHALLENGER",
	}

	for i := 0; i < len(expectedOrder)-1; i++ {
		current := expectedOrder[i]
		next := expectedOrder[i+1]
		if TierOrder[current] >= TierOrder[next] {
			t.Errorf("Tier order incorrect: %s (%d) should be less than %s (%d)",
				current, TierOrder[current], next, TierOrder[next])
		}
	}
}

func TestDivisionOrder(t *testing.T) {
	// Verify division ordering (IV is lowest, I is highest)
	if DivisionOrder["IV"] >= DivisionOrder["III"] {
		t.Error("IV should be lower than III")
	}
	if DivisionOrder["III"] >= DivisionOrder["II"] {
		t.Error("III should be lower than II")
	}
	if DivisionOrder["II"] >= DivisionOrder["I"] {
		t.Error("II should be lower than I")
	}
}

// Test: GetTopChallengerPUUID helper method
func TestGetTopChallengerPUUID_Integration(t *testing.T) {
	godotenv.Load("../../.env")

	if os.Getenv("RIOT_API_KEY") == "" {
		t.Skip("RIOT_API_KEY not set, skipping integration test")
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()

	// Get top challenger PUUID
	t.Log("Fetching top challenger player...")
	puuid, err := client.GetTopChallengerPUUID(ctx)
	if err != nil {
		t.Fatalf("GetTopChallengerPUUID failed: %v", err)
	}

	if puuid == "" {
		t.Fatal("GetTopChallengerPUUID returned empty PUUID")
	}
	t.Logf("Got PUUID: %s...%s", puuid[:8], puuid[len(puuid)-4:])

	// Get player's Riot ID
	t.Log("Fetching player's Riot ID...")
	account, err := client.GetAccountByPUUID(ctx, puuid)
	if err != nil {
		t.Fatalf("GetAccountByPUUID failed: %v", err)
	}
	t.Logf("Top Challenger: %s#%s", account.GameName, account.TagLine)

	// Verify this player is actually Challenger
	t.Log("Verifying player rank...")
	tier, division, hasRank, err := client.GetSoloQueueRank(ctx, puuid)
	if err != nil {
		t.Fatalf("GetSoloQueueRank failed: %v", err)
	}

	if !hasRank {
		t.Fatal("Top challenger player has no rank")
	}

	if tier != "CHALLENGER" {
		t.Errorf("Expected CHALLENGER tier, got %s %s", tier, division)
	} else {
		t.Logf("Verified: %s#%s is %s", account.GameName, account.TagLine, tier)
	}

	// Verify they qualify for data collection
	if !IsEmerald4OrHigher(tier, division) {
		t.Errorf("Challenger player should qualify for Emerald4+")
	}
}

// Integration test - calls actual Riot API
func TestGetSoloQueueRank_Integration(t *testing.T) {
	// Load .env from project root
	godotenv.Load("../../.env")

	if os.Getenv("RIOT_API_KEY") == "" {
		t.Skip("RIOT_API_KEY not set, skipping integration test")
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()

	testCases := []struct {
		gameName       string
		tagLine        string
		expectQualify  bool // true = should be Emerald 4+, false = should be below
	}{
		{"pentaless", "NA5", true},   // Expected: Emerald 4+
		{"Miso", "Chasu", false},     // Expected: Below Emerald 4
	}

	for _, tc := range testCases {
		t.Run(tc.gameName+"#"+tc.tagLine, func(t *testing.T) {
			// Step 1: Get PUUID from Riot ID
			t.Logf("Looking up account: %s#%s", tc.gameName, tc.tagLine)
			account, err := client.GetAccountByRiotID(ctx, tc.gameName, tc.tagLine)
			if err != nil {
				t.Fatalf("GetAccountByRiotID failed: %v", err)
			}
			t.Logf("Got PUUID: %s...%s", account.PUUID[:8], account.PUUID[len(account.PUUID)-4:])

			// Step 2: Get ranked entries directly by PUUID
			t.Logf("Getting ranked entries...")
			entries, err := client.GetRankedEntriesByPUUID(ctx, account.PUUID)
			if err != nil {
				t.Fatalf("GetRankedEntriesByPUUID failed: %v", err)
			}
			t.Logf("Got %d ranked entries", len(entries))
			for _, entry := range entries {
				t.Logf("  %s: %s %s (%d LP)", entry.QueueType, entry.Tier, entry.Rank, entry.LeaguePoints)
			}

			// Step 3: Test GetSoloQueueRank convenience method
			t.Logf("Testing GetSoloQueueRank...")
			tier, division, hasRank, err := client.GetSoloQueueRank(ctx, account.PUUID)
			if err != nil {
				t.Fatalf("GetSoloQueueRank failed: %v", err)
			}

			if hasRank {
				t.Logf("Solo Queue Rank: %s %s", tier, division)

				// Step 5: Test IsEmerald4OrHigher
				qualifies := IsEmerald4OrHigher(tier, division)
				t.Logf("IsEmerald4OrHigher: %v (expected: %v)", qualifies, tc.expectQualify)

				// Verify result matches expectation
				if qualifies != tc.expectQualify {
					t.Errorf("IsEmerald4OrHigher(%s, %s) = %v, expected %v",
						tier, division, qualifies, tc.expectQualify)
				}

				// Verify tier is valid
				if _, ok := TierOrder[tier]; !ok {
					t.Errorf("Invalid tier returned: %s", tier)
				}

				// Verify division is valid (for non-apex tiers)
				if tier != "MASTER" && tier != "GRANDMASTER" && tier != "CHALLENGER" {
					if _, ok := DivisionOrder[division]; !ok {
						t.Errorf("Invalid division returned: %s", division)
					}
				}
			} else {
				t.Logf("Player has no solo queue rank")
				// No rank should not qualify
				if tc.expectQualify {
					t.Errorf("Expected player to qualify but they have no rank")
				}
			}
		})
	}
}
