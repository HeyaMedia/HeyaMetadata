package fingerprint

import (
	"errors"
	"net/url"
	"slices"
	"testing"
)

func TestPackRoundTrip(t *testing.T) {
	want := []uint32{0, 1, 0xffffffff, 0x10203040}
	if got := Unpack(Pack(want)); !slices.Equal(got, want) {
		t.Fatalf("round trip: got %x want %x", got, want)
	}
}

func TestLandmarkTokensAreStableAndBounded(t *testing.T) {
	data := Pack([]uint32{0x10203040, 2, 3, 4, 0x10203040, 6, 7, 8})
	a, b := LandmarkTokens(data), LandmarkTokens(data)
	if !slices.Equal(a, b) {
		t.Fatalf("landmarks changed: %v %v", a, b)
	}
	if len(a) == 0 || len(a) > 4 {
		t.Fatalf("unexpected landmarks: %v", a)
	}
}

func TestPreviewURLAllowlistRejectsSSRFAndCredentials(t *testing.T) {
	for _, raw := range []string{"http://cdnt-preview.dzcdn.net/a", "https://127.0.0.1/a", "https://user:pass@audio-ssl.itunes.apple.com/a", "https://dzcdn.net.attacker.test/a"} {
		parsed, _ := url.Parse(raw)
		var permanent *PermanentError
		if err := validatePreviewURL(parsed); !errors.As(err, &permanent) {
			t.Errorf("%s: expected permanent rejection, got %v", raw, err)
		}
	}
	for _, raw := range []string{"https://audio-ssl.itunes.apple.com/a.m4a", "https://is1-ssl.mzstatic.com/a.m4a", "https://cdnt-preview.dzcdn.net/a.mp3"} {
		parsed, _ := url.Parse(raw)
		if err := validatePreviewURL(parsed); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
	}
}

func TestSourceChecksumIgnoresSignedQueryRotation(t *testing.T) {
	a := SourceChecksum("deezer", "42", "https://cdnt-preview.dzcdn.net/a.mp3?hdnea=one")
	b := SourceChecksum("deezer", "42", "https://cdnt-preview.dzcdn.net/a.mp3?hdnea=two")
	if a != b {
		t.Fatal("signed query string changed source checksum")
	}
}

func TestMatchFindsOffsetPreview(t *testing.T) {
	base := make([]uint32, 180)
	for i := range base {
		base[i] = uint32(i * 7919)
	}
	result := Match(Fingerprint{Hashes: base}, Fingerprint{Hashes: append([]uint32(nil), base[20:160]...)})
	if !result.Match || result.Overlap < MinOverlapItems {
		t.Fatalf("match: %+v", result)
	}
}
