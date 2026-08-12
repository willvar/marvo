import { readdir, readFile } from 'node:fs/promises'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

const sourceRoot = fileURLToPath(new URL('../src/', import.meta.url))
const unsafeOpenExpression = /(?:!!|Boolean\s*\(|(?:!==?|===?)\s*(?:null|undefined)\b)/
const violations = []

async function visit(directory) {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) {
      await visit(path)
      continue
    }
    if (!entry.isFile() || !entry.name.endsWith('.vue')) continue

    const source = await readFile(path, 'utf8')
    for (const match of source.matchAll(/<Dialog\.Root\b[\s\S]*?>/g)) {
      const expression = match[0].match(/:open\s*=\s*"([^"]+)"/)?.[1]
      if (!expression || !unsafeOpenExpression.test(expression)) continue
      const line = source.slice(0, match.index).split('\n').length
      violations.push(`${relative(sourceRoot, path)}:${line} uses :open="${expression}"`)
    }
  }
}

await visit(sourceRoot)

if (violations.length) {
  console.error('Dialog open state must not be derived from render payload. Use useRetainedDialog instead:')
  for (const violation of violations) console.error(`- ${violation}`)
  process.exitCode = 1
}
