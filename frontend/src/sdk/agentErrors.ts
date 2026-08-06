function record(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : {}
}

function parseJSON(value: string) {
  const text = value.trim()
  if ((!text.startsWith('{') || !text.endsWith('}')) && (!text.startsWith('[') || !text.endsWith(']'))) return
  try {
    return JSON.parse(text) as unknown
  } catch {
    return undefined
  }
}

export function unwrapAgentError(value: unknown, depth = 0): string {
  if (depth > 6 || value === undefined || value === null) return ''
  if (typeof value === 'string') {
    const parsed = parseJSON(value)
    return parsed === undefined ? value : unwrapAgentError(parsed, depth + 1) || value
  }
  if (typeof value !== 'object') return String(value)
  const data = record(value)
  for (const key of ['message', 'error', 'data', 'cause', 'body', 'response']) {
    const text = unwrapAgentError(data[key], depth + 1)
    if (text) return text
  }
  return ''
}

function agentErrorName(error: unknown, depth = 0): string {
  if (depth > 5 || typeof error !== 'object' || error === null) return ''
  const data = record(error)
  if (typeof data.name === 'string') return data.name
  if (typeof data._tag === 'string') return data._tag
  for (const key of ['error', 'data', 'cause']) {
    const nested = agentErrorName(data[key], depth + 1)
    if (nested) return nested
  }
  return ''
}

export function isAbortedAgentError(error: unknown) {
  if (agentErrorName(error) === 'MessageAbortedError') return true
  const text = unwrapAgentError(error)
    .replace(/^error:\s*/i, '')
    .trim()
    .toLowerCase()
  return ['aborted', 'interrupted', 'cancelled', 'canceled', '已停止', '已中断'].includes(text)
}

export function formatAgentError(error: unknown): string {
  const name = agentErrorName(error)
  const raw = unwrapAgentError(error)
    .replace(/^error:\s*/i, '')
    .trim()
  const lower = raw.toLowerCase()

  if (name === 'MessageOutputLengthError') return '回答达到长度上限，请缩小问题范围后重试'
  if (name === 'ContextOverflowError') return '当前对话内容过多，请新建对话后继续'
  if (name === 'ContentFilterError') return '请求或回答被模型服务拒绝，请调整表达后重试'
  if (name === 'StructuredOutputError') return '智能体返回的结果不完整，请重试'
  if (name === 'ProviderAuthError') return '模型服务认证失败，请检查服务配置'
  if (isAbortedAgentError(error)) return '已中断'
  if (!raw) return '智能体未能完成这次操作'
  if (/context.{0,20}(length|window|limit)|maximum context|too many tokens/.test(lower)) {
    return '当前对话内容过多，请新建对话后继续'
  }
  if (/quota|insufficient.{0,20}(credit|balance)/.test(lower)) return '当前服务额度不足，请检查配置后重试'
  if (/rate.?limit|too many requests|429/.test(lower)) return '请求较多，请稍后重试'
  if (/unauthori[sz]ed|authentication|invalid api key|401|403/.test(lower)) {
    return '模型服务认证失败，请检查服务配置'
  }
  if (/model.{0,20}(not found|unavailable)|unknown model/.test(lower)) {
    return '当前模型暂不可用，请在设置中选择其他模型'
  }
  if (/connection refused|failed to fetch|network error|econnrefused|service unavailable/.test(lower)) {
    return '智能体服务暂时不可用'
  }
  return raw.length > 240 ? `${raw.slice(0, 239)}…` : raw
}

export function conciseAgentErrorDetail(error: unknown) {
  const raw = unwrapAgentError(error)
    .replace(/^error:\s*/i, '')
    .trim()
  if (!raw || raw === formatAgentError(error)) return ''
  return raw.length > 500 ? `${raw.slice(0, 499)}…` : raw
}
