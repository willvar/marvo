<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { resolveMerge, threeWayMerge } from '../sdk'
import {
  ArrowLeftOutlined,
  ArrowRightOutlined,
  CheckOutlined,
  CloseOutlined,
  MergeCellsOutlined,
} from '@ant-design/icons-vue'

const props = defineProps<{
  open: boolean
  base: string
  local: string
  remote: string
  reason?: string
}>()

const emit = defineEmits<{
  accept: [content: string]
  cancel: []
}>()

const host = ref<HTMLElement>()
const choices = ref<Array<'local' | 'remote' | 'both' | null>>([])
const current = ref('')
const manuallyEdited = ref(false)
const loading = ref(false)
const merge = computed(() => threeWayMerge(props.base, props.local, props.remote))
const canAccept = computed(() => merge.value.clean || manuallyEdited.value || choices.value.every(Boolean))
let view: any = null
let programmaticUpdate = false

watch(
  () => [props.open, props.base, props.local, props.remote] as const,
  async ([open]) => {
    destroyView()
    if (!open) return
    const result = merge.value
    choices.value = result.conflicts.map(() => null)
    current.value = result.clean ? result.merged : props.local
    manuallyEdited.value = false
    loading.value = true
    await nextTick()
    try {
      const [{ MergeView }, { EditorState }, { EditorView }, { markdown }] = await Promise.all([
        import('@codemirror/merge'),
        import('@codemirror/state'),
        import('@codemirror/view'),
        import('@codemirror/lang-markdown'),
      ])
      if (!props.open || !host.value) return
      const common = [
        markdown(),
        EditorView.lineWrapping,
        EditorView.theme({
          '&': { height: '100%', fontSize: 'var(--marvo-type-13)' },
          '.cm-scroller': { overflow: 'auto', fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace' },
          '.cm-content': { minHeight: '280px' },
        }),
      ]
      view = new MergeView({
        parent: host.value,
        a: {
          doc: current.value,
          extensions: [
            ...common,
            EditorView.updateListener.of((update) => {
              if (!update.docChanged) return
              current.value = update.state.doc.toString()
              if (!programmaticUpdate) manuallyEdited.value = true
            }),
          ],
        },
        b: {
          doc: props.remote,
          extensions: [...common, EditorState.readOnly.of(true), EditorView.editable.of(false)],
        },
        orientation: 'a-b',
        revertControls: 'b-to-a',
        highlightChanges: true,
        gutter: true,
        collapseUnchanged: { margin: 3, minSize: 8 },
      })
    } finally {
      loading.value = false
    }
  },
  { immediate: true },
)

function choose(index: number, choice: 'local' | 'remote' | 'both') {
  choices.value[index] = choice
  choices.value = [...choices.value]
  replaceResult(resolveMerge(merge.value, choices.value))
}

function chooseAll(choice: 'local' | 'remote' | 'both') {
  choices.value = choices.value.map(() => choice)
  replaceResult(resolveMerge(merge.value, choices.value))
}

function replaceResult(content: string) {
  current.value = content
  if (!view) return
  programmaticUpdate = true
  view.a.dispatch({ changes: { from: 0, to: view.a.state.doc.length, insert: content } })
  programmaticUpdate = false
}

function accept() {
  if (!canAccept.value) return
  emit('accept', view ? view.a.state.doc.toString() : current.value)
}

function destroyView() {
  view?.destroy()
  view = null
  if (host.value) host.value.textContent = ''
}

onBeforeUnmount(destroyView)
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="merge-backdrop" @keydown.esc="emit('cancel')">
      <section class="merge-dialog" role="dialog" aria-modal="true" aria-labelledby="merge-title">
        <header class="merge-header">
          <div>
            <h2 id="merge-title">保存前确认合并</h2>
            <p>{{ reason || '笔记在您编辑期间发生了变化。左侧是待保存结果，右侧是服务器最新内容。' }}</p>
          </div>
          <button class="merge-close" aria-label="关闭" @click="emit('cancel')"><CloseOutlined /></button>
        </header>

        <div v-if="merge.conflicts.length" class="merge-resolution">
          <div class="merge-resolution-top">
            <strong>{{ merge.conflicts.length }} 处冲突尚需确认</strong>
            <div class="merge-actions-inline">
              <button @click="chooseAll('local')"><ArrowLeftOutlined aria-hidden="true" />全部用本地</button>
              <button @click="chooseAll('remote')"><ArrowRightOutlined aria-hidden="true" />全部用远端</button>
              <button @click="chooseAll('both')"><MergeCellsOutlined aria-hidden="true" />全部保留</button>
            </div>
          </div>
          <div class="merge-conflicts">
            <article v-for="(chunk, index) in merge.conflicts" :key="index" class="merge-conflict">
              <div class="merge-conflict-label">冲突 {{ index + 1 }}</div>
              <pre>
本地：{{ chunk.local.join('\n') || '（删除）' }}
远端：{{ chunk.remote.join('\n') || '（删除）' }}</pre>
              <div class="merge-actions-inline">
                <button :class="{ selected: choices[index] === 'local' }" @click="choose(index, 'local')">
                  <ArrowLeftOutlined aria-hidden="true" />本地
                </button>
                <button :class="{ selected: choices[index] === 'remote' }" @click="choose(index, 'remote')">
                  <ArrowRightOutlined aria-hidden="true" />远端
                </button>
                <button :class="{ selected: choices[index] === 'both' }" @click="choose(index, 'both')">
                  <MergeCellsOutlined aria-hidden="true" />两者
                </button>
              </div>
            </article>
          </div>
          <p v-if="manuallyEdited" class="merge-manual-note">已手工修改左侧结果，可直接确认。</p>
        </div>
        <div v-else class="merge-clean-note">非重叠改动已自动合并；仍需预览并确认后才会保存。</div>

        <div class="merge-labels"><span>待保存结果（可编辑）</span><span>服务器最新内容（只读）</span></div>
        <div ref="host" class="merge-view-host"><span v-if="loading" class="page-loading-spinner" /></div>

        <details class="merge-base">
          <summary>查看共同基础版本</summary>
          <pre>{{ base }}</pre>
        </details>

        <footer class="merge-footer">
          <span v-if="!canAccept" class="merge-warning">请选择每处冲突，或直接手工编辑左侧结果。</span>
          <div class="merge-footer-buttons">
            <button class="admin-btn" @click="emit('cancel')">
              <CloseOutlined aria-hidden="true" />保留草稿，暂不保存
            </button>
            <button class="admin-btn admin-btn-primary" :disabled="!canAccept" @click="accept">
              <CheckOutlined aria-hidden="true" />接受并重试保存
            </button>
          </div>
        </footer>
      </section>
    </div>
  </Teleport>
</template>

<style lang="scss">
.merge-backdrop {
  position: fixed;
  inset: 0;
  z-index: 300;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  background: rgba(0, 0, 0, 0.55);
}
.merge-dialog {
  width: min(1180px, 100%);
  max-height: calc(100vh - 40px);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--border-primary);
  border-radius: 14px;
  background: var(--bg-card);
  color: var(--text-primary);
  box-shadow: var(--shadow-card);
}
.merge-header {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  padding: 18px 20px 12px;
  h2 {
    margin: 0 0 4px;
    font-size: var(--marvo-type-17);
  }
  p {
    margin: 0;
    color: var(--text-secondary);
    font-size: var(--marvo-type-13);
  }
}
.merge-close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--marvo-type-18);
  cursor: pointer;
}
.merge-clean-note,
.merge-resolution {
  margin: 0 20px 10px;
  padding: 10px 12px;
  border-radius: 8px;
  background: var(--bg-secondary);
  font-size: var(--marvo-type-12);
}
.merge-resolution-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.merge-conflicts {
  display: flex;
  gap: 8px;
  margin-top: 8px;
  overflow-x: auto;
}
.merge-conflict {
  min-width: 250px;
  max-width: 390px;
  padding: 8px;
  border: 1px solid var(--border-light);
  border-radius: 7px;
  background: var(--bg-primary);
  pre {
    max-height: 90px;
    overflow: auto;
    margin: 5px 0;
    white-space: pre-wrap;
    font-size: var(--marvo-type-11);
  }
}
.merge-conflict-label {
  font-weight: 600;
}
.merge-actions-inline {
  display: flex;
  gap: 5px;
  button {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    border: 1px solid var(--border-light);
    border-radius: 5px;
    background: var(--bg-primary);
    color: var(--text-secondary);
    cursor: pointer;
    font-size: var(--marvo-type-11);
    padding: 4px 7px;
  }
  button.selected {
    border-color: var(--marvo-accent-color);
    color: var(--text-accent);
  }
}
.merge-manual-note {
  margin: 8px 0 0;
  color: var(--text-accent);
}
.merge-labels {
  display: grid;
  grid-template-columns: 1fr 1fr;
  padding: 0 24px 5px;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}
.merge-view-host {
  height: min(48vh, 520px);
  min-height: 280px;
  margin: 0 20px;
  overflow: auto;
  border: 1px solid var(--border-primary);
  border-radius: 8px;
  position: relative;
}
.merge-view-host > .page-loading-spinner {
  position: absolute;
  top: 50%;
  left: 50%;
}
.merge-view-host .cm-mergeView {
  height: 100%;
}
.merge-view-host .cm-editor {
  background: var(--bg-primary);
  color: var(--text-primary);
}
.merge-view-host .cm-gutters {
  background: var(--bg-secondary);
  color: var(--text-muted);
  border-color: var(--border-light);
}
.merge-base {
  margin: 8px 20px 0;
  color: var(--text-secondary);
  font-size: var(--marvo-type-12);
  pre {
    max-height: 130px;
    overflow: auto;
    padding: 8px;
    background: var(--bg-secondary);
    white-space: pre-wrap;
  }
}
.merge-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 20px 18px;
}
.merge-warning {
  color: var(--text-danger);
  font-size: var(--marvo-type-12);
}
.merge-footer-buttons {
  display: flex;
  gap: 8px;
  margin-left: auto;
}
@media (max-width: 760px) {
  .merge-backdrop {
    padding: 0;
  }
  .merge-dialog {
    width: 100%;
    height: 100%;
    max-height: none;
    border-radius: 0;
  }
  .merge-view-host {
    height: 46vh;
    margin-inline: 10px;
  }
  .merge-labels {
    padding-inline: 14px;
  }
  .merge-resolution,
  .merge-clean-note,
  .merge-base {
    margin-inline: 10px;
  }
  .merge-footer {
    align-items: flex-end;
    flex-direction: column;
    padding-inline: 10px;
  }
}
</style>
