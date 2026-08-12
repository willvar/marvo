import { scopedAPIPath } from '../workspace'

function noteAssetFilename(src: string) {
  if (!src || /^(?:[a-z][a-z0-9+.-]*:|\/)/i.test(src)) return null

  const normalized = src.replace(/^\.\//, '')
  if (!normalized.startsWith('assets/')) return null

  const encodedFilename = normalized.slice('assets/'.length)
  let filename = encodedFilename
  try {
    // Markdown renderers URL-encode non-ASCII paths before this conversion runs.
    // Decode one URL segment first so encodeURIComponent below stays idempotent.
    filename = decodeURIComponent(encodedFilename)
  } catch {
    // A malformed percent sequence is a literal filename and will be escaped below.
  }
  if (!filename || filename.includes('/') || filename.includes('\\')) return null

  return filename
}

export function toMarkdownAssetPath(src: string) {
  const filename = noteAssetFilename(src)
  return filename ? `assets/${filename}` : src
}

export function toNoteAssetUrl(src: string, title: string) {
  const filename = noteAssetFilename(src)
  if (!filename) return src

  return scopedAPIPath(`/api/notes/${encodeURIComponent(title)}/assets/${encodeURIComponent(filename)}`)
}

export function toRelativeAssetPath(filename: string) {
  return `assets/${filename}`
}
