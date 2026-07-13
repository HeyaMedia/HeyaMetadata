<script setup lang="ts">
// Trailers & clips from movie/tv `data.videos`, as a MediaShelf. YouTube-hosted
// entries get a thumbnail + link (external thumbnails load directly — this is a
// same-origin dev app, not a CSP-restricted artifact).
const props = withDefaults(defineProps<{ videos?: any[] }>(), { videos: () => [] })

const videos = computed(() =>
  (props.videos ?? [])
    .filter(video => /youtube/i.test(video.host ?? '') && video.key)
    .map(video => ({
      key: video.key,
      name: formatValue(video.name) || 'Video',
      type: titleCase(video.type) || 'Clip',
      official: !!video.official,
      url: `https://www.youtube.com/watch?v=${video.key}`,
      thumb: `https://img.youtube.com/vi/${video.key}/mqdefault.jpg`,
    })),
)
</script>

<template>
  <MediaShelf title="Trailers & clips" kicker="Watch" :items="videos" shape="landscape" :item-key="v => v.key">
    <template #default="{ item: video }">
      <a :href="video.url" target="_blank" rel="noopener noreferrer" class="video">
        <span class="video__thumb">
          <img :src="video.thumb" :alt="video.name" loading="lazy">
          <span class="video__play" aria-hidden="true">▶</span>
        </span>
        <span class="video__body">
          <small>{{ video.type }}<template v-if="video.official"> · Official</template></small>
          <strong>{{ video.name }}</strong>
        </span>
      </a>
    </template>
  </MediaShelf>
</template>

<style scoped>
.video {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--line-soft);
  border-radius: var(--radius);
  background: var(--panel);
  transition: transform 0.18s ease, border-color 0.18s ease;
}
.video:hover { transform: translateY(-3px); border-color: #5a5236; }
.video__thumb { position: relative; aspect-ratio: 16 / 9; overflow: hidden; background: #12171c; }
.video__thumb img { width: 100%; height: 100%; object-fit: cover; }
.video__play {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  color: #fff;
  font-size: 1.4rem;
  text-shadow: 0 2px 12px rgba(0, 0, 0, 0.7);
  opacity: 0.9;
}
.video__body { display: flex; flex-direction: column; padding: 0.55rem 0.65rem 0.7rem; }
.video__body small { color: var(--gold); font-family: var(--font-mono); font-size: 0.56rem; text-transform: uppercase; }
.video__body strong { margin-top: 0.25rem; overflow: hidden; font-size: 0.78rem; font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
</style>
