<script setup lang="ts">
// Season → episode browser for tv/anime, rendered from the show document's
// embedded `seasons`/`episodes`. The active season lives in the URL (?season=N)
// so back/forward and reload restore it; individual episodes expand inline.
const props = defineProps<{ seasons?: any[]; episodes?: any[] }>()

const route = useRoute()
const { languages } = useLocale()

interface Episode { id?: string; season: number | null; number: number | null; title: string; air: string; runtime: string; summary: string; schemes: any[]; type: string; isSpecial: boolean }

const allEpisodes = computed<Episode[]>(() =>
  (props.episodes ?? []).map(ep => {
    const season = canonicalEpisodeSeason(ep, props.seasons)
    const primary = canonicalEpisodeNumber(ep, season) ?? {}
    return {
      id: ep.id,
      season,
      number: primary.number ?? null,
      title: preferredText(ep.titles, languages()) || (primary.number != null ? `Episode ${primary.number}` : 'Episode'),
      air: formatValue(ep.air_date),
      runtime: formatRuntime(ep.runtime_minutes),
      summary: preferredText(ep.overviews, languages()) || formatValue(ep.summary),
      schemes: displayEpisodeNumbers(ep.numbers),
      type: String(ep.episode_type || (ep.is_special ? 'special' : 'regular')).toLowerCase(),
      isSpecial: Boolean(ep.is_special),
    }
  }),
)

const seasonList = computed<{ number: number; name: string; count: number; id?: string }[]>(() => {
  const declared = (props.seasons ?? []).map(s => ({ number: Number(s.number), name: formatValue(s.name), id: s.id as string | undefined }))
  const numbers = new Set<number>(declared.map(s => s.number))
  for (const ep of allEpisodes.value) if (ep.season != null) numbers.add(ep.season)
  return [...numbers].filter(n => Number.isFinite(n)).sort((a, b) => a - b).map(number => ({
    number,
    name: number === 0 ? 'Specials & extras' : `Season ${number}`,
    id: declared.find(s => s.number === number)?.id,
    count: allEpisodes.value.filter(ep => ep.season === number).length,
  }))
})

const hasSeasons = computed(() => seasonList.value.length > 0)
const activeSeason = computed(() => {
  const requested = Number(route.query.season)
  if (Number.isFinite(requested) && seasonList.value.some(s => s.number === requested)) return requested
  // Default past empty seasons (e.g. Specials/season 0) to the first with episodes.
  const firstWithEpisodes = seasonList.value.find(s => s.count > 0)
  return (firstWithEpisodes ?? seasonList.value[0])?.number ?? null
})
// The active season's standalone page id, for a one-click jump to /seasons/{id}.
const activeSeasonMeta = computed(() => seasonList.value.find(s => s.number === activeSeason.value))

const extraTypes = computed(() => {
  const episodes = allEpisodes.value.filter(ep => ep.season === 0)
  const order = ['special', 'trailer', 'credit', 'parody']
  const values = [...new Set(episodes.map(ep => ep.type || 'special'))]
    .sort((a, b) => {
      const left = order.indexOf(a)
      const right = order.indexOf(b)
      return (left < 0 ? 100 : left) - (right < 0 ? 100 : right) || a.localeCompare(b)
    })
  const labels: Record<string, string> = { special: 'Specials', trailer: 'Trailers', credit: 'Credits', parody: 'Parodies' }
  return values.map(value => ({ value, label: labels[value] || titleCase(value), count: episodes.filter(ep => ep.type === value).length }))
})

const activeExtraType = computed(() => {
  const requested = String(route.query.episode_type || '').toLowerCase()
  if (extraTypes.value.some(item => item.value === requested)) return requested
  if (extraTypes.value.some(item => item.value === 'special')) return 'special'
  return extraTypes.value[0]?.value ?? ''
})

const shownEpisodes = computed(() => {
  let list = hasSeasons.value ? allEpisodes.value.filter(ep => ep.season === activeSeason.value) : allEpisodes.value
  if (activeSeason.value === 0 && activeExtraType.value) list = list.filter(ep => ep.type === activeExtraType.value)
  return [...list].sort((a, b) => (a.number ?? 0) - (b.number ?? 0))
})

const regularEpisodeCount = computed(() => allEpisodes.value.filter(ep => ep.season !== 0 && !ep.isSpecial).length)
const extrasCount = computed(() => allEpisodes.value.filter(ep => ep.season === 0 || ep.isSpecial).length)

function selectSeason(number: number) {
  const query = { ...route.query, season: String(number) }
  delete query.episode_type
  navigateTo({ path: route.path, query })
}

function selectExtraType(type: string) {
  navigateTo({ path: route.path, query: { ...route.query, season: '0', episode_type: type } })
}

const openKey = ref<string | null>(null)
function toggle(key: string) { openKey.value = openKey.value === key ? null : key }
</script>

<template>
  <section v-if="allEpisodes.length" class="seasons">
    <header class="section-head">
      <div><span class="section-label">Structure</span><h2>Episodes</h2></div>
      <span class="seasons__count">
        {{ regularEpisodeCount }} episodes<template v-if="extrasCount"> · {{ extrasCount }} extras</template>
      </span>
    </header>

    <div v-if="hasSeasons" class="seasons__nav">
      <div class="seasons__pills" role="tablist" aria-label="Seasons">
        <button
          v-for="season in seasonList"
          :key="season.number"
          type="button"
          role="tab"
          :aria-selected="season.number === activeSeason"
          class="season-pill"
          :class="{ 'is-active': season.number === activeSeason }"
          @click="selectSeason(season.number)"
        >
          {{ season.name }}<small>{{ season.count }}</small>
        </button>
      </div>
      <NuxtLink
        v-if="activeSeasonMeta?.id"
        :to="activeSeason === 0 && activeExtraType ? `/seasons/${activeSeasonMeta.id}?episode_type=${activeExtraType}` : `/seasons/${activeSeasonMeta.id}`"
        class="btn--link seasons__open"
      >
        View full season ↗
      </NuxtLink>
    </div>

    <div v-if="activeSeason === 0 && extraTypes.length > 1" class="seasons__types" role="tablist" aria-label="Extra type">
      <button
        v-for="type in extraTypes"
        :key="type.value"
        type="button"
        role="tab"
        :aria-selected="type.value === activeExtraType"
        class="type-pill"
        :class="{ 'is-active': type.value === activeExtraType }"
        @click="selectExtraType(type.value)"
      >
        {{ type.label }}<small>{{ type.count }}</small>
      </button>
    </div>

    <ul class="episode-list">
      <li v-for="(ep, index) in shownEpisodes" :key="ep.id || `${ep.season}-${ep.number}-${index}`" class="episode" :class="{ 'is-open': openKey === `${ep.season}-${ep.number}-${index}` }">
        <button type="button" class="episode__row" @click="toggle(`${ep.season}-${ep.number}-${index}`)">
          <span class="episode__num">{{ ep.number != null ? String(ep.number).padStart(2, '0') : '—' }}</span>
          <span class="episode__title">{{ ep.title }}</span>
          <span class="episode__meta">
            <template v-if="ep.air">{{ formatDate(ep.air) }}</template>
            <template v-if="ep.runtime"> · {{ ep.runtime }}</template>
          </span>
          <span class="episode__chev" aria-hidden="true">{{ openKey === `${ep.season}-${ep.number}-${index}` ? '−' : '+' }}</span>
        </button>
        <div v-if="openKey === `${ep.season}-${ep.number}-${index}`" class="episode__detail">
          <p v-if="ep.summary" class="episode__summary">{{ ep.summary }}</p>
          <p v-else class="muted episode__summary">No synopsis available for this episode.</p>
          <div v-if="ep.schemes.length" class="episode__schemes">
            <span v-for="(scheme, si) in ep.schemes" :key="si" class="chip">
              <template v-if="scheme.scheme === 'absolute'">Absolute #{{ scheme.number }}</template>
              <template v-else>{{ formatKey(scheme.scheme || 'num') }} <template v-if="scheme.season != null">S{{ scheme.season }}·</template>E{{ scheme.number ?? '?' }}</template>
            </span>
          </div>
          <NuxtLink v-if="ep.id" :to="`/episodes/${ep.id}`" class="btn--link episode__open">Open episode page ↗</NuxtLink>
        </div>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.seasons { margin-top: 2.5rem; }
.section-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1.1rem;
  padding-bottom: 0.75rem;
  border-bottom: 1px solid var(--line);
}
.section-head h2 { margin: 0.3rem 0 0; font-size: 1.2rem; font-weight: 500; }
.seasons__count { color: var(--muted-2); font-family: var(--font-mono); font-size: 0.68rem; }
.seasons__nav {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem 1rem;
  margin-bottom: 1.25rem;
}
.seasons__pills { display: flex; flex-wrap: wrap; gap: 0.5rem; }
.seasons__open { flex: none; color: var(--gold); font-size: 0.72rem; white-space: nowrap; }
.seasons__open:hover { text-decoration: underline; }
.season-pill {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.45rem 0.8rem;
  border: 1px solid var(--line-strong);
  border-radius: 2rem;
  background: var(--panel);
  color: var(--muted);
  font-size: 0.72rem;
}
.season-pill:hover { color: var(--text); }
.season-pill.is-active { border-color: var(--gold); color: var(--gold); }
.season-pill small { color: var(--muted-2); font-family: var(--font-mono); font-size: 0.62rem; }
.season-pill.is-active small { color: var(--gold); }
.seasons__types { display: flex; flex-wrap: wrap; gap: 0.4rem; margin: -0.45rem 0 1.1rem; }
.type-pill {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  padding: 0.3rem 0.65rem;
  border: 1px solid var(--line);
  border-radius: 2rem;
  background: transparent;
  color: var(--muted-2);
  font-family: var(--font-mono);
  font-size: 0.62rem;
  text-transform: uppercase;
}
.type-pill:hover, .type-pill.is-active { border-color: var(--line-strong); color: var(--text); }
.type-pill.is-active { background: var(--panel); }
.type-pill small { color: var(--gold); font-size: 0.58rem; }

.episode-list { margin: 0; padding: 0; list-style: none; }
.episode { border-top: 1px solid var(--line-soft); }
.episode:first-child { border-top: 0; }
.episode__row {
  display: grid;
  grid-template-columns: 2.5rem 1fr auto 1.5rem;
  align-items: center;
  gap: 1rem;
  width: 100%;
  padding: 0.8rem 0.25rem;
  border: 0;
  background: none;
  text-align: left;
}
.episode__row:hover { background: rgba(255, 255, 255, 0.015); }
.episode__num { color: var(--gold); font-family: var(--font-mono); font-size: 0.72rem; }
.episode__title { overflow: hidden; font-size: 0.82rem; text-overflow: ellipsis; white-space: nowrap; }
.episode__meta { color: var(--muted-2); font-size: 0.68rem; white-space: nowrap; }
.episode__chev { color: var(--muted-2); font-family: var(--font-mono); text-align: center; }
.episode__detail { padding: 0 0.25rem 1.1rem 5rem; }
.episode__summary { max-width: 52rem; margin: 0 0 0.75rem; color: #a0aaa7; font-size: 0.8rem; line-height: 1.7; }
.episode__schemes { display: flex; flex-wrap: wrap; gap: 0.4rem; }
.episode__schemes .chip { font-family: var(--font-mono); font-size: 0.6rem; }
.episode__open { display: inline-block; margin-top: 0.85rem; color: var(--gold); }

@media (max-width: 640px) {
  .episode__row { grid-template-columns: 2rem 1fr 1.5rem; }
  .episode__meta { display: none; }
  .episode__detail { padding-left: 0.25rem; }
}
</style>
