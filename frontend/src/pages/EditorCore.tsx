import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import type { Editor } from '@tiptap/core'
import { EditorContent, useEditor } from '@tiptap/react'
import { Step } from '@tiptap/pm/transform'
import { receiveTransaction, sendableSteps } from 'prosemirror-collab'
import StarterKit from '@tiptap/starter-kit'
import Placeholder from '@tiptap/extension-placeholder'
import ImageExt from '@tiptap/extension-image'
import { Table } from '@tiptap/extension-table'
import TableRow from '@tiptap/extension-table-row'
import TableCell from '@tiptap/extension-table-cell'
import TableHeader from '@tiptap/extension-table-header'
import TaskList from '@tiptap/extension-task-list'
import TaskItem from '@tiptap/extension-task-item'
import TextAlign from '@tiptap/extension-text-align'
import Highlight from '@tiptap/extension-highlight'
import Mathematics from '@tiptap/extension-mathematics'
import { Markdown } from 'tiptap-markdown'
import { App } from 'antd'
import {
  CheckSquareOutlined,
  CodeOutlined,
  MessageOutlined,
  OrderedListOutlined,
  PictureOutlined,
  UnorderedListOutlined,
} from '@ant-design/icons'
import { useNoteStore } from '../stores/note'
import { connect, getClientId, on } from '../hooks/useWebSocket'
import { OTCollab } from '../extensions/otCollab'

declare module '@tiptap/core' {
  interface Commands {
    markdown: {
      setMarkdown: (content: string) => void
    }
  }
}

interface MarkdownStorage {
  getMarkdown(): string
}

declare module '@tiptap/core' {
  interface Storage {
    markdown?: MarkdownStorage
  }
}

interface EditorCoreProps {
  title: string
  content: string
  version: number
  uploadAttachment: (file: File) => Promise<{ url: string }>
  onContentChange: (content: string, version?: number) => void
  onResetRequired: () => void
  toolbarTarget: HTMLElement | null
}

export default function EditorCore({
  title,
  content,
  version,
  uploadAttachment,
  onContentChange,
  onResetRequired,
  toolbarTarget,
}: EditorCoreProps) {
  const { message } = App.useApp()
  const noteStore = useNoteStore()
  const [clientID, setClientID] = useState(getClientId())
  const fileInputRef = useRef<HTMLInputElement>(null)
  const applyingRemoteRef = useRef(false)
  const inFlightRef = useRef(false)

  useEffect(() => {
    connect().then(setClientID).catch(() => undefined)
  }, [])

  const flushSteps = useCallback((ed: Editor) => {
    if (applyingRemoteRef.current || inFlightRef.current) return
    const sendable = sendableSteps(ed.state)
    if (!sendable || sendable.steps.length === 0) return

    const markdown = ed.storage.markdown?.getMarkdown() ?? ed.getHTML()
    inFlightRef.current = true
    noteStore.sendSteps(
      title,
      sendable.version,
      sendable.steps.map(step => step.toJSON()),
      markdown,
    )
  }, [noteStore, title])

  const editor = useEditor({
    content,
    editable: true,
    extensions: [
      StarterKit.configure({
        link: { openOnClick: true },
      }),
      Placeholder.configure({ placeholder: '开始书写...' }),
      ImageExt,
      Table.configure({ resizable: true }),
      TableRow,
      TableCell,
      TableHeader,
      TaskList,
      TaskItem.configure({ nested: true }),
      TextAlign.configure({ types: ['heading', 'paragraph'] }),
      Highlight,
      Mathematics,
      Markdown.configure({
        html: true,
        breaks: true,
        linkify: true,
        transformPastedText: true,
        transformCopiedText: true,
      }),
      OTCollab.configure({ version, clientID }),
    ],
    onTransaction: ({ editor: ed, transaction }) => {
      if (!transaction.docChanged) return
      const markdown = ed.storage.markdown?.getMarkdown() ?? ed.getHTML()
      onContentChange(markdown)
      flushSteps(ed)
    },
  }, [title, clientID])

  useEffect(() => {
    return on('ot_steps', (msg: any) => {
      if (msg.title !== title || !editor) return
      applyingRemoteRef.current = true
      try {
        applyRemoteSteps(editor, msg.steps, msg.client_ids)
      } finally {
        applyingRemoteRef.current = false
      }
      if (msg.client_ids?.includes(clientID)) {
        inFlightRef.current = false
      }
      onContentChange(editor.storage.markdown?.getMarkdown() ?? editor.getHTML(), msg.new_version)
      flushSteps(editor)
    })
  }, [title, editor, clientID, flushSteps, onContentChange])

  useEffect(() => {
    return on('ot_rebase', (msg: any) => {
      if (msg.title !== title || !editor) return
      applyingRemoteRef.current = true
      try {
        applyRemoteSteps(editor, msg.steps, msg.client_ids)
      } finally {
        applyingRemoteRef.current = false
      }
      inFlightRef.current = false
      onContentChange(editor.storage.markdown?.getMarkdown() ?? editor.getHTML(), msg.new_version)
      flushSteps(editor)
    })
  }, [title, editor, flushSteps, onContentChange])

  useEffect(() => {
    return on('ot_snapshot', (msg: any) => {
      if (msg.title !== title) return
      onContentChange(msg.content, msg.version)
    })
  }, [title, onContentChange])

  useEffect(() => {
    return on('ot_reset_required', (msg: any) => {
      if (msg.title !== title) return
      inFlightRef.current = false
      onResetRequired()
    })
  }, [title, onResetRequired])

  useEffect(() => {
    return () => {
      editor?.destroy()
    }
  }, [editor])

  function triggerImageUpload() {
    fileInputRef.current?.click()
  }

  async function handleImageUpload(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    if (!file || !editor) return
    try {
      const result = await uploadAttachment(file)
      editor
        .chain()
        .focus()
        .insertContent([
          { type: 'image', attrs: { src: result.url, alt: file.name } },
          { type: 'paragraph' },
        ])
        .run()
    } catch {
      message.error('图片上传失败')
    }
    event.target.value = ''
  }

  const toolbar = editor ? (
    <div className="toolbar-format">
      <span className="toolbar-divider" />
      <FormatBtn active={editor.isActive('bold')} onClick={() => editor.chain().focus().toggleBold().run()}><strong>B</strong></FormatBtn>
      <FormatBtn active={editor.isActive('italic')} onClick={() => editor.chain().focus().toggleItalic().run()}><em>I</em></FormatBtn>
      <FormatBtn active={editor.isActive('underline')} onClick={() => editor.chain().focus().toggleUnderline().run()}><u>U</u></FormatBtn>
      <FormatBtn active={editor.isActive('strike')} onClick={() => editor.chain().focus().toggleStrike().run()}><s>S</s></FormatBtn>
      <span className="toolbar-divider" />
      <FormatBtn active={editor.isActive('heading', { level: 1 })} onClick={() => editor.chain().focus().toggleHeading({ level: 1 }).run()}>H1</FormatBtn>
      <FormatBtn active={editor.isActive('heading', { level: 2 })} onClick={() => editor.chain().focus().toggleHeading({ level: 2 }).run()}>H2</FormatBtn>
      <FormatBtn active={editor.isActive('heading', { level: 3 })} onClick={() => editor.chain().focus().toggleHeading({ level: 3 }).run()}>H3</FormatBtn>
      <span className="toolbar-divider" />
      <FormatBtn active={editor.isActive('bulletList')} onClick={() => editor.chain().focus().toggleBulletList().run()}><UnorderedListOutlined /></FormatBtn>
      <FormatBtn active={editor.isActive('orderedList')} onClick={() => editor.chain().focus().toggleOrderedList().run()}><OrderedListOutlined /></FormatBtn>
      <FormatBtn active={editor.isActive('taskList')} onClick={() => editor.chain().focus().toggleTaskList().run()}><CheckSquareOutlined /></FormatBtn>
      <span className="toolbar-divider" />
      <FormatBtn active={editor.isActive('codeBlock')} onClick={() => editor.chain().focus().toggleCodeBlock().run()}><CodeOutlined /></FormatBtn>
      <FormatBtn active={editor.isActive('blockquote')} onClick={() => editor.chain().focus().toggleBlockquote().run()}><MessageOutlined /></FormatBtn>
      <FormatBtn onClick={triggerImageUpload}><PictureOutlined /></FormatBtn>
      <input
        ref={fileInputRef}
        type="file"
        accept="image/*"
        className="hidden-file-input"
        onChange={handleImageUpload}
      />
    </div>
  ) : null

  return (
    <>
      {toolbar && (toolbarTarget ? createPortal(toolbar, toolbarTarget) : toolbar)}

      <div className="editor-content">
        <EditorContent editor={editor} />
      </div>
    </>
  )
}

function FormatBtn({ active, onClick, children }: {
  active?: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      className={`fmt-btn${active ? ' active' : ''}`}
      onClick={onClick}
    >
      {children}
    </button>
  )
}

function applyRemoteSteps(editor: Editor, stepsJson: any[], clientIDs: string[]) {
  const steps = stepsJson.map(step => Step.fromJSON(editor.schema, step))
  const transaction = receiveTransaction(editor.state, steps, clientIDs)
  editor.view.dispatch(transaction)
}
