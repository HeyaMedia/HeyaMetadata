<script setup lang="ts">
// Product feature showcase for HeyaMediaServer — the media server itself
// (playback, transcoding, clients, networking), not the metadata service.
// Facts mirror the Heya repo: github.com/HeyaMedia/Heya.
useSeoMeta({
  title: 'Features',
  description: 'A fast, self-hosted media server with adaptive playback, hardware transcoding, a real music player, and metadata already sorted.',
  twitterCard: 'summary_large_image',
})

interface Feature { title: string; body: string; path: string }
const FEATURES: Feature[] = [
  {
    title: 'Bring your own client',
    body: 'A Jellyfin-compatible API means your favourite apps just work — point Infuse, Finamp, Streamyfin, or Findroid straight at Heya.',
    path: 'M3 5h13v9H3zM7.5 18h4M9.5 14v4M18 8.5h3v9.5h-3z',
  },
  {
    title: 'Adaptive playback',
    body: 'HLS transcoding negotiates per-client capabilities on the fly, with ASS subtitle rendering and trickplay scrubbing thumbnails.',
    path: 'M9 6.5v11l9-5.5zM3.5 5v14',
  },
  {
    title: 'Hardware transcoding',
    body: 'Offload the heavy lifting to the GPU with dedicated CUDA and OpenVINO image variants — smooth 4K without pinning your CPU.',
    path: 'M2.5 7h17a1 1 0 0 1 1 1v8a1 1 0 0 1-1 1h-17zM5.5 17v3M9.5 17v3M9 12a2 2 0 1 1-4 0 2 2 0 0 1 4 0zM13.5 10.5h4M13.5 13.5h4',
  },
  {
    title: 'A real music player',
    body: 'Gapless and crossfade playback right in the browser, backed by sonic analysis performed on the server for smarter listening.',
    path: 'M4 14V8l14-3v9M4 14a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5zm14-3a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5z',
  },
  {
    title: 'Libraries that keep up',
    body: 'Local disks and SMB mounts, a filesystem watch plus periodic re-scan, and NFO-aware matching so your existing metadata is respected.',
    path: 'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z',
  },
  {
    title: 'Metadata that’s actually right',
    body: 'Backed by the Heya metadata catalog: reconciled titles, correct matches, and artwork cached on your own server — no hotlinking.',
    path: 'M12 3l7 3v5c0 4.2-2.9 7.4-7 8.5-4.1-1.1-7-4.3-7-8.5V6l7-3zM9 11.5l2 2 4-4',
  },
  {
    title: 'Yours, per user',
    body: 'Per-user watch state, favourites, and lists behind session auth — everyone in the house gets their own view of the library.',
    path: 'M9 11a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM3.5 19.5a5.5 5.5 0 0 1 11 0M16.5 5.4a3 3 0 0 1 0 5.7M18 14.2a5.5 5.5 0 0 1 2.5 4.6',
  },
  {
    title: 'Reach it anywhere',
    body: 'An optional tsnet node puts your server on your tailnet — reach it from anywhere with no port forwarding and nothing exposed.',
    path: 'M12 3a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5zM5 21a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5zM19 21a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5zM12 8v3.5M11 12.5l-4 2.5M13 12.5l4 2.5',
  },
]

const CLIENTS = ['Infuse', 'Finamp', 'Streamyfin', 'Findroid']
</script>

<template>
  <div class="shell page features">
    <header class="page-heading">
      <div>
        <span class="section-label">Features</span>
        <h1 class="editorial">Your media, <em>done right.</em></h1>
        <p>HeyaMediaServer is a fast, self-hosted media server: one Go binary, one Postgres, and a clean streaming experience for movies, TV, and music — with the metadata already sorted.</p>
      </div>
      <NuxtLink to="/downloads" class="btn btn--gold features__cta">Download</NuxtLink>
    </header>

    <section class="clients">
      <div class="clients__label">
        <span class="section-label">Jellyfin-compatible API</span>
        <p>Already have an app you love? Keep it.</p>
      </div>
      <ul class="clients__list">
        <li v-for="client in CLIENTS" :key="client" class="chip chip--accent">{{ client }}</li>
        <li class="chip">&amp; more</li>
      </ul>
    </section>

    <section class="features__grid">
      <article v-for="feature in FEATURES" :key="feature.title" class="feature">
        <span class="feature__icon" aria-hidden="true">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" width="22" height="22">
            <path :d="feature.path" />
          </svg>
        </span>
        <h2>{{ feature.title }}</h2>
        <p>{{ feature.body }}</p>
      </article>
    </section>

    <section class="features__foot">
      <div>
        <span class="section-label">Ready when you are</span>
        <h2>One container. Point it at your media. Done.</h2>
      </div>
      <div class="features__foot-actions">
        <NuxtLink to="/downloads" class="btn btn--gold">Download HeyaMediaServer</NuxtLink>
        <a href="https://github.com/HeyaMedia/Heya" target="_blank" rel="noopener noreferrer" class="btn btn--ghost">View on GitHub ↗</a>
      </div>
    </section>
  </div>
</template>

<style scoped>
.features__cta { align-self: flex-end; }

.clients {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 1rem 1.5rem;
  margin-bottom: 2rem;
  padding: 1.25rem 1.4rem;
  border: 1px solid var(--line);
  border-radius: var(--radius);
  background: var(--panel);
}
.clients__label p { margin: 0.35rem 0 0; color: var(--muted); font-size: 0.78rem; }
.clients__list { display: flex; flex-wrap: wrap; gap: 0.5rem; margin: 0; padding: 0; list-style: none; }

.features__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(268px, 1fr));
  gap: 1rem;
}
.feature {
  padding: 1.4rem;
  border: 1px solid var(--line);
  border-radius: var(--radius);
  background: rgba(17, 22, 25, 0.6);
  transition: border-color 0.18s ease;
}
.feature:hover { border-color: var(--line-strong); }
.feature__icon {
  display: grid;
  place-items: center;
  width: 2.6rem;
  height: 2.6rem;
  margin-bottom: 1rem;
  border: 1px solid var(--line-strong);
  border-radius: 50%;
  color: var(--gold);
}
.feature h2 { margin: 0 0 0.5rem; font-size: 1rem; font-weight: 500; }
.feature p { margin: 0; color: var(--muted); font-size: 0.78rem; line-height: 1.65; }

.features__foot {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 1.5rem;
  margin-top: 2.5rem;
  padding: 2rem;
  border: 1px solid var(--line);
  border-radius: var(--radius);
  background: linear-gradient(120deg, rgba(241, 201, 107, 0.06), rgba(17, 22, 25, 0.4));
}
.features__foot h2 { margin: 0.5rem 0 0; max-width: 30rem; font-size: 1.35rem; font-weight: 500; letter-spacing: -0.02em; }
.features__foot-actions { display: flex; flex-wrap: wrap; gap: 0.75rem; }
</style>
