<script setup lang="ts">
import type { Fact } from '~/components/FactList.vue'
import type { AlbumEdition, EntityDescription, EntityDocument } from '~/utils/types'

// release_group / album.
const props = defineProps<{ entity: EntityDocument }>()
const data = computed<any>(() => props.entity.data ?? {})

const facts = computed<Fact[]>(() => {
  const d = data.value
  const firstRelease = (d.dates ?? []).find((date: any) => date.type === 'first_release')?.value
  return [
    { label: 'Type', value: titleCase(d.classification?.primary_type) },
    { label: 'Secondary types', value: (d.classification?.secondary_types ?? []).map((t: any) => titleCase(t)) },
    { label: 'Released', value: formatDate(firstRelease) },
    { label: 'Genres', value: d.genres },
    { label: 'Tags', value: (d.tags ?? []).map((tag: any) => tag.name) },
  ]
})

// Longest English entry from data.descriptions — provider descriptions beat
// one-line wikidata stubs. Skipped when the hero already shows the same text.
const about = computed(() => {
  const descriptions: EntityDescription[] = Array.isArray(data.value.descriptions) ? data.value.descriptions : []
  const english = descriptions
    .map(item => ({ language: formatValue(item.language).toLowerCase(), value: formatValue(item.value) }))
    .filter(item => item.value && (!item.language || item.language === 'en' || item.language.startsWith('en-')))
  if (!english.length) return ''
  const best = english.reduce((a, b) => (b.value.length > a.value.length ? b : a))
  return best.value !== formatValue(props.entity.presentation?.description) ? best.value : ''
})

const tracks = computed(() => (Array.isArray(data.value.tracks) ? data.value.tracks : []))

// Editions straight from the canonical document — this is where the new
// labels[] {name, catalog_number} and formats[] live. Falls back to the
// relations feed for documents that predate data.editions.
interface EditionEntry { title: string; sub: string; date: string; to?: string }
const editions = computed<EditionEntry[]>(() => {
  const list: AlbumEdition[] = Array.isArray(data.value.editions) ? data.value.editions : []
  const seen = new Set<string>()
  const out: EditionEntry[] = []
  for (const edition of list) {
    const title = formatValue(edition.title) || 'Release'
    const key = edition.entity_id || `${edition.provider}:${edition.provider_id}` || title
    if (seen.has(key)) continue
    seen.add(key)
    const labels = formatList(edition.labels ?? [], label =>
      [formatValue(label.name), formatValue(label.catalog_number)].filter(Boolean).join(' · '))
    const formats = formatList(edition.formats ?? [], format => titleCase(format))
    out.push({
      title,
      sub: [formatValue(edition.country), titleCase(edition.status), labels, formats].filter(Boolean).join(' · '),
      date: typeof edition.date === 'string' ? edition.date : formatValue(edition.date?.value),
      to: edition.entity_id && edition.resolution_state !== 'unresolved'
        ? entityPath({ id: edition.entity_id, kind: 'release' })
        : undefined,
    })
  }
  return out
})
const linkTag = resolveComponent('NuxtLink')
</script>

<template>
  <div class="overview-grid">
    <OverviewPanel title="Overview" kicker="Combined record">
      <FactList :facts="facts" />
    </OverviewPanel>

    <ExternalIdsPanel :external-ids="entity.external_ids" />

    <OverviewPanel v-if="about" title="About" kicker="Description" full>
      <p class="album-about">{{ about }}</p>
    </OverviewPanel>

    <LinksList :links="data.links" />
    <RatingsPanel :ratings="data.ratings" />

    <TracklistPanel :tracks="tracks" />

    <OverviewPanel v-if="editions.length" :title="`Releases (${editions.length})`" kicker="Editions" full>
      <ol class="line-list">
        <li v-for="(edition, index) in editions" :key="index">
          <span class="line-list__main">
            <component :is="edition.to ? linkTag : 'span'" :to="edition.to" class="edition__title" :class="{ 'is-link': edition.to }">
              {{ edition.title }}<template v-if="edition.to"> ↗</template>
            </component>
            <span v-if="edition.sub" class="line-list__sub">{{ edition.sub }}</span>
          </span>
          <span v-if="edition.date" class="line-list__meta">{{ formatDate(edition.date) }}</span>
        </li>
      </ol>
    </OverviewPanel>
    <RelationsList v-else :entity-id="entity.id" type="editions" title="Releases" kicker="Editions" />
  </div>
</template>

<style scoped>
.album-about { max-width: 62rem; margin: 0; color: var(--muted); font-size: 0.8rem; line-height: 1.72; }
.edition__title.is-link:hover { color: var(--gold); }
</style>
