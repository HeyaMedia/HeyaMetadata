package episodic

import "testing"

func TestMergeKeepsSameDayEpisodesSeparateWithinOneAuthority(t *testing.T) {
	record := NormalizedRecord{Provider: "tmdb", Episodes: []Episode{
		{ProviderID: "1", AirDate: "2020-01-01", Titles: []Title{{Value: "Part One"}}, Numbers: []EpisodeNumber{{Scheme: "tmdb", Season: 0, Number: 1}}},
		{ProviderID: "2", AirDate: "2020-01-01", Titles: []Title{{Value: "Part Two"}}, Numbers: []EpisodeNumber{{Scheme: "tmdb", Season: 0, Number: 2}}},
	}}
	merged := Merge([]NormalizedRecord{record})
	if len(merged.Episodes) != 2 {
		t.Fatalf("got %d episodes", len(merged.Episodes))
	}
}

func TestMergeCombinesCrossAuthorityEpisodeNumbers(t *testing.T) {
	a := NormalizedRecord{Provider: "tvmaze", Episodes: []Episode{{ProviderID: "1", AirDate: "2020-01-01", Titles: []Title{{Value: "Pilot"}}, Numbers: []EpisodeNumber{{Scheme: "tvmaze", Season: 1, Number: 1}}}}}
	b := NormalizedRecord{Provider: "tmdb", Episodes: []Episode{{ProviderID: "2", AirDate: "2020-01-01", Titles: []Title{{Value: "Pilot"}}, Numbers: []EpisodeNumber{{Scheme: "tmdb", Season: 1, Number: 1}}}}}
	merged := Merge([]NormalizedRecord{a, b})
	if len(merged.Episodes) != 1 || len(merged.Episodes[0].Numbers) != 2 {
		t.Fatalf("episodes: %+v", merged.Episodes)
	}
}

func TestMergeNeverCollapsesDistinctEpisodesFromOneRecord(t *testing.T) {
	record := NormalizedRecord{Provider: "anidb", Episodes: []Episode{
		{ProviderID: "1", AirDate: "2020-01-01", Titles: []Title{{Value: "Recap"}}, Numbers: []EpisodeNumber{{Scheme: "aired", Number: 1}}},
		{ProviderID: "S1", AirDate: "2020-01-01", Titles: []Title{{Value: "Recap"}}, Numbers: []EpisodeNumber{{Scheme: "special", Number: 1}}},
	}}
	if got := len(Merge([]NormalizedRecord{record}).Episodes); got != 2 {
		t.Fatalf("got %d episodes", got)
	}
}
