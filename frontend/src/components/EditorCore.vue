<script setup lang="ts">
import { useNoteStore } from '../stores/note'
import { toMarkdownAssetPath, toNoteAssetUrl, toRelativeAssetPath, type MediaAsset } from '../sdk'
import { shallowRef, ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { EditorContent, Node } from '@tiptap/vue-3'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import Placeholder from '@tiptap/extension-placeholder'
import Highlight from '@tiptap/extension-highlight'
import TaskList from '@tiptap/extension-task-list'
import TaskItem from '@tiptap/extension-task-item'
import TextAlign from '@tiptap/extension-text-align'
import ImageExt from '@tiptap/extension-image'
import LinkExt from '@tiptap/extension-link'
import { Table as TableExt } from '@tiptap/extension-table'
import TableRow from '@tiptap/extension-table-row'
import TableCell from '@tiptap/extension-table-cell'
import TableHeader from '@tiptap/extension-table-header'
import CodeBlock from '@tiptap/extension-code-block'
import { Markdown } from 'tiptap-markdown'
import type { AnyExtension } from '@tiptap/core'
import {
  BoldOutlined,
  ItalicOutlined,
  UnderlineOutlined,
  StrikethroughOutlined,
  OrderedListOutlined,
  UnorderedListOutlined,
  CheckSquareOutlined,
  CodeOutlined,
  MessageOutlined,
  PictureOutlined,
} from '@ant-design/icons-vue'

const props = withDefaults(
  defineProps<{
    content: string
    title: string
    instanceToken: string
    editable?: boolean
    toolbarTarget?: HTMLElement | null
  }>(),
  { editable: true },
)
const emit = defineEmits<{
  change: [md: string]
  ready: [api: { updateContent: (c: string) => void; setEditable: (value: boolean) => void }]
  assetStart: [payload: { id: string; file: File }]
  assetRemove: [id: string]
}>()

const noteStore = useNoteStore()
const ed = shallowRef<any>(null)

const emptyActive = {
  bold: false,
  italic: false,
  underline: false,
  strike: false,
  heading1: false,
  heading2: false,
  heading3: false,
  bulletList: false,
  orderedList: false,
  taskList: false,
  codeBlock: false,
  blockquote: false,
}
const active = ref({ ...emptyActive })
const emptyMobileMenu = { visible: false, left: 0, top: 0 }
const mobileMenu = ref({ ...emptyMobileMenu })
const MOBILE_MENU_WIDTH = 320

const applyingExternal = ref(false)

const uploadError = ref('')
let uploadErrorTimer: ReturnType<typeof setTimeout> | null = null

function showUploadError(msg: string) {
  uploadError.value = msg
  if (uploadErrorTimer) clearTimeout(uploadErrorTimer)
  uploadErrorTimer = setTimeout(() => {
    uploadError.value = ''
  }, 3000)
}

function toolbarState(ed: Editor) {
  return {
    bold: ed.isActive('bold'),
    italic: ed.isActive('italic'),
    underline: ed.isActive('underline'),
    strike: ed.isActive('strike'),
    heading1: ed.isActive('heading', { level: 1 }),
    heading2: ed.isActive('heading', { level: 2 }),
    heading3: ed.isActive('heading', { level: 3 }),
    bulletList: ed.isActive('bulletList'),
    orderedList: ed.isActive('orderedList'),
    taskList: ed.isActive('taskList'),
    codeBlock: ed.isActive('codeBlock'),
    blockquote: ed.isActive('blockquote'),
  }
}

function syncToolbarState(ed: Editor) {
  if (ed.isFocused) active.value = toolbarState(ed)
  updateMobileMenu(ed)
}

function updateMobileMenu(ed: Editor) {
  if (typeof window === 'undefined' || window.innerWidth > 768 || !ed.isFocused || ed.state.selection.empty) {
    mobileMenu.value = { ...emptyMobileMenu }
    return
  }
  const start = ed.view.coordsAtPos(ed.state.selection.from)
  const end = ed.view.coordsAtPos(ed.state.selection.to)
  const width = Math.min(MOBILE_MENU_WIDTH, window.innerWidth - 24)
  const center = (start.left + end.right) / 2
  mobileMenu.value = {
    visible: true,
    left: Math.min(Math.max(center - width / 2, 12), window.innerWidth - width - 12),
    top: Math.max(Math.min(start.top, end.top) - 12, 16),
  }
}

function escapeHtml(value: string) {
  return value.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

const ResolvedImage = ImageExt.extend({
  addAttributes() {
    const parent = (this.parent?.() || {}) as Record<string, any>
    return {
      ...parent,
      src: {
        ...parent.src,
        default: null,
        parseHTML: (el: HTMLElement) => toMarkdownAssetPath(el.getAttribute('src') || ''),
        renderHTML: (attrs: Record<string, any>) => ({ src: toNoteAssetUrl(attrs.src, props.title) }),
      },
    }
  },
})

const ResolvedLink = LinkExt.extend({
  addAttributes() {
    const parent = (this.parent?.() || {}) as Record<string, any>
    return {
      ...parent,
      href: {
        ...parent.href,
        default: null,
        parseHTML: (element: HTMLElement) => toMarkdownAssetPath(element.getAttribute('href') || ''),
      },
    }
  },
  renderHTML({ HTMLAttributes }: any) {
    const href = toNoteAssetUrl(String(HTMLAttributes.href || ''), props.title)
    return ['a', { ...HTMLAttributes, href, rel: 'noopener noreferrer' }, 0]
  },
}).configure({ openOnClick: false })

const VideoNode = Node.create({
  name: 'video',
  group: 'block',
  atom: true,
  selectable: true,
  draggable: true,
  addAttributes() {
    return {
      src: {
        default: null,
        parseHTML: (element: HTMLElement) => toMarkdownAssetPath(element.getAttribute('src') || ''),
      },
      controls: { default: true },
    }
  },
  parseHTML() {
    return [{ tag: 'video' }]
  },
  renderHTML({ HTMLAttributes }: any) {
    return [
      'video',
      {
        ...HTMLAttributes,
        src: toNoteAssetUrl(String(HTMLAttributes.src || ''), props.title),
        controls: '',
        preload: 'metadata',
        playsinline: '',
      },
    ]
  },
  addStorage() {
    return {
      markdown: {
        serialize(state: any, node: any) {
          const src = String(node.attrs.src || '')
          state.write(
            `<video controls src="${escapeHtml(src)}">\n  <p>您的浏览器不支持视频播放，请下载文件：${escapeHtml(src)}</p>\n</video>`,
          )
          state.closeBlock(node)
        },
      },
    }
  },
})

const assetStateLabels: Record<string, string> = {
  reserved: '等待上传',
  uploading: '正在上传',
  queued: '等待转换',
  probing: '正在检查媒体',
  transcoding: '正在转换为兼容格式',
  ready: '处理完成',
  abandoned: '已放弃',
  failed: '处理失败',
}

function placeholderLabel(attrs: Record<string, any>) {
  const state = assetStateLabels[String(attrs.state || 'reserved')] || '处理中'
  const name = String(attrs.name || '媒体文件')
  const error = String(attrs.error || '')
  return error ? `${name} · ${state}：${error}` : `${name} · ${state}`
}

function placeholderAttrs(attrs: Record<string, any>) {
  return {
    class: `marvo-asset-placeholder state-${String(attrs.state || 'reserved')}`,
    'data-marvo-asset-id': String(attrs.id || ''),
    'data-marvo-asset-kind': String(attrs.kind || 'image'),
    'data-marvo-asset-name': String(attrs.name || ''),
    'data-marvo-asset-state': String(attrs.state || 'reserved'),
    'data-marvo-asset-error': String(attrs.error || ''),
  }
}

const AssetPlaceholder = Node.create({
  name: 'assetPlaceholder',
  group: 'block',
  atom: true,
  selectable: true,
  draggable: true,
  addAttributes() {
    return {
      id: { default: '', parseHTML: (el: HTMLElement) => el.getAttribute('data-marvo-asset-id') || '' },
      kind: { default: 'image', parseHTML: (el: HTMLElement) => el.getAttribute('data-marvo-asset-kind') || 'image' },
      name: { default: '', parseHTML: (el: HTMLElement) => el.getAttribute('data-marvo-asset-name') || '' },
      state: {
        default: 'reserved',
        parseHTML: (el: HTMLElement) => el.getAttribute('data-marvo-asset-state') || 'reserved',
      },
      error: { default: '', parseHTML: (el: HTMLElement) => el.getAttribute('data-marvo-asset-error') || '' },
    }
  },
  parseHTML() {
    return [{ tag: 'div[data-marvo-asset-id]' }]
  },
  renderHTML({ node }: any) {
    return ['div', placeholderAttrs(node.attrs)]
  },
  addNodeView() {
    return ({ node, editor, getPos }: any) => {
      let currentNode = node
      const dom = document.createElement('div')
      const label = document.createElement('span')
      const remove = document.createElement('button')
      remove.type = 'button'
      remove.className = 'marvo-asset-remove'
      remove.textContent = '移除'
      remove.setAttribute('aria-label', '取消并移除媒体')
      remove.addEventListener('pointerdown', (event) => event.preventDefault())
      remove.addEventListener('click', () => {
        const pos = typeof getPos === 'function' ? getPos() : undefined
        if (typeof pos !== 'number') return
        editor
          .chain()
          .focus()
          .deleteRange({ from: pos, to: pos + currentNode.nodeSize })
          .run()
      })
      dom.append(label, remove)
      const render = (attrs: Record<string, any>) => {
        for (const [key, value] of Object.entries(placeholderAttrs(attrs))) dom.setAttribute(key, value)
        label.textContent = placeholderLabel(attrs)
      }
      render(node.attrs)
      return {
        dom,
        update(updated: any) {
          if (updated.type.name !== 'assetPlaceholder') return false
          currentNode = updated
          render(updated.attrs)
          return true
        },
      }
    }
  },
  addStorage() {
    return {
      markdown: {
        serialize(state: any, node: any) {
          const id = escapeHtml(String(node.attrs.id || ''))
          const kind = escapeHtml(String(node.attrs.kind || 'image'))
          const name = escapeHtml(String(node.attrs.name || ''))
          state.write(
            `<div data-marvo-asset-id="${id}" data-marvo-asset-kind="${kind}" data-marvo-asset-name="${name}"></div>`,
          )
          state.closeBlock(node)
        },
      },
    }
  },
})

function attachmentContent(kind: string, filename: string, name: string) {
  const path = toRelativeAssetPath(filename)
  if (kind === 'video') return { type: 'video' as const, attrs: { src: path } }
  return { type: 'image' as const, attrs: { src: path, alt: name } }
}

function placeholderIDs(doc: any) {
  const ids = new Set<string>()
  doc.descendants((node: any) => {
    if (node.type.name === 'assetPlaceholder' && node.attrs.id) ids.add(String(node.attrs.id))
  })
  return ids
}

function applyAssetState(asset: MediaAsset) {
  const editor = ed.value as Editor | null
  if (!editor) return
  editor.commands.command(({ state, tr, dispatch }: any) => {
    let changed = false
    state.doc.descendants((node: any, pos: number) => {
      if (node.type.name !== 'assetPlaceholder' || node.attrs.id !== asset.id) return
      if (asset.state === 'ready' && asset.filename) {
        const spec = attachmentContent(asset.kind, asset.filename, asset.original_name)
        const replacement = state.schema.nodes[spec.type]?.create(spec.attrs)
        if (replacement) {
          tr.replaceWith(pos, pos + node.nodeSize, replacement)
          changed = true
        }
        return
      }
      const nextAttrs = {
        ...node.attrs,
        kind: asset.kind,
        name: asset.original_name,
        state: asset.state,
        error: asset.error || '',
      }
      if (JSON.stringify(nextAttrs) !== JSON.stringify(node.attrs)) {
        tr.setNodeMarkup(pos, undefined, nextAttrs)
        changed = true
      }
    })
    if (!changed) return false
    tr.setMeta('marvoAssetTransition', asset.state === 'ready' ? 'resolve' : 'status')
    dispatch?.(tr)
    return true
  })
}

function onEditorReady(editor: Editor) {
  emit('ready', {
    updateContent: (c: string) => {
      if (!editor || applyingExternal.value) return
      const md = (editor.storage as any).markdown?.getMarkdown?.() || ''
      if (c !== md) {
        applyingExternal.value = true
        editor.commands.setContent(c, { emitUpdate: false })
        applyingExternal.value = false
      }
    },
    setEditable: (value: boolean) => editor.setEditable(value),
  })
}

onMounted(() => {
  const extensions: AnyExtension[] = [
    StarterKit.configure({ heading: { levels: [1, 2, 3, 4] }, codeBlock: false, link: false }),
    Placeholder.configure({ placeholder: '开始写点什么...' }),
    Highlight,
    TaskList,
    TaskItem.configure({ nested: true }),
    TextAlign.configure({ types: ['heading', 'paragraph'] }),
    ResolvedImage,
    ResolvedLink,
    CodeBlock,
    TableExt.configure({ resizable: true }),
    TableRow,
    TableCell,
    TableHeader,
    VideoNode,
    AssetPlaceholder,
    Markdown.configure({
      html: true,
      breaks: true,
      linkify: true,
      transformPastedText: true,
      transformCopiedText: true,
    }),
  ]

  const editor = new Editor({
    extensions,
    content: props.content,
    editable: props.editable,
    onTransaction: ({ editor: ed, transaction }) => {
      syncToolbarState(ed)
      if (!transaction.docChanged || applyingExternal.value) return
      const assetTransition = transaction.getMeta('marvoAssetTransition')
      if (assetTransition !== 'resolve') {
        const before = placeholderIDs(transaction.before)
        const after = placeholderIDs(transaction.doc)
        for (const id of before) if (!after.has(id)) emit('assetRemove', id)
      }
      if (assetTransition === 'status') return
      const markdown = (ed.storage as any).markdown?.getMarkdown?.() ?? ed.getHTML()
      emit('change', markdown)
    },
    onSelectionUpdate: ({ editor }) => syncToolbarState(editor),
    onFocus: ({ editor }) => syncToolbarState(editor),
    onBlur: () => {
      active.value = { ...emptyActive }
      mobileMenu.value = { ...emptyMobileMenu }
    },
  })

  ed.value = editor
  onEditorReady(editor)
  for (const asset of Object.values(noteStore.mediaAssets)) applyAssetState(asset)
})

watch(
  () => props.editable,
  (value) => {
    ed.value?.setEditable(value)
    if (!value) mobileMenu.value = { ...emptyMobileMenu }
  },
)
watch(
  () => noteStore.mediaAssets,
  (assets) => {
    for (const asset of Object.values(assets)) applyAssetState(asset)
  },
  { deep: true },
)

onBeforeUnmount(() => {
  ed.value?.destroy()
})

function upload() {
  if (!props.editable) return
  const inp = document.createElement('input')
  inp.type = 'file'
  inp.accept = 'image/*,video/*,.heic,.heif,.mov,.m4v'
  inp.onchange = async () => {
    const f = inp.files?.[0]
    if (!f || !ed.value) return
    const id = crypto.randomUUID()
    const kind = f.type.startsWith('video/') || /\.(?:mov|m4v|mp4|webm)$/i.test(f.name) ? 'video' : 'image'
    noteStore.trackMediaAsset({ id, kind, state: 'reserved', original_name: f.name, content_type: f.type })
    ed.value
      .chain()
      .focus()
      .insertContent([
        { type: 'assetPlaceholder', attrs: { id, kind, name: f.name, state: 'reserved', error: '' } },
        { type: 'paragraph' },
      ])
      .run()
    emit('assetStart', { id, file: f })
  }
  inp.click()
}
</script>

<template>
  <div v-if="ed" class="ecore">
    <Teleport :to="props.toolbarTarget" :disabled="!props.toolbarTarget">
      <fieldset class="ecore-bar" :disabled="!props.editable" aria-label="编辑快捷操作">
        <span class="toolbar-group">
          <button :class="['fb', { active: active.bold }]" title="加粗" @click="ed?.chain().focus().toggleBold().run()">
            <BoldOutlined />
          </button>
          <button
            :class="['fb', { active: active.italic }]"
            title="斜体"
            @click="ed?.chain().focus().toggleItalic().run()"
          >
            <ItalicOutlined />
          </button>
          <button
            :class="['fb', { active: active.underline }]"
            title="下划线"
            @click="ed?.chain().focus().toggleUnderline().run()"
          >
            <UnderlineOutlined />
          </button>
          <button
            :class="['fb', { active: active.strike }]"
            title="删除线"
            @click="ed?.chain().focus().toggleStrike().run()"
          >
            <StrikethroughOutlined />
          </button>
        </span>
        <span class="toolbar-divider" />
        <span class="toolbar-group">
          <button
            :class="['fb', { active: active.heading1 }]"
            title="一级标题"
            @click="ed?.chain().focus().toggleHeading({ level: 1 }).run()"
          >
            H1
          </button>
          <button
            :class="['fb', { active: active.heading2 }]"
            title="二级标题"
            @click="ed?.chain().focus().toggleHeading({ level: 2 }).run()"
          >
            H2
          </button>
          <button
            :class="['fb', { active: active.heading3 }]"
            title="三级标题"
            @click="ed?.chain().focus().toggleHeading({ level: 3 }).run()"
          >
            H3
          </button>
        </span>
        <span class="toolbar-divider" />
        <span class="toolbar-group">
          <button
            :class="['fb', { active: active.bulletList }]"
            title="无序列表"
            @click="ed?.chain().focus().toggleBulletList().run()"
          >
            <UnorderedListOutlined />
          </button>
          <button
            :class="['fb', { active: active.orderedList }]"
            title="有序列表"
            @click="ed?.chain().focus().toggleOrderedList().run()"
          >
            <OrderedListOutlined />
          </button>
          <button
            :class="['fb', { active: active.taskList }]"
            title="任务列表"
            @click="ed?.chain().focus().toggleTaskList().run()"
          >
            <CheckSquareOutlined />
          </button>
        </span>
        <span class="toolbar-divider" />
        <span class="toolbar-group">
          <button
            :class="['fb', { active: active.codeBlock }]"
            title="代码块"
            @click="ed?.chain().focus().toggleCodeBlock().run()"
          >
            <CodeOutlined />
          </button>
          <button
            :class="['fb', { active: active.blockquote }]"
            title="引用块"
            @click="ed?.chain().focus().toggleBlockquote().run()"
          >
            <MessageOutlined />
          </button>
          <button class="fb" title="插入附件" @click="upload"><PictureOutlined /></button>
        </span>
        <span v-if="uploadError" class="ecore-upload-error">{{ uploadError }}</span>
      </fieldset>
    </Teleport>
    <div class="ecore-body">
      <EditorContent :editor="ed" />
    </div>

    <div
      v-if="mobileMenu.visible && props.editable"
      class="mobile-format-menu"
      :style="{ left: mobileMenu.left + 'px', top: mobileMenu.top + 'px' }"
      @pointerdown.prevent
    >
      <button :class="['fb', { active: active.bold }]" title="加粗" @click="ed?.chain().focus().toggleBold().run()">
        <BoldOutlined />
      </button>
      <button :class="['fb', { active: active.italic }]" title="斜体" @click="ed?.chain().focus().toggleItalic().run()">
        <ItalicOutlined />
      </button>
      <button
        :class="['fb', { active: active.underline }]"
        title="下划线"
        @click="ed?.chain().focus().toggleUnderline().run()"
      >
        <UnderlineOutlined />
      </button>
      <button
        :class="['fb', { active: active.strike }]"
        title="删除线"
        @click="ed?.chain().focus().toggleStrike().run()"
      >
        <StrikethroughOutlined />
      </button>
      <span class="toolbar-divider" />
      <button
        :class="['fb', { active: active.heading1 }]"
        title="一级标题"
        @click="ed?.chain().focus().toggleHeading({ level: 1 }).run()"
      >
        H1
      </button>
      <button
        :class="['fb', { active: active.heading2 }]"
        title="二级标题"
        @click="ed?.chain().focus().toggleHeading({ level: 2 }).run()"
      >
        H2
      </button>
      <button
        :class="['fb', { active: active.heading3 }]"
        title="三级标题"
        @click="ed?.chain().focus().toggleHeading({ level: 3 }).run()"
      >
        H3
      </button>
    </div>
  </div>

  <div v-else class="page-loading" style="min-height: 200px">
    <span class="page-loading-spinner" />
    <span>编辑器加载中...</span>
  </div>
</template>

<style lang="scss">
@use '../styles/tiptap' as *;

.ecore {
  display: flex;
  flex-direction: column;
  min-height: 100%;
}

.ecore-loading {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  min-height: 200px;
  color: var(--text-muted);
  font-size: var(--marvo-type-13);
}

.ecore-bar {
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 6px 12px;
  min-width: 0;
  margin: 0;
  flex-shrink: 0;
  background: var(--bg-secondary);
  border: 0;
  border-bottom: 1px solid var(--border-light);
  overflow-x: auto;
  &[disabled] .fb {
    opacity: 0.45;
    cursor: not-allowed;
  }
  &[disabled] .fb:hover {
    background: transparent;
    color: var(--text-secondary);
  }
}

.ecore-upload-error {
  font-size: var(--marvo-type-12);
  color: var(--text-danger, #ff4d4f);
  margin-left: 8px;
  flex-shrink: 0;
}

.toolbar-group {
  display: flex;
  align-items: center;
  gap: 2px;
}
.toolbar-divider {
  width: 1px;
  height: 18px;
  background: var(--border-light);
  margin: 0 4px;
  flex-shrink: 0;
}

.fb {
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 28px;
  height: 28px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: var(--marvo-type-12);
  font-weight: 600;
  transition:
    background 0.1s,
    color 0.1s;
  flex-shrink: 0;
  &:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }
  &.active {
    background: color-mix(in srgb, var(--marvo-accent-color, #4f46e5) 12%, transparent);
    color: var(--text-accent);
  }
}

.mobile-format-menu {
  position: fixed;
  z-index: 80;
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 6px 8px;
  border-radius: 10px;
  background: var(--bg-card);
  border: 1px solid var(--border-primary);
  box-shadow: var(--shadow-card);
}

.ecore-body {
  flex: 1;
  min-height: 0;
}

.ecore-body .tiptap {
  padding: 24px 32px;
  outline: none;
  min-height: 100%;
  font-size: var(--marvo-content-font-size, var(--marvo-type-15));
  line-height: var(--marvo-content-line-height, 1.8);
  color: var(--text-primary);
  max-width: var(--marvo-content-width, none);
  margin: 0 auto;
  @include tiptap-styles;
}

.ecore-body .tiptap p.is-editor-empty:first-child::before {
  content: attr(data-placeholder);
  color: var(--text-muted);
  float: left;
  pointer-events: none;
  height: 0;
}

@media (max-width: 600px) {
  .ecore-body .tiptap {
    padding: 18px 16px max(28px, env(safe-area-inset-bottom));
  }
  .ecore-bar {
    padding-inline: 8px;
  }
}
</style>
