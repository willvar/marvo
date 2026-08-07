import { diff3Merge } from 'node-diff3'

interface MergeConflictChunk {
  local: string[]
  base: string[]
  remote: string[]
}

type MergeSegment =
  { kind: 'clean'; lines: string[] } | { kind: 'conflict'; conflictIndex: number; chunk: MergeConflictChunk }

interface ThreeWayMerge {
  clean: boolean
  merged: string
  segments: MergeSegment[]
  conflicts: MergeConflictChunk[]
}

export function threeWayMerge(base: string, local: string, remote: string): ThreeWayMerge {
  const regions = diff3Merge(splitLines(local), splitLines(base), splitLines(remote), {
    excludeFalseConflicts: true,
    stringSeparator: '\n',
  }) as Array<{ ok?: string[]; conflict?: { a: string[]; o: string[]; b: string[] } }>
  const segments: MergeSegment[] = []
  const conflicts: MergeConflictChunk[] = []
  for (const region of regions) {
    if (region.ok) {
      segments.push({ kind: 'clean', lines: region.ok })
      continue
    }
    if (region.conflict) {
      const chunk = {
        local: region.conflict.a || [],
        base: region.conflict.o || [],
        remote: region.conflict.b || [],
      }
      const conflictIndex = conflicts.push(chunk) - 1
      segments.push({ kind: 'conflict', conflictIndex, chunk })
    }
  }
  return {
    clean: conflicts.length === 0,
    merged: joinLines(segments.flatMap((segment) => (segment.kind === 'clean' ? segment.lines : segment.chunk.local))),
    segments,
    conflicts,
  }
}

export function resolveMerge(merge: ThreeWayMerge, choices: Array<'local' | 'remote' | 'both' | null>): string {
  const lines = merge.segments.flatMap((segment) => {
    if (segment.kind === 'clean') return segment.lines
    const choice = choices[segment.conflictIndex]
    if (choice === 'remote') return segment.chunk.remote
    if (choice === 'both') return [...segment.chunk.local, ...segment.chunk.remote]
    return segment.chunk.local
  })
  return joinLines(lines)
}

function splitLines(value: string): string[] {
  return value.split('\n')
}

function joinLines(lines: string[]): string {
  return lines.join('\n')
}
