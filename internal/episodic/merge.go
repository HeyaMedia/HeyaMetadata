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
	out.Overviews = nil
	out.Genres = nil
	out.Keywords = nil
	out.Countries = nil
	out.Networks = nil
	out.Studios = nil
	out.Organizations = nil
	out.Seasons = nil
	out.Episodes = nil
	out.Images = nil
	out.Ratings = nil
	out.Credits = nil
	out.Links = nil
	out.Videos = nil
	out.Certifications = nil
	out.Recommendations = nil
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
		for _, overview := range record.Overviews {
			if !hasText(out.Overviews, overview) {
				out.Overviews = append(out.Overviews, overview)
			}
		}
		out.Genres = unionStrings(out.Genres, record.Genres)
		out.Keywords = unionStrings(out.Keywords, record.Keywords)
		out.Countries = unionStrings(out.Countries, record.Countries)
		out.Studios = unionStrings(out.Studios, record.Studios)
		for _, organization := range record.Organizations {
			if index := organizationIndex(out.Organizations, organization); index >= 0 {
				mergeOrganization(&out.Organizations[index], organization)
			} else {
				out.Organizations = append(out.Organizations, organization)
			}
		}
		for _, network := range record.Networks {
			if index := networkIndex(out.Networks, network); index >= 0 {
				mergeNetwork(&out.Networks[index], network)
			} else {
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
		if out.SeasonCount == 0 {
			out.SeasonCount = record.SeasonCount
		}
		if out.SourceMaterial == "" {
			out.SourceMaterial = record.SourceMaterial
		}
		for _, season := range record.Seasons {
			found := false
			for i := range out.Seasons {
				if out.Seasons[i].Number == season.Number {
					found = true
					mergeSeason(&out.Seasons[i], season)
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
			normalizeEpisode(&episode)
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
			for _, external := range episode.ExternalIDs {
				if !hasExternal(target.ExternalIDs, external) {
					target.ExternalIDs = append(target.ExternalIDs, external)
				}
			}
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
			for _, overview := range episode.Overviews {
				if !hasText(target.Overviews, overview) {
					target.Overviews = append(target.Overviews, overview)
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
			if target.EpisodeType == "" {
				target.EpisodeType = episode.EpisodeType
			}
			target.IsSpecial = target.IsSpecial || episode.IsSpecial
			for _, rating := range episode.Ratings {
				if !hasRating(target.Ratings, rating) {
					target.Ratings = append(target.Ratings, rating)
				}
			}
			for _, image := range episode.Images {
				if !hasImage(target.Images, image) {
					target.Images = append(target.Images, image)
				}
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
		for _, rating := range record.Ratings {
			if !hasRating(out.Ratings, rating) {
				out.Ratings = append(out.Ratings, rating)
			}
		}
		for _, credit := range record.Credits {
			if !hasCredit(out.Credits, credit) {
				out.Credits = append(out.Credits, credit)
			}
		}
		for _, link := range record.Links {
			if !hasLink(out.Links, link) {
				out.Links = append(out.Links, link)
			}
		}
		for _, video := range record.Videos {
			if !hasVideo(out.Videos, video) {
				out.Videos = append(out.Videos, video)
			}
		}
		for _, certification := range record.Certifications {
			if !hasCertification(out.Certifications, certification) {
				out.Certifications = append(out.Certifications, certification)
			}
		}
		for _, recommendation := range record.Recommendations {
			if !hasRecommendation(out.Recommendations, recommendation) {
				out.Recommendations = append(out.Recommendations, recommendation)
			}
		}
	}
	if len(out.Seasons) > 0 {
		out.SeasonCount = len(out.Seasons)
	}
	if len(out.Episodes) > 0 {
		out.EpisodeCount = len(out.Episodes)
	}
	sortEpisodes(out.Episodes)
	return out
}
func hasRating(values []Rating, value Rating) bool {
	for _, v := range values {
		if v.System == value.System {
			return true
		}
	}
	return false
}
func hasCredit(values []Credit, value Credit) bool {
	for _, v := range values {
		if v.Provider == value.Provider && v.ProviderPersonID == value.ProviderPersonID && v.CreditType == value.CreditType && v.Character == value.Character && v.Job == value.Job {
			return true
		}
	}
	return false
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
	for i, existing := range values {
		for _, left := range existing.ExternalIDs {
			for _, right := range incoming.ExternalIDs {
				if left.Provider == right.Provider && left.Namespace == right.Namespace && strings.EqualFold(left.Value, right.Value) {
					return i
				}
			}
		}
	}
	// Within one numbering authority, only an exact number identifies an
	// episode. This prevents two specials released on one day (or sharing a
	// generic title) from collapsing into each other.
	for i, existing := range values {
		for _, a := range existing.Numbers {
			for _, b := range incoming.Numbers {
				if episodeNumbersMatch(existing, incoming, a, b) {
					return i
				}
			}
		}
	}
	for i, existing := range values {
		if sharesNumberingAuthority(existing.Numbers, incoming.Numbers) {
			continue
		}
		for _, a := range existing.Titles {
			for _, b := range incoming.Titles {
				if existing.AirDate != "" && incoming.AirDate != "" && existing.AirDate == incoming.AirDate && normalizedEpisodeTitle(a.Value) == normalizedEpisodeTitle(b.Value) && normalizedEpisodeTitle(a.Value) != "" {
					return i
				}
			}
		}
	}
	// Different providers often disagree on localized episode titles while
	// still sharing one unambiguous air date. Match only when exactly one
	// pre-existing episode has that date; same-day double episodes and
	// specials remain separate.
	if incoming.AirDate != "" {
		candidate := -1
		for i, existing := range values {
			if existing.AirDate != incoming.AirDate || sharesNumberingAuthority(existing.Numbers, incoming.Numbers) {
				continue
			}
			if candidate >= 0 {
				return -1
			}
			candidate = i
		}
		if candidate >= 0 {
			return candidate
		}
	}
	return -1
}

func normalizeEpisode(episode *Episode) {
	for i := range episode.Numbers {
		episode.Numbers[i].Scheme = strings.ToLower(strings.TrimSpace(episode.Numbers[i].Scheme))
		episode.Numbers[i].Provider = strings.ToLower(strings.TrimSpace(episode.Numbers[i].Provider))
	}
	if episode.EpisodeType == "" {
		episode.EpisodeType = "regular"
	}
	for _, number := range episode.Numbers {
		if number.Scheme == "special" || number.Scheme == "credit" || number.Scheme == "trailer" || number.Scheme == "parody" || (number.Scheme == "aired" && number.Season == 0) {
			episode.IsSpecial = true
			if episode.EpisodeType == "regular" {
				episode.EpisodeType = number.Scheme
			}
		}
	}
	if episode.Summary != "" && len(episode.Overviews) == 0 {
		episode.Overviews = []Text{{Value: episode.Summary, Type: "overview"}}
	}
}

func mergeSeason(target *Season, incoming Season) {
	targetHasStructureAuthority := hasSeasonExternalID(*target, "thexem", "anime_season")
	incomingHasStructureAuthority := hasSeasonExternalID(incoming, "thexem", "anime_season")
	if target.ProviderID == "" {
		target.ProviderID = incoming.ProviderID
	}
	if target.Name == "" {
		target.Name = incoming.Name
	}
	for _, title := range incoming.Titles {
		if !hasTitle(target.Titles, title) {
			target.Titles = append(target.Titles, title)
		}
	}
	for _, overview := range incoming.Overviews {
		if !hasText(target.Overviews, overview) {
			target.Overviews = append(target.Overviews, overview)
		}
	}
	for _, external := range incoming.ExternalIDs {
		if !hasExternal(target.ExternalIDs, external) {
			target.ExternalIDs = append(target.ExternalIDs, external)
		}
	}
	for _, image := range incoming.Images {
		if !hasImage(target.Images, image) {
			target.Images = append(target.Images, image)
		}
	}
	if target.Status == "" {
		target.Status = incoming.Status
	}
	if incomingHasStructureAuthority {
		target.EpisodeOrder = incoming.EpisodeOrder
		target.EpisodeCount = incoming.EpisodeCount
		target.AiredEpisodeCount = incoming.AiredEpisodeCount
	} else if !targetHasStructureAuthority && target.EpisodeOrder == 0 {
		target.EpisodeOrder = incoming.EpisodeOrder
	}
	if !targetHasStructureAuthority && !incomingHasStructureAuthority && target.EpisodeCount == 0 {
		target.EpisodeCount = incoming.EpisodeCount
	}
	if !targetHasStructureAuthority && !incomingHasStructureAuthority && target.AiredEpisodeCount == 0 {
		target.AiredEpisodeCount = incoming.AiredEpisodeCount
	}
	if target.PremiereDate == "" {
		target.PremiereDate = incoming.PremiereDate
	}
	if target.EndDate == "" {
		target.EndDate = incoming.EndDate
	}
}

func hasSeasonExternalID(season Season, provider, namespace string) bool {
	for _, external := range season.ExternalIDs {
		if strings.EqualFold(external.Provider, provider) && strings.EqualFold(external.Namespace, namespace) {
			return true
		}
	}
	return false
}

func hasText(values []Text, value Text) bool {
	for _, existing := range values {
		if existing.Value == value.Value && existing.Language == value.Language && existing.Country == value.Country && existing.Type == value.Type {
			return true
		}
	}
	return false
}

func organizationIndex(values []Organization, value Organization) int {
	for i := range values {
		if strings.EqualFold(values[i].Name, value.Name) && values[i].Type == value.Type {
			return i
		}
		for _, left := range values[i].ExternalIDs {
			for _, right := range value.ExternalIDs {
				if left.Provider == right.Provider && left.Namespace == right.Namespace && left.Value == right.Value {
					return i
				}
			}
		}
	}
	return -1
}

func networkIndex(values []Network, value Network) int {
	for i := range values {
		if strings.EqualFold(values[i].Name, value.Name) {
			return i
		}
		for _, left := range values[i].ExternalIDs {
			for _, right := range value.ExternalIDs {
				if left.Provider == right.Provider && left.Namespace == right.Namespace && left.Value == right.Value {
					return i
				}
			}
		}
	}
	return -1
}

func mergeNetwork(target *Network, incoming Network) {
	if target.Country == "" {
		target.Country = incoming.Country
	}
	if target.Type == "" {
		target.Type = incoming.Type
	}
	if target.LogoURL == "" {
		target.LogoURL = incoming.LogoURL
		target.LogoProvider = incoming.LogoProvider
		target.LogoProviderID = incoming.LogoProviderID
	}
	for _, external := range incoming.ExternalIDs {
		if !hasExternal(target.ExternalIDs, external) {
			target.ExternalIDs = append(target.ExternalIDs, external)
		}
	}
}

func mergeOrganization(target *Organization, incoming Organization) {
	if target.Country == "" {
		target.Country = incoming.Country
	}
	if target.Type == "" {
		target.Type = incoming.Type
	}
	if target.LogoURL == "" {
		target.LogoURL = incoming.LogoURL
		target.LogoProvider = incoming.LogoProvider
		target.LogoProviderID = incoming.LogoProviderID
	}
	for _, external := range incoming.ExternalIDs {
		if !hasExternal(target.ExternalIDs, external) {
			target.ExternalIDs = append(target.ExternalIDs, external)
		}
	}
}

func hasLink(values []Link, value Link) bool {
	for _, existing := range values {
		if existing.Type == value.Type && existing.URL == value.URL {
			return true
		}
	}
	return false
}

func hasVideo(values []Video, value Video) bool {
	for _, existing := range values {
		if existing.Provider == value.Provider && existing.Key == value.Key && existing.URL == value.URL {
			return true
		}
	}
	return false
}

func hasCertification(values []Certification, value Certification) bool {
	for _, existing := range values {
		if existing.System == value.System && existing.Country == value.Country && existing.Rating == value.Rating {
			return true
		}
	}
	return false
}

func hasRecommendation(values []Recommendation, value Recommendation) bool {
	for _, existing := range values {
		if existing.Provider == value.Provider && existing.ProviderID == value.ProviderID {
			return true
		}
	}
	return false
}

func sharesNumberingAuthority(a, b []EpisodeNumber) bool {
	for _, left := range a {
		for _, right := range b {
			if left.Scheme != right.Scheme {
				continue
			}
			if left.Scheme == "absolute" || (left.Provider != "" && left.Provider == right.Provider) || providerNumberingScheme(left.Scheme) {
				return true
			}
		}
	}
	return false
}

func episodeNumbersMatch(existing, incoming Episode, left, right EpisodeNumber) bool {
	if left.Scheme != right.Scheme || left.Season != right.Season || left.Number != right.Number {
		return false
	}
	if left.Scheme == "absolute" {
		return true
	}
	if left.Provider != "" && left.Provider == right.Provider {
		return true
	}
	if providerNumberingScheme(left.Scheme) {
		return true
	}
	if left.Scheme != "aired" {
		return false
	}
	return sameEpisodeDate(existing, incoming) || episodesShareTitle(existing, incoming)
}

func providerNumberingScheme(scheme string) bool {
	switch scheme {
	case "tmdb", "tvdb", "tvmaze", "anidb":
		return true
	default:
		return false
	}
}

func sameEpisodeDate(left, right Episode) bool {
	return left.AirDate != "" && left.AirDate == right.AirDate
}

func episodesShareTitle(left, right Episode) bool {
	for _, a := range left.Titles {
		for _, b := range right.Titles {
			if title := normalizedEpisodeTitle(a.Value); title != "" && title == normalizedEpisodeTitle(b.Value) {
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
		if existing.Scheme == value.Scheme && existing.Season == value.Season && existing.Number == value.Number && existing.Provider == value.Provider {
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
