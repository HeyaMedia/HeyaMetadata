<script setup lang="ts">
import type { CardShape } from '~/utils/kinds'

// Reusable collection shelf. Default: a horizontal rail (~a row of cards) with
// prev/next pan buttons; "Expand all" folds it out into a full grid. Off-screen
// cells are virtualized with `content-visibility` (paired with MetadataImage's
// lazy loading), so a 200-item filmography never renders or fetches everything
// up front. Callers provide the card via the default scoped slot.
const props = withDefaults(defineProps<{
  title: string
  kicker?: string
  items: any[]
  shape?: CardShape
  itemKey?: (item: any, index: number) => string | number
}>(), { kicker: '', shape: 'poster' })

const expanded = ref(false)
const rail = ref<HTMLElement | null>(null)
const atStart = ref(true)
const atEnd = ref(false)

function updateEdges() {
  const el = rail.value
  if (!el) return
  atStart.value = el.scrollLeft <= 4
  atEnd.value = el.scrollLeft + el.clientWidth >= el.scrollWidth - 4
}
function pan(direction: number) {
  const el = rail.value
  if (!el) return
  el.scrollBy({ left: direction * Math.max(240, el.clientWidth * 0.8), behavior: 'smooth' })
}
function keyOf(item: any, index: number) {
  return props.itemKey ? props.itemKey(item, index) : (item?.id ?? index)
}

// Recompute edges whenever the rail appears/changes. A template @scroll binding
// (below) drives updates on scroll — attaching in onMounted would miss the rail
// for shelves whose items load asynchronously (the section is v-if'd on
// items.length, so the rail does not exist yet at mount).
watch([() => props.items, expanded], () => nextTick(updateEdges), { immediate: true })
onMounted(() => nextTick(updateEdges))

</script>

<template>
  <section v-if="items.length" class="shelf detail-section">
    <header class="section-head">
      <div>
        <span v-if="kicker" class="section-label">{{ kicker }}</span>
        <h2>{{ title }} <small>{{ items.length }}</small></h2>
      </div>
      <div class="shelf__controls">
        <template v-if="!expanded">
          <button type="button" class="shelf__nav" :disabled="atStart" aria-label="Scroll left" @click="pan(-1)">‹</button>
          <button type="button" class="shelf__nav" :disabled="atEnd" aria-label="Scroll right" @click="pan(1)">›</button>
        </template>
        <button type="button" class="btn--link shelf__toggle" :aria-expanded="expanded" @click="expanded = !expanded">
          {{ expanded ? 'Collapse' : 'Expand all' }}
        </button>
      </div>
    </header>

    <div v-if="!expanded" ref="rail" class="rail-track shelf__rail" :class="`is-${shape}`" @scroll.passive="updateEdges">
      <div v-for="(item, index) in items" :key="keyOf(item, index)" class="shelf-cell">
        <slot :item="item" :index="index" />
      </div>
    </div>
    <div v-else class="media-grid" :class="`is-${shape}`">
      <div v-for="(item, index) in items" :key="keyOf(item, index)" class="shelf-cell-grid">
        <slot :item="item" :index="index" />
      </div>
    </div>
  </section>
</template>

<style scoped>
.shelf__controls { display: flex; align-items: center; gap: 0.6rem; }
.shelf__nav {
  display: grid;
  place-items: center;
  width: 1.9rem;
  height: 1.9rem;
  padding-bottom: 2px;
  border: 1px solid var(--line-strong);
  border-radius: 50%;
  background: var(--panel);
  color: var(--text-dim);
  font-size: 1.05rem;
  line-height: 1;
}
.shelf__nav:hover:not(:disabled) { border-color: var(--gold); color: var(--gold); }
.shelf__nav:disabled { opacity: 0.32; cursor: default; }
.shelf__toggle { color: var(--muted); }
.shelf__toggle:hover { color: var(--gold); }

/* The card fills its cell's width in both layouts. */
.shelf-cell, .shelf-cell-grid { min-width: 0; }
.shelf-cell > :deep(*), .shelf-cell-grid > :deep(*) { width: 100%; }

/* Virtualize off-screen rail cells; reserve height so scroll geometry holds. */
.shelf__rail .shelf-cell { content-visibility: auto; }
.shelf__rail.is-poster .shelf-cell { contain-intrinsic-size: auto 310px; }
.shelf__rail.is-portrait .shelf-cell { contain-intrinsic-size: auto 255px; }
.shelf__rail.is-square .shelf-cell { contain-intrinsic-size: auto 250px; }
.shelf__rail.is-landscape .shelf-cell { contain-intrinsic-size: auto 205px; }
</style>
