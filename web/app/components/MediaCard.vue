<script setup lang="ts">
import type { CardShape } from '~/utils/kinds'
import type { EntityDocument, EntitySummary } from '~/utils/types'

const props = withDefaults(defineProps<{
  entity: EntitySummary | EntityDocument | Record<string, any>
  shape?: CardShape
  variant?: 'thumb' | 'card'
}>(), { variant: 'card' })

const to = computed(() => entityPath(props.entity as any))
const shape = computed<CardShape>(() => props.shape ?? cardShape((props.entity as any)?.kind))
const title = computed(() => entityTitle(props.entity))
const subtitle = computed(() => entitySubtitle(props.entity))
const kindText = computed(() => entityKindLabel(props.entity))
const imageId = computed(() => entityImageId(props.entity))
</script>

<template>
  <NuxtLink :to="to" class="card" :class="`card--${shape}`">
    <span class="card__art">
      <MetadataImage :image-id="imageId" :alt="title" :variant="variant" />
    </span>
    <span class="card__body">
      <small class="card__kind">{{ kindText }}</small>
      <strong class="card__title">{{ title }}</strong>
      <span v-if="subtitle" class="card__meta">{{ subtitle }}</span>
    </span>
  </NuxtLink>
</template>

<style scoped>
.card {
  display: flex;
  flex-direction: column;
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--line-soft);
  border-radius: var(--radius);
  background: var(--panel);
  transition: transform 0.18s ease, border-color 0.18s ease;
}
.card:hover { transform: translateY(-3px); border-color: #5a5236; }
.card:focus-visible { outline-offset: 3px; }

.card__art {
  display: block;
  width: 100%;
  overflow: hidden;
  background: #12171c;
}
.card--poster .card__art { aspect-ratio: 2 / 3; }
.card--portrait .card__art { aspect-ratio: 3 / 4; }
.card--square .card__art { aspect-ratio: 1 / 1; }
.card--landscape .card__art { aspect-ratio: 16 / 9; }

.card__body {
  display: flex;
  flex-direction: column;
  min-width: 0;
  padding: 0.7rem 0.75rem 0.85rem;
}
.card__kind {
  color: var(--gold);
  font-family: var(--font-mono);
  font-size: 0.56rem;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.card__title {
  margin-top: 0.35rem;
  overflow: hidden;
  font-size: 0.82rem;
  font-weight: 600;
  line-height: 1.25;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.card__meta {
  margin-top: 0.2rem;
  overflow: hidden;
  color: var(--muted-2);
  font-size: 0.68rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
