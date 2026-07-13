<script setup lang="ts">
// Trailers & clips from movie/tv `data.videos`. YouTube-hosted entries get a
// thumbnail + link. External thumbnails load directly (this is a same-origin dev
// app, not a CSP-restricted artifact).
const props = withDefaults(defineProps<{ videos?: any[]; limit?: number }>(), { videos: () => [], limit: 12 })

interface Video { key: string; name: string; type: string; url: string; thumb: string; official: boolean }

const videos = computed<Video[]>(() =>
  (props.videos ?? [])
    .filter(video => /youtube/i.test(video.host ?? '') && video.key)
    .slice(0, props.limit)
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
  <section v-if="videos.length" class="videos">
    <header class="section-head">
      <div><span class="section-label">Watch</span><h2>Trailers &amp; clips</h2></div>
    </header>
    <div class="rail-track is-landscape">
      <a v-for="video in videos" :key="video.key" :href="video.url" target="_blank" rel="noopener noreferrer" class="video">
        <span class="video__thumb">
          <img :src="video.thumb" :alt="video.name" loading="lazy">
          <span class="video__play" aria-hidden="true">▶</span>
        </span>
        <span class="video__body">
          <small>{{ video.type }}<template v-if="video.official"> · Official</template></small>
          <strong>{{ video.name }}</strong>
        </span>
      </a>
    </div>
  </section>
</template>

<style scoped>
.videos { margin-top: 2.5rem; }
.section-head { margin-bottom: 1.25rem; padding-bottom: 0.75rem; border-bottom: 1px solid var(--line); }
.section-head h2 { margin: 0.3rem 0 0; font-size: 1.2rem; font-weight: 500; }
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
.video__body { display: flex; flex-direction: column; padding: 0.6rem 0.7rem 0.75rem; }
.video__body small { color: var(--gold); font-family: var(--font-mono); font-size: 0.56rem; text-transform: uppercase; }
.video__body strong { margin-top: 0.25rem; overflow: hidden; font-size: 0.78rem; font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
</style>
