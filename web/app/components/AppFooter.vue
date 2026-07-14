<script setup lang="ts">
// Attribution footer, in the spirit of the old HeyaMedia site: a "Data from"
// row crediting the upstream sources the service actually uses, then a meta line
// with the canonical-truth tagline, docs links, source, and licence.
interface Source { name: string; url: string; logo?: string; h?: string; invert?: boolean }

const SOURCES: Source[] = [
  { name: 'TMDB', url: 'https://www.themoviedb.org/', logo: '/logos/tmdb.svg', h: '14px' },
  { name: 'TheTVDB', url: 'https://thetvdb.com/', logo: '/logos/tvdb.png', h: '18px' },
  { name: 'IMDb', url: 'https://www.imdb.com/', logo: '/logos/imdb.svg', h: '15px' },
  { name: 'TVmaze', url: 'https://www.tvmaze.com/', logo: '/logos/tvmaze.png', h: '16px' },
  { name: 'MusicBrainz', url: 'https://musicbrainz.org/', logo: '/logos/musicbrainz.svg', h: '15px' },
  { name: 'Discogs', url: 'https://www.discogs.com/', logo: '/logos/discogs.svg', h: '14px', invert: true },
  { name: 'Apple Music', url: 'https://music.apple.com/', logo: '/logos/applemusic.svg', h: '15px' },
  { name: 'Deezer', url: 'https://www.deezer.com/', logo: '/logos/deezer.svg', h: '14px' },
  { name: 'Last.fm', url: 'https://www.last.fm/', logo: '/logos/lastfm.svg', h: '15px' },
  { name: 'AniDB', url: 'https://anidb.net/', logo: '/logos/anidb.png', h: '18px' },
  { name: 'Wikidata', url: 'https://www.wikidata.org/', logo: '/logos/wikidata.svg', h: '15px' },
  { name: 'Fanart.tv', url: 'https://fanart.tv/' },
  { name: 'Spotify', url: 'https://www.spotify.com/' },
  { name: 'AllMusic', url: 'https://www.allmusic.com/' },
  { name: 'AniList', url: 'https://anilist.co/' },
  { name: 'MyAnimeList', url: 'https://myanimelist.net/' },
  { name: 'Kitsu', url: 'https://kitsu.io/' },
  { name: 'Google Books', url: 'https://books.google.com/' },
  { name: 'Open Library', url: 'https://openlibrary.org/' },
  { name: 'OpenOpus', url: 'https://openopus.org/' },
]
</script>

<template>
  <footer class="app-footer">
    <div class="shell app-footer__inner">
      <div class="app-footer__data">
        <span class="app-footer__label">Data from</span>
        <ul class="app-footer__sources">
          <li v-for="source in SOURCES" :key="source.name">
            <a
              :href="source.url"
              target="_blank"
              rel="noopener"
              class="app-footer__source"
              :class="source.logo ? 'has-logo' : 'is-text'"
              :title="source.name"
            >
              <img
                v-if="source.logo"
                :src="source.logo"
                :alt="source.name"
                :class="{ 'is-invert': source.invert }"
                :style="{ height: source.h }"
                loading="lazy"
              >
              <span v-else>{{ source.name }}</span>
            </a>
          </li>
        </ul>
      </div>

      <div class="app-footer__meta">
        <div class="app-footer__brandline">
          <span class="app-footer__brand">Heya</span>
          <p>One canonical truth, assembled from many imperfect sources.</p>
        </div>
        <div class="app-footer__links">
          <a href="/api/docs" target="_blank" rel="noopener">API reference ↗</a>
          <a href="/api/openapi.json" target="_blank" rel="noopener">OpenAPI ↗</a>
          <a href="https://github.com/HeyaMedia/HeyaMetadata" target="_blank" rel="noopener">Source ↗</a>
          <span class="app-footer__license">MIT License</span>
        </div>
      </div>
    </div>
  </footer>
</template>

<style scoped>
.app-footer { margin-top: 4rem; border-top: 1px solid var(--line); }
.app-footer__inner {
  display: flex;
  flex-direction: column;
  gap: 1.75rem;
  padding-block: 2.5rem 3rem;
}

/* Data-from row */
.app-footer__data {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 0.9rem 1.5rem;
}
.app-footer__label {
  color: var(--muted-2);
  font-family: var(--font-mono);
  font-size: 0.62rem;
  letter-spacing: 0.16em;
  text-transform: uppercase;
}
.app-footer__sources {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 1rem 1.5rem;
  margin: 0;
  padding: 0;
  list-style: none;
}
.app-footer__source {
  display: inline-flex;
  align-items: center;
  opacity: 0.62;
  transition: opacity 0.18s ease, color 0.18s ease;
}
.app-footer__source:hover { opacity: 1; }
.app-footer__source img { display: block; width: auto; }
.app-footer__source img.is-invert { filter: brightness(0) invert(0.82); }
.app-footer__source.is-text {
  color: #8e999b;
  font-family: var(--font-mono);
  font-size: 0.7rem;
  opacity: 0.85;
}
.app-footer__source.is-text:hover { color: #fff; opacity: 1; }

/* Meta row */
.app-footer__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 1rem 1.5rem;
  padding-top: 1.75rem;
  border-top: 1px solid var(--line);
  color: #626d72;
  font-size: 0.68rem;
}
.app-footer__brandline { display: flex; flex-wrap: wrap; align-items: baseline; gap: 0.35rem 0.9rem; }
.app-footer__brand { color: #8e999b; }
.app-footer__brandline p { margin: 0; }
.app-footer__links { display: flex; flex-wrap: wrap; align-items: center; gap: 1.25rem; }
.app-footer__links a { color: #8e999b; }
.app-footer__links a:hover { color: #fff; }
.app-footer__license { color: var(--muted-2); }

@media (max-width: 640px) {
  .app-footer__meta { flex-direction: column; align-items: flex-start; }
}
</style>
