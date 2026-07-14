<script setup lang="ts">
import { kindMeta } from '~/utils/kinds'

// Shared detail shell. Fetches the entity independently from the URL id, redirects
// to the correct route on a kind mismatch, and renders hero + URL-synced tabs +
// the generic Artwork/Provenance/Raw panels. The overview tab is a scoped slot so
// each domain supplies its own panel; without one, GenericOverview is used.
const props = withDefaults(defineProps<{
  id: string
  expectedKind?: string
  backTo?: string
  backLabel?: string
  /** When on the generic /entities route, send known kinds to their canonical URL. */
  redirectToCanonical?: boolean
}>(), { expectedKind: '', backTo: '', backLabel: '', redirectToCanonical: false })

const route = useRoute()
const { entity, images, pending, error, refreshing, refreshFromProviders } = useEntity(() => props.id)

// Reactive SEO (title/description/og:image/twitter/JSON-LD) for all 12 detail
// pages that render through this shell. Fields stay empty until the entity loads.
useEntitySeo(entity)

const activeTab = computed(() => (route.query.tab as string) || 'overview')

// Redirect to the canonical route when the loaded entity's kind does not match
// the route, or when a known kind lands on the generic /entities fallback.
watch(entity, value => {
  if (!value) return
  if (props.expectedKind && value.kind !== props.expectedKind) {
    navigateTo(entityPath(value), { replace: true })
  } else if (props.redirectToCanonical && kindMeta(value.kind)?.route) {
    navigateTo(entityPath(value), { replace: true })
  }
})

const tabs = computed(() => [
  { key: 'overview', label: 'Overview' },
  { key: 'artwork', label: 'Artwork', count: images.value?.results?.length },
  { key: 'provenance', label: 'Provenance' },
  { key: 'raw', label: 'Raw' },
])

const backLink = computed(() => {
  if (props.backTo) return { to: props.backTo, label: props.backLabel || 'Back' }
  const meta = kindMeta(entity.value?.kind)
  if (meta?.route) {
    return { to: `/${meta.route.split('/')[0]}`, label: `All ${meta.plural.toLowerCase()}` }
  }
  return { to: '/browse', label: 'Browse library' }
})
</script>

<template>
  <div class="shell detail-page">
    <NuxtLink :to="backLink.to" class="back-link">← {{ backLink.label }}</NuxtLink>

    <!-- Loading skeleton keeps hero geometry stable. -->
    <div v-if="pending && !entity" class="detail-skeleton" aria-hidden="true">
      <span class="detail-skeleton__art skeleton" />
      <div class="detail-skeleton__body">
        <span class="skeleton detail-skeleton__line" style="width: 30%" />
        <span class="skeleton detail-skeleton__line detail-skeleton__line--title" />
        <span class="skeleton detail-skeleton__line" style="width: 80%" />
        <span class="skeleton detail-skeleton__line" style="width: 65%" />
      </div>
    </div>

    <EmptyState
      v-else-if="!entity"
      title="Entity unavailable."
      :message="error || 'This canonical entity could not be loaded. It may have been merged or removed.'"
    >
      <NuxtLink :to="backLink.to" class="btn btn--ghost">← {{ backLink.label }}</NuxtLink>
    </EmptyState>

    <template v-else>
      <EntityHero :entity="entity" :images="images" :refreshing="refreshing" @refresh="refreshFromProviders" />

      <div v-if="error" class="notice">
        <strong>That didn't work.</strong><span>{{ error }}</span>
      </div>

      <EntityTabs :tabs="tabs" />

      <div class="tab-content">
        <div v-show="activeTab === 'overview'">
          <slot :entity="entity" :images="images">
            <GenericOverview :entity="entity" />
          </slot>
        </div>
        <ArtworkGallery v-if="activeTab === 'artwork'" :images="images" />
        <ProvenancePanel v-else-if="activeTab === 'provenance'" :entity="entity" />
        <RawDocumentPanel v-else-if="activeTab === 'raw'" :entity="entity" />
      </div>
    </template>
  </div>
</template>

<style scoped>
.detail-skeleton {
  display: grid;
  grid-template-columns: minmax(11rem, 16rem) 1fr;
  gap: clamp(1.75rem, 4vw, 3.5rem);
  padding-top: clamp(1.5rem, 3vw, 2.5rem);
}
.detail-skeleton__art { aspect-ratio: 2 / 3; border-radius: var(--radius); }
.detail-skeleton__body { display: flex; flex-direction: column; gap: 0.9rem; padding-top: 1rem; }
.detail-skeleton__line { height: 0.85rem; border-radius: 4px; }
.detail-skeleton__line--title { height: 2.4rem; width: 60%; }

@media (max-width: 720px) {
  .detail-skeleton { grid-template-columns: 1fr; }
  .detail-skeleton__art { width: min(15rem, 62vw); }
}
</style>
