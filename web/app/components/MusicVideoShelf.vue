<script setup lang="ts">
import type { MusicVideo } from '~/utils/types'

// Artist music videos from `data.music_videos`, as a MediaShelf mirroring
// VideoGallery's card treatment. Entries carry full (YouTube) URLs, so the
// thumbnail is derived from the video id in the URL; the provider description
// doubles as the hover title.
const props = withDefaults(defineProps<{ videos?: MusicVideo[] }>(), { videos: () => [] })

function youtubeId(url: string): string {
  const match = url.match(/(?:[?&]v=|youtu\.be\/|\/embed\/|\/shorts\/)([\w-]{6,})/)
  return match?.[1] ?? ''
}

const videos = computed(() =>
  (props.videos ?? [])
    .map(video => {
      const url = formatValue(video.url)
      const key = youtubeId(url)
      return {
        id: formatValue(video.provider_video_id) || url,
        title: formatValue(video.track_title) || 'Music video',
        url,
        description: formatValue(video.description),
        thumb: key ? `https://img.youtube.com/vi/${key}/mqdefault.jpg` : '',
      }
    })
    .filter(video => video.url),
)
</script>

<template>
  <MediaShelf title="Music videos" kicker="Watch" :items="videos" shape="landscape" :item-key="v => v.id">
    <template #default="{ item: video }">
      <a :href="video.url" target="_blank" rel="noopener noreferrer" class="video" :title="video.description || video.title">
        <span class="video__thumb">
          <img v-if="video.thumb" :src="video.thumb" :alt="video.title" loading="lazy">
          <span class="video__play" aria-hidden="true">▶</span>
        </span>
        <span class="video__body">
          <strong>{{ video.title }}</strong>
          <small v-if="video.description">{{ video.description }}</small>
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
.video__body strong { overflow: hidden; font-size: 0.78rem; font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.video__body small { overflow: hidden; margin-top: 0.25rem; color: var(--muted-2); font-size: 0.62rem; text-overflow: ellipsis; white-space: nowrap; }
</style>
