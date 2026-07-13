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

func TestMergeKeepsDistinctSameDaySpecialsSeparateAcrossAuthorities(t *testing.T) {
	a := NormalizedRecord{Provider: "tvmaze", Episodes: []Episode{
		{ProviderID: "1", AirDate: "2020-01-01", Titles: []Title{{Value: "Behind the Scenes"}}, Numbers: []EpisodeNumber{{Scheme: "tvmaze", Season: 0, Number: 1}}, IsSpecial: true, EpisodeType: "special"},
		{ProviderID: "2", AirDate: "2020-01-01", Titles: []Title{{Value: "Cast Reunion"}}, Numbers: []EpisodeNumber{{Scheme: "tvmaze", Season: 0, Number: 2}}, IsSpecial: true, EpisodeType: "special"},
	}}
	b := NormalizedRecord{Provider: "tmdb", Episodes: []Episode{
		{ProviderID: "3", AirDate: "2020-01-01", Titles: []Title{{Value: "Behind the Scenes"}}, Numbers: []EpisodeNumber{{Scheme: "tmdb", Season: 0, Number: 7}}, IsSpecial: true, EpisodeType: "special"},
		{ProviderID: "4", AirDate: "2020-01-01", Titles: []Title{{Value: "Cast Reunion"}}, Numbers: []EpisodeNumber{{Scheme: "tmdb", Season: 0, Number: 8}}, IsSpecial: true, EpisodeType: "special"},
	}}
	merged := Merge([]NormalizedRecord{a, b})
	if len(merged.Episodes) != 2 {
		t.Fatalf("same-day specials collapsed: %+v", merged.Episodes)
	}
	for _, episode := range merged.Episodes {
		if !episode.IsSpecial || len(episode.Numbers) != 2 {
			t.Fatalf("special evidence was not merged safely: %+v", episode)
		}
	}
}

func TestMergeUsesAnimeAbsoluteOrderWhenProviderAiredOrdersConflict(t *testing.T) {
	anidb := NormalizedRecord{Provider: "anidb", Episodes: []Episode{
		{ProviderID: "a1", Titles: []Title{{Value: "Asteroid Blues"}}, Numbers: []EpisodeNumber{{Scheme: "aired", Season: 1, Number: 1, Provider: "anidb"}, {Scheme: "absolute", Number: 1, Provider: "anidb"}}},
		{ProviderID: "a2", Titles: []Title{{Value: "Stray Dog Strut"}}, Numbers: []EpisodeNumber{{Scheme: "aired", Season: 1, Number: 2, Provider: "anidb"}, {Scheme: "absolute", Number: 2, Provider: "anidb"}}},
	}}
	tvdb := NormalizedRecord{Provider: "tvdb", Episodes: []Episode{
		{ProviderID: "t2", Titles: []Title{{Value: "Stray Dog Strut"}}, Numbers: []EpisodeNumber{{Scheme: "aired", Season: 1, Number: 1, Provider: "tvdb"}, {Scheme: "tvdb", Season: 1, Number: 1, Provider: "tvdb"}, {Scheme: "absolute", Number: 2, Provider: "tvdb"}}},
		{ProviderID: "t1", Titles: []Title{{Value: "Asteroid Blues"}}, Numbers: []EpisodeNumber{{Scheme: "aired", Season: 1, Number: 13, Provider: "tvdb"}, {Scheme: "tvdb", Season: 1, Number: 13, Provider: "tvdb"}, {Scheme: "absolute", Number: 1, Provider: "tvdb"}}},
	}}
	merged := Merge([]NormalizedRecord{anidb, tvdb})
	if len(merged.Episodes) != 2 {
		t.Fatalf("episodes=%+v", merged.Episodes)
	}
	for _, episode := range merged.Episodes {
		if len(episode.Numbers) != 4 {
			t.Fatalf("conflicting aired evidence did not merge through absolute order: %+v", episode)
		}
		if episode.Titles[0].Value == "Stray Dog Strut" {
			preferred, ok := preferredEpisodeNumber(episode.Numbers)
			if !ok || preferred.Provider != "anidb" || preferred.Number != 2 {
				t.Fatalf("root anime numbering was not preferred: %+v", episode.Numbers)
			}
		}
	}
}
