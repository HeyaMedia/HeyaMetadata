<script setup lang="ts">
import type { Fact } from '~/components/FactList.vue'
import type { EntityDocument } from '~/utils/types'

// book_work.
const props = defineProps<{ entity: EntityDocument }>()
const data = computed<any>(() => props.entity.data ?? {})

const facts = computed<Fact[]>(() => {
  const d = data.value
  return [
    { label: 'Authors', value: (d.authors ?? []).map((author: any) => formatValue(author.name)) },
    { label: 'Editions', value: Array.isArray(d.editions) ? d.editions.length : '' },
    { label: 'Medium', value: titleCase(d.publication?.medium) },
    { label: 'Published', value: formatDate(d.publication?.first_published ?? d.first_publish_date) },
  ]
})

const subjects = computed(() => (Array.isArray(data.value.subjects) ? data.value.subjects : []))
const editions = computed(() => (Array.isArray(data.value.editions) ? data.value.editions : []))
const EDITION_CAP = 20
const showAllEditions = ref(false)
const shownEditions = computed(() => (showAllEditions.value ? editions.value : editions.value.slice(0, EDITION_CAP)))
</script>

<template>
  <div class="overview-grid">
    <OverviewPanel title="Overview" kicker="Combined record">
      <FactList :facts="facts" />
    </OverviewPanel>

    <ExternalIdsPanel :external-ids="entity.external_ids" />

    <ChipCloud title="Subjects" kicker="Themes" :items="subjects" full />

    <OverviewPanel v-if="editions.length" :title="`Editions (${editions.length})`" kicker="Publications" full>
      <ol class="line-list">
        <li v-for="(edition, index) in shownEditions" :key="edition.id || index">
          <span class="line-list__main">
            <span class="line-list__title">{{ formatValue(edition.title) || 'Edition' }}</span>
            <span class="line-list__sub">{{ [formatValue(edition.format), formatValue(edition.publishers), formatValue(edition.languages)].filter(Boolean).join(' · ') }}</span>
          </span>
        </li>
      </ol>
      <button v-if="editions.length > EDITION_CAP" type="button" class="btn--link editions__more" @click="showAllEditions = !showAllEditions">
        {{ showAllEditions ? 'Show fewer' : `Show all ${editions.length} editions` }}
      </button>
    </OverviewPanel>
  </div>
</template>

<style scoped>
.editions__more { margin-top: 0.85rem; color: var(--gold); }
</style>
