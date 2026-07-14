package server

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
)

// seoRenderer injects per-route <head> metadata into the SPA shell so that
// non-JS crawlers and social scrapers receive real titles, descriptions,
// canonical links, Open Graph, Twitter Card, and JSON-LD. Every code path here
// is strictly read-only and best-effort: any error, timeout, or panic falls
// back to serving the unmodified shell.
type seoRenderer struct {
	runtime *platform.Runtime
	siteURL string
}

// entityResolveTimeout bounds the read-only resolution so a slow database can
// never stall page delivery; on timeout the plain shell is served.
const entityResolveTimeout = 1500 * time.Millisecond

// uuidPattern matches a canonical entity id in the trailing path segment.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ogTypeMeta matches the shell's static og:type tag so it can be replaced with
// the per-route value instead of duplicated.
var ogTypeMeta = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:type["'][^>]*>`)

// kindRoutes mirrors web/app/utils/kinds.ts: canonical kind -> detail-route
// base. Only kinds that have a public detail route appear here.
var kindRoutes = map[string]string{
	"movie":         "movies",
	"tv_show":       "tv",
	"anime":         "anime",
	"artist":        "artists",
	"release_group": "albums",
	"release":       "releases",
	"recording":     "recordings",
	"book_work":     "books",
	"manga":         "manga",
	"manga_volume":  "manga/volumes",
	"comic_volume":  "comics/volumes",
	"person":        "people",
	"season":        "seasons",
	"episode":       "episodes",
}

// ogTypes maps a canonical kind to its Open Graph object type.
var ogTypes = map[string]string{
	"movie":         "video.movie",
	"tv_show":       "video.tv_show",
	"anime":         "video.tv_show",
	"season":        "video.tv_show",
	"episode":       "video.episode",
	"artist":        "profile",
	"person":        "profile",
	"release_group": "music.album",
	"release":       "music.album",
	"recording":     "music.song",
	"musical_work":  "music.song",
	"book_work":     "book",
	"manga":         "book",
	"manga_volume":  "book",
	"comic_volume":  "book",
}

// jsonLDTypes maps a canonical kind to its schema.org @type.
var jsonLDTypes = map[string]string{
	"movie":         "Movie",
	"tv_show":       "TVSeries",
	"anime":         "TVSeries",
	"season":        "TVSeason",
	"episode":       "TVEpisode",
	"artist":        "MusicGroup",
	"person":        "Person",
	"release_group": "MusicAlbum",
	"release":       "MusicAlbum",
	"recording":     "MusicRecording",
	"musical_work":  "MusicComposition",
	"book_work":     "Book",
	"manga":         "Book",
	"manga_volume":  "Book",
	"comic_volume":  "ComicSeries",
}

// staticMeta is the metadata for a known non-entity route.
type staticMeta struct {
	title       string
	description string
	private     bool
}

// staticRoutes is the small table of public and private application routes.
// Private routes are marked noindex so they never enter a search index.
var staticRoutes = map[string]staticMeta{
	"/":            {title: "Heya", description: "Heya is a canonical, provenance-aware metadata service for movies, TV, anime, music, books, manga, and comics."},
	"/movies":      {title: "Movies · Heya", description: "Browse canonical movie metadata, artwork, cast, and ratings on Heya."},
	"/tv":          {title: "TV Shows · Heya", description: "Browse canonical TV series metadata, seasons, episodes, and artwork on Heya."},
	"/anime":       {title: "Anime · Heya", description: "Browse canonical anime metadata, artwork, and episode data on Heya."},
	"/books":       {title: "Books · Heya", description: "Browse canonical book metadata, editions, authors, and cover art on Heya."},
	"/manga":       {title: "Manga · Heya", description: "Browse canonical manga metadata, volumes, and cover art on Heya."},
	"/comics":      {title: "Comics · Heya", description: "Browse canonical comic metadata, volumes, and cover art on Heya."},
	"/collections": {title: "Collections · Heya", description: "Explore movie franchises and curated collections in the Heya catalog."},
	"/music":       {title: "Music · Heya", description: "Browse canonical artists, albums, and recordings with rich metadata on Heya."},
	"/browse":      {title: "Browse · Heya", description: "Browse the full Heya catalog across movies, TV, anime, music, books, manga, and comics."},
	"/search":      {title: "Search · Heya", description: "Search canonical metadata across every domain in the Heya catalog."},
	"/stats":       {title: "Statistics · Heya", description: "Coverage statistics for the canonical Heya metadata catalog."},
	"/blog":        {title: "Blog · Heya", description: "News, updates, and engineering notes from the Heya team."},
	"/docs":        {title: "Documentation · Heya", description: "Developer documentation for the Heya metadata API."},
	"/downloads":   {title: "Downloads · Heya", description: "Download Heya clients, integrations, and tools."},
	"/features":    {title: "Features · Heya", description: "What Heya delivers: canonical identity, provenance, artwork, and localized metadata."},

	"/account":  {title: "Account · Heya", description: "Manage your Heya account.", private: true},
	"/admin":    {title: "Admin · Heya", description: "Heya administration console.", private: true},
	"/login":    {title: "Sign in · Heya", description: "Sign in to Heya.", private: true},
	"/register": {title: "Create account · Heya", description: "Create a Heya account.", private: true},
}

// seoMeta is the resolved metadata used to build the injected <head> block.
type seoMeta struct {
	title       string
	description string
	canonical   string // absolute canonical URL
	ogType      string
	imageURL    string // absolute image URL, empty when no artwork
	jsonLD      string // pre-marshalled JSON-LD node, empty for static routes
	noindex     bool
}

// render returns the shell with an injected <head>, or (nil, false) when no
// metadata applies or injection is not possible. It never panics.
func (r *seoRenderer) render(ctx context.Context, index []byte, cleanPath string) (out []byte, ok bool) {
	defer func() {
		if recover() != nil {
			out, ok = nil, false
		}
	}()
	meta, ok := r.metaFor(ctx, cleanPath)
	if !ok {
		return nil, false
	}
	return injectHead(index, meta.block())
}

// metaFor resolves the metadata for a route: a static-table entry, or a
// read-only entity lookup when the path ends in a UUID.
func (r *seoRenderer) metaFor(ctx context.Context, cleanPath string) (seoMeta, bool) {
	if entry, ok := staticRoutes[cleanPath]; ok {
		return seoMeta{
			title:       entry.title,
			description: entry.description,
			canonical:   r.siteURL + cleanPath,
			ogType:      "website",
			noindex:     entry.private,
		}, true
	}

	segments := strings.Split(strings.Trim(cleanPath, "/"), "/")
	last := segments[len(segments)-1]
	if !uuidPattern.MatchString(last) {
		// A listing or unknown static route: nothing to inject beyond the shell.
		return seoMeta{}, false
	}
	if r.runtime == nil {
		return seoMeta{}, false
	}

	resolveCtx, cancel := context.WithTimeout(ctx, entityResolveTimeout)
	defer cancel()
	summary, ok := seoEntitySummary(resolveCtx, r.runtime, last)
	if !ok {
		return seoMeta{}, false
	}

	canonicalPath := "/" + strings.Join(segments, "/")
	if route := kindRoutes[summary.kind]; route != "" {
		canonicalPath = "/" + route + "/" + summary.canonicalID
	}
	meta := seoMeta{
		title:       summary.title + " · Heya",
		description: truncateDescription(summary.description),
		canonical:   r.siteURL + canonicalPath,
		ogType:      ogTypeOr(summary.kind, "website"),
	}
	if summary.imageID != "" {
		meta.imageURL = fmt.Sprintf("%s/api/v2/images/%s/variants/webp/1200", r.siteURL, summary.imageID)
	}
	meta.jsonLD = buildJSONLD(summary.kind, summary.title, meta.description, meta.canonical, meta.imageURL)
	return meta, true
}

// block renders the <head> fragment for the resolved metadata. Every
// interpolated value is HTML-escaped; the JSON-LD payload is pre-marshalled.
func (m seoMeta) block() string {
	var b strings.Builder
	if m.noindex {
		b.WriteString(`<meta name="robots" content="noindex, nofollow">`)
	}
	b.WriteString("<title>")
	b.WriteString(html.EscapeString(m.title))
	b.WriteString("</title>")
	writeMetaName(&b, "description", m.description)
	if m.canonical != "" {
		b.WriteString(`<link rel="canonical" href="`)
		b.WriteString(html.EscapeString(m.canonical))
		b.WriteString(`">`)
	}
	writeMetaProperty(&b, "og:site_name", "Heya")
	writeMetaProperty(&b, "og:title", m.title)
	writeMetaProperty(&b, "og:description", m.description)
	writeMetaProperty(&b, "og:type", m.ogType)
	writeMetaProperty(&b, "og:url", m.canonical)
	twitterCard := "summary"
	if m.imageURL != "" {
		twitterCard = "summary_large_image"
		writeMetaProperty(&b, "og:image", m.imageURL)
	}
	writeMetaName(&b, "twitter:card", twitterCard)
	writeMetaName(&b, "twitter:title", m.title)
	writeMetaName(&b, "twitter:description", m.description)
	if m.imageURL != "" {
		writeMetaName(&b, "twitter:image", m.imageURL)
	}
	if m.jsonLD != "" {
		b.WriteString(`<script type="application/ld+json">`)
		b.WriteString(m.jsonLD)
		b.WriteString("</script>")
	}
	return b.String()
}

func writeMetaName(b *strings.Builder, name, content string) {
	if content == "" {
		return
	}
	b.WriteString(`<meta name="`)
	b.WriteString(name)
	b.WriteString(`" content="`)
	b.WriteString(html.EscapeString(content))
	b.WriteString(`">`)
}

func writeMetaProperty(b *strings.Builder, property, content string) {
	if content == "" {
		return
	}
	b.WriteString(`<meta property="`)
	b.WriteString(property)
	b.WriteString(`" content="`)
	b.WriteString(html.EscapeString(content))
	b.WriteString(`">`)
}

// injectHead strips the shell's static og:type tag and inserts the block
// immediately before the final </head>. It returns (nil, false) when no
// </head> exists so the caller serves the shell unmodified.
func injectHead(index []byte, block string) ([]byte, bool) {
	shell := string(index)
	marker := "</head>"
	position := strings.LastIndex(shell, marker)
	if position < 0 {
		return nil, false
	}
	shell = ogTypeMeta.ReplaceAllString(shell, "")
	position = strings.LastIndex(shell, marker)
	if position < 0 {
		return nil, false
	}
	var out strings.Builder
	out.Grow(len(shell) + len(block))
	out.WriteString(shell[:position])
	out.WriteString(block)
	out.WriteString(shell[position:])
	return []byte(out.String()), true
}

// seoSummary is the read-only projection consumed by the SEO builder.
type seoSummary struct {
	title       string
	description string
	imageID     string
	kind        string
	canonicalID string
}

// seoEntitySummary resolves an id to display metadata using only read paths:
// resolveActiveEntity + the stored detail document + presentEntity (which reads
// images and applies presentation). It NEVER inserts refresh jobs or mutates
// state, unlike the entity-detail HTTP handler. On any failure it returns ok=false.
func seoEntitySummary(ctx context.Context, runtime *platform.Runtime, id string) (seoSummary, bool) {
	if runtime == nil {
		return seoSummary{}, false
	}
	resolvedID, kind, err := resolveActiveEntity(ctx, runtime, id)
	if err != nil || resolvedID == "" {
		return seoSummary{}, false
	}

	// Prefer the rich detail document localized through presentEntity. This is
	// the same read used by entity-detail, minus the job insertion.
	var document []byte
	if err := runtime.DB.QueryRow(ctx, `SELECT document FROM api_documents WHERE entity_id=$1 AND document_kind='detail'`, resolvedID).Scan(&document); err == nil && len(document) > 0 {
		if presented, presentErr := presentEntity(ctx, runtime, resolvedID, kind, json.RawMessage(document), localeRequest{Language: "en"}); presentErr == nil {
			if summary, ok := extractSEOFields(presented.Body, kind, resolvedID); ok {
				return summary, true
			}
		}
	}

	// Fallback: the compact search projection covers kinds without a detail
	// document (people, seasons, episodes) and is always cheap and read-only.
	var title, imageID string
	if err := runtime.DB.QueryRow(ctx, `SELECT display_title,COALESCE(summary->'display'->>'image_id','') FROM search_entities WHERE entity_id=$1`, resolvedID).Scan(&title, &imageID); err != nil {
		return seoSummary{}, false
	}
	if strings.TrimSpace(title) == "" {
		return seoSummary{}, false
	}
	return seoSummary{title: strings.TrimSpace(title), imageID: imageID, kind: kind, canonicalID: resolvedID}, true
}

// extractSEOFields pulls the display title, description, and image id out of a
// presented entity body.
func extractSEOFields(body any, kind, canonicalID string) (seoSummary, bool) {
	raw, err := json.Marshal(body)
	if err != nil {
		return seoSummary{}, false
	}
	var view struct {
		Presentation struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		} `json:"presentation"`
		Display struct {
			Title   string `json:"title"`
			Name    string `json:"name"`
			ImageID string `json:"image_id"`
		} `json:"display"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		return seoSummary{}, false
	}
	title := firstNonEmpty(view.Presentation.Title, view.Display.Title, view.Display.Name)
	if title == "" {
		return seoSummary{}, false
	}
	return seoSummary{
		title:       title,
		description: view.Presentation.Description,
		imageID:     view.Display.ImageID,
		kind:        kind,
		canonicalID: canonicalID,
	}, true
}

// buildJSONLD returns a marshalled schema.org node. json.Marshal escapes <, >,
// and & so the payload is safe to embed inside a <script> element.
func buildJSONLD(kind, name, description, url, imageURL string) string {
	node := map[string]any{
		"@context": "https://schema.org",
		"@type":    jsonLDTypeOr(kind, "CreativeWork"),
		"name":     name,
		"url":      url,
	}
	if description != "" {
		node["description"] = description
	}
	if imageURL != "" {
		node["image"] = imageURL
	}
	encoded, err := json.Marshal(node)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func ogTypeOr(kind, fallback string) string {
	if value, ok := ogTypes[kind]; ok {
		return value
	}
	return fallback
}

func jsonLDTypeOr(kind, fallback string) string {
	if value, ok := jsonLDTypes[kind]; ok {
		return value
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// truncateDescription collapses whitespace and caps the description at a
// crawler-friendly length on a rune boundary.
func truncateDescription(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const limit = 300
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

// writeRobots serves the authoritative robots.txt: crawling is allowed except
// the private and API paths, and the sitemap is advertised.
func writeRobots(writer http.ResponseWriter, siteURL string) {
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.Header().Set("Cache-Control", "public, max-age=3600")
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	b.WriteString("Allow: /\n")
	for _, path := range []string{"/account", "/admin", "/login", "/register", "/api/"} {
		b.WriteString("Disallow: ")
		b.WriteString(path)
		b.WriteString("\n")
	}
	if siteURL != "" {
		b.WriteString("\nSitemap: ")
		b.WriteString(siteURL)
		b.WriteString("/sitemap.xml\n")
	}
	_, _ = writer.Write([]byte(b.String()))
}

// sitemapEntityCap bounds the number of entity URLs emitted. The sitemap
// protocol allows 50,000 URLs per file; we cap to the most-recently-updated
// entities and rely on client-side navigation for deeper discovery.
const sitemapEntityCap = 50000

// sitemapKinds are the routable canonical kinds included in the sitemap.
var sitemapKinds = []string{
	"movie", "tv_show", "anime", "artist", "release_group",
	"recording", "book_work", "manga", "manga_volume", "comic_volume", "person",
}

// writeSitemap streams a valid urlset: the public static routes followed by
// canonical URLs for the most-recently-updated routable entities. The XML write
// is streamed so memory stays bounded even for large catalogs. Enumeration
// failures still yield a valid (static-only) sitemap rather than an error page.
func (r *seoRenderer) writeSitemap(ctx context.Context, writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "application/xml; charset=utf-8")
	writer.Header().Set("Cache-Control", "public, max-age=3600")

	_, _ = writer.Write([]byte(xml.Header))
	_, _ = writer.Write([]byte("<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n"))

	for path, entry := range staticRoutes {
		if entry.private {
			continue
		}
		writeSitemapURL(writer, r.siteURL+path, time.Time{})
	}

	if r.runtime != nil {
		queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		r.streamEntityURLs(queryCtx, writer)
	}

	_, _ = writer.Write([]byte("</urlset>\n"))
}

func (r *seoRenderer) streamEntityURLs(ctx context.Context, writer http.ResponseWriter) {
	rows, err := r.runtime.DB.Query(ctx, `SELECT entity_id::text,kind,updated_at FROM search_entities WHERE kind=ANY($1) ORDER BY updated_at DESC LIMIT $2`, sitemapKinds, sitemapEntityCap)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, kind string
		var updated time.Time
		if err := rows.Scan(&id, &kind, &updated); err != nil {
			return
		}
		route := kindRoutes[kind]
		if route == "" {
			continue
		}
		writeSitemapURL(writer, r.siteURL+"/"+route+"/"+id, updated)
	}
}

func writeSitemapURL(writer http.ResponseWriter, loc string, lastmod time.Time) {
	var b strings.Builder
	b.WriteString("  <url><loc>")
	b.WriteString(xmlEscape(loc))
	b.WriteString("</loc>")
	if !lastmod.IsZero() {
		b.WriteString("<lastmod>")
		b.WriteString(lastmod.UTC().Format(time.RFC3339))
		b.WriteString("</lastmod>")
	}
	b.WriteString("</url>\n")
	_, _ = writer.Write([]byte(b.String()))
}

func xmlEscape(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}
