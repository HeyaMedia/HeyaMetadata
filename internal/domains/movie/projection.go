package movie

import (
	"sort"
	"strings"
	"time"
)

type ExternalID struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

type Display struct {
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title,omitempty"`
	Year          int    `json:"year,omitempty"`
	ImageID       string `json:"image_id,omitempty"`
}

type Freshness struct {
	State      string                       `json:"state"`
	UpdatedAt  time.Time                    `json:"updated_at"`
	FreshUntil time.Time                    `json:"fresh_until"`
	Providers  map[string]ProviderFreshness `json:"providers"`
}

type ProviderFreshness struct {
	State             string    `json:"state"`
	LastSuccessAt     time.Time `json:"last_success_at"`
	LastObservationID string    `json:"last_observation_id"`
}

type ProjectedImage struct {
	ID            string  `json:"id"`
	Class         string  `json:"class"`
	Language      string  `json:"language,omitempty"`
	Width         int     `json:"width,omitempty"`
	Height        int     `json:"height,omitempty"`
	Provider      string  `json:"provider"`
	ProviderScore float64 `json:"provider_score,omitempty"`
}

type ProjectedCompany struct {
	ProviderID  string `json:"provider_id"`
	Name        string `json:"name"`
	Role        string `json:"role"`
	Country     string `json:"country,omitempty"`
	LogoImageID string `json:"logo_image_id,omitempty"`
}

type ProjectedCredit struct {
	Provider         string `json:"provider"`
	ProviderPersonID string `json:"provider_person_id"`
	DisplayName      string `json:"display_name"`
	CreditType       string `json:"credit_type"`
	Character        string `json:"character,omitempty"`
	Department       string `json:"department,omitempty"`
	Job              string `json:"job,omitempty"`
	Order            int    `json:"order,omitempty"`
	ProfileImageID   string `json:"profile_image_id,omitempty"`
}

type ProjectedCollectionMember struct {
	ProviderID string `json:"provider_id"`
	Title      string `json:"title"`
	Year       int    `json:"year,omitempty"`
	ImageID    string `json:"image_id,omitempty"`
	Order      int    `json:"order"`
}

type ProjectedCollection struct {
	ProviderID string                      `json:"provider_id"`
	Name       string                      `json:"name"`
	Overview   string                      `json:"overview,omitempty"`
	Images     []ProjectedImage            `json:"images"`
	Members    []ProjectedCollectionMember `json:"members"`
}

type ProjectedRecommendation struct {
	ProviderTargetID string  `json:"provider_target_id"`
	Title            string  `json:"title"`
	Year             int     `json:"year,omitempty"`
	ImageID          string  `json:"image_id,omitempty"`
	ProviderScore    float64 `json:"provider_score,omitempty"`
}

type DetailData struct {
	Titles          []LocalizedText           `json:"titles"`
	Overviews       []LocalizedText           `json:"overviews"`
	Taglines        []LocalizedText           `json:"taglines"`
	Classification  Classification            `json:"classification"`
	Release         Lifecycle                 `json:"release"`
	Measurements    Measurements              `json:"measurements"`
	Ratings         []Rating                  `json:"ratings"`
	Links           []Link                    `json:"links"`
	Videos          []Video                   `json:"videos"`
	Studios         []ProjectedCompany        `json:"studios"`
	Credits         []ProjectedCredit         `json:"credits"`
	Images          []ProjectedImage          `json:"images"`
	Collection      *ProjectedCollection      `json:"collection,omitempty"`
	Recommendations []ProjectedRecommendation `json:"recommendations"`
}

type DetailDocument struct {
	SchemaVersion     int                     `json:"schema_version"`
	ProjectionVersion int64                   `json:"projection_version"`
	ID                string                  `json:"id"`
	Kind              string                  `json:"kind"`
	Slug              string                  `json:"slug"`
	Display           Display                 `json:"display"`
	ExternalIDs       []ExternalID            `json:"external_ids"`
	Data              DetailData              `json:"data"`
	Freshness         Freshness               `json:"freshness"`
	Provenance        map[string][]Provenance `json:"provenance"`
}

type SummaryDocument struct {
	SchemaVersion     int     `json:"schema_version"`
	ProjectionVersion int64   `json:"projection_version"`
	ID                string  `json:"id"`
	Kind              string  `json:"kind"`
	Slug              string  `json:"slug"`
	Display           Display `json:"display"`
	Attributes        struct {
		Status           string   `json:"status,omitempty"`
		Genres           []string `json:"genres"`
		Countries        []string `json:"countries"`
		OriginalLanguage string   `json:"original_language,omitempty"`
	} `json:"attributes"`
	ExternalIDs []ExternalID `json:"external_ids"`
	Freshness   Freshness    `json:"freshness"`
}

type Provenance struct {
	Provider           string `json:"provider"`
	NormalizedRecordID string `json:"normalized_record_id"`
	ObservationID      string `json:"observation_id"`
}

type RecordInput struct {
	ID     string
	Record NormalizedRecordV1
}

type Projection struct {
	Detail      DetailDocument
	Summary     SummaryDocument
	SearchNames []LocalizedText
}

func Combine(entityID, slug string, projectionVersion int64, records []RecordInput, imageIDs map[string]string, now time.Time) Projection {
	sort.Slice(records, func(i, j int) bool {
		left := providerPriority(records[i].Record.ProviderRecord.Provider)
		right := providerPriority(records[j].Record.ProviderRecord.Provider)
		if left != right {
			return left < right
		}
		return records[i].ID < records[j].ID
	})
	freshUntil := now.Add(7 * 24 * time.Hour)
	detail := DetailDocument{
		SchemaVersion: ProjectionSchemaVersion, ProjectionVersion: projectionVersion,
		ID: entityID, Kind: "movie", Slug: slug,
		Freshness:  Freshness{State: "fresh", UpdatedAt: now, FreshUntil: freshUntil, Providers: map[string]ProviderFreshness{}},
		Provenance: map[string][]Provenance{},
	}
	seenIDs := map[string]bool{}
	seenText := map[string]bool{}
	seenRatings := map[string]bool{}
	seenImages := map[string]bool{}
	seenReleases := map[string]bool{}
	seenLinks := map[string]bool{}
	seenVideos := map[string]bool{}
	seenCompanies := map[string]bool{}
	seenCredits := map[string]bool{}
	seenRecommendations := map[string]bool{}
	for _, input := range records {
		record := input.Record
		detail.Freshness.Providers[record.ProviderRecord.Provider] = ProviderFreshness{State: "fresh", LastSuccessAt: record.ProviderRecord.ObservedAt, LastObservationID: record.ProviderRecord.PrimaryObservationID}
		provenance := Provenance{Provider: record.ProviderRecord.Provider, NormalizedRecordID: input.ID, ObservationID: record.ProviderRecord.PrimaryObservationID}
		addProvenance(detail.Provenance, "identity.external_ids", provenance)
		for _, candidate := range record.IdentityCandidates {
			key := candidate.Provider + ":" + candidate.Namespace + ":" + candidate.NormalizedValue
			if !seenIDs[key] {
				seenIDs[key] = true
				detail.ExternalIDs = append(detail.ExternalIDs, ExternalID{Provider: candidate.Provider, Namespace: candidate.Namespace, Value: candidate.NormalizedValue})
			}
		}
		for _, title := range record.Titles {
			key := strings.ToLower(title.Value) + ":" + title.Language + ":" + title.Country + ":" + title.Type
			if title.Value != "" && !seenText["title:"+key] {
				seenText["title:"+key] = true
				detail.Data.Titles = append(detail.Data.Titles, title)
			}
			if detail.Display.Title == "" && (title.Type == "display" || title.Type == "translated") {
				detail.Display.Title = title.Value
				addProvenance(detail.Provenance, "display.title", provenance)
			}
			if detail.Display.OriginalTitle == "" && title.Type == "original" {
				detail.Display.OriginalTitle = title.Value
				addProvenance(detail.Provenance, "display.original_title", provenance)
			}
		}
		for _, overview := range record.Descriptions {
			key := strings.ToLower(overview.Value) + ":" + overview.Language + ":" + overview.Country
			if overview.Value != "" && !seenText["overview:"+key] {
				seenText["overview:"+key] = true
				detail.Data.Overviews = append(detail.Data.Overviews, overview)
				addProvenance(detail.Provenance, "data.overviews", provenance)
			}
		}
		for _, tagline := range record.Taglines {
			key := strings.ToLower(tagline.Value) + ":" + tagline.Language + ":" + tagline.Country
			if tagline.Value != "" && !seenText["tagline:"+key] {
				seenText["tagline:"+key] = true
				detail.Data.Taglines = append(detail.Data.Taglines, tagline)
			}
		}
		if detail.Data.Classification.ProviderMediaType == "" {
			detail.Data.Classification.ProviderMediaType = record.Classification.ProviderMediaType
		}
		if detail.Data.Classification.OriginalLanguage == "" {
			detail.Data.Classification.OriginalLanguage = record.Classification.OriginalLanguage
		}
		detail.Data.Classification.Genres = unionStrings(detail.Data.Classification.Genres, record.Classification.Genres)
		detail.Data.Classification.Keywords = unionStrings(detail.Data.Classification.Keywords, record.Classification.Keywords)
		detail.Data.Classification.SpokenLanguages = unionStrings(detail.Data.Classification.SpokenLanguages, record.Classification.SpokenLanguages)
		detail.Data.Classification.Countries = unionStrings(detail.Data.Classification.Countries, record.Classification.Countries)
		detail.Data.Classification.AnimationEvidence = detail.Data.Classification.AnimationEvidence || record.Classification.AnimationEvidence
		if len(record.Classification.Genres)+len(record.Classification.Keywords) > 0 {
			addProvenance(detail.Provenance, "data.classification", provenance)
		}
		if detail.Data.Release.RawStatus == "" && record.Lifecycle.RawStatus != "" {
			detail.Data.Release.RawStatus = record.Lifecycle.RawStatus
			detail.Data.Release.NormalizedStatus = record.Lifecycle.NormalizedStatus
			addProvenance(detail.Provenance, "data.release.status", provenance)
		}
		for _, event := range record.Lifecycle.ReleaseEvents {
			key := event.Country + ":" + event.Type + ":" + event.Date + ":" + event.Certification
			if !seenReleases[key] {
				seenReleases[key] = true
				detail.Data.Release.ReleaseEvents = append(detail.Data.Release.ReleaseEvents, event)
				addProvenance(detail.Provenance, "data.release.events", provenance)
			}
		}
		if detail.Data.Measurements.RuntimeMinutes == nil && record.Measurements.RuntimeMinutes != nil {
			detail.Data.Measurements.RuntimeMinutes = record.Measurements.RuntimeMinutes
			addProvenance(detail.Provenance, "data.measurements.runtime_minutes", provenance)
		}
		if detail.Data.Measurements.Budget == nil && record.Measurements.Budget != nil {
			detail.Data.Measurements.Budget = record.Measurements.Budget
			addProvenance(detail.Provenance, "data.measurements.budget", provenance)
		}
		if detail.Data.Measurements.Revenue == nil && record.Measurements.Revenue != nil {
			detail.Data.Measurements.Revenue = record.Measurements.Revenue
			addProvenance(detail.Provenance, "data.measurements.revenue", provenance)
		}
		if detail.Data.Measurements.Popularity == nil && record.Measurements.Popularity != nil {
			detail.Data.Measurements.Popularity = record.Measurements.Popularity
			addProvenance(detail.Provenance, "data.measurements.popularity", provenance)
		}
		for _, rating := range record.Ratings {
			if !seenRatings[rating.System] {
				seenRatings[rating.System] = true
				detail.Data.Ratings = append(detail.Data.Ratings, rating)
				addProvenance(detail.Provenance, "data.ratings."+rating.System, provenance)
			}
		}
		for _, link := range record.Links {
			key := link.Kind + ":" + link.Value
			if !seenLinks[key] {
				seenLinks[key] = true
				detail.Data.Links = append(detail.Data.Links, link)
			}
		}
		for _, video := range record.Videos {
			key := video.Host + ":" + video.Key
			if !seenVideos[key] {
				seenVideos[key] = true
				detail.Data.Videos = append(detail.Data.Videos, video)
			}
		}
		for _, company := range record.Companies {
			key := record.ProviderRecord.Provider + ":" + company.ProviderID + ":" + company.Role
			if seenCompanies[key] {
				continue
			}
			seenCompanies[key] = true
			detail.Data.Studios = append(detail.Data.Studios, ProjectedCompany{ProviderID: company.ProviderID, Name: company.Name, Role: company.Role, Country: company.Country, LogoImageID: imageIDs[AuxiliaryImageKey(record.ProviderRecord.Provider, "company", company.ProviderID, company.LogoURL)]})
		}
		for _, credit := range record.Credits {
			key := record.ProviderRecord.Provider + ":" + credit.ProviderPersonID + ":" + credit.CreditType + ":" + credit.Character + ":" + credit.Department + ":" + credit.Job
			if seenCredits[key] {
				continue
			}
			seenCredits[key] = true
			detail.Data.Credits = append(detail.Data.Credits, ProjectedCredit{Provider: record.ProviderRecord.Provider, ProviderPersonID: credit.ProviderPersonID, DisplayName: credit.DisplayName, CreditType: credit.CreditType, Character: credit.Character, Department: credit.Department, Job: credit.Job, Order: credit.Order, ProfileImageID: imageIDs[AuxiliaryImageKey(record.ProviderRecord.Provider, "credit", credit.ProviderPersonID, credit.ProfileURL)]})
		}
		for _, candidate := range record.Images {
			key := record.ProviderRecord.Provider + ":" + candidate.Class + ":" + candidate.ProviderImageID
			if seenImages[key] {
				continue
			}
			seenImages[key] = true
			projected := ProjectedImage{ID: imageIDs[key], Class: candidate.Class, Language: candidate.Language, Width: candidate.Width, Height: candidate.Height, Provider: record.ProviderRecord.Provider, ProviderScore: candidate.ProviderScore}
			detail.Data.Images = append(detail.Data.Images, projected)
			if detail.Display.ImageID == "" && candidate.Class == "poster" && projected.ID != "" {
				detail.Display.ImageID = projected.ID
				addProvenance(detail.Provenance, "display.image_id", provenance)
			}
		}
		if record.Collection != nil {
			collection := &ProjectedCollection{ProviderID: record.Collection.ProviderID, Name: record.Collection.Name, Overview: record.Collection.Overview}
			for _, image := range record.Collection.Images {
				key := AuxiliaryImageKey(record.ProviderRecord.Provider, "collection_"+image.Class, record.Collection.ProviderID, image.SourceURL)
				collection.Images = append(collection.Images, ProjectedImage{ID: imageIDs[key], Class: image.Class, Language: image.Language, Width: image.Width, Height: image.Height, Provider: record.ProviderRecord.Provider, ProviderScore: image.ProviderScore})
			}
			for _, member := range record.Collection.Members {
				collection.Members = append(collection.Members, ProjectedCollectionMember{ProviderID: member.ProviderID, Title: member.Title, Year: member.Year, ImageID: imageIDs[AuxiliaryImageKey(record.ProviderRecord.Provider, "collection_member", member.ProviderID, member.ImageURL)], Order: member.Order})
			}
			detail.Data.Collection = collection
		}
		for _, recommendation := range record.Recommendations {
			key := record.ProviderRecord.Provider + ":" + recommendation.ProviderTargetID
			if seenRecommendations[key] {
				continue
			}
			seenRecommendations[key] = true
			detail.Data.Recommendations = append(detail.Data.Recommendations, ProjectedRecommendation{ProviderTargetID: recommendation.ProviderTargetID, Title: recommendation.Title, Year: recommendation.Year, ImageID: imageIDs[AuxiliaryImageKey(record.ProviderRecord.Provider, "recommendation", recommendation.ProviderTargetID, recommendation.ImageURL)], ProviderScore: recommendation.ProviderScore})
		}
	}
	if detail.Display.Title == "" && len(detail.Data.Titles) > 0 {
		detail.Display.Title = detail.Data.Titles[0].Value
	}
	detail.Display.Year = primaryYear(detail.Data.Release.ReleaseEvents)
	sort.Slice(detail.ExternalIDs, func(i, j int) bool {
		return detail.ExternalIDs[i].Provider+detail.ExternalIDs[i].Namespace < detail.ExternalIDs[j].Provider+detail.ExternalIDs[j].Namespace
	})
	summary := SummaryDocument{SchemaVersion: ProjectionSchemaVersion, ProjectionVersion: projectionVersion, ID: entityID, Kind: "movie", Slug: slug, Display: detail.Display, ExternalIDs: detail.ExternalIDs, Freshness: detail.Freshness}
	summary.Attributes.Status = detail.Data.Release.NormalizedStatus
	summary.Attributes.Genres = detail.Data.Classification.Genres
	summary.Attributes.Countries = detail.Data.Classification.Countries
	summary.Attributes.OriginalLanguage = detail.Data.Classification.OriginalLanguage
	return Projection{Detail: detail, Summary: summary, SearchNames: detail.Data.Titles}
}

func AuxiliaryImageKey(provider, scope, providerID, sourceURL string) string {
	if sourceURL == "" {
		return ""
	}
	return provider + ":" + scope + ":" + providerID + ":" + sourceURL
}

func providerPriority(provider string) int {
	switch provider {
	case "tmdb":
		return 10
	case "omdb":
		return 20
	case "tvdb":
		return 30
	case "fanart":
		return 40
	default:
		return 100
	}
}

func unionStrings(existing, incoming []string) []string {
	seen := make(map[string]bool, len(existing))
	for _, value := range existing {
		seen[strings.ToLower(value)] = true
	}
	for _, value := range incoming {
		key := strings.ToLower(value)
		if value != "" && !seen[key] {
			seen[key] = true
			existing = append(existing, value)
		}
	}
	return existing
}

func addProvenance(target map[string][]Provenance, scope string, value Provenance) {
	for _, existing := range target[scope] {
		if existing == value {
			return
		}
	}
	target[scope] = append(target[scope], value)
}

func primaryYear(events []ReleaseEvent) int {
	result := 0
	for _, event := range events {
		if len(event.Date) < 4 {
			continue
		}
		year := 0
		for _, char := range event.Date[:4] {
			if char < '0' || char > '9' {
				year = 0
				break
			}
			year = year*10 + int(char-'0')
		}
		if year > 0 && (result == 0 || year < result) {
			result = year
		}
	}
	return result
}
