package jobs

import "github.com/riverqueue/river"

const (
	MusicQueue = "music"
	MovieQueue = "movie"
	TVQueue    = "tv"
	AnimeQueue = "anime"
	BooksQueue = "books"

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
