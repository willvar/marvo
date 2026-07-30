import { Suspense, lazy, useCallback, useEffect, useMemo, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { Button, Tag, Input, Spin, Empty, Space, App } from 'antd'
import { DeleteOutlined } from '@ant-design/icons'
import { useNoteStore } from '../stores/note'
import { on } from '../hooks/useWebSocket'
import './NoteEditor.css'

const EditorCore = lazy(() => import('./EditorCore'))
const NotePreview = lazy(() => import('./NotePreview'))

function preloadEditorCore() {
  void import('./EditorCore')
}

export default function NoteEditor() {
  const { title: paramTitle } = useParams()
  const navigate = useNavigate()
  const { message, modal } = App.useApp()
  const noteStore = useNoteStore()

  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [noteData, setNoteData] = useState<any>(null)
  const [editMode, setEditMode] = useState(false)
  const [showTagInput, setShowTagInput] = useState(false)
  const [newTag, setNewTag] = useState('')
  const [toolbarTarget, setToolbarTarget] = useState<HTMLDivElement | null>(null)

  const title = useMemo(() => (paramTitle ? decodeURIComponent(paramTitle) : ''), [paramTitle])

  const loadNote = useCallback(async (noteTitle: string) => {
    if (!noteTitle) return
    setLoading(true)
    setLoadError(false)
    if (noteStore.currentNote?.note?.title === noteTitle) {
      setNoteData(noteStore.currentNote)
      setLoading(false)
      return
    }
    try {
      const data = await noteStore.getNote(noteTitle)
      setNoteData(data)
    } catch {
      setNoteData(null)
      setLoadError(true)
    } finally {
      setLoading(false)
    }
  }, [noteStore])

  useEffect(() => {
    loadNote(title)
    noteStore.subscribeNote(title)
    return () => { noteStore.unsubscribeNote(title) }
  }, [title])

  useEffect(() => {
    return on('subscribed', (msg: any) => {
      if (msg.title !== title) return
      setNoteData((prev: any) => prev ? { ...prev, content: msg.content, version: msg.version } : null)
    })
  }, [title])

  useEffect(() => {
    return on('ot_steps', (msg: any) => {
      if (msg.title !== title || editMode) return
      setNoteData((prev: any) => prev ? { ...prev, content: msg.content, version: msg.new_version } : null)
    })
  }, [title, editMode])

  useEffect(() => {
    return on('ot_snapshot', (msg: any) => {
      if (msg.title !== title) return
      setNoteData((prev: any) => prev ? { ...prev, content: msg.content, version: msg.version } : null)
    })
  }, [title])

  useEffect(() => {
    return on('ot_reset_required', (msg: any) => {
      if (msg.title !== title) return
      loadNote(title)
    })
  }, [title, loadNote])

  useEffect(() => {
    const handleSaveShortcut = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
        event.preventDefault()
        message.info('内容已自动保存')
      }
    }

    window.addEventListener('keydown', handleSaveShortcut)
    return () => window.removeEventListener('keydown', handleSaveShortcut)
  }, [message])

  useEffect(() => {
    if (typeof window === 'undefined') return
    if ('requestIdleCallback' in window) {
      const id = window.requestIdleCallback(preloadEditorCore)
      return () => window.cancelIdleCallback(id)
    }
    const id = globalThis.setTimeout(preloadEditorCore, 1000)
    return () => globalThis.clearTimeout(id)
  }, [])

  async function addTag() {
    if (!newTag || !noteData) {
      setShowTagInput(false)
      return
    }
    try {
      const tags = [...noteData.note.tags, newTag]
      await noteStore.updateMeta(title, tags)
      setNoteData({ ...noteData, note: { ...noteData.note, tags } })
    } catch {
      message.error('标签添加失败')
    }
    setNewTag('')
    setShowTagInput(false)
  }

  async function removeTag(tag: string) {
    if (!noteData) return
    const tags = noteData.note.tags.filter((t: string) => t !== tag)
    try {
      await noteStore.updateMeta(title, tags)
      setNoteData({ ...noteData, note: { ...noteData.note, tags } })
    } catch {
      message.error('标签删除失败')
    }
  }

  function handleDelete() {
    modal.confirm({
      title: '确认删除',
      content: `确定要删除笔记「${title}」吗？`,
      okText: '删除',
      okType: 'danger',
      cancelText: '取消',
      onOk: async () => {
        await noteStore.deleteNote(title)
        navigate('/')
        message.success('已删除')
      },
    })
  }

  if (loading) {
    return (
      <div className="note-editor-center">
        <Spin size="large" />
      </div>
    )
  }

  if (!noteData) {
    if (loadError) {
      return (
        <div className="note-editor-center">
          <Empty description="笔记不存在" />
        </div>
      )
    }
    return (
      <div className="note-editor-center">
        <Spin size="large" />
      </div>
    )
  }

  const uploadAttachment = (file: File) => noteStore.uploadAttachment(title, file)
  const updateContent = (content: string, version?: number) => {
    setNoteData((prev: any) => prev ? { ...prev, content, version: version ?? prev.version } : null)
  }

  return (
    <div className="note-editor">
      <div className="note-editor-toolbar">
        <div className="note-editor-toolbar-left">
          <Space.Compact>
            <Button
              type={editMode ? 'primary' : 'default'}
              size="small"
              onClick={() => setEditMode(true)}
            >
              编辑
            </Button>
            <Button
              type={!editMode ? 'primary' : 'default'}
              size="small"
              onClick={() => setEditMode(false)}
            >
              阅读
            </Button>
          </Space.Compact>
          {editMode && <div ref={setToolbarTarget} />}
        </div>

        <div className="note-editor-toolbar-right">
          {noteData.note.tags.map((tag: string) => (
            <Tag key={tag} onClose={() => removeTag(tag)}>{tag}</Tag>
          ))}
          {!showTagInput ? (
            <button className="add-tag-btn" onClick={() => setShowTagInput(true)}>+ 标签</button>
          ) : (
            <Input
              value={newTag}
              onChange={(e) => setNewTag(e.target.value)}
              size="small"
              className="tag-input"
              placeholder="标签名"
              onPressEnter={addTag}
              onBlur={addTag}
              autoFocus
            />
          )}
          <FormatBtn danger onClick={handleDelete}><DeleteOutlined /></FormatBtn>
        </div>
      </div>

      {editMode ? (
        <Suspense fallback={<EditorLoading />}>
          <EditorCore
            title={title}
            content={noteData.content}
            version={noteData.version ?? 0}
            uploadAttachment={uploadAttachment}
            onContentChange={updateContent}
            onResetRequired={() => loadNote(title)}
            toolbarTarget={toolbarTarget}
          />
        </Suspense>
      ) : (
        <Suspense fallback={<div className="note-editor-center"><Spin /></div>}>
          <NotePreview content={noteData.content} />
        </Suspense>
      )}

    </div>
  )
}

function EditorLoading() {
  return (
    <div className="editor-content editor-loading">
      <div className="editor-loading-indicator">
        <Spin size="small" />
        <span>编辑器加载中...</span>
      </div>
    </div>
  )
}

function FormatBtn({ onClick, children, danger }: {
  onClick: () => void
  children: React.ReactNode
  danger?: boolean
}) {
  return (
    <button
      className={`fmt-btn${danger ? ' danger' : ''}`}
      onClick={onClick}
    >
      {children}
    </button>
  )
}
