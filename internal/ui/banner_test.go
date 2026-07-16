package ui

import (
	"strings"
	"testing"
)

func TestHelpBannerListsArtistCommands(t *testing.T) {
	t.Parallel()
	banner := HelpBanner("test")
	if !strings.Contains(banner, "artist") || !strings.Contains(banner, "Ingest, update, and inspect canonical artists") {
		t.Fatalf("artist command missing from help banner: %q", banner)
	}
}
