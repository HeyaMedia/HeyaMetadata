import { ref, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { useHeyaApi } from './useHeyaApi'
import { useLocale } from './useLocale'
import type { EntityDocument, ImagesResponse } from '../utils/types'

// URL + locale driven entity loading. A direct load, reload, pasted link, or
// locale change all reconstruct the view from the id alone — nothing depends on
// a previously populated ref. Detail pages pass `route.params.id`.

export function useEntity(idInput: MaybeRefOrGetter<string>) {
  const api = useHeyaApi()
  const { signature } = useLocale()

  const entity = ref<EntityDocument | null>(null)
  const images = ref<ImagesResponse | null>(null)
  const pending = ref(false)
  const error = ref('')
  const refreshing = ref(false)

  let generation = 0

  async function load() {
    const id = toValue(idInput)
    const current = ++generation
    if (!id) {
      entity.value = null
      images.value = null
      error.value = ''
      return
    }
    pending.value = true
    error.value = ''
    try {
      const [doc, imageSet] = await Promise.all([
        api.entity(id),
        api.entityImages(id).catch(() => null),
      ])
      if (current !== generation) return
      entity.value = doc
      images.value = imageSet
    } catch (reason: any) {
      if (current !== generation) return
      entity.value = null
      images.value = null
      error.value = reason?.message || 'Failed to load entity'
    } finally {
      if (current === generation) pending.value = false
    }
  }

  /** Trigger a provider refresh job, then reload the rebuilt entity. */
  async function refreshFromProviders() {
    const id = toValue(idInput)
    if (!id || refreshing.value) return
    refreshing.value = true
    error.value = ''
    try {
      const job = await api.refreshEntity(id)
      if (job?.id != null) await api.pollJob(job.id)
      await load()
    } catch (reason: any) {
      error.value = reason?.message || 'Refresh failed'
    } finally {
      refreshing.value = false
    }
  }

  watch([() => toValue(idInput), signature], load, { immediate: true })

  return { entity, images, pending, error, refreshing, reload: load, refreshFromProviders }
}
