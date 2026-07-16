package cmd

import (
	"strings"
	"testing"
)

const testArtistID = "cac9d9f2-e1ed-4e9b-93f0-0a7e8e6fc8aa"

func TestParseArtistReference(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		siteURL string
		want    string
		wantErr string
	}{
		{name: "UUID", input: " CAC9D9F2-E1ED-4E9B-93F0-0A7E8E6FC8AA ", siteURL: "https://heya.media", want: testArtistID},
		{name: "production URL", input: "https://heya.media/artists/" + testArtistID, siteURL: "https://heya.media", want: testArtistID},
		{name: "production URL with page state", input: "https://heya.media/artists/" + testArtistID + "/?tab=discography#albums", siteURL: "https://heya.media", want: testArtistID},
		{name: "configured development site", input: "http://localhost:3030/artists/" + testArtistID, siteURL: "http://localhost:3030", want: testArtistID},
		{name: "wrong host", input: "https://example.com/artists/" + testArtistID, siteURL: "https://heya.media", wantErr: "not the configured Heya site"},
		{name: "wrong collection", input: "https://heya.media/movies/" + testArtistID, siteURL: "https://heya.media", wantErr: "/artists/{heya-id}"},
		{name: "invalid URL UUID", input: "https://heya.media/artists/not-a-uuid", siteURL: "https://heya.media", wantErr: "invalid Heya ID"},
		{name: "unrecognized input", input: "Ado", siteURL: "https://heya.media", wantErr: "URL or UUID"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseArtistReference(test.input, test.siteURL)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("parseArtistReference() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestArtistCommandExposesUpdate(t *testing.T) {
	t.Parallel()
	command, _, err := newArtistCommand().Find([]string{"update"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Name() != "update" || !strings.Contains(command.Use, "Heya artist URL or UUID") {
		t.Fatalf("unexpected update command: name=%q use=%q", command.Name(), command.Use)
	}
}
