import { computed, ref, toValue, type MaybeRefOrGetter } from 'vue'
import { useHeyaApi } from './useHeyaApi'
import { useLocale } from './useLocale'
import { sessionCached } from './useSessionCache'
import type { EntityDocument, ImagesResponse } from '../utils/types'

// URL + locale driven entity loading. A direct load, reload, pasted link, or
// locale change all reconstruct the view from the id alone — nothing depends on
// a previously populated ref. Detail pages pass `route.params.id`.
// Each id+locale gets its own useAsyncData slot so revisiting an entity this
// session renders instantly from the payload cache; refreshFromProviders
// bypasses the cache to pick up the rebuilt document.

export function useEntity(idInput: MaybeRefOrGetter<string>) {
  const api = useHeyaApi()
  const { signature } = useLocale()

  const actionError = ref('')
  const refreshing = ref(false)

  const { data, status, error: loadError, refresh } = useAsyncData(
    () => `entity:${toValue(idInput) || 'none'}:${signature()}`,
    async () => {
      const id = toValue(idInput)
      if (!id) return { doc: null as EntityDocument | null, imageSet: null as ImagesResponse | null }
      const [doc, imageSet] = await Promise.all([
        api.entity(id),
        api.entityImages(id).catch(() => null),
      ])
      return { doc, imageSet: imageSet as ImagesResponse | null }
    },
    { getCachedData: sessionCached },
  )

  const entity = computed(() => data.value?.doc ?? null)
  const images = computed(() => data.value?.imageSet ?? null)
  const pending = computed(() => status.value === 'pending')
  const error = computed(() =>
    actionError.value || (loadError.value ? (loadError.value.message || 'Failed to load entity') : ''))

  /** Trigger a provider refresh job, then reload the rebuilt entity. */
  async function refreshFromProviders() {
    const id = toValue(idInput)
    if (!id || refreshing.value) return
    refreshing.value = true
    actionError.value = ''
    try {
      const job = await api.refreshEntity(id)
      if (job?.id != null) await api.pollJob(job.id)
      await refresh()
    } catch (reason: any) {
      actionError.value = reason?.message || 'Refresh failed'
    } finally {
      refreshing.value = false
    }
  }

  return { entity, images, pending, error, refreshing, reload: refresh, refreshFromProviders }
}
