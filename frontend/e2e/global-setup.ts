import { rm, mkdir } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

export default async function globalSetup() {
  // Containerized WebKit can reuse servers started on the host. In that case
  // the host has already reset the test directory before those servers start.
  if (process.env.E2E_REUSE_SERVERS === '1') return

  const here = dirname(fileURLToPath(import.meta.url))
  const dataDir = join(here, '.data')
  await rm(dataDir, { recursive: true, force: true })
  await mkdir(dataDir, { recursive: true, mode: 0o700 })
}
