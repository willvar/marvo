export interface NoteDraft {
  key: string
  instanceToken: string
  draftId: string
  titleAtSave: string
  baseRevision: string
  baseContent: string
  content: string
  updatedAt: number
}

const DB_NAME = 'marvo-drafts'
const STORE_NAME = 'drafts'
const BRANCH_KEY = 'marvo.draftBranchId'

export function currentDraftId(): string {
  let id = sessionStorage.getItem(BRANCH_KEY)
  if (!id) {
    id =
      typeof crypto.randomUUID === 'function'
        ? crypto.randomUUID()
        : `${Date.now().toString(36)}-${crypto.getRandomValues(new Uint32Array(4)).join('-')}`
    sessionStorage.setItem(BRANCH_KEY, id)
  }
  return id
}

function draftKey(instanceToken: string, draftId: string) {
  return `${instanceToken}:${draftId}`
}

export async function saveDraft(draft: Omit<NoteDraft, 'key' | 'updatedAt'>): Promise<NoteDraft> {
  const record: NoteDraft = {
    ...draft,
    key: draftKey(draft.instanceToken, draft.draftId),
    updatedAt: Date.now(),
  }
  const db = await openDraftDB()
  await transactionPromise(db, 'readwrite', (store) => store.put(record))
  db.close()
  return record
}

export async function getDraft(instanceToken: string, draftId: string): Promise<NoteDraft | undefined> {
  const db = await openDraftDB()
  const result = await requestPromise<NoteDraft | undefined>(
    db.transaction(STORE_NAME, 'readonly').objectStore(STORE_NAME).get(draftKey(instanceToken, draftId)),
  )
  db.close()
  return result
}

export async function listBranchDrafts(draftId: string): Promise<NoteDraft[]> {
  const db = await openDraftDB()
  const tx = db.transaction(STORE_NAME, 'readonly')
  const records = await requestPromise<NoteDraft[]>(tx.objectStore(STORE_NAME).index('by_branch').getAll(draftId))
  db.close()
  return records.sort((a, b) => b.updatedAt - a.updatedAt)
}

export async function removeDraft(instanceToken: string, draftId: string): Promise<void> {
  const db = await openDraftDB()
  await transactionPromise(db, 'readwrite', (store) => store.delete(draftKey(instanceToken, draftId)))
  db.close()
}

function openDraftDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, 1)
    request.onupgradeneeded = () => {
      const db = request.result
      const store = db.createObjectStore(STORE_NAME, { keyPath: 'key' })
      store.createIndex('by_branch', 'draftId', { unique: false })
      store.createIndex('by_updated', 'updatedAt', { unique: false })
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error || new Error('failed to open draft database'))
    request.onblocked = () => reject(new Error('draft database upgrade is blocked'))
  })
}

function requestPromise<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error || new Error('draft database request failed'))
  })
}

function transactionPromise(
  db: IDBDatabase,
  mode: IDBTransactionMode,
  action: (store: IDBObjectStore) => IDBRequest,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, mode)
    action(tx.objectStore(STORE_NAME))
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error || new Error('draft database transaction failed'))
    tx.onabort = () => reject(tx.error || new Error('draft database transaction aborted'))
  })
}
