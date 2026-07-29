package jobs

import "github.com/riverqueue/river"

const (
	MusicQueue = "music"
	// MusicCatalogQueue isolates artist catalog syncs: they run for around a
	// minute each and River serves strict priority within a queue, so leaving
	// them in MusicQueue lets a refresh wave occupy every slot and starve the
	// scheduled release-group and recording work behind it indefinitely.
	MusicCatalogQueue = "music_catalog"
	MovieQueue        = "movie"
	TVQueue           = "tv"
	AnimeQueue        = "anime"
	BooksQueue        = "books"

	// Legacy/shared queues remain configured during rolling upgrades so work
	// enqueued by the previous release can drain safely. Migration 0056 moves
	// all waiting domain work onto the queues above.
	BackgroundQueue = "background"
	CatalogQueue    = "catalog"
)

// MetadataQueueForKind is the one routing table used by generic discovery.
// Domain-specific job types set the same queue directly in their InsertOpts.
func MetadataQueueForKind(kind string) string {
	switch kind {
	case "artist", "release_group", "release", "recording", "musical_work":
		return MusicQueue
	case "movie":
		return MovieQueue
	case "tv_show", "season", "episode":
		return TVQueue
	case "anime":
		return AnimeQueue
	case "book_work", "manga", "manga_volume", "comic_volume":
		return BooksQueue
	default:
		return river.QueueDefault
	}
}
