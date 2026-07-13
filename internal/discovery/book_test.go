package discovery

import "testing"

func TestPublicationSubjectClassificationIsConservative(t *testing.T) {
	t.Parallel()
	if !publicationSubjectsMatch(KindMangaVolume, []string{"Japanese Manga", "Graphic novels"}) {
		t.Fatal("manga subjects were not recognized")
	}
	if publicationSubjectsMatch(KindComicVolume, []string{"Japanese Manga", "Comic books"}) {
		t.Fatal("manga leaked into conventional comics")
	}
	if !publicationSubjectsMatch(KindComicVolume, []string{"Superhero comic books", "Sequential art"}) {
		t.Fatal("comic subjects were not recognized")
	}
	if publicationSubjectsMatch(KindMangaVolume, []string{"Fantasy fiction"}) || publicationSubjectsMatch(KindComicVolume, []string{"Fantasy fiction"}) {
		t.Fatal("ordinary book was guessed to be a sequential publication")
	}
}
