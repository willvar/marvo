import { useEffect, useRef, useState, useMemo } from 'react'
import { App, Spin, Button, Dropdown, Alert, Avatar, Modal, Card, Radio, Checkbox, Space, Tag } from 'antd'
import { PlusOutlined, DeleteOutlined, RobotOutlined, UserOutlined, MessageOutlined, ExclamationCircleOutlined, ToolOutlined } from '@ant-design/icons'
import { Bubble, Sender, Welcome, ThoughtChain, Think } from '@ant-design/x'
import MarkdownIt from 'markdown-it'
import { useAIStore, PermissionRequest, QuestionRequest } from '../stores/ai'

const md = new MarkdownIt({ html: false, breaks: true, linkify: true })

const ERR_UNAVAILABLE = 'AI 服务不可用，请检查 opencode serve 是否已启动'

function RenderMarkdown({ content }: { content: string }) {
  if (!content) return null
  const html = md.render(content)
  return <div className="ai-markdown" dangerouslySetInnerHTML={{ __html: html }} />
}

function PermissionPrompt({ request, onRespond }: { request: PermissionRequest; onRespond: (reply: 'once' | 'always' | 'reject') => void }) {
  return (
    <Card
      size="small"
      style={{ margin: '0 16px 12px', borderRadius: 8, border: '1px solid #faad14', background: '#fffbe6' }}
      title={<span style={{ color: '#d48806' }}><ExclamationCircleOutlined /> 权限确认</span>}
      extra={
        <Space>
          <Button size="small" danger onClick={() => onRespond('reject')}>拒绝</Button>
          <Button size="small" onClick={() => onRespond('once')}>允许本次</Button>
          <Button size="small" type="primary" onClick={() => onRespond('always')}>始终允许</Button>
        </Space>
      }
    >
      <p style={{ margin: '0 0 4px', fontWeight: 500 }}>需要执行操作：<Tag color="orange">{request.permission}</Tag></p>
      {request.patterns && request.patterns.length > 0 && (
        <p style={{ margin: 0, color: '#666', fontSize: 13 }}>
          匹配规则：{request.patterns.join(', ')}
        </p>
      )}
    </Card>
  )
}

function QuestionPrompt({ request, onRespond, onReject }: { request: QuestionRequest; onRespond: (answers: string[][]) => void; onReject: () => void }) {
  const [answers, setAnswers] = useState<string[][]>(() =>
    request.questions.map(() => [])
  )

  function toggleOption(qIdx: number, label: string, multiple: boolean) {
    setAnswers((prev) => {
      const next = prev.map((a, i) => {
        if (i !== qIdx) return a
        if (multiple) {
          return a.includes(label) ? a.filter((l) => l !== label) : [...a, label]
        }
        return [label]
      })
      return next
    })
  }

  return (
    <Modal
      title={request.questions[0]?.header || '确认操作'}
      open
      onOk={() => onRespond(answers)}
      onCancel={onReject}
      okText="确认"
      cancelText="取消"
      destroyOnClose
    >
      {request.questions.map((q, qi) => (
        <div key={qi} style={{ marginBottom: qi < request.questions.length - 1 ? 16 : 0 }}>
          <p style={{ fontWeight: 500, marginBottom: 8 }}>{q.question}</p>
          {q.multiple ? (
            q.options.map((opt, oi) => (
              <div key={oi} style={{ marginBottom: 6 }}>
                <Checkbox
                  checked={answers[qi]?.includes(opt.label)}
                  onChange={() => toggleOption(qi, opt.label, true)}
                >
                  <strong>{opt.label}</strong> - {opt.description}
                </Checkbox>
              </div>
            ))
          ) : (
            <Radio.Group
              value={answers[qi]?.[0]}
              onChange={(e) => setAnswers((prev) => prev.map((a, i) => i === qi ? [e.target.value] : a))}
            >
              <Space direction="vertical">
                {q.options.map((opt, oi) => (
                  <Radio key={oi} value={opt.label}>
                    <strong>{opt.label}</strong> - {opt.description}
                  </Radio>
                ))}
              </Space>
            </Radio.Group>
          )}
        </div>
      ))}
    </Modal>
  )
}

function toolStatus(status: string): 'loading' | 'success' | 'error' {
  if (status === 'completed') return 'success'
  if (status === 'error') return 'error'
  return 'loading'
}

export default function AIChat() {
  const { message: toast } = App.useApp()
  const store = useAIStore()
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const [inputValue, setInputValue] = useState('')
  const [initError, setInitError] = useState<string | null>(null)
  const [permResponding, setPermResponding] = useState(false)
  const [qResponding, setQResponding] = useState(false)

  useEffect(() => {
    store.connect()
    store.loadSessions().catch(() => setInitError(ERR_UNAVAILABLE))
    return () => store.disconnect()
  }, [])

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [store.messages, store.parts])

  const currentPermissions = useMemo(() => {
    if (!store.currentSessionId) return []
    return store.permissions[store.currentSessionId] || []
  }, [store.permissions, store.currentSessionId])

  const currentQuestions = useMemo(() => {
    if (!store.currentSessionId) return []
    return store.questions[store.currentSessionId] || []
  }, [store.questions, store.currentSessionId])

  const blocked = currentPermissions.length > 0 || currentQuestions.length > 0

  const bubbleItems = useMemo(() => {
    const items: Array<{ key: string; role: string; content: React.ReactNode; footer?: React.ReactNode }> = []
    let i = 0

    while (i < store.messages.length) {
      const msg = store.messages[i]

      if (msg.role === 'assistant') {
        const group = [msg]
        let j = i + 1
        while (j < store.messages.length && store.messages[j].role === 'assistant') {
          group.push(store.messages[j])
          j++
        }

        const allParts = group.flatMap((m) => store.parts[m.id] || [])
        const textParts = allParts.filter((p) => p.type === 'text')
        const reasoningParts = allParts.filter((p) => p.type === 'reasoning')
        const toolParts = allParts.filter((p) => p.type === 'tool' && p.tool !== 'question')

        const lastMsg = group[group.length - 1]
        const isStreaming = lastMsg.role === 'assistant' && typeof lastMsg.time?.completed !== 'number'
        const textContent = textParts.map((p) => p.text || '').join('\n')
        const reasoningContent = reasoningParts.map((p) => p.text || '').join('\n')
        const hasContent = !!textContent || !!reasoningContent || toolParts.length > 0

        const hasThink = !!reasoningContent || toolParts.length > 0

        const thinkLoading = isStreaming && (
          reasoningParts.some((p) => !p.time?.end) ||
          toolParts.some((p) => p.state?.status === 'pending' || p.state?.status === 'running')
        )

        items.push({
          key: group[0].id,
          role: 'ai',
          content: (
            <div>
              {hasThink ? (
                <div style={{ marginBottom: 12 }}>
                  <Think
                  title="思考过程"
                  loading={thinkLoading}
                  defaultExpanded={false}>
                  {reasoningContent ? <RenderMarkdown content={reasoningContent} /> : null}
                  {toolParts.length > 0 ? (
                    <ThoughtChain
                      items={toolParts.map((p) => ({
                        key: p.id,
                        title: (p.state as any)?.title || p.tool || '工具调用',
                        description: p.state?.status === 'completed' && p.state?.output
                          ? typeof p.state.output === 'string'
                            ? p.state.output.slice(0, 300)
                            : JSON.stringify(p.state.output).slice(0, 300)
                          : p.state?.status === 'running'
                            ? '执行中...'
                            : '',
                        status: toolStatus(p.state?.status || ''),
                        icon: <ToolOutlined />,
                      }))}
                    />
                  ) : null}
                </Think>
                </div>
              ) : null}
              {textContent ? <RenderMarkdown content={textContent} /> : null}
              {isStreaming && !hasContent ? <Spin size="small" /> : null}
            </div>
          ),
        })
        i = j
      } else {
        const parts = store.parts[msg.id] || []
        const textParts = parts.filter((p) => p.type === 'text')
        const content = textParts.map((p) => p.text || '').join('\n')

        items.push({
          key: msg.id,
          role: msg.role === 'assistant' ? 'ai' : msg.role,
          content: <div>{content ? <RenderMarkdown content={content} /> : null}</div>,
        })
        i++
      }
    }

    return items
  }, [store.messages, store.parts])

  const bubbleRoles = useMemo(() => ({
    ai: {
      placement: 'start' as const,
      avatar: <Avatar icon={<RobotOutlined />} />,
      variant: 'shadow' as const,
      styles: { content: { borderRadius: 12 } },
    },
    user: {
      placement: 'end' as const,
      avatar: <Avatar icon={<UserOutlined />} />,
      variant: 'shadow' as const,
      styles: { content: { borderRadius: 12 } },
    },
  }), [])

  async function handleSend(text: string) {
    if (!text.trim()) return
    if (store.sending) return
    setInitError(null)
    if (!store.currentSessionId) {
      try {
        const id = await store.createSession()
        if (!id) return
      } catch {
        setInitError(ERR_UNAVAILABLE)
        return
      }
    }
    setInputValue('')
    try {
      await store.sendMessage(text)
    } catch {
      setInitError('发送失败，AI 服务可能不可用')
    }
  }

  async function handleDeleteSession(id: string) {
    try {
      await store.deleteSession(id)
      toast.success('已删除')
    } catch {
      toast.error('删除失败')
    }
  }

  async function handlePermRespond(reply: 'once' | 'always' | 'reject') {
    const perm = currentPermissions[0]
    if (!perm || permResponding) return
    setPermResponding(true)
    try {
      await store.respondPermission(perm.id, reply)
    } catch {
      toast.error('响应失败')
    } finally {
      setPermResponding(false)
    }
  }

  async function handleQRespond(answers: string[][]) {
    const q = currentQuestions[0]
    if (!q || qResponding) return
    setQResponding(true)
    try {
      await store.respondQuestion(q.id, answers)
    } catch {
      toast.error('响应失败')
    } finally {
      setQResponding(false)
    }
  }

  async function handleQReject() {
    const q = currentQuestions[0]
    if (!q || qResponding) return
    setQResponding(true)
    try {
      await store.rejectQuestion(q.id)
    } catch {
      toast.error('响应失败')
    } finally {
      setQResponding(false)
    }
  }

  const sessionMenuItems = useMemo(() =>
    store.sessions.map((s) => ({
      key: s.id,
      label: (
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
          <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {s.title || '新对话'}
          </span>
          <DeleteOutlined
            style={{ color: '#999', flexShrink: 0 }}
            onClick={(e) => { e.stopPropagation(); handleDeleteSession(s.id) }}
          />
        </div>
      ),
    })),
    [store.sessions],
  )

  const currentSessionTitle = useMemo(() => {
    const s = store.sessions.find((s) => s.id === store.currentSessionId)
    return s?.title || '新对话'
  }, [store.sessions, store.currentSessionId])

  return (
    <div className="ai-page">
      <div className="ai-toolbar">
        <div className="ai-toolbar-left">
          <Dropdown
            menu={{
              items: [
                { key: '_new', label: '新对话', icon: <PlusOutlined /> },
                { type: 'divider' },
                ...sessionMenuItems,
              ],
              onClick: ({ key }) => {
                if (key === '_new') {
                  store.createSession().catch(() => setInitError(ERR_UNAVAILABLE))
                } else {
                  store.selectSession(key)
                }
              },
              selectedKeys: store.currentSessionId ? [store.currentSessionId] : [],
            }}
            trigger={['click']}
          >
            <Button type="text" size="small" style={{ maxWidth: 200 }}>
              <MessageOutlined style={{ marginRight: 6 }} />
              <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {store.currentSessionId ? currentSessionTitle : '选择对话'}
              </span>
            </Button>
          </Dropdown>
          <Button
            type="text" size="small" icon={<PlusOutlined />}
            onClick={() => store.createSession().catch(() => setInitError(ERR_UNAVAILABLE))}
          />
        </div>
      </div>

      {initError && (
        <Alert
          type="error" icon={<ExclamationCircleOutlined />} message={initError}
          showIcon closable onClose={() => setInitError(null)}
          style={{ margin: '0 16px', borderRadius: 8 }}
        />
      )}

      <div className="ai-messages">
        {!store.currentSessionId ? (
          <div className="ai-welcome">
            <Welcome
              icon={<RobotOutlined style={{ fontSize: 48, color: '#4f46e5' }} />}
              title="AI 助手"
              description="基于 OpenCode，可以读写笔记、搜索内容"
            />
          </div>
        ) : store.loading ? (
          <div className="ai-loading"><Spin /></div>
        ) : (
          <>
            <Bubble.List
              items={bubbleItems}
              role={bubbleRoles}
              style={{ flex: 1, padding: '16px 24px' }}
            />
            <div ref={messagesEndRef} />
          </>
        )}
      </div>

      <div className="ai-sender">
        {currentPermissions[0] && (
          <PermissionPrompt request={currentPermissions[0]} onRespond={handlePermRespond} />
        )}

        {currentQuestions[0] && (
          <QuestionPrompt
            request={currentQuestions[0]}
            onRespond={handleQRespond}
            onReject={handleQReject}
          />
        )}

        <Sender
          value={inputValue}
          onChange={setInputValue}
          onSubmit={handleSend}
          loading={store.sending}
          onCancel={() => store.abortSession()}
          placeholder={blocked ? '请先回应 AI 的请求...' : '输入消息...'}
          submitType="enter"
          disabled={blocked}
        />
      </div>

      <style>{`
        .ai-page { height: 100%; display: flex; flex-direction: column; background: #fff; }
        .ai-toolbar { display: flex; align-items: center; justify-content: space-between; padding: 8px 16px; border-bottom: 1px solid #f0f0f0; flex-shrink: 0; }
        .ai-toolbar-left { display: flex; align-items: center; gap: 2px; }
        .ai-messages { flex: 1; overflow-y: auto; display: flex; flex-direction: column; }
        .ai-welcome { flex: 1; display: flex; align-items: center; justify-content: center; }
        .ai-loading { flex: 1; display: flex; align-items: center; justify-content: center; }
        .ai-sender { padding: 12px 24px 20px; border-top: 1px solid #f0f0f0; flex-shrink: 0; }
        .ai-markdown pre { background: #1e1e1e; color: #d4d4d4; padding: 12px 16px; border-radius: 8px; overflow-x: auto; font-size: 13px; line-height: 1.5; }
        .ai-markdown code { background: rgba(0,0,0,0.06); padding: 2px 6px; border-radius: 4px; font-size: 13px; font-family: 'SF Mono','Fira Code',monospace; }
        .ai-markdown pre code { background: none; padding: 0; }
        .ai-markdown p { margin: 0 0 8px; }
        .ai-markdown p:last-child { margin-bottom: 0; }
        .ai-markdown ul, .ai-markdown ol { padding-left: 20px; margin: 4px 0; }
        .ai-markdown blockquote { border-left: 3px solid #ddd; margin: 8px 0; padding-left: 12px; color: #666; }
      `}</style>
    </div>
  )
}
