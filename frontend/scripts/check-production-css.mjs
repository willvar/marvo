import { readdir, readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const assetsDirectory = fileURLToPath(new URL('../dist/assets/', import.meta.url))
const files = (await readdir(assetsDirectory)).filter((name) => name.endsWith('.css'))
const incompatible = []

for (const file of files) {
  const css = await readFile(join(assetsDirectory, file), 'utf8')
  if (/@media\s+not all\b/.test(css) || /@media\s*\([^)]*(?:width|height)\s*[<>]=/.test(css)) {
    incompatible.push(file)
  }
}

if (incompatible.length > 0) {
  throw new Error(
    `Production CSS contains media queries unsupported by the Android WebView: ${incompatible.join(', ')}`,
  )
}

console.log(`Checked ${files.length} production CSS assets for Android WebView media-query compatibility.`)
