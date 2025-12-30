package ugg

// BuildData holds champion build information from U.GG
type BuildData struct {
	ChampionID       int     `json:"championId"`
	ChampionName     string  `json:"championName"`
	Role             string  `json:"role"`
	WinRate          float64 `json:"winRate"`
	PickRate         float64 `json:"pickRate"`
	StartingItems    []int   `json:"startingItems"`
	CoreItems        []int   `json:"coreItems"`
	FourthItemOptions []int  `json:"fourthItemOptions"` // Best 4th item choices
	FifthItemOptions  []int  `json:"fifthItemOptions"`  // Best 5th item choices
	SixthItemOptions  []int  `json:"sixthItemOptions"`  // Best 6th item choices
	Counters         []int   `json:"counters"`
}

// MatchupData holds matchup information
type MatchupData struct {
	EnemyChampionID int     `json:"enemyChampionId"`
	Wins            float64 `json:"wins"`
	Games           float64 `json:"games"`
	WinRate         float64 `json:"winRate"`
}
