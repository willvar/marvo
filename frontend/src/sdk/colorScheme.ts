import { currentUserID } from './workspace'

export type ColorSchemePreference = boolean | 'system'

const COLOR_SCHEME_STORAGE_PREFIX = 'marvo.ui.colorScheme'
let systemColorScheme: MediaQueryList | null = null
let preference: ColorSchemePreference = 'system'

type AndroidThemeBridge = {
  postMessage(value: 'dark' | 'light'): void
}

function resolveSystemColorScheme() {
  if (typeof window === 'undefined') return null
  if (!systemColorScheme) {
    systemColorScheme = window.matchMedia('(prefers-color-scheme: dark)')
    systemColorScheme.addEventListener('change', applyColorScheme)
  }
  return systemColorScheme
}

function applyColorScheme() {
  if (typeof document === 'undefined') return
  const dark = preference === 'system' ? (resolveSystemColorScheme()?.matches ?? false) : preference
  const scheme = dark ? 'dark' : 'light'
  document.documentElement.dataset.colorScheme = scheme
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', dark ? '#1a1b1e' : '#ffffff')

  const bridge = (window as typeof window & { marvoAndroidTheme?: AndroidThemeBridge }).marvoAndroidTheme
  try {
    bridge?.postMessage(scheme)
  } catch {
    // The same frontend also runs in ordinary browsers, where no native bridge exists.
  }
}

function storageKey() {
  const userID = currentUserID()
  return userID ? `${COLOR_SCHEME_STORAGE_PREFIX}.${userID}` : ''
}

function serializedPreference(value: ColorSchemePreference) {
  if (value === 'system') return value
  return value ? 'dark' : 'light'
}

function storedPreference(): ColorSchemePreference {
  const key = storageKey()
  if (!key || typeof window === 'undefined') return 'system'
  try {
    const stored = window.localStorage.getItem(key)
    if (stored === 'dark') return true
    if (stored === 'light') return false
  } catch {
    // Browser storage is optional; the system preference remains a safe fallback.
  }
  return 'system'
}

export function setColorSchemePreference(next: ColorSchemePreference) {
  preference = next
  const key = storageKey()
  if (key && typeof window !== 'undefined') {
    try {
      window.localStorage.setItem(key, serializedPreference(next))
    } catch {
      // Applying the theme must not depend on browser storage being available.
    }
  }
  applyColorScheme()
}

export function restoreColorSchemePreference() {
  preference = storedPreference()
  applyColorScheme()
}
