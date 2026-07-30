import { FileTextOutlined, EditOutlined } from '@ant-design/icons'
import { useNoteStore } from '../stores/note'

export default function NotesList() {
  const noteStore = useNoteStore()
  const hasNotes = noteStore.notes.length > 0

  return (
    <div style={styles.placeholder}>
      {hasNotes ? (
        <FileTextOutlined style={{ fontSize: 40, color: '#b0b4c0' }} />
      ) : (
        <EditOutlined style={{ fontSize: 48, color: '#b0b4c0' }} />
      )}
      <div style={styles.emptyTitle}>
        {hasNotes ? '选择或创建一个笔记' : '创建你的第一个笔记'}
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  placeholder: {
    height: '100%',
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 12,
    padding: 24,
  },
  emptyTitle: {
    fontSize: 16,
    fontWeight: 600,
    color: '#374151',
  },
}
