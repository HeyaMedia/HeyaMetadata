import { ref } from 'vue'

// Shared quick-look song drawer state. Any track list (artist top tracks, album
// or release tracklist) can open the drawer for a canonical recording id; a
// single <SongPanel> mounted at the app root renders it. Module-level singleton
// so every caller drives the same drawer.
const recordingId = ref<string | null>(null)

export function useSongPanel() {
  function open(id?: string | null) {
    if (id) recordingId.value = id
  }
  function close() {
    recordingId.value = null
  }
  return { recordingId, open, close }
}
