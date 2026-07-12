package fingerprintmatch

import "testing"

func TestRequestHashCoversBothEncodingsAndDuration(t *testing.T) {
	base := Request{RawFingerprint: []byte{1, 2, 3, 4}, AcoustIDFingerprint: "abc", DurationMS: 30000}
	hash := RequestHash(base)
	for _, changed := range []Request{{RawFingerprint: []byte{1, 2, 3, 5}, AcoustIDFingerprint: "abc", DurationMS: 30000}, {RawFingerprint: base.RawFingerprint, AcoustIDFingerprint: "abd", DurationMS: 30000}, {RawFingerprint: base.RawFingerprint, AcoustIDFingerprint: "abc", DurationMS: 30001}} {
		if RequestHash(changed) == hash {
			t.Fatalf("request hash ignored change: %+v", changed)
		}
	}
}
