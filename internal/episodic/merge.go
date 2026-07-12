package episodic

import "strings"

func Merge(records []NormalizedRecord) NormalizedRecord {
	if len(records) == 0 {
		return NormalizedRecord{SchemaVersion: 1}
	}
	out := records[0]
	out.Contributors = nil
	out.ExternalIDs = nil
	out.Titles = nil
	out.Genres = nil
	out.Countries = nil
	out.Networks = nil
	out.Studios = nil
	out.Seasons = nil
	out.Episodes = nil
	out.Images = nil
	for _, record := range records {
		out.Contributors = append(out.Contributors, Contributor{Provider: record.Provider, ObservationID: record.PrimaryObservationID, NormalizerVersion: record.NormalizerVersion})
		for _, id := range record.ExternalIDs {
			if !hasExternal(out.ExternalIDs, id) {
				out.ExternalIDs = append(out.ExternalIDs, id)
			}
		}
		for _, title := range record.Titles {
			if !hasTitle(out.Titles, title) {
				out.Titles = append(out.Titles, title)
			}
		}
		out.Genres = unionStrings(out.Genres, record.Genres)
		out.Countries = unionStrings(out.Countries, record.Countries)
		out.Studios = unionStrings(out.Studios, record.Studios)
		for _, network := range record.Networks {
			found := false
			for _, existing := range out.Networks {
				if strings.EqualFold(existing.Name, network.Name) {
					found = true
					break
				}
			}
			if !found {
				out.Networks = append(out.Networks, network)
			}
		}
		if out.Overview == "" {
			out.Overview = record.Overview
		}
		if out.Format == "" {
			out.Format = record.Format
		}
		if out.Status == "" {
			out.Status = record.Status
		}
		if out.Language == "" {
			out.Language = record.Language
		}
		if out.StartDate == "" {
			out.StartDate = record.StartDate
		}
		if out.EndDate == "" {
			out.EndDate = record.EndDate
		}
		if out.RuntimeMinutes == 0 {
			out.RuntimeMinutes = record.RuntimeMinutes
		}
		if out.EpisodeCount == 0 {
			out.EpisodeCount = record.EpisodeCount
		}
		if out.SourceMaterial == "" {
			out.SourceMaterial = record.SourceMaterial
		}
		for _, season := range record.Seasons {
			found := false
			for i := range out.Seasons {
				if out.Seasons[i].Number == season.Number {
					found = true
					if out.Seasons[i].Name == "" {
						out.Seasons[i].Name = season.Name
					}
					break
				}
			}
			if !found {
				out.Seasons = append(out.Seasons, season)
			}
		}
		existingEpisodeCount := len(out.Episodes)
		matchedEpisodes := map[int]bool{}
		for _, episode := range record.Episodes {
			index := episodeIndex(out.Episodes[:existingEpisodeCount], episode)
			if index >= 0 && matchedEpisodes[index] {
				index = -1
			}
			if index < 0 {
				out.Episodes = append(out.Episodes, episode)
				continue
			}
			matchedEpisodes[index] = true
			target := &out.Episodes[index]
			for _, number := range episode.Numbers {
				if !hasNumber(target.Numbers, number) {
					target.Numbers = append(target.Numbers, number)
				}
			}
			for _, title := range episode.Titles {
				if !hasTitle(target.Titles, title) {
					target.Titles = append(target.Titles, title)
				}
			}
			if target.Summary == "" {
				target.Summary = episode.Summary
			}
			if target.AirDate == "" {
				target.AirDate = episode.AirDate
			}
			if target.RuntimeMinutes == 0 {
				target.RuntimeMinutes = episode.RuntimeMinutes
			}
		}
		for _, image := range record.Images {
			if image.Provider == "" {
				image.Provider = record.Provider
			}
			if !hasImage(out.Images, image) {
				out.Images = append(out.Images, image)
			}
		}
	}
	return out
}
func hasExternal(values []ExternalID, value ExternalID) bool {
	for _, existing := range values {
		if existing.Provider == value.Provider && existing.Namespace == value.Namespace && strings.EqualFold(existing.Value, value.Value) {
			return true
		}
	}
	return false
}
func hasTitle(values []Title, value Title) bool {
	for _, existing := range values {
		if strings.EqualFold(existing.Value, value.Value) && existing.Language == value.Language && existing.Type == value.Type {
			return true
		}
	}
	return false
}
func unionStrings(values, add []string) []string {
	for _, value := range add {
		found := false
		for _, existing := range values {
			if strings.EqualFold(existing, value) {
				found = true
				break
			}
		}
		if value != "" && !found {
			values = append(values, value)
		}
	}
	return values
}
func episodeIndex(values []Episode, incoming Episode) int {
	// Within one numbering authority, only an exact number identifies an
	// episode. This prevents two specials released on one day (or sharing a
	// generic title) from collapsing into each other.
	for i, existing := range values {
		if sharesNumberingScheme(existing.Numbers, incoming.Numbers) {
			for _, a := range existing.Numbers {
				for _, b := range incoming.Numbers {
					if a.Scheme == b.Scheme && a.Season == b.Season && a.Number == b.Number {
						return i
					}
				}
			}
		}
	}
	for i, existing := range values {
		if sharesNumberingScheme(existing.Numbers, incoming.Numbers) {
			continue
		}
		if existing.AirDate != "" && incoming.AirDate != "" && existing.AirDate == incoming.AirDate {
			return i
		}
		for _, a := range existing.Titles {
			for _, b := range incoming.Titles {
				if normalizedEpisodeTitle(a.Value) == normalizedEpisodeTitle(b.Value) && normalizedEpisodeTitle(a.Value) != "" {
					return i
				}
			}
		}
	}
	return -1
}

func sharesNumberingScheme(a, b []EpisodeNumber) bool {
	for _, left := range a {
		for _, right := range b {
			if left.Scheme == right.Scheme {
				return true
			}
		}
	}
	return false
}
func normalizedEpisodeTitle(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
func hasNumber(values []EpisodeNumber, value EpisodeNumber) bool {
	for _, existing := range values {
		if existing.Scheme == value.Scheme && existing.Season == value.Season && existing.Number == value.Number {
			return true
		}
	}
	return false
}

func hasImage(values []Image, value Image) bool {
	for _, existing := range values {
		if existing.Provider == value.Provider && existing.ProviderID == value.ProviderID && existing.URL == value.URL {
			return true
		}
	}
	return false
}
