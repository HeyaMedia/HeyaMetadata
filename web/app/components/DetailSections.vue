<script setup lang="ts">
import type { EntityDocument, ImageCandidate } from '~/utils/types'

// URL-synced Overview/Artwork/Provenance/Raw tabs for detail pages that are NOT
// canonical entities (standalone seasons and episodes). Reuses the exact same
// panels as EntityDetail so the experience is identical everywhere. The Overview
// tab is a slot; images/provenance/raw are supplied by the host page from the
// resource payload (episodic images and provenance arrive inline, not from the
// /entities/{id} endpoints).
const props = withDefaults(defineProps<{
  images?: ImageCandidate[]
  provenance?: Record<string, unknown[]>
  raw?: unknown
}>(), { images: () => [], provenance: () => ({}), raw: () => ({}) })

const route = useRoute()
const active = computed(() => (route.query.tab as string) || 'overview')

const imagesResponse = computed(() => ({ results: props.images ?? [] }))
// ProvenancePanel/RawDocumentPanel are typed against EntityDocument but only read
// .provenance / stringify the whole object, so a partial is safe here.
const provenanceEntity = computed(() => ({ provenance: props.provenance } as unknown as EntityDocument))
const rawEntity = computed(() => props.raw as EntityDocument)

const tabs = computed(() => [
  { key: 'overview', label: 'Overview' },
  { key: 'artwork', label: 'Artwork', count: props.images?.length },
  { key: 'provenance', label: 'Provenance' },
  { key: 'raw', label: 'Raw' },
])
</script>

<template>
  <div>
    <EntityTabs :tabs="tabs" />
    <div class="tab-content">
      <div v-show="active === 'overview'"><slot /></div>
      <ArtworkGallery v-if="active === 'artwork'" :images="imagesResponse" />
      <ProvenancePanel v-else-if="active === 'provenance'" :entity="provenanceEntity" />
      <RawDocumentPanel v-else-if="active === 'raw'" :entity="rawEntity" />
    </div>
  </div>
</template>

<style scoped>
.tab-content { margin-top: 1.75rem; }
</style>
