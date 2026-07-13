package musiccatalog

import "testing"

func TestStrongEvidenceMatchesNormalizedBarcode(t *testing.T) {
	t.Parallel()
	a := detailEvidence{Barcode: normalizedBarcode("0 602547670342"), ISRCs: map[string]bool{}}
	b := detailEvidence{Barcode: normalizedBarcode("602547670342"), ISRCs: map[string]bool{}}
	reason, confidence, ok := strongEvidenceMatch(a, b)
	if !ok || reason != "shared_barcode" || confidence < .99 {
		t.Fatalf("barcode evidence: %q/%v/%v", reason, confidence, ok)
	}
}

func TestStrongEvidenceRequiresEnoughSharedISRCs(t *testing.T) {
	t.Parallel()
	a := detailEvidence{ISRCs: map[string]bool{"A": true, "B": true, "C": true}}
	b := detailEvidence{ISRCs: map[string]bool{"A": true, "B": true, "X": true}}
	if reason, _, ok := strongEvidenceMatch(a, b); !ok || reason != "shared_isrc_trackset" {
		t.Fatalf("ISRC evidence was not accepted: %q/%v", reason, ok)
	}
	b = detailEvidence{ISRCs: map[string]bool{"A": true, "X": true, "Y": true}}
	if _, _, ok := strongEvidenceMatch(a, b); ok {
		t.Fatal("one shared recording incorrectly established an album match")
	}
}

func TestStrongEvidenceMatchesOrderedTracklistAndDuration(t *testing.T) {
	t.Parallel()
	a := detailEvidence{ISRCs: map[string]bool{}, Tracks: []trackEvidence{{Title: "普変", DurationMS: 190000}, {Title: "デリート", DurationMS: 180000}}}
	b := detailEvidence{ISRCs: map[string]bool{}, Tracks: []trackEvidence{{Title: "Fuhen", DurationMS: 190500}, {Title: "Delete", DurationMS: 180500}}}
	// Pure semantic translation is intentionally not guessed without ISRCs.
	if _, _, ok := strongEvidenceMatch(a, b); ok {
		t.Fatal("unrelated translated titles were guessed from position alone")
	}
	b = detailEvidence{ISRCs: map[string]bool{}, Tracks: []trackEvidence{{Title: "Shoka", DurationMS: 190500}, {Title: "Delete", DurationMS: 180500}}}
	a = detailEvidence{ISRCs: map[string]bool{}, Tracks: []trackEvidence{{Title: "初夏", DurationMS: 190000}, {Title: "Delete", DurationMS: 180000}}}
	if reason, confidence, ok := strongEvidenceMatch(a, b); !ok || reason != "ordered_tracklist_duration" || confidence < .9 {
		t.Fatalf("tracklist evidence: %q/%v/%v", reason, confidence, ok)
	}
}

func TestStrongEvidenceChecksEveryIssuedMusicBrainzTracklist(t *testing.T) {
	t.Parallel()
	storefront := detailEvidence{ISRCs: map[string]bool{}, Tracks: []trackEvidence{{Title: "First", DurationMS: 180000}, {Title: "Second", DurationMS: 210000}}}
	musicbrainz := detailEvidence{ISRCs: map[string]bool{}, Tracklists: [][]trackEvidence{
		{{Title: "Unrelated", DurationMS: 120000}, {Title: "Edition", DurationMS: 130000}},
		{{Title: "First", DurationMS: 180500}, {Title: "Second", DurationMS: 210500}},
	}}
	if reason, _, ok := strongEvidenceMatch(storefront, musicbrainz); !ok || reason != "ordered_tracklist_duration" {
		t.Fatalf("issued release evidence was not matched: %q/%v", reason, ok)
	}
}

func TestDetailEvidenceCanBridgeStorefrontTypeDisagreement(t *testing.T) {
	t.Parallel()
	storefront := cluster{Sources: []candidate{{Provider: "apple", Title: "Possibly Maybe", Date: "1996-01-01", Kind: "ep"}}}
	spine := cluster{Sources: []candidate{{Provider: "musicbrainz", Title: "Possibly Maybe", Date: "1996-10-28", Kind: "single"}}}
	if !detailEvidenceCandidates(storefront, spine) {
		t.Fatal("provider type disagreement prevented a detail-evidence comparison")
	}
	spine.Sources[0].Date = "1997-10-28"
	if detailEvidenceCandidates(storefront, spine) {
		t.Fatal("different release years became detail-evidence candidates")
	}
}
