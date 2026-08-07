export interface ThemeFile {
  fontFamily?: string
  fontSize?: number | string
  darkMode?: boolean | 'system'
  contentFontSize?: number | string
  contentLineHeight?: number | string
  contentWidth?: number | 'full'
  accentColor?: string
  radius?: number | string
}

export const DEFAULT_FONT_FAMILY =
  '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, "Noto Sans SC", sans-serif'
export const DEFAULT_FONT_SIZE = 14
export const DEFAULT_CONTENT_FONT_SIZE = 15
export const DEFAULT_ACCENT_COLOR = '#4f46e5'

export function normalizeTheme(data: unknown): ThemeFile {
  if (!data || typeof data !== 'object') return {}

  const source = data as Record<string, unknown>
  const next: ThemeFile = {}

  if (typeof source.fontFamily === 'string' && source.fontFamily.trim()) {
    next.fontFamily = source.fontFamily.trim()
  }

  if (typeof source.fontSize === 'number' && source.fontSize >= 10 && source.fontSize <= 24) {
    next.fontSize = source.fontSize
  } else if (typeof source.fontSize === 'string') {
    const parsed = Number.parseFloat(source.fontSize)
    if (Number.isFinite(parsed) && parsed >= 10 && parsed <= 24) {
      next.fontSize = parsed
    }
  }

  if (typeof source.darkMode === 'boolean' || source.darkMode === 'system') {
    next.darkMode = source.darkMode
  }

  next.contentFontSize = numberInRange(source.contentFontSize, 10, 28)
  next.contentLineHeight = numberInRange(source.contentLineHeight, 1.2, 2.4)
  if (source.contentWidth === 'full') {
    next.contentWidth = 'full'
  } else {
    next.contentWidth = numberInRange(source.contentWidth, 560, Number.POSITIVE_INFINITY)
  }
  next.radius = numberInRange(source.radius, 0, 24)

  if (typeof source.accentColor === 'string' && /^#[0-9a-f]{6}$/i.test(source.accentColor.trim())) {
    next.accentColor = source.accentColor.trim()
  }

  return next
}

function numberInRange(value: unknown, min: number, max: number) {
  const parsed = typeof value === 'number' ? value : typeof value === 'string' ? Number.parseFloat(value) : Number.NaN

  if (!Number.isFinite(parsed) || parsed < min || parsed > max) return undefined
  return parsed
}
