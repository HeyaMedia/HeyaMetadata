import { ref } from 'vue'

// Shared horizontal-rail scroll state for MediaRail and MediaShelf: tracks
// whether the rail is at its start/end (to disable the pan arrows) and scrolls
// by roughly a page. Bind `rail` as the track ref and call `update` from a
// template @scroll handler plus whenever the item set changes — attaching a
// listener in onMounted would miss rails whose items load asynchronously.
export function useRailPan() {
  const rail = ref<HTMLElement | null>(null)
  const atStart = ref(true)
  const atEnd = ref(false)

  function update() {
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

  return { rail, atStart, atEnd, pan, update }
}
