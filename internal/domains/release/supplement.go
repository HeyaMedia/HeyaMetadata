package release

import (
	"strconv"
	"strings"

	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
	"github.com/HeyaMedia/HeyaMetadata/internal/textmatch"
)

func FromReleaseGroup(source rgdomain.NormalizedRecordV1) NormalizedRecord {
	r := NormalizedRecord{ProviderRecord: ProviderRecord{Provider: source.ProviderRecord.Provider, Namespace: source.ProviderRecord.Namespace, Value: source.ProviderRecord.Value, PrimaryObservationID: source.ProviderRecord.PrimaryObservationID, ObservedAt: source.ProviderRecord.ObservedAt, NormalizerVersion: source.ProviderRecord.NormalizerVersion, SchemaVersion: source.ProviderRecord.SchemaVersion}}
	if len(source.Editions) > 0 {
		e := source.Editions[0]
		r.Title = e.Title
		r.Barcode = e.Barcode
		r.Date = e.Date.Value
		r.Country = e.Country
		r.Link = e.Link
		r.ExternalIDs = []ExternalID{{Provider: e.Provider, Namespace: e.Namespace, Value: e.ProviderID, Evidence: "verified_catalog_edition"}}
	} else if len(source.Titles) > 0 {
		r.Title = source.Titles[0].Value
	}
	for _, credit := range source.ArtistCredits {
		r.ArtistCredits = append(r.ArtistCredits, ArtistCredit{Position: credit.Position, Name: credit.Name, JoinPhrase: credit.JoinPhrase, ArtistProvider: credit.ArtistProvider, ArtistNamespace: credit.ArtistNamespace, ArtistID: credit.ArtistID, ArtistName: credit.ArtistName})
	}
	byDisc := map[int]int{}
	for _, track := range source.Tracks {
		disc := track.DiscNumber
		if disc < 1 {
			disc = 1
		}
		index, ok := byDisc[disc]
		if !ok {
			index = len(r.Media)
			byDisc[disc] = index
			r.Media = append(r.Media, Medium{Position: disc})
		}
		m := &r.Media[index]
		m.Tracks = append(m.Tracks, Track{ProviderID: track.ProviderID, Position: track.Position, Number: strconv.Itoa(track.Number), Sequence: len(m.Tracks) + 1, Title: track.Title, DurationMS: track.DurationMS, Recording: Recording{Provider: source.ProviderRecord.Provider, Namespace: "track", ProviderID: track.ProviderID, Title: track.Title, DurationMS: track.DurationMS, ISRCs: nonEmpty(track.ISRC)}})
		m.TrackCount = len(m.Tracks)
	}
	return r
}
func Compatible(spine, candidate NormalizedRecord) bool {
	if normalizeBarcode(spine.Barcode) == "" || normalizeBarcode(spine.Barcode) != normalizeBarcode(candidate.Barcode) {
		return false
	}
	left, right := trackTotal(spine), trackTotal(candidate)
	if left == 0 || right == 0 || left != right {
		return false
	}
	leftYear, rightYear := yearValue(spine.Date), yearValue(candidate.Date)
	if leftYear > 0 && leftYear == rightYear {
		return true
	}
	return textmatch.EquivalentRelease(spine.Title, leftYear, candidate.Title, rightYear)
}
func MatchTrack(spine Track, candidate NormalizedRecord, disc int) *Track {
	for mi := range candidate.Media {
		for ti := range candidate.Media[mi].Tracks {
			track := &candidate.Media[mi].Tracks[ti]
			if sharedISRC(spine.Recording.ISRCs, track.Recording.ISRCs) {
				return track
			}
		}
	}
	for mi := range candidate.Media {
		medium := &candidate.Media[mi]
		if medium.Position != disc {
			continue
		}
		for ti := range medium.Tracks {
			track := &medium.Tracks[ti]
			if track.Sequence == spine.Sequence && trackTitle(spine.Title) == trackTitle(track.Title) && durationCompatible(spine.DurationMS, track.DurationMS) {
				return track
			}
		}
	}
	return nil
}
func sharedISRC(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x != "" && strings.EqualFold(x, y) {
				return true
			}
		}
	}
	return false
}
func trackTitle(v string) string {
	keys := textmatch.ReleaseKeys(strings.NewReplacer(" (remastered)", "", " (remaster)", "", " - remastered", "", " - remaster", "").Replace(v), 0)
	if len(keys) > 0 {
		return keys[0]
	}
	return ""
}
func durationCompatible(a, b int64) bool { return a == 0 || b == 0 || a-b < 3000 && b-a < 3000 }
func trackTotal(r NormalizedRecord) int {
	n := 0
	for _, m := range r.Media {
		n += len(m.Tracks)
	}
	return n
}
func normalizeBarcode(v string) string {
	var b strings.Builder
	for _, r := range v {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return strings.TrimLeft(b.String(), "0")
}
func yearValue(v string) int {
	if len(v) < 4 {
		return 0
	}
	n, _ := strconv.Atoi(v[:4])
	return n
}
func nonEmpty(v string) []string {
	v = strings.ToUpper(strings.TrimSpace(v))
	if v == "" {
		return nil
	}
	return []string{v}
}
