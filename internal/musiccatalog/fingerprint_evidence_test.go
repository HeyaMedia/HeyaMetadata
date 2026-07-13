package musiccatalog

import "testing"

func TestFingerprintCoverageRequiresAlbumBreadth(t *testing.T) {
	t.Parallel()
	if _, ok := fingerprintCoverage(1, 10, .99); ok {
		t.Fatal("one recording established an album bridge")
	}
	if confidence, ok := fingerprintCoverage(5, 10, 4.75); !ok || confidence < .9 {
		t.Fatalf("broad fingerprint coverage rejected: %v/%v", confidence, ok)
	}
}

func TestFingerprintCoverageAllowsTrueSingle(t *testing.T) {
	t.Parallel()
	if confidence, ok := fingerprintCoverage(1, 1, .91); !ok || confidence != .91 {
		t.Fatalf("single fingerprint coverage: %v/%v", confidence, ok)
	}
}

func TestFingerprintCoverageRejectsWeakAudioMatch(t *testing.T) {
	t.Parallel()
	if _, ok := fingerprintCoverage(2, 2, 1.6); ok {
		t.Fatal("weak average fingerprint confidence was accepted")
	}
}
