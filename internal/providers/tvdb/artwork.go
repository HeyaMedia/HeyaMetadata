package tvdb

// ArtworkClass translates TVDB's global artwork type registry into Heya's
// semantic image classes. Keep this mapping centralized: the numeric IDs are
// shared by movies, series, seasons, episodes, people, companies, and lists.
func ArtworkClass(artworkType int) string {
	switch artworkType {
	case 1, 6, 16:
		return "banner"
	case 2, 7, 14, 27:
		return "poster"
	case 3, 8, 15:
		return "backdrop"
	case 5, 10, 18, 19, 26:
		return "icon"
	case 11, 12:
		return "still"
	case 13:
		return "profile"
	case 20, 21:
		return "cinemagraph"
	case 22, 24:
		return "clearart"
	case 23, 25:
		return "clearlogo"
	default:
		return ""
	}
}
