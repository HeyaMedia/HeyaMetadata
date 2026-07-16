<script setup lang="ts">
import type { Fact } from '~/components/FactList.vue'
import type { EntityDocument, RecordingCredit } from '~/utils/types'

// recording (a single track/performance).
const props = defineProps<{ entity: EntityDocument }>()
const data = computed<any>(() => props.entity.data ?? {})

const facts = computed<Fact[]>(() => {
  const d = data.value
  const rating = d.rating && d.rating.value != null
    ? `${d.rating.value}${d.rating.votes ? ` · ${formatCount(d.rating.votes)} votes` : ''}`
    : ''
  return [
    { label: 'Duration', value: formatDuration(d.duration_ms) },
    { label: 'ISRCs', value: d.isrcs },
    { label: 'Provider', value: formatKey(d.provider) },
    { label: 'Rating', value: rating },
    { label: 'Fingerprints', value: Array.isArray(d.fingerprints) ? d.fingerprints.length : '' },
  ]
})

const releases = computed(() => (Array.isArray(data.value.releases) ? data.value.releases : []))

// Personnel from data.credits, grouped by role. One entry per artist per role,
// with the snake_case attributes (instruments, vocal parts, …) humanized and
// merged. Links go to the canonical artist page only when the credit carries
// artist_entity_id — the provider-scoped artist_id is never used for routing.
const ROLE_ORDER = ['producer', 'engineer', 'mix', 'vocal', 'instrument']
interface CreditArtist { name: string; to?: string; attributes: string }
const creditGroups = computed<{ role: string; artists: CreditArtist[] }[]>(() => {
  const credits: RecordingCredit[] = Array.isArray(data.value.credits) ? data.value.credits : []
  const groups = new Map<string, Map<string, { name: string; to?: string; attributes: string[] }>>()
  for (const credit of credits) {
    const name = formatValue(credit.artist_name)
    if (!name) continue
    const role = formatValue(credit.role).toLowerCase() || 'other'
    if (!groups.has(role)) groups.set(role, new Map())
    const artists = groups.get(role)!
    const key = credit.artist_entity_id || name.toLowerCase()
    if (!artists.has(key)) {
      artists.set(key, {
        name,
        to: credit.artist_entity_id ? entityPath({ id: credit.artist_entity_id, kind: 'artist' }) : undefined,
        attributes: [],
      })
    }
    const entry = artists.get(key)!
    for (const attribute of credit.attributes ?? []) {
      const text = humanizeToken(attribute)
      if (text && !entry.attributes.includes(text)) entry.attributes.push(text)
    }
  }
  const rank = (role: string) => {
    const index = ROLE_ORDER.indexOf(role)
    return index === -1 ? ROLE_ORDER.length : index
  }
  return [...groups.entries()]
    .sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b))
    .map(([role, artists]) => ({
      role: titleCase(role),
      artists: [...artists.values()].map(artist => ({ ...artist, attributes: artist.attributes.join(', ') })),
    }))
})
</script>

<template>
  <div class="overview-grid">
    <OverviewPanel title="Overview" kicker="Combined record">
      <FactList :facts="facts" />
    </OverviewPanel>

    <ExternalIdsPanel :external-ids="entity.external_ids" />
    <LinksList :links="data.links" />

    <OverviewPanel v-if="creditGroups.length" title="Credits" kicker="Personnel" full>
      <dl class="facts">
        <template v-for="group in creditGroups" :key="group.role">
          <dt>{{ group.role }}</dt>
          <dd>
            <template v-for="(artist, index) in group.artists" :key="index"><NuxtLink v-if="artist.to" :to="artist.to" class="credit-artist">{{ artist.name }}</NuxtLink><span v-else>{{ artist.name }}</span><span v-if="artist.attributes" class="credit-attrs"> ({{ artist.attributes }})</span><template v-if="index < group.artists.length - 1">, </template></template>
          </dd>
        </template>
      </dl>
    </OverviewPanel>

    <OverviewPanel v-if="releases.length" title="Appears on" kicker="Releases" full>
      <ol class="line-list">
        <li v-for="(release, index) in releases" :key="index">
          <span class="line-list__main">
            <span class="line-list__title">{{ formatValue(release.title) || 'Release' }}</span>
            <span class="line-list__sub">{{ [formatValue(release.country), titleCase(release.status)].filter(Boolean).join(' · ') }}</span>
          </span>
          <span class="line-list__meta">{{ formatDate(release.date) }}</span>
        </li>
      </ol>
    </OverviewPanel>

    <LyricsPanel :recording-id="entity.id" />
  </div>
</template>

<style scoped>
.credit-artist { color: var(--text-dim); }
.credit-artist:hover { color: var(--gold); }
.credit-attrs { color: var(--muted-2); }
</style>
