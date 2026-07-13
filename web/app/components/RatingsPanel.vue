<script setup lang="ts">
import type { Rating } from '~/utils/display'

const props = defineProps<{ ratings?: Rating[] }>()

const rows = computed(() => (props.ratings ?? []).filter(rating => rating && rating.value != null))
</script>

<template>
  <OverviewPanel v-if="rows.length" title="Ratings" kicker="Provider-native scales">
    <div class="ratings">
      <div v-for="rating in rows" :key="`${rating.system}:${rating.provider}`" class="rating">
        <strong>{{ rating.value }}</strong>
        <span v-if="rating.scale_max">/ {{ rating.scale_max }}</span>
        <i v-if="rating.scale_max" class="rating__meter"><b :style="{ width: `${Math.min(100, (Number(rating.value) / rating.scale_max) * 100)}%` }" /></i>
        <b>{{ formatKey(rating.system || rating.provider || '') }}</b>
        <small v-if="rating.votes">{{ formatCount(rating.votes) }} votes</small>
      </div>
    </div>
  </OverviewPanel>
</template>

<style scoped>
.ratings { display: flex; flex-wrap: wrap; gap: 2.5rem; }
.rating { display: grid; grid-template-columns: auto auto; align-items: baseline; column-gap: 0.3rem; }
.rating strong { font-family: var(--font-mono); font-size: 1.7rem; font-weight: 400; }
.rating span { color: var(--muted-2); font-size: 0.68rem; }
.rating__meter { grid-column: 1 / -1; overflow: hidden; height: 0.3rem; margin-top: 0.5rem; border-radius: 1rem; background: #252d31; }
.rating__meter b { display: block; height: 100%; border-radius: inherit; background: var(--gold); }
.rating b { grid-column: 1 / -1; margin-top: 0.2rem; color: var(--gold); font-size: 0.64rem; font-weight: 500; }
.rating small { grid-column: 1 / -1; color: var(--muted-2); font-size: 0.58rem; }
</style>
