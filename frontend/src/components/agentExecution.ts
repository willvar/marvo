import type { MessagePart } from '../sdk'
import type { XThoughtItem, XThoughtStatus } from './x'

const TOOL_PART_TYPES = new Set(['tool', 'tool_use', 'tool_result'])
const VISIBLE_EVENT_PART_TYPES = new Set(['subtask', 'patch', 'retry'])

type ActivityKind =
  'research' | 'inspect' | 'modify' | 'command' | 'task' | 'plan' | 'setup' | 'marvo' | 'repository' | 'unknown'

export interface AgentExecutionOutcome {
  streaming: boolean
  failed?: boolean
  aborted?: boolean
}

interface SemanticActivity {
  key: string
  kind: ActivityKind
  groupKey: string
  title: string
  targets: string[]
  files: string[]
  errors: string[]
  status: XThoughtStatus
}

interface ActivityGroup {
  key: string
  kind: ActivityKind
  items: SemanticActivity[]
}

export function buildExecutionThoughtChainFromParts(
  parts: MessagePart[],
  rootKey: string,
  outcome: AgentExecutionOutcome,
  duration = '',
): XThoughtItem[] {
  const executionParts = normalizeExecutionParts(parts)
  const actionParts = executionParts.filter((part) => part.type !== 'retry')
  const activities = actionParts.map((part, index) => activityFromPart(part, index, actionParts.length, outcome))
  if (activities.length === 0) return []

  const children = groupActivities(activities).map((group) => groupThoughtItem(group, outcome))
  const errorCount = activities.filter((activity) => activity.status === 'error').length
  const retryCount = executionParts.filter((part) => part.type === 'retry').length
  const loadingItem = [...children].reverse().find((item) => item.status === 'loading')
  const completedCount = children.filter((item) => item.status !== 'loading' && item.status !== 'stopped').length

  let title: string
  let status: XThoughtStatus
  if (outcome.streaming) {
    title = '正在处理'
    status = 'loading'
  } else if (outcome.aborted) {
    title = '已停止'
    status = 'stopped'
  } else if (outcome.failed) {
    title = '执行失败'
    status = 'error'
  } else {
    title = '已完成'
    status = errorCount > 0 ? 'warning' : 'success'
  }

  const visibleCount = outcome.streaming ? completedCount : children.length
  const description = [
    outcome.streaming && loadingItem ? loadingItem.title : '',
    visibleCount > 0 ? `${visibleCount} 项处理` : '',
    errorCount > 0 ? `${errorCount} 项未成功` : '',
    retryCount > 0 ? `重试 ${retryCount} 次` : '',
    outcome.streaming ? '' : duration,
  ]
    .filter(Boolean)
    .join(' · ')

  return [
    {
      key: `${rootKey}-execution`,
      title,
      description,
      status,
      collapsible: true,
      children,
    },
  ]
}

function normalizeExecutionParts(parts: MessagePart[]) {
  const result: MessagePart[] = []
  const toolIndexes = new Map<string, number>()

  for (const part of parts) {
    if (TOOL_PART_TYPES.has(part.type)) {
      if (toolName(part) === 'question') continue
      const key = part.callID || part.id
      const existingIndex = toolIndexes.get(key)
      if (existingIndex === undefined) {
        toolIndexes.set(key, result.length)
        result.push(part)
      } else if (toolStateRank(part) >= toolStateRank(result[existingIndex])) {
        result[existingIndex] = part
      }
      continue
    }
    if (VISIBLE_EVENT_PART_TYPES.has(part.type)) result.push(part)
  }

  return result
}

export function isAgentExecutionPart(part: MessagePart) {
  return TOOL_PART_TYPES.has(part.type) || VISIBLE_EVENT_PART_TYPES.has(part.type)
}

export function isAgentTaskToolPart(part: MessagePart) {
  return TOOL_PART_TYPES.has(part.type) && toolName(part) === 'task'
}

function toolStateRank(part: MessagePart) {
  if (part.type === 'tool_result') return 3
  const ranks: Record<string, number> = { pending: 0, running: 1, completed: 3, error: 3 }
  return ranks[part.state?.status || ''] ?? 0
}

function activityFromPart(
  part: MessagePart,
  index: number,
  actionCount: number,
  outcome: AgentExecutionOutcome,
): SemanticActivity {
  if (TOOL_PART_TYPES.has(part.type)) return toolActivity(part, index, outcome)

  const key = part.id || `${part.type}-${index}`
  if (part.type === 'patch') {
    const files = stringArray(part.files).map(compactPath)
    return {
      key,
      kind: 'modify',
      groupKey: 'modify',
      title: files.length > 1 ? `更新 ${files.length} 个文件` : '更新文件',
      targets: files,
      files,
      errors: [],
      status: 'success',
    }
  }

  const isActive = outcome.streaming && index === actionCount - 1
  const agent = stringValue(part.agent)
  const description = stringValue(part.description)
  return {
    key,
    kind: 'task',
    groupKey: 'task',
    title: agent ? `交给 ${agent} 处理` : '处理子任务',
    targets: description ? [description] : [],
    files: [],
    errors: [],
    status: isActive ? 'loading' : 'success',
  }
}

function toolActivity(part: MessagePart, index: number, outcome: AgentExecutionOutcome): SemanticActivity {
  const tool = toolName(part)
  const kind = toolKind(tool)
  const files = toolFiles(part, tool)
  const targets = uniqueStrings([...files, ...toolTargets(part, tool)])
  const status = toolStatus(part, outcome.streaming)
  const state = record(part.state)
  const error = status === 'error' ? apiErrorText(state.error ?? part.error) : ''

  return {
    key: part.callID || part.id || `tool-${index}`,
    kind,
    groupKey:
      kind === 'unknown'
        ? `unknown:${tool || index}`
        : kind === 'marvo'
          ? `marvo:${tool}:${toolActionTitle(part, tool)}`
          : kind,
    title: toolActionTitle(part, tool),
    targets,
    files,
    errors: error ? [inlineText(error, 90)] : [],
    status,
  }
}

function groupActivities(activities: SemanticActivity[]) {
  const groups: ActivityGroup[] = []
  for (const activity of activities) {
    const previous = groups[groups.length - 1]
    if (previous && previous.items[0].groupKey === activity.groupKey) {
      previous.items.push(activity)
      continue
    }
    groups.push({ key: activity.key, kind: activity.kind, items: [activity] })
  }
  return groups
}

function groupThoughtItem(group: ActivityGroup, outcome: AgentExecutionOutcome): XThoughtItem {
  const targets = uniqueStrings(group.items.flatMap((item) => item.targets))
  const files = uniqueStrings(group.items.flatMap((item) => item.files))
  const errors = uniqueStrings(group.items.flatMap((item) => item.errors))
  const status = groupStatus(group.items, outcome)
  const description = [
    summarizeTargets(targets),
    status === 'loading' ? '进行中' : '',
    errors.length > 0 ? `${errors.length} 项未成功${errors.length === 1 ? `：${errors[0]}` : ''}` : '',
    status === 'stopped' ? '已停止' : '',
  ]
    .filter(Boolean)
    .join(' · ')

  return {
    key: group.key,
    title: groupTitle(group, files),
    description,
    status,
  }
}

function groupTitle(group: ActivityGroup, files: string[]) {
  const count = group.items.length
  if (group.kind === 'research') return count === 1 ? group.items[0].title : `检索 ${count} 项资料`
  if (group.kind === 'inspect') return count === 1 ? group.items[0].title : `检查 ${count} 项文件与内容`
  if (group.kind === 'modify') {
    if (files.length > 1) return `更新 ${files.length} 个文件`
    return count === 1 ? group.items[0].title : '更新文件'
  }
  if (group.kind === 'command') return count === 1 ? group.items[0].title : `执行 ${count} 条命令`
  if (group.kind === 'task') return count === 1 ? group.items[0].title : `处理 ${count} 个子任务`
  if (group.kind === 'plan') return '更新执行计划'
  if (group.kind === 'setup') return count === 1 ? group.items[0].title : `加载 ${count} 项能力`
  if (group.kind === 'marvo') return count === 1 ? group.items[0].title : `${group.items[0].title} ${count} 次`
  if (group.kind === 'repository') return count === 1 ? group.items[0].title : '处理代码仓库'
  if (count === 1) return group.items[0].title
  return `${group.items[0].title} ${count} 次`
}

function groupStatus(items: SemanticActivity[], outcome: AgentExecutionOutcome): XThoughtStatus {
  if (items.some((item) => item.status === 'loading')) return 'loading'
  if (items.some((item) => item.status === 'error')) return outcome.failed ? 'error' : 'warning'
  if (items.some((item) => item.status === 'stopped' || item.status === 'default')) {
    return outcome.aborted ? 'stopped' : 'default'
  }
  return 'success'
}

function toolStatus(part: MessagePart, streaming: boolean): XThoughtStatus {
  if (part.type === 'tool_result') return 'success'
  const status = part.state?.status
  if (status === 'completed') return 'success'
  if (status === 'error') return 'error'
  if (status === 'running' || status === 'pending') return streaming ? 'loading' : 'default'
  return 'default'
}

function toolKind(tool: string): ActivityKind {
  if (tool.startsWith('marvo_')) return 'marvo'
  if (['webfetch', 'websearch'].includes(tool)) return 'research'
  if (['read', 'list', 'glob', 'grep', 'lsp'].includes(tool)) return 'inspect'
  if (['edit', 'write', 'patch', 'apply_patch', 'multiedit'].includes(tool)) return 'modify'
  if (['bash', 'shell'].includes(tool)) return 'command'
  if (['task', 'task_status'].includes(tool)) return 'task'
  if (['todowrite', 'todoread', 'plan_enter', 'plan_exit'].includes(tool)) return 'plan'
  if (tool === 'skill') return 'setup'
  if (['repo_clone', 'repo_overview'].includes(tool)) return 'repository'
  return 'unknown'
}

function toolActionTitle(part: MessagePart, tool: string) {
  const marvoTitle = marvoToolTitle(part, tool)
  if (marvoTitle) return marvoTitle
  const titles: Record<string, string> = {
    bash: '执行命令',
    shell: '执行命令',
    edit: '更新文件',
    write: '更新文件',
    patch: '更新文件',
    apply_patch: '更新文件',
    multiedit: '更新文件',
    read: '读取文件',
    list: '查看目录',
    grep: '搜索文件内容',
    glob: '查找文件',
    lsp: '检查代码',
    webfetch: '读取网页',
    websearch: '搜索网页',
    task: '处理子任务',
    task_status: '检查子任务',
    todowrite: '更新执行计划',
    todoread: '查看执行计划',
    plan_enter: '制定执行计划',
    plan_exit: '完成执行计划',
    skill: '加载能力',
    repo_clone: '准备代码仓库',
    repo_overview: '分析代码仓库',
    invalid: '修正操作参数',
  }
  if (titles[tool]) return titles[tool]

  const stateTitle = cleanToolTitle(stringValue(record(part.state).title))
  if (stateTitle) return inlineText(stateTitle, 60)
  return tool ? `执行 ${humanizeToolName(tool)}` : '执行操作'
}

function toolFiles(part: MessagePart, tool: string) {
  const state = record(part.state)
  const input = record(state.input || part.input)
  const metadata = record(state.metadata || part.metadata)
  const display = record(metadata.display)
  const files: string[] = []

  if (['read', 'edit', 'write', 'patch', 'apply_patch', 'multiedit'].includes(tool)) {
    if (typeof input.filePath === 'string') files.push(compactPath(input.filePath))
    if (typeof input.path === 'string') files.push(compactPath(input.path))
  }
  if (typeof display.path === 'string') files.push(compactPath(display.path))
  return uniqueStrings(files)
}

function toolTargets(part: MessagePart, tool: string) {
  const state = record(part.state)
  const input = record(state.input || part.input)
  const metadata = record(state.metadata || part.metadata)
  const targets: string[] = []

  if (marvoToolTitle(part, tool)) {
    return []
  } else if (tool === 'bash' || tool === 'shell') {
    const title = stringValue(state.title)
    if (title) targets.push(cleanToolTitle(title))
    else if (typeof input.command === 'string') targets.push(inlineText(input.command, 140))
  } else if (tool === 'list' && typeof input.path === 'string') {
    targets.push(compactPath(input.path))
  } else if (['grep', 'glob'].includes(tool) && typeof input.pattern === 'string') {
    targets.push(inlineText(input.pattern, 140))
  } else if (tool === 'webfetch' && typeof input.url === 'string') {
    targets.push(inlineText(input.url, 140))
  } else if (tool === 'websearch' && typeof input.query === 'string') {
    targets.push(inlineText(input.query, 140))
  } else if (['task', 'task_status'].includes(tool) && typeof input.description === 'string') {
    targets.push(inlineText(input.description, 140))
  } else if (tool === 'todowrite' && Array.isArray(input.todos)) {
    targets.push(`${input.todos.length} 项`)
  } else if (tool === 'skill' && typeof input.name === 'string') {
    targets.push(inlineText(input.name, 140))
  } else if (['repo_clone', 'repo_overview'].includes(tool)) {
    const repository = stringValue(input.repository || metadata.repository)
    const path = stringValue(input.path || metadata.path)
    if (repository) targets.push(inlineText(repository, 140))
    else if (path) targets.push(compactPath(path))
  }

  if (targets.length === 0 && typeof metadata.subtitle === 'string') {
    targets.push(inlineText(metadata.subtitle, 140))
  }
  if (targets.length === 0 && typeof input.description === 'string') {
    targets.push(inlineText(input.description, 140))
  }
  return uniqueStrings(targets.filter(Boolean))
}

function marvoToolTitle(part: MessagePart, tool: string) {
  const input = record(record(part.state).input || part.input)
  const action = stringValue(input.action)
  if (tool === 'marvo_activity') return input.kind === 'choice' ? '发布选择活动' : '发布活动'
  if (tool === 'marvo_memories') {
    if (action === 'list') return '查看记忆'
    if (action === 'add') return '添加记忆'
    if (action === 'update') return '更新记忆'
    if (action === 'remove') return '删除记忆'
  }
  if (tool === 'marvo_space') return action === 'set_brand' ? '更新空间名称' : '查看空间信息'
  if (tool === 'marvo_agent_settings') return action === 'update' ? '更新智能体设置' : '查看智能体设置'
  if (tool === 'marvo_devices') {
    if (action === 'approve') return '批准设备'
    if (action === 'reject') return '拒绝设备'
    if (action === 'rename') return '重命名设备'
    if (action === 'revoke') return '撤销设备'
    return '查看设备'
  }
  return ''
}

function summarizeTargets(targets: string[]) {
  if (targets.length <= 2) return targets.join('、')
  return `${targets.slice(0, 2).join('、')}等 ${targets.length} 项`
}

export function formatAgentExecutionDuration(milliseconds: number) {
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return ''
  if (milliseconds < 1000) return `${Math.max(1, Math.round(milliseconds))} ms`
  if (milliseconds < 60_000) return `${(milliseconds / 1000).toFixed(milliseconds < 10_000 ? 1 : 0)} 秒`
  const minutes = Math.floor(milliseconds / 60_000)
  const seconds = Math.round((milliseconds % 60_000) / 1000)
  return seconds > 0 ? `${minutes} 分 ${seconds} 秒` : `${minutes} 分钟`
}

function cleanToolTitle(value: string) {
  return compactPath(value.replace(/^Exa Web Search:\s*/i, ''))
}

function compactPath(value: string) {
  return (
    value.replace(/^\/?workspace\/?/, '').replace(/^\/home\/[^/]+\/\.local\/share\/opencode\//, 'opencode/') || value
  )
}

function humanizeToolName(value: string) {
  return (
    value
      .replace(/^mcp[_:-]?/i, '')
      .replace(/[_:-]+/g, ' ')
      .trim() || '操作'
  )
}

function apiErrorText(value: unknown): string {
  if (typeof value === 'string') return value
  const error = record(value)
  const data = record(error.data)
  return (
    stringValue(error.message || data.message || data.error) || (Object.keys(error).length ? safeStringify(error) : '')
  )
}

function record(value: unknown): Record<string, unknown> {
  if (typeof value === 'object' && value !== null) return value as Record<string, unknown>
  return {}
}

function stringArray(value: unknown) {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []
}

function stringValue(value: unknown) {
  return typeof value === 'string' ? value : ''
}

function safeStringify(value: unknown) {
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

function inlineText(value: string, maximum: number) {
  const text = value.replace(/\s+/g, ' ').trim()
  return text.length > maximum ? `${text.slice(0, maximum - 1)}…` : text
}

function uniqueStrings(values: string[]) {
  return [...new Set(values.filter(Boolean))]
}

function toolName(part: MessagePart) {
  return stringValue(part.tool || part.name).toLowerCase()
}
