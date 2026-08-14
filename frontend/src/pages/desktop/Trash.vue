<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Dialog } from '@ark-ui/vue/dialog'
import { Field } from '@ark-ui/vue/field'
import { ApiError, workspaceRoute, type TrashEntry } from '../../sdk'
import { useNoteStore } from '../../stores/note'
import { CheckOutlined, CloseOutlined, DeleteOutlined, RollbackOutlined } from '@ant-design/icons-vue'
import { useRetainedDialog } from '../../composables/useRetainedDialog'

const noteStore = useNoteStore()
const router = useRouter()
const loading = ref(true)
const error = ref('')
const restoringID = ref('')
const deletingID = ref('')
const emptying = ref(false)
const confirmationDialog = useRetainedDialog<{ kind: 'delete'; entry: TrashEntry } | { kind: 'empty'; count: number }>()
const { open: confirmationOpen, payload: confirmation } = confirmationDialog
const form = reactive({ title: '' })

onMounted(async () => {
  try {
    await noteStore.fetchTrash()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '回收站加载失败'
  } finally {
    loading.value = false
  }
})

function beginRestore(entry: TrashEntry) {
  restoringID.value = entry.id
  form.title = entry.title
  error.value = ''
}

async function restore(entry: TrashEntry) {
  const title = form.title.trim()
  if (!title) {
    error.value = '标题不能为空'
    return
  }
  error.value = ''
  try {
    const note = await noteStore.restoreTrash(entry.id, title)
    restoringID.value = ''
    await router.push(workspaceRoute(`/note/${encodeURIComponent(note.note.title)}`))
  } catch (cause) {
    if (cause instanceof ApiError && cause.status === 409) {
      error.value = '该标题已存在。请填写一个新标题；恢复不会覆盖现有笔记。'
    } else {
      error.value = cause instanceof Error ? cause.message : '恢复失败'
    }
  }
}

function requestPermanentDelete(entry: TrashEntry) {
  confirmationDialog.show({ kind: 'delete', entry })
  error.value = ''
}

function requestEmptyTrash() {
  if (!noteStore.trash.length) return
  confirmationDialog.show({ kind: 'empty', count: noteStore.trash.length })
  error.value = ''
}

function updateConfirmationOpen(open: boolean) {
  confirmationDialog.updateOpen(open, !deletingID.value && !emptying.value)
}

function completeConfirmationClose() {
  confirmationDialog.clearAfterExit()
}

async function confirmPermanentAction() {
  const target = confirmation.value
  if (!target || deletingID.value || emptying.value) return
  error.value = ''
  if (target.kind === 'delete') deletingID.value = target.entry.id
  else emptying.value = true
  try {
    if (target.kind === 'delete') await noteStore.permanentlyDeleteTrash(target.entry.id)
    else await noteStore.emptyTrash()
    confirmationDialog.close()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : target.kind === 'delete' ? '永久删除失败' : '清空失败'
  } finally {
    deletingID.value = ''
    emptying.value = false
  }
}

function formatTime(value: string) {
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}
</script>

<template>
  <section class="trash-page">
    <div class="trash-heading">
      <div>
        <h1>回收站</h1>
        <p>笔记不会自动过期；恢复时绝不会覆盖同名笔记。</p>
      </div>
      <button
        v-if="noteStore.trash.length"
        class="admin-btn admin-btn-danger"
        :disabled="emptying"
        @click="requestEmptyTrash"
      >
        <DeleteOutlined aria-hidden="true" />清空回收站
      </button>
    </div>

    <p v-if="error" class="trash-error">{{ error }}</p>
    <div v-if="loading" class="page-loading"><span class="page-loading-spinner" /></div>
    <div v-else-if="!noteStore.trash.length" class="trash-empty">回收站是空的</div>
    <div v-else class="trash-list">
      <article v-for="entry in noteStore.trash" :key="entry.id" class="trash-card">
        <div class="trash-card-main">
          <strong>{{ entry.title }}</strong>
          <span class="trash-time">删除于 {{ formatTime(entry.deleted_at) }}</span>
          <div v-if="entry.tags?.length" class="trash-tags">
            <span v-for="tag in entry.tags" :key="tag">{{ tag }}</span>
          </div>
        </div>
        <div v-if="restoringID !== entry.id" class="trash-actions">
          <button class="admin-btn" @click="beginRestore(entry)"><RollbackOutlined aria-hidden="true" />恢复</button>
          <button
            class="admin-btn admin-btn-danger"
            :disabled="deletingID === entry.id || emptying"
            @click="requestPermanentDelete(entry)"
          >
            <DeleteOutlined aria-hidden="true" />永久删除
          </button>
        </div>
        <form v-else class="trash-restore" @submit.prevent="restore(entry)">
          <Field.Root class="trash-restore-field">
            <Field.Label class="trash-restore-label">新标题</Field.Label>
            <Field.Input
              v-model="form.title"
              class="trash-restore-input"
              autocomplete="off"
              autofocus
              required
              @keydown.esc="restoringID = ''"
            />
          </Field.Root>
          <div class="trash-actions">
            <button type="button" class="admin-btn" @click="restoringID = ''">
              <CloseOutlined aria-hidden="true" />取消</button
            ><button type="submit" class="admin-btn admin-btn-primary">
              <CheckOutlined aria-hidden="true" />确认恢复
            </button>
          </div>
        </form>
      </article>
    </div>

    <Dialog.Root
      :open="confirmationOpen"
      lazy-mount
      unmount-on-exit
      :close-on-interact-outside="!deletingID && !emptying"
      @exit-complete="completeConfirmationClose"
      @update:open="updateConfirmationOpen"
    >
      <Teleport to="body">
        <Dialog.Backdrop class="dialog-backdrop" />
        <Dialog.Positioner class="dialog-positioner">
          <Dialog.Content class="dialog-panel" style="max-width: 420px">
            <div class="dialog-header">
              <Dialog.Title>{{ confirmation?.kind === 'delete' ? '永久删除笔记' : '清空回收站' }}</Dialog.Title>
              <Dialog.CloseTrigger class="dialog-close" :disabled="!!deletingID || emptying"
                ><CloseOutlined
              /></Dialog.CloseTrigger>
            </div>
            <div class="dialog-body">
              <p class="trash-confirm-copy">
                {{
                  confirmation?.kind === 'delete'
                    ? `「${confirmation.entry.title}」及其媒体将被永久删除，此操作无法恢复。`
                    : `回收站中的 ${confirmation?.count ?? 0} 篇笔记及其媒体将被永久删除，此操作无法恢复。`
                }}
              </p>
              <p v-if="error" class="trash-error trash-confirm-error" role="alert">{{ error }}</p>
              <div class="trash-actions trash-confirm-actions">
                <button
                  type="button"
                  class="admin-btn"
                  :disabled="!!deletingID || emptying"
                  @click="confirmationDialog.close"
                >
                  <CloseOutlined aria-hidden="true" />取消
                </button>
                <button
                  type="button"
                  class="admin-btn admin-btn-danger"
                  :disabled="!!deletingID || emptying"
                  @click="confirmPermanentAction"
                >
                  <DeleteOutlined aria-hidden="true" />{{
                    confirmation?.kind === 'delete' ? '确认永久删除' : '确认清空'
                  }}
                </button>
              </div>
            </div>
          </Dialog.Content>
        </Dialog.Positioner>
      </Teleport>
    </Dialog.Root>
  </section>
</template>

<style lang="scss" scoped>
.trash-page {
  height: 100%;
  overflow-y: auto;
  box-sizing: border-box;
  padding: clamp(20px, 4vw, 48px);
  color: var(--text-primary);
}
.trash-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  max-width: 900px;
  margin: 0 auto 24px;
  h1 {
    margin: 0 0 6px;
    font-size: var(--marvo-type-24);
  }
  p {
    margin: 0;
    color: var(--text-muted);
  }
}
.trash-error {
  max-width: 900px;
  margin: 0 auto 16px;
  color: var(--text-danger);
  font-size: var(--marvo-type-13);
}
.trash-empty {
  display: grid;
  place-items: center;
  min-height: 45vh;
  color: var(--text-muted);
}
.trash-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  max-width: 900px;
  margin: 0 auto;
}
.trash-card {
  display: flex;
  align-items: center;
  gap: 18px;
  padding: 16px;
  border: 1px solid var(--border-primary);
  border-radius: 10px;
  background: var(--bg-card);
}
.trash-card-main {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1;
  gap: 4px;
}
.trash-time {
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}
.trash-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  span {
    padding: 2px 6px;
    border-radius: 4px;
    background: var(--bg-secondary);
    color: var(--text-secondary);
    font-size: var(--marvo-type-11);
  }
}
.trash-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
}
.trash-restore {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) auto;
  gap: 10px;
  align-items: end;
  flex: 2;
}
.trash-restore-field {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 6px;
}
.trash-restore-label {
  color: var(--text-secondary);
  font-size: var(--marvo-type-11);
  font-weight: 600;
}
.trash-restore-input {
  width: 100%;
  height: 36px;
  box-sizing: border-box;
  padding: 0 11px;
  appearance: none;
  border: 1px solid var(--border-light);
  border-radius: 8px;
  outline: none;
  background: var(--bg-primary);
  color: var(--text-primary);
  font: inherit;
  font-size: var(--marvo-type-13);
  transition:
    border-color 0.15s,
    box-shadow 0.15s;
  &:hover {
    border-color: var(--border-secondary);
  }
  &:focus-visible {
    border-color: var(--text-accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--marvo-accent-color) 13%, transparent);
  }
}
.trash-confirm-copy {
  margin: 0;
  color: var(--text-secondary);
  line-height: 1.65;
}
.trash-confirm-error {
  margin-top: 12px;
}
.trash-confirm-actions {
  margin-top: 20px;
}
@media (max-width: 700px) {
  .trash-heading,
  .trash-card {
    align-items: stretch;
    flex-direction: column;
  }
  .trash-restore {
    width: 100%;
    grid-template-columns: 1fr;
  }
  .trash-actions {
    justify-content: flex-start;
  }
  .trash-restore-input {
    height: 40px;
  }
}

@media (hover: none) and (pointer: coarse) {
  .trash-restore-input {
    min-height: 40px;
  }
}
</style>
