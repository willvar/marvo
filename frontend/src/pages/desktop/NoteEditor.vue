<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Dialog } from '@ark-ui/vue/dialog'
import {
  ArrowLeftOutlined,
  CloseOutlined,
  DeleteOutlined,
  DiffOutlined,
  EditOutlined,
  EyeOutlined,
  FileAddOutlined,
} from '@ant-design/icons-vue'
import { useNoteStore } from '../../stores/note'
import {
  ApiError,
  api,
  currentDraftId,
  getDraft,
  listBranchDrafts,
  registerEditorPreparation,
  removeDraft,
  saveDraft,
  type NoteDetail,
  type NoteDraft,
} from '../../sdk'

const EditorCore = defineAsyncComponent(() => import('../../components/EditorCore.vue'))
const NotePreview = defineAsyncComponent(() => import('../../components/NotePreview.vue'))
const MergeConflictDialog = defineAsyncComponent(() => import('../../components/MergeConflictDialog.vue'))
type NoteSaveState = 'saved' | 'draft' | 'saving' | 'conflict' | 'error'

interface NoteSaveStatus {
  state: NoteSaveState
  label: string
  error: string
}

const emit = defineEmits<{
  noteMutationBlocked: [blocked: boolean]
  noteSaveStatus: [status: NoteSaveStatus | null]
}>()

interface EditorAPI {
  updateContent: (content: string) => void
  setEditable: (editable: boolean) => void
}

interface MergeState {
  open: boolean
  base: string
  local: string
  remote: string
  remoteSnapshot: NoteDetail | null
  reason: string
}

const route = useRoute()
const router = useRouter()
const noteStore = useNoteStore()
const title = computed(() => String(route.params.title || ''))
const branchId = currentDraftId()

const loading = ref(true)
const notFound = ref(false)
const loadError = ref('')
const editMode = ref(false)
const editor = ref<EditorAPI | null>(null)
const serverBase = ref<NoteDetail | null>(null)
const localContent = ref('')
const tags = ref<string[]>([])
const addTagInput = ref('')
const saveState = ref<NoteSaveState>('saved')
const saveError = ref('')
const showDeleteConfirm = ref(false)
const subscribedTitle = ref('')
const toolbarTarget = ref<HTMLElement>()
const orphanDraft = ref<NoteDraft | null>(null)
const orphanRemote = ref<NoteDetail | null>(null)
const missingDraft = ref<NoteDraft | null>(null)
const mergeState = ref<MergeState>({ open: false, base: '', local: '', remote: '', remoteSnapshot: null, reason: '' })

let loadSequence = 0
let saveTimer: ReturnType<typeof setTimeout> | null = null
let draftTimer: ReturnType<typeof setTimeout> | null = null
let savingPromise: Promise<boolean> | null = null
let unregisterPreparation: (() => void) | null = null
const canceledAssets = new Set<string>()

const dirty = computed(() => !!serverBase.value && localContent.value !== serverBase.value.content)
const editorLocked = computed(() => mergeState.value.open || !!orphanDraft.value)
const saveLabel = computed(
  () =>
    ({
      saved: '已保存',
      draft: '草稿已保护',
      saving: '保存中…',
      conflict: '等待合并',
      error: '保存失败',
    })[saveState.value],
)

watch(title, loadNote, { immediate: true })
watch(editorLocked, (blocked) => emit('noteMutationBlocked', blocked), { immediate: true })
watch(
  [saveState, saveLabel, saveError, serverBase, loading, notFound, loadError],
  () => {
    const visible = !loading.value && !!serverBase.value && !notFound.value && !loadError.value
    emit('noteSaveStatus', visible ? { state: saveState.value, label: saveLabel.value, error: saveError.value } : null)
  },
  { immediate: true },
)

watch(
  () => noteStore.latestRemote,
  (remote) => {
    if (!remote || !serverBase.value) return
    if (
      remote.instance_token === serverBase.value.instance_token &&
      remote.content_revision === serverBase.value.content_revision
    ) {
      serverBase.value = { ...serverBase.value, note: remote.note, meta_revision: remote.meta_revision }
      tags.value = [...remote.note.tags]
      return
    }
    if (savingPromise) return
    handleRemoteSnapshot(remote, '智能体或其他文件写入已更新这篇笔记。')
  },
)

watch(
  () => mergeState.value.open,
  () => editor.value?.setEditable(!editorLocked.value),
)
watch(orphanDraft, () => editor.value?.setEditable(!editorLocked.value))

async function loadNote(nextTitle: string, previousTitle?: string) {
  if (!nextTitle) return
  const sequence = ++loadSequence
  const previousInstanceToken = serverBase.value?.instance_token
  const previousEditMode = editMode.value
  clearScheduledWork()
  if (previousTitle && previousTitle !== nextTitle) noteStore.unsubscribeNote(previousTitle)
  if (subscribedTitle.value && subscribedTitle.value !== nextTitle) noteStore.unsubscribeNote(subscribedTitle.value)
  unregisterPreparation?.()
  unregisterPreparation = null
  loading.value = true
  editMode.value = false
  notFound.value = false
  loadError.value = ''
  editor.value = null
  serverBase.value = null
  orphanDraft.value = null
  orphanRemote.value = null
  missingDraft.value = null
  mergeState.value.open = false
  canceledAssets.clear()

  try {
    const snapshot = await noteStore.getNote(nextTitle)
    if (sequence !== loadSequence) return
    editMode.value = previousInstanceToken === snapshot.instance_token ? previousEditMode : false
    serverBase.value = snapshot
    localContent.value = snapshot.content
    tags.value = [...snapshot.note.tags]
    noteStore.subscribeNote(nextTitle)
    subscribedTitle.value = nextTitle
    unregisterPreparation = registerEditorPreparation(nextTitle, prepareForAgent)
    await noteStore.fetchMediaAssets(nextTitle, snapshot.instance_token).catch(() => [])
    await restoreDraft(snapshot)
  } catch (error) {
    if (sequence !== loadSequence) return
    if (error instanceof ApiError && error.status === 404) {
      notFound.value = true
      const drafts = await safeBranchDrafts()
      missingDraft.value =
        drafts.find((draft) => draft.titleAtSave === nextTitle && draft.content !== draft.baseContent) || null
    } else {
      loadError.value = error instanceof Error ? error.message : '加载笔记失败'
    }
  } finally {
    if (sequence === loadSequence) loading.value = false
  }
}

async function restoreDraft(snapshot: NoteDetail) {
  const exact = await getDraft(snapshot.instance_token, branchId).catch(() => undefined)
  if (exact && exact.content !== snapshot.content) {
    localContent.value = exact.content
    saveState.value = 'draft'
    if (exact.baseRevision !== snapshot.content_revision) {
      openMerge(exact.baseContent, exact.content, snapshot, '页面刷新期间，服务器内容也发生了变化。')
    }
    return
  }
  if (exact) await removeDraft(exact.instanceToken, exact.draftId).catch(() => {})

  const candidates = await safeBranchDrafts()
  const orphan = candidates.find(
    (draft) =>
      draft.instanceToken !== snapshot.instance_token &&
      draft.titleAtSave === snapshot.note.title &&
      draft.content !== draft.baseContent,
  )
  if (orphan) {
    orphanDraft.value = orphan
    orphanRemote.value = snapshot
  }
}

async function safeBranchDrafts() {
  return listBranchDrafts(branchId).catch(() => [] as NoteDraft[])
}

function onEditorReady(api: EditorAPI) {
  editor.value = api
  api.setEditable(!editorLocked.value)
}

function handleContentChange(content: string) {
  localContent.value = content
  saveState.value = 'draft'
  saveError.value = ''
  scheduleDraft()
  scheduleSave()
}

async function handleAssetStart(payload: { id: string; file: File }) {
  const { id, file } = payload
  const initial = serverBase.value
  if (!initial) return
  const mediaKind = file.type.startsWith('video/') || /\.(?:mov|m4v|mp4|webm)$/i.test(file.name) ? 'video' : 'image'
  try {
    await persistDraft()
    await persistContent()
    const base = serverBase.value
    if (!base || base.instance_token !== initial.instance_token) throw new Error('笔记实例已经改变，上传未开始')
    if (!base.content.includes(id)) {
      if (mergeState.value.open) throw new Error('请先处理内容冲突，再重新选择媒体')
      throw new Error('媒体占位尚未安全写入，上传未开始')
    }
    if (canceledAssets.has(id)) return

    await noteStore.reserveMediaAsset(base.note.title, base.instance_token, id, file)
    if (canceledAssets.has(id)) {
      await noteStore.abandonMediaAsset(base.note.title, base.instance_token, id).catch(() => null)
      return
    }
    await noteStore.uploadMediaAsset(base.note.title, base.instance_token, id, file)
  } catch (error) {
    if (canceledAssets.has(id)) return
    const current = noteStore.mediaAssets[id]
    noteStore.trackMediaAsset({
      id,
      kind: current?.kind || mediaKind,
      state: 'failed',
      original_name: current?.original_name || file.name,
      content_type: current?.content_type || file.type,
      filename: current?.filename,
      error: error instanceof Error ? error.message : '上传失败',
    })
  }
}

function handleAssetRemove(id: string) {
  canceledAssets.add(id)
  const base = serverBase.value
  if (!base) return
  void noteStore.abandonMediaAsset(base.note.title, base.instance_token, id).catch(() => null)
}

function scheduleDraft() {
  if (draftTimer) clearTimeout(draftTimer)
  draftTimer = setTimeout(() => {
    void persistDraft()
  }, 180)
}

function scheduleSave(delay = 1100) {
  if (saveTimer) clearTimeout(saveTimer)
  saveTimer = setTimeout(() => {
    void persistContent()
  }, delay)
}

async function persistDraft() {
  if (!serverBase.value || !dirty.value) return
  const base = serverBase.value
  try {
    await saveDraft({
      instanceToken: base.instance_token,
      draftId: branchId,
      titleAtSave: base.note.title,
      baseRevision: base.content_revision,
      baseContent: base.content,
      content: localContent.value,
    })
    if (saveState.value !== 'saving' && saveState.value !== 'conflict') saveState.value = 'draft'
  } catch {
    saveState.value = 'error'
    saveError.value = '浏览器草稿存储不可用，请复制内容后再刷新页面。'
  }
}

async function persistContent(): Promise<boolean> {
  if (mergeState.value.open || !serverBase.value || !dirty.value) return !dirty.value
  if (savingPromise) return savingPromise
  if (saveTimer) {
    clearTimeout(saveTimer)
    saveTimer = null
  }
  await persistDraft()

  const base = serverBase.value
  const sentContent = localContent.value
  saveState.value = 'saving'
  savingPromise = (async () => {
    try {
      const saved = await noteStore.updateContent(base.note.title, sentContent, base)
      serverBase.value = saved
      tags.value = [...saved.note.tags]
      if (localContent.value === sentContent) {
        localContent.value = saved.content
        saveState.value = 'saved'
        await removeDraft(base.instance_token, branchId).catch(() => {})
      } else {
        saveState.value = 'draft'
        await persistDraft()
        scheduleSave(100)
      }
      return localContent.value === sentContent
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        const conflict = error.data || {}
        if (conflict.code === 'note_instance_changed') {
          if (conflict.moved_to) {
            await router.replace(`/note/${encodeURIComponent(conflict.moved_to)}`)
          } else {
            orphanDraft.value = await saveCurrentAsOrphan(base, sentContent)
            orphanRemote.value = conflict.current || null
            saveState.value = 'conflict'
          }
          return false
        }
        if (conflict.current) {
          openMerge(base.content, sentContent, conflict.current as NoteDetail, '保存时检测到新的文件版本。')
          return false
        }
      }
      saveState.value = 'error'
      saveError.value = error instanceof Error ? error.message : '保存失败'
      return false
    } finally {
      savingPromise = null
    }
  })()
  return savingPromise
}

async function saveCurrentAsOrphan(base: NoteDetail, content: string) {
  const payload = {
    instanceToken: base.instance_token,
    draftId: branchId,
    titleAtSave: base.note.title,
    baseRevision: base.content_revision,
    baseContent: base.content,
    content,
  }
  return saveDraft(payload).catch(() => ({
    ...payload,
    key: `${payload.instanceToken}:${payload.draftId}`,
    updatedAt: Date.now(),
  }))
}

function handleRemoteSnapshot(remote: NoteDetail, reason: string) {
  const base = serverBase.value
  if (!base) return
  if (remote.instance_token !== base.instance_token) {
    clearScheduledWork()
    if (!dirty.value) {
      applyRemoteSnapshot(remote)
      return
    }
    saveState.value = 'conflict'
    const draftContent = localContent.value
    void saveCurrentAsOrphan(base, draftContent).then((draft) => {
      // Navigation may have loaded another instance while IndexedDB settled.
      if (serverBase.value?.instance_token !== base.instance_token) return
      orphanDraft.value = draft
      orphanRemote.value = remote
    })
    return
  }
  if (remote.content_revision === base.content_revision) return
  if (!dirty.value) {
    applyRemoteSnapshot(remote)
    return
  }
  openMerge(base.content, localContent.value, remote, reason)
}

function applyRemoteSnapshot(remote: NoteDetail) {
  serverBase.value = remote
  localContent.value = remote.content
  tags.value = [...remote.note.tags]
  noteStore.currentNote = remote
  noteStore.latestRemote = remote
  editor.value?.updateContent(remote.content)
  saveState.value = 'saved'
  void noteStore.fetchMediaAssets(remote.note.title, remote.instance_token).catch(() => [])
}

function openMerge(base: string, local: string, remoteSnapshot: NoteDetail, reason: string) {
  saveState.value = 'conflict'
  mergeState.value = {
    open: true,
    base,
    local,
    remote: remoteSnapshot.content,
    remoteSnapshot,
    reason,
  }
}

async function acceptMerge(content: string) {
  const remote = mergeState.value.remoteSnapshot
  if (!remote) return
  mergeState.value.open = false
  serverBase.value = remote
  localContent.value = content
  tags.value = [...remote.note.tags]
  noteStore.currentNote = { ...remote, content }
  editor.value?.updateContent(content)
  await persistDraft()
  await nextTick()
  await persistContent()
}

function cancelMerge() {
  mergeState.value.open = false
  saveState.value = 'draft'
  void persistDraft()
}

async function prepareForAgent() {
  if (mergeState.value.open) throw new Error('请先处理当前内容冲突')
  if (saveTimer) {
    clearTimeout(saveTimer)
    saveTimer = null
  }
  const saved = await persistContent()
  if (!saved && dirty.value) throw new Error('当前草稿尚未安全写入，请先处理保存状态')
}

async function recoverOrphan() {
  const draft = orphanDraft.value
  const remote = orphanRemote.value
  if (!draft || !remote) return
  orphanDraft.value = null
  orphanRemote.value = null
  localContent.value = draft.content
  editor.value?.updateContent(draft.content)
  openMerge(draft.baseContent, draft.content, remote, '这是来自旧笔记实例或服务重启前的草稿，请明确确认恢复结果。')
}

async function discardOrphan() {
  const draft = orphanDraft.value
  const remote = orphanRemote.value
  orphanDraft.value = null
  orphanRemote.value = null
  if (draft) await removeDraft(draft.instanceToken, draft.draftId).catch(() => {})
  if (remote && serverBase.value?.instance_token !== remote.instance_token) applyRemoteSnapshot(remote)
}

async function createMissingFromDraft() {
  const draft = missingDraft.value
  if (!draft) return
  const snapshot = await noteStore.createNote(title.value, [], draft.content)
  await removeDraft(draft.instanceToken, draft.draftId).catch(() => {})
  missingDraft.value = null
  serverBase.value = snapshot
  localContent.value = snapshot.content
  notFound.value = false
  await loadNote(title.value)
}

async function mutateTags(operation: (current: string[]) => string[]) {
  if (editorLocked.value) return
  let base = serverBase.value
  if (!base) return
  for (let attempt = 0; attempt < 4; attempt++) {
    const desired = operation([...base.note.tags])
    try {
      const saved = await noteStore.updateMeta(base.note.title, { tags: desired }, base)
      serverBase.value = { ...saved, content: dirty.value ? base.content : saved.content }
      tags.value = [...saved.note.tags]
      return
    } catch (error) {
      if (!(error instanceof ApiError) || error.status !== 409 || !error.data?.current) {
        saveError.value = '标签保存失败'
        return
      }
      base = error.data.current as NoteDetail
      if (error.data.code === 'note_instance_changed') return
    }
  }
  saveError.value = '标签持续发生冲突，请稍后重试'
}

async function addTag() {
  if (editorLocked.value) return
  const tag = addTagInput.value.trim()
  if (!tag) return
  addTagInput.value = ''
  await mutateTags((current) => (current.includes(tag) ? current : [...current, tag]))
}

async function removeTag(tag: string) {
  if (editorLocked.value) return
  await mutateTags((current) => current.filter((value) => value !== tag))
}

async function deleteNote() {
  if (!serverBase.value || editorLocked.value) return
  try {
    await noteStore.deleteNote(serverBase.value.note.title, serverBase.value.instance_token)
    await removeDraft(serverBase.value.instance_token, branchId).catch(() => {})
    showDeleteConfirm.value = false
    await router.push('/')
  } catch (error) {
    saveError.value = error instanceof Error ? error.message : '移到回收站失败'
    showDeleteConfirm.value = false
  }
}

function handleSaveShortcut(event: KeyboardEvent) {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
    event.preventDefault()
    if (editorLocked.value) return
    void persistContent()
  }
}

function handlePageHide() {
  void persistDraft()
}

function clearScheduledWork() {
  if (saveTimer) clearTimeout(saveTimer)
  if (draftTimer) clearTimeout(draftTimer)
  saveTimer = null
  draftTimer = null
}

onMounted(() => {
  document.addEventListener('keydown', handleSaveShortcut)
  window.addEventListener('pagehide', handlePageHide)
})

onBeforeUnmount(() => {
  emit('noteMutationBlocked', false)
  emit('noteSaveStatus', null)
  clearScheduledWork()
  void persistDraft()
  document.removeEventListener('keydown', handleSaveShortcut)
  window.removeEventListener('pagehide', handlePageHide)
  if (subscribedTitle.value) noteStore.unsubscribeNote(subscribedTitle.value)
  unregisterPreparation?.()
})
</script>

<template>
  <div v-if="loading" class="page-loading"><span class="page-loading-spinner" /></div>

  <div v-else-if="notFound" class="editor-empty-state">
    <h2>笔记不存在</h2>
    <p v-if="missingDraft">检测到这个页面留下的受保护草稿。创建笔记是一次明确恢复操作，不会覆盖其他笔记。</p>
    <p v-else>该标题对应的笔记可能已被移动或删除。</p>
    <button v-if="missingDraft" class="admin-btn admin-btn-primary" @click="createMissingFromDraft">
      <FileAddOutlined aria-hidden="true" />用草稿创建「{{ title }}」
    </button>
    <button class="admin-btn" @click="router.push('/')"><ArrowLeftOutlined aria-hidden="true" />返回笔记列表</button>
  </div>

  <div v-else-if="loadError" class="editor-empty-state">
    <h2>无法加载笔记</h2>
    <p>{{ loadError }}</p>
  </div>

  <div v-else-if="serverBase" class="editor-shell">
    <div class="editor-toolbar">
      <div class="editor-toolbar-left">
        <button class="toolbar-btn" :class="{ active: !editMode }" @click="editMode = false">
          <EyeOutlined />查看
        </button>
        <button class="toolbar-btn" :class="{ active: editMode }" @click="editMode = true"><EditOutlined />编辑</button>
      </div>

      <div class="editor-toolbar-right">
        <div class="editor-tags">
          <span v-for="tag in tags" :key="tag" class="editor-tag"
            >{{ tag }}<button :disabled="editorLocked" @click="removeTag(tag)"><CloseOutlined /></button
          ></span>
          <input
            v-model="addTagInput"
            class="editor-tag-input"
            placeholder="添加标签"
            :disabled="editorLocked"
            @keydown.enter="addTag"
          />
        </div>
        <button
          class="toolbar-btn danger"
          title="移到回收站"
          :disabled="editorLocked"
          @click="showDeleteConfirm = true"
        >
          <DeleteOutlined aria-hidden="true" /><span>移到回收站</span>
        </button>
      </div>
    </div>

    <div class="editor-body">
      <div v-if="editMode" ref="toolbarTarget" class="editor-floating-toolbar" />
      <EditorCore
        v-if="editMode"
        :key="serverBase.instance_token"
        :content="localContent"
        :title="serverBase.note.title"
        :instance-token="serverBase.instance_token"
        :editable="!editorLocked"
        :toolbar-target="toolbarTarget"
        @change="handleContentChange"
        @ready="onEditorReady"
        @asset-start="handleAssetStart"
        @asset-remove="handleAssetRemove"
      />
      <NotePreview v-else :content="localContent" :title="serverBase.note.title" />
    </div>

    <Dialog.Root :open="showDeleteConfirm" lazy-mount unmount-on-exit @update:open="showDeleteConfirm = $event">
      <Teleport to="body">
        <Dialog.Backdrop class="dialog-backdrop" />
        <Dialog.Positioner class="dialog-positioner">
          <Dialog.Content class="dialog-panel" style="max-width: 380px">
            <div class="dialog-header">
              <Dialog.Title>移到回收站</Dialog.Title
              ><Dialog.CloseTrigger class="dialog-close"><CloseOutlined /></Dialog.CloseTrigger>
            </div>
            <div class="dialog-body">
              <p class="delete-copy">「{{ serverBase.note.title }}」会保留在回收站中，不会自动过期。</p>
              <div class="btn-group delete-buttons">
                <button class="admin-btn" @click="showDeleteConfirm = false">
                  <CloseOutlined aria-hidden="true" />取消</button
                ><button class="admin-btn admin-btn-danger" @click="deleteNote">
                  <DeleteOutlined aria-hidden="true" />移到回收站
                </button>
              </div>
            </div>
          </Dialog.Content>
        </Dialog.Positioner>
      </Teleport>
    </Dialog.Root>

    <Dialog.Root :open="!!orphanDraft" lazy-mount unmount-on-exit :close-on-interact-outside="false">
      <Teleport to="body">
        <Dialog.Backdrop class="dialog-backdrop" />
        <Dialog.Positioner class="dialog-positioner">
          <Dialog.Content class="dialog-panel" style="max-width: 520px">
            <div class="dialog-header"><Dialog.Title>发现旧实例草稿</Dialog.Title></div>
            <div class="dialog-body">
              <p class="delete-copy">
                当前笔记的实例令牌与草稿不同，可能是服务重启或同名笔记被替换。Marvo 不会自动把它合并进当前笔记。
              </p>
              <div class="btn-group delete-buttons">
                <button class="admin-btn" @click="discardOrphan"><DeleteOutlined aria-hidden="true" />放弃旧草稿</button
                ><button class="admin-btn admin-btn-primary" @click="recoverOrphan">
                  <DiffOutlined aria-hidden="true" />明确预览并恢复
                </button>
              </div>
            </div>
          </Dialog.Content>
        </Dialog.Positioner>
      </Teleport>
    </Dialog.Root>

    <MergeConflictDialog
      :open="mergeState.open"
      :base="mergeState.base"
      :local="mergeState.local"
      :remote="mergeState.remote"
      :reason="mergeState.reason"
      @accept="acceptMerge"
      @cancel="cancelMerge"
    />
  </div>
</template>

<style lang="scss" scoped>
.editor-shell {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.editor-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: var(--dsh-pane-toolbar-height, 52px);
  padding: 0 12px;
  flex-shrink: 0;
  border-bottom: 1px solid var(--border-primary);
  background: var(--bg-secondary);
  gap: 8px;
}
.editor-toolbar-left,
.editor-toolbar-right {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
}
.editor-toolbar-left {
  flex: 0 0 auto;
}
.editor-toolbar-right {
  flex: 1 1 auto;
  justify-content: flex-end;
  overflow: hidden;
}
.toolbar-btn {
  display: flex;
  align-items: center;
  gap: 4px;
  height: 28px;
  padding: 0 8px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font: inherit;
  font-size: var(--marvo-type-12);
  flex: none;
  white-space: nowrap;
  &:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }
  &.active {
    background: color-mix(in srgb, var(--marvo-accent-color) 12%, transparent);
    color: var(--text-accent);
  }
  &.danger:hover {
    color: var(--text-danger);
  }
  &:disabled {
    opacity: 0.45;
    cursor: not-allowed;
    &:hover {
      background: transparent;
      color: var(--text-secondary);
    }
  }
}
.editor-tags {
  display: flex;
  flex: 1 1 auto;
  min-width: 0;
  align-items: center;
  gap: 4px;
  margin-right: 8px;
  overflow-x: auto;
}
.editor-tag {
  display: flex;
  flex: none;
  align-items: center;
  gap: 2px;
  padding: 1px 8px;
  border-radius: 4px;
  background: color-mix(in srgb, var(--marvo-accent-color) 10%, transparent);
  color: var(--text-accent);
  font-size: var(--marvo-type-12);
  white-space: nowrap;
  button {
    border: none;
    background: none;
    color: inherit;
    cursor: pointer;
    font-size: var(--marvo-type-14);
    padding: 0;
    line-height: 1;
  }
}
.editor-tag-input {
  flex: none;
  border: none;
  background: transparent;
  color: var(--text-primary);
  font: inherit;
  font-size: var(--marvo-type-12);
  outline: none;
  width: 80px;
  min-width: 60px;
}
.editor-body {
  flex: 1;
  overflow-y: auto;
  min-height: 0;
  position: relative;
}
.editor-floating-toolbar {
  position: sticky;
  top: 0;
  z-index: 10;
}
.editor-empty-state {
  display: flex;
  min-height: 60%;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 24px;
  text-align: center;
  color: var(--text-secondary);
  h2,
  p {
    margin: 0;
  }
}
.delete-copy {
  margin: 0 0 16px;
  color: var(--text-secondary);
  font-size: var(--marvo-type-14);
}
.delete-buttons {
  justify-content: flex-end;
}
@media (max-width: 760px) {
  .editor-toolbar {
    align-items: flex-start;
    flex-wrap: wrap;
    padding-block: 7px;
  }
  .editor-toolbar-left,
  .editor-toolbar-right {
    width: 100%;
  }
  .editor-toolbar-right {
    min-width: 0;
  }
  .editor-tags {
    flex: 1;
    max-width: none;
  }
}
</style>
