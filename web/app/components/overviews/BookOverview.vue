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
    { label: 'Subjects', value: (d.subjects ?? []).slice(0, 12) },
  ]
})

const editions = computed(() => (Array.isArray(data.value.editions) ? data.value.editions : []))
const shownEditions = computed(() => editions.value.slice(0, 16))
</script>

<template>
  <div class="overview-grid">
    <OverviewPanel title="Overview" kicker="Combined record">
      <FactList :facts="facts" />
    </OverviewPanel>

    <ExternalIdsPanel :external-ids="entity.external_ids" />

    <OverviewPanel v-if="editions.length" :title="`Editions (${editions.length})`" kicker="Publications" full>
      <ol class="line-list">
        <li v-for="(edition, index) in shownEditions" :key="edition.id || index">
          <span class="line-list__main">
            <span class="line-list__title">{{ formatValue(edition.title) || 'Edition' }}</span>
            <span class="line-list__sub">{{ [formatValue(edition.format), formatValue(edition.publishers), formatValue(edition.languages)].filter(Boolean).join(' · ') }}</span>
          </span>
        </li>
      </ol>
      <p v-if="editions.length > shownEditions.length" class="muted more-note">+ {{ editions.length - shownEditions.length }} more editions</p>
    </OverviewPanel>
  </div>
</template>

<style scoped>
.more-note { margin: 0.8rem 0 0; font-size: 0.72rem; }
</style>
