package tvdb

import (
	"strconv"
	"strings"
)

// NormalizeTMDBID accepts the two forms TVDB currently emits for TheMovieDB
// remote IDs: the canonical numeric ID and a TMDB-style "<id>-<slug>" value.
// Only the leading positive integer is identity; the slug is presentation.
func NormalizeTMDBID(value string) string {
	value = strings.TrimSpace(value)
	if prefix, _, found := strings.Cut(value, "-"); found {
		value = prefix
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}
