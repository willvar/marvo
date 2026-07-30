import { useState, useRef, useEffect, useCallback, useMemo } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { Input, Button, App } from 'antd'
import { MenuOutlined, CloseOutlined, SearchOutlined, PlusOutlined } from '@ant-design/icons'
import { useNoteStore } from '../stores/note'

export default function HomePage() {
  const navigate = useNavigate()
  const location = useLocation()
  const { message } = App.useApp()
  const noteStore = useNoteStore()

  const [searchQuery, setSearchQuery] = useState('')
  const [siderCollapsed, setSiderCollapsed] = useState(false)

  const [editingTitle, setEditingTitle] = useState(false)
  const [editTitleValue, setEditTitleValue] = useState('')
  const titleInputRef = useRef<any>(null)

  const currentTitle = useMemo(() => {
    const match = location.pathname.match(/^\/note\/(.+)/)
    return match ? decodeURIComponent(match[1]) : ''
  }, [location.pathname])

  const sortedNotes = useMemo(
    () => [...noteStore.notes].sort((a, b) =>
      new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
    ),
    [noteStore.notes],
  )

  useEffect(() => {
    noteStore.fetchNotes()
    if (window.innerWidth <= 768) {
      setSiderCollapsed(true)
    }
  }, [])

  const existingTitles = useMemo(() => new Set(noteStore.notes.map((n: any) => n.title)), [noteStore.notes])

  const handleSearch = useCallback((value: string) => {
    setSearchQuery(value)
    noteStore.search(value, 10)
  }, [noteStore])

  async function handleOpenOrCreate() {
    if (!searchQuery) return
    const title = searchQuery
    await noteStore.getOrCreateNote(title)
    navigate(`/note/${encodeURIComponent(title)}`)
    setSearchQuery('')
    if (window.innerWidth <= 768) {
      setSiderCollapsed(true)
    }
  }

  function openNote(title: string) {
    navigate(`/note/${encodeURIComponent(title)}`)
    setSearchQuery('')
    if (window.innerWidth <= 768) {
      setSiderCollapsed(true)
    }
  }

  function formatDate(dateStr: string) {
    if (!dateStr) return ''
    const d = new Date(dateStr)
    const now = new Date()
    const diffMs = now.getTime() - d.getTime()
    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))
    if (diffDays === 0) {
      return `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`
    }
    if (diffDays < 7) return `${diffDays}天前`
    if (d.getFullYear() === now.getFullYear()) return `${d.getMonth() + 1}月${d.getDate()}日`
    return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()}`
  }

  function startEditTitle() {
    if (!currentTitle) return
    setEditTitleValue(currentTitle)
    setEditingTitle(true)
    setTimeout(() => titleInputRef.current?.focus(), 0)
  }

  async function confirmRename() {
    if (!editingTitle) return
    const newTitle = editTitleValue.trim()
    const oldTitle = currentTitle
    setEditingTitle(false)
    if (!newTitle || newTitle === oldTitle) return
    try {
      await noteStore.renameNote(oldTitle, newTitle)
      navigate(`/note/${newTitle}`, { replace: true })
      message.success('重命名成功')
    } catch {
      message.error('重命名失败')
    }
  }

  function cancelRename() {
    setEditingTitle(false)
  }

  return (
    <div className="home-layout">
      {/* Sidebar */}
      <aside className={`home-sider${siderCollapsed ? ' home-sider-collapsed' : ''}`}>
        <div className="sider-header">
          <Input
            value={searchQuery}
            onChange={(e) => handleSearch(e.target.value)}
            placeholder="搜索或新建..."
            size="small"
            variant="filled"
            allowClear
            prefix={<SearchOutlined style={{ color: '#999' }} />}
            onPressEnter={handleOpenOrCreate}
            style={{ borderRadius: 20 }}
          />
          <Button
            type="text"
            size="small"
            icon={<CloseOutlined />}
            onClick={() => setSiderCollapsed(true)}
            className="sider-close-btn"
          />
        </div>

        <div className="notes-scroll">
          {searchQuery && (
            <div
              className="note-card note-card-create"
              onClick={handleOpenOrCreate}
            >
              <div className="note-card-title">
                <PlusOutlined style={{ marginRight: 6, color: '#4f46e5' }} />
                {existingTitles.has(searchQuery) ? `打开「${searchQuery}」` : `新建「${searchQuery}」`}
              </div>
            </div>
          )}
          {(searchQuery ? noteStore.searchResults : sortedNotes).map((item: any) => {
            const title = item.title
            return (
              <div
                key={title}
                className={`note-card${currentTitle === title ? ' note-card-active' : ''}`}
                onClick={() => openNote(title)}
              >
                <div className="note-card-title">{title}</div>
                {'content' in item && item.content ? (
                  <div className="note-card-preview">{item.content}</div>
                ) : (
                  <div className="note-card-meta">
                    {item.tags?.length > 0 && (
                      <span className="note-card-tags">
                        {item.tags.slice(0, 2).map((tag: string) => (
                          <span key={tag} className="note-tag">{tag}</span>
                        ))}
                      </span>
                    )}
                    <span className="note-card-time">{formatDate(item.updated_at)}</span>
                  </div>
                )}
              </div>
            )
          })}
          {!noteStore.notes.length && !searchQuery && (
            <div className="notes-empty">还没有笔记</div>
          )}
        </div>
      </aside>

      {/* Overlay for mobile */}
      {!siderCollapsed && (
        <div className="sider-overlay" onClick={() => setSiderCollapsed(true)} />
      )}

      {/* Main area */}
      <main className="main-area">
        <header className="main-header">
          <div className="header-left">
            <button className="icon-btn" onClick={() => setSiderCollapsed(!siderCollapsed)}>
              <MenuOutlined style={{ fontSize: 18 }} />
            </button>
            {!editingTitle ? (
              currentTitle ? (
                <span className="header-title" onClick={startEditTitle}>
                  {currentTitle}
                </span>
              ) : null
            ) : (
              <Input
                ref={titleInputRef}
                value={editTitleValue}
                onChange={(e) => setEditTitleValue(e.target.value)}
                size="small"
                className="header-title-input"
                onPressEnter={confirmRename}
                onBlur={confirmRename}
                onKeyDown={(e) => { if (e.key === 'Escape') cancelRename() }}
              />
            )}
          </div>
        </header>
        <div className="main-content">
          <Outlet />
        </div>
      </main>
      <style>{`
        .home-layout { height: 100vh; display: flex; overflow: hidden; background: #fff; }
        .home-sider {
          width: 280px; min-width: 280px; height: 100vh;
          display: flex; flex-direction: column;
          background: #f8f9fa; border-right: 1px solid #e9ecef;
          transition: margin-left 0.25s cubic-bezier(0.4, 0, 0.2, 1), opacity 0.25s ease;
          overflow: hidden;
        }
        .home-sider-collapsed { margin-left: -280px; opacity: 0; pointer-events: none; }
        .sider-header { padding: 16px 12px 8px; display: flex; align-items: center; gap: 6px; }
        .sider-close-btn { display: none; flex-shrink: 0; }
        .notes-scroll { flex: 1; overflow-y: auto; padding: 8px 8px 16px; }
        .note-card { padding: 10px 12px; border-radius: 8px; cursor: pointer; transition: background 0.15s; margin-bottom: 2px; }
        .note-card:hover { background: rgba(0,0,0,0.04); }
        .note-card-active { background: #eef2ff; }
        .note-card-active:hover { background: #e0e7ff; }
        .note-card-create { background: #f5f3ff; }
        .note-card-create:hover { background: #ede9fe; }
        .note-card-title { font-size: 14px; font-weight: 500; color: #1a1a1a; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; line-height: 1.4; }
        .note-card-preview { font-size: 12px; color: #8b8fa3; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; margin-top: 2px; line-height: 1.4; }
        .note-card-meta { display: flex; align-items: center; gap: 8px; margin-top: 4px; }
        .note-card-tags { display: flex; gap: 4px; overflow: hidden; }
        .note-tag { font-size: 11px; color: #6b7280; background: rgba(0,0,0,0.05); padding: 1px 6px; border-radius: 4px; white-space: nowrap; }
        .note-card-time { font-size: 11px; color: #b0b4c0; white-space: nowrap; }
        .notes-empty { text-align: center; color: #b0b4c0; font-size: 13px; padding: 32px 0; }
        .sider-overlay { display: none; }
        .main-area { flex: 1; display: flex; flex-direction: column; min-width: 0; overflow: hidden; }
        .main-header { display: flex; align-items: center; justify-content: space-between; padding: 0 16px; height: 52px; min-height: 52px; background: #fff; }
        .header-left { display: flex; align-items: center; gap: 8px; min-width: 0; flex: 1; }
        .icon-btn {
          display: inline-flex; align-items: center; justify-content: center;
          width: 36px; height: 36px; border: none; background: transparent;
          border-radius: 8px; cursor: pointer; color: #6b7280; flex-shrink: 0;
          transition: background 0.15s, color 0.15s;
        }
        .icon-btn:hover { background: #f3f4f6; color: #1a1a1a; }
        .header-title {
          font-size: 15px; font-weight: 600; color: #1a1a1a; cursor: pointer;
          white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
          padding: 4px 8px; border-radius: 6px; transition: background 0.15s;
        }
        .header-title:hover { background: #f3f4f6; }
        .header-title-input { max-width: 300px; }
        .main-content { flex: 1; overflow: hidden; }

        @media (max-width: 768px) {
          .home-sider {
            position: fixed; z-index: 100; top: 0; left: 0;
            margin-left: 0; opacity: 1; pointer-events: auto;
            box-shadow: 4px 0 24px rgba(0,0,0,0.1);
          }
          .home-sider-collapsed {
            margin-left: -280px; opacity: 0; pointer-events: none;
            box-shadow: none;
          }
          .sider-overlay { display: block; position: fixed; z-index: 99; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.25); backdrop-filter: blur(2px); }
          .sider-close-btn { display: inline-flex; }
          .header-title-input { max-width: 160px; }
        }
      `}</style>
    </div>
  )
}
