package heyametadata

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/HeyaMedia/HeyaMetadata/internal/books"
	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
	moviedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/movie"
	releasedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/release"
	releasegroupdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
	"github.com/HeyaMedia/HeyaMetadata/internal/manga"
	"github.com/HeyaMedia/HeyaMetadata/internal/musicalworks"
	"github.com/HeyaMedia/HeyaMetadata/internal/people"
)

// CanonicalKind is the stable discriminator used by canonical entity
// documents. Slugs and provider identifiers are presentation/evidence only;
// consumers should persist the document UUID together with this kind.
type CanonicalKind string

const (
	CanonicalKindMovie        CanonicalKind = "movie"
	CanonicalKindTVShow       CanonicalKind = "tv_show"
	CanonicalKindAnime        CanonicalKind = "anime"
	CanonicalKindArtist       CanonicalKind = "artist"
	CanonicalKindReleaseGroup CanonicalKind = "release_group"
	CanonicalKindRelease      CanonicalKind = "release"
	CanonicalKindRecording    CanonicalKind = "recording"
	CanonicalKindMusicalWork  CanonicalKind = "musical_work"
	CanonicalKindBookWork     CanonicalKind = "book_work"
	CanonicalKindBookEdition  CanonicalKind = "book_edition"
	CanonicalKindAuthor       CanonicalKind = "author"
	CanonicalKindManga        CanonicalKind = "manga"
	CanonicalKindMangaVolume  CanonicalKind = "manga_volume"
	CanonicalKindMangaEdition CanonicalKind = "manga_edition"
	CanonicalKindComicVolume  CanonicalKind = "comic_volume"
	CanonicalKindComicEdition CanonicalKind = "comic_edition"
	CanonicalKindPerson       CanonicalKind = "person"
)

// CanonicalDocument is implemented by every kind-specific document returned
// by DecodeCanonicalDocument. Use a type switch to consume domain fields while
// retaining an exhaustive discriminator at the API boundary.
type CanonicalDocument interface {
	DocumentKind() CanonicalKind
}

// These exported document types intentionally mirror the canonical projection
// types served by HeyaMetadata. They provide a supported decoding surface while
// the generated OpenAPI response must remain polymorphic.
type CanonicalMovieDocument moviedomain.DetailDocument
type CanonicalArtistDocument artistdomain.DetailDocument
type CanonicalReleaseGroupDocument releasegroupdomain.DetailDocument
type CanonicalReleaseDocument releasedomain.DetailDocument
type CanonicalRecordingDocument releasedomain.RecordingDocument
type CanonicalMusicalWorkDocument musicalworks.Document
type CanonicalEpisodicDocument episodic.Document
type CanonicalBookDocument books.Document
type CanonicalMangaDocument manga.Document
type CanonicalPersonDocument people.PersonDocument

type CanonicalAuthorDocument struct {
	SchemaVersion     int                   `json:"schema_version"`
	ProjectionVersion int64                 `json:"projection_version"`
	ID                string                `json:"id"`
	Kind              string                `json:"kind"`
	Slug              string                `json:"slug"`
	Display           AuthorDisplay         `json:"display"`
	ExternalIDs       []CanonicalExternalID `json:"external_ids"`
	Freshness         books.Freshness       `json:"freshness"`
}

type AuthorDisplay struct {
	Name string `json:"name"`
}

type CanonicalExternalID struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

func (CanonicalMovieDocument) DocumentKind() CanonicalKind        { return CanonicalKindMovie }
func (CanonicalArtistDocument) DocumentKind() CanonicalKind       { return CanonicalKindArtist }
func (CanonicalReleaseGroupDocument) DocumentKind() CanonicalKind { return CanonicalKindReleaseGroup }
func (CanonicalReleaseDocument) DocumentKind() CanonicalKind      { return CanonicalKindRelease }
func (CanonicalRecordingDocument) DocumentKind() CanonicalKind    { return CanonicalKindRecording }
func (CanonicalMusicalWorkDocument) DocumentKind() CanonicalKind  { return CanonicalKindMusicalWork }
func (d CanonicalEpisodicDocument) DocumentKind() CanonicalKind   { return CanonicalKind(d.Kind) }
func (d CanonicalBookDocument) DocumentKind() CanonicalKind       { return CanonicalKind(d.Kind) }
func (CanonicalMangaDocument) DocumentKind() CanonicalKind        { return CanonicalKindManga }
func (CanonicalPersonDocument) DocumentKind() CanonicalKind       { return CanonicalKindPerson }
func (CanonicalAuthorDocument) DocumentKind() CanonicalKind       { return CanonicalKindAuthor }

var ErrUnsupportedCanonicalKind = errors.New("unsupported canonical document kind")

// DecodeCanonicalDocument decodes a raw entity-detail response using its kind
// discriminator. Unknown kinds return ErrUnsupportedCanonicalKind so callers
// cannot silently treat a newly introduced schema as an existing domain.
func DecodeCanonicalDocument(body []byte) (CanonicalDocument, error) {
	var header struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(body, &header); err != nil {
		return nil, fmt.Errorf("decode canonical document discriminator: %w", err)
	}
	if header.Kind == "" {
		return nil, fmt.Errorf("decode canonical document discriminator: %w", ErrUnsupportedCanonicalKind)
	}

	var document CanonicalDocument
	switch CanonicalKind(header.Kind) {
	case CanonicalKindMovie:
		document = &CanonicalMovieDocument{}
	case CanonicalKindTVShow, CanonicalKindAnime:
		document = &CanonicalEpisodicDocument{}
	case CanonicalKindArtist:
		document = &CanonicalArtistDocument{}
	case CanonicalKindReleaseGroup:
		document = &CanonicalReleaseGroupDocument{}
	case CanonicalKindRelease:
		document = &CanonicalReleaseDocument{}
	case CanonicalKindRecording:
		document = &CanonicalRecordingDocument{}
	case CanonicalKindMusicalWork:
		document = &CanonicalMusicalWorkDocument{}
	case CanonicalKindBookWork, CanonicalKindBookEdition, CanonicalKindMangaVolume,
		CanonicalKindMangaEdition, CanonicalKindComicVolume, CanonicalKindComicEdition:
		document = &CanonicalBookDocument{}
	case CanonicalKindAuthor:
		document = &CanonicalAuthorDocument{}
	case CanonicalKindManga:
		document = &CanonicalMangaDocument{}
	case CanonicalKindPerson:
		document = &CanonicalPersonDocument{}
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedCanonicalKind, header.Kind)
	}
	if err := json.Unmarshal(body, document); err != nil {
		return nil, fmt.Errorf("decode %s canonical document: %w", header.Kind, err)
	}
	return document, nil
}

// DecodeCanonicalValue is useful for polymorphic fields such as a completed
// resolution's entity value. It round-trips the generated value through JSON
// and then applies the same strict kind decoder as entity detail responses.
func DecodeCanonicalValue(value any) (CanonicalDocument, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode canonical value: %w", err)
	}
	return DecodeCanonicalDocument(body)
}
