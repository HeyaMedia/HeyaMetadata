package jobs

import (
	"errors"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/discovery"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func TestArtistCatalogReleaseEvidenceKeepsSupportedExactReleaseIDs(t *testing.T) {
	t.Parallel()
	request := discovery.NormalizeRequest(discovery.Request{Kind: discovery.KindArtist, Hints: discovery.Hints{Releases: []discovery.ReleaseHint{
		{Title: "Freaks Out", Identifiers: []discovery.Identifier{{Scheme: "itunes_album", Value: "1630125755"}, {Scheme: "deezer_album", Value: "123"}, {Scheme: "spotify", Value: "ignored"}}},
	}}})
	got := ArtistCatalogReleaseEvidence(request)
	if len(got) != 2 || got[0].Provider != "apple" || got[0].Namespace != "album" || got[0].ID != "1630125755" || got[1].Provider != "deezer" || got[1].ID != "123" {
		t.Fatalf("release evidence: %#v", got)
	}
}

func TestDiscoveryRunFailsOnlyOnTerminalWorkerError(t *testing.T) {
	t.Parallel()
	job := &river.Job[DiscoverySearchArgs]{JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 4}}
	providerFailure := errors.New("temporary upstream failure")
	if shouldFailDiscoveryRun(job, providerFailure) {
		t.Fatal("first retryable attempt was exposed as a failed discovery")
	}

	job.Attempt = job.MaxAttempts
	if !shouldFailDiscoveryRun(job, providerFailure) {
		t.Fatal("exhausted job did not fail its discovery")
	}

	job.Attempt = 1
	if !shouldFailDiscoveryRun(job, river.JobCancel(errors.New("terminal setup failure"))) {
		t.Fatal("cancelled job did not fail its discovery")
	}
}
