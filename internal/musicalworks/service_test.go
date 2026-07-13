package musicalworks

import (
	"errors"
	"testing"
)

func TestNormalizeOpenOpusWork(t *testing.T) {
	t.Parallel()
	body := []byte(`{"status":{"success":"true"},"composer":{"id":"145","name":"Beethoven","complete_name":"Ludwig van Beethoven","epoch":"Early Romantic"},"work":{"genre":"Orchestral","title":"Symphony no. 5 in C minor, op. 67","id":"16406","searchterms":["op 67 no. 5","op 67 no. 5"],"catalogue":"op","catalogue_number":"67","additional_number":"5"}}`)
	data, _, err := normalizeOpenOpusWork("16406", body)
	if err != nil {
		t.Fatal(err)
	}
	if data.Title != "Symphony no. 5 in C minor, op. 67" || data.Composer.Name != "Ludwig van Beethoven" {
		t.Fatalf("unexpected work: %+v", data)
	}
	if data.Catalogue.System != "op" || data.Catalogue.Number != "67" || data.Catalogue.AdditionalNumber != "5" {
		t.Fatalf("unexpected catalogue: %+v", data.Catalogue)
	}
	if len(data.SearchTerms) != 1 {
		t.Fatalf("search terms were not deduplicated: %#v", data.SearchTerms)
	}
}

func TestNormalizeOpenOpusWorkRejectsLogicalMissAndWrongIdentity(t *testing.T) {
	t.Parallel()
	if _, _, err := normalizeOpenOpusWork("16406", []byte(`{"status":{"success":"false"}}`)); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("logical miss: %v", err)
	}
	if _, _, err := normalizeOpenOpusWork("16406", []byte(`{"status":{"success":"true"},"work":{"id":"1","title":"Other"}}`)); err == nil {
		t.Fatal("wrong provider identity was accepted")
	}
}
