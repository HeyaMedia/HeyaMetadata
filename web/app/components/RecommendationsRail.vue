<script setup lang="ts">
// "More like this" from movie data.recommendations, as a MediaShelf. Resolved
// titles (entity_id) link; the rest stay display-only (never a silent
// resolution on click).
const props = withDefaults(defineProps<{ recommendations?: any[] }>(), { recommendations: () => [] })

const items = computed(() =>
  (props.recommendations ?? []).map(rec => ({
    title: formatValue(rec.title) || 'Untitled',
    year: rec.year,
    imageId: rec.image_id as string | undefined,
    to: rec.entity_id ? entityPath({ id: rec.entity_id, kind: 'movie' }) : undefined,
  })),
)
const linkTag = resolveComponent('NuxtLink')
</script>

<template>
  <MediaShelf title="More like this" kicker="Neighbours" :items="items" shape="poster" :item-key="(_, i) => i">
    <template #default="{ item: rec }">
      <component :is="rec.to ? linkTag : 'div'" :to="rec.to" class="rec-card" :class="{ 'is-ghost': !rec.to }">
        <span class="rec-card__art"><MetadataImage :image-id="rec.imageId" :alt="rec.title" variant="card" /></span>
        <span class="rec-card__body">
          <small>{{ rec.year || '—' }}</small>
          <strong>{{ rec.title }}</strong>
          <span class="rec-card__status" :class="{ 'is-canonical': rec.to }">{{ rec.to ? 'Canonical ↗' : 'Not ingested' }}</span>
        </span>
      </component>
    </template>
  </MediaShelf>
</template>

<style scoped>
.rec-card {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--line-soft);
  border-radius: var(--radius);
  background: var(--panel);
  transition: transform 0.18s ease, border-color 0.18s ease;
}
.rec-card:not(.is-ghost):hover { transform: translateY(-3px); border-color: #5a5236; }
.rec-card.is-ghost { opacity: 0.6; }
.rec-card__art { aspect-ratio: 2 / 3; overflow: hidden; }
.rec-card__body { display: flex; flex-direction: column; padding: 0.55rem 0.65rem 0.7rem; }
.rec-card__body small { color: var(--gold); font-family: var(--font-mono); font-size: 0.56rem; }
.rec-card__body strong { margin-top: 0.28rem; overflow: hidden; font-size: 0.78rem; text-overflow: ellipsis; white-space: nowrap; }
.rec-card__status { margin-top: 0.2rem; color: var(--muted-2); font-size: 0.62rem; }
.rec-card__status.is-canonical { color: var(--green); }
</style>
