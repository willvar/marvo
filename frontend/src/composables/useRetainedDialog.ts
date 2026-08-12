import { ref, shallowRef } from 'vue'

/**
 * Keeps a dialog's render payload alive until Ark UI finishes its exit
 * transition. `open` and `payload` must not share the same lifecycle: closing
 * starts the transition, while clearing the payload ends it.
 */
export function useRetainedDialog<T>() {
  const open = ref(false)
  const payload = shallowRef<T | null>(null)

  function show(nextPayload: T) {
    payload.value = nextPayload
    open.value = true
  }

  function close() {
    open.value = false
  }

  function updateOpen(nextOpen: boolean, canClose = true) {
    if (!nextOpen && !canClose) return
    open.value = nextOpen
  }

  function clearAfterExit() {
    if (open.value) return false
    payload.value = null
    return true
  }

  function reset() {
    open.value = false
    payload.value = null
  }

  return { open, payload, show, close, updateOpen, clearAfterExit, reset }
}
