<script setup lang="ts">
// Named organisations with their logo artwork — movie `studios[]`, TV
// `networks[]`/`organizations[]`, all of which may carry logo_image_id. Falls
// back to a text chip when an entry has no logo. Title/kicker are overridable so
// the same strip renders "Studios", "Networks", or "Companies".
const props = withDefaults(defineProps<{ studios?: any[]; title?: string; kicker?: string }>(), {
  title: 'Studios',
  kicker: 'Production',
})

interface Studio { name: string; logoId?: string; role: string }

const studios = computed<Studio[]>(() => {
  const seen = new Set<string>()
  const out: Studio[] = []
  for (const studio of props.studios ?? []) {
    const name = formatValue(studio?.name ?? studio)
    if (!name || seen.has(name.toLowerCase())) continue
    seen.add(name.toLowerCase())
    out.push({ name, logoId: studio?.logo_image_id, role: titleCase(studio?.role) })
  }
  return out
})

const hasLogos = computed(() => studios.value.some(s => s.logoId))
</script>

<template>
  <OverviewPanel v-if="studios.length" :title="title" :kicker="kicker" full>
    <div class="studios" :class="{ 'has-logos': hasLogos }">
      <div v-for="studio in studios" :key="studio.name" class="studio" :title="studio.role || undefined">
        <span v-if="studio.logoId" class="studio__logo">
          <MetadataImage :image-id="studio.logoId" :alt="studio.name" variant="thumb" />
        </span>
        <span class="studio__name">{{ studio.name }}</span>
      </div>
    </div>
  </OverviewPanel>
</template>

<style scoped>
.studios { display: flex; flex-wrap: wrap; gap: 0.75rem 1rem; align-items: stretch; }
.studio {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  justify-content: space-between;
  padding: 0.75rem 0.9rem;
  border: 1px solid var(--line-soft);
  border-radius: var(--radius-sm);
  background: var(--panel-2);
}
.studio__logo {
  display: block;
  width: clamp(4rem, 9vw, 6.5rem);
  height: 2.5rem;
}
.studio__logo :deep(.metadata-image) { width: 100%; height: 100%; background: transparent; }
.studio__logo :deep(img) { object-fit: contain; object-position: left center; }
.studio__name { color: var(--text-dim); font-size: 0.72rem; }
/* When no studio has a logo, render a compact chip row instead. */
.studios:not(.has-logos) .studio { flex-direction: row; padding: 0.4rem 0.7rem; }
.studios:not(.has-logos) .studio__name { font-size: 0.74rem; }
</style>
