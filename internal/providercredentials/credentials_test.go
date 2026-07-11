package providercredentials

import "testing"

func TestCredentialsUseNormalizedProviderName(t *testing.T) {
	t.Parallel()
	credentials := Credentials{APIKeys: map[string]string{"tmdb": "secret"}}
	if credentials.APIKey(" TMDB ") != "secret" {
		t.Fatal("provider API key lookup was not normalized")
	}
}
