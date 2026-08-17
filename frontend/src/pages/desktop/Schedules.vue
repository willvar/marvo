<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { Checkbox } from '@ark-ui/vue/checkbox'
import { Dialog } from '@ark-ui/vue/dialog'
import { Field } from '@ark-ui/vue/field'
import { SegmentGroup } from '@ark-ui/vue/segment-group'
import {
  CalendarOutlined,
  CheckOutlined,
  ClockCircleOutlined,
  CloseOutlined,
  DeleteOutlined,
  EditOutlined,
  HistoryOutlined,
  LoadingOutlined,
  MessageOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  SaveOutlined,
  StopOutlined,
} from '@ant-design/icons-vue'
import { useRouter } from 'vue-router'
import WorkspaceActivityNav from '../../components/WorkspaceActivityNav.vue'
import { XButton, XFullscreenTextarea, XWelcome } from '../../components/x'
import { useRetainedDialog } from '../../composables/useRetainedDialog'
import {
  on as onWorkspaceEvent,
  useAppBackHandler,
  workspaceRoute,
  type AutomaticTask,
  type ScheduleDefinition,
  type ScheduleInput,
  type ScheduleRun,
} from '../../sdk'
import { useAgentStore } from '../../stores/agent'
import { useSchedulesStore } from '../../stores/schedules'

defineEmits(['noteMutationBlocked', 'noteSaveStatus'])

type EditorMode = 'at' | 'every' | 'daily' | 'weekly' | 'adaptive' | 'preserved'

interface EditorPayload {
  task?: AutomaticTask
}

const schedules = useSchedulesStore()
const agent = useAgentStore()
const router = useRouter()
const editorDialog = useRetainedDialog<EditorPayload>()
const deleteDialog = useRetainedDialog<AutomaticTask>()
const actionError = ref('')
const saving = ref(false)
const deleting = ref(false)
const actionID = ref('')
const historyID = ref('')
const historyLoadingID = ref('')
const formError = ref('')
const deleteError = ref('')

const editorTask = computed(() => editorDialog.payload.value?.task || null)
const calendarTimezone = computed(() =>
  editorTask.value?.schedule.kind === 'cron' && editorTask.value.schedule.timezone
    ? editorTask.value.schedule.timezone
    : browserTimezone(),
)
const calendarTimezoneText = computed(() => `按${timezoneLabel(calendarTimezone.value)}执行。`)
const weekdays = [
  { value: 1, label: '一' },
  { value: 2, label: '二' },
  { value: 3, label: '三' },
  { value: 4, label: '四' },
  { value: 5, label: '五' },
  { value: 6, label: '六' },
  { value: 0, label: '日' },
]

const draft = reactive({
  name: '',
  instruction: '',
  mode: 'every' as EditorMode,
  at: '',
  everyValue: '24',
  everyUnit: 'hour' as 'minute' | 'hour' | 'day',
  calendarTime: '09:00',
  weeklyDays: [1] as number[],
  minimumMinutes: '30',
  defaultMinutes: '60',
  maximumMinutes: '1440',
  preserved: null as ScheduleDefinition | null,
})

onMounted(() => void retry())
const stopScheduleEvents = onWorkspaceEvent('schedules_changed', () => {
  if (historyID.value) void schedules.loadRuns(historyID.value, true).catch(() => undefined)
})
onBeforeUnmount(stopScheduleEvents)

async function retry() {
  actionError.value = ''
  try {
    await schedules.load(true)
  } catch {
    // The store owns the persistent list error.
  }
}

function beginCreate() {
  resetDraft()
  editorDialog.show({})
}

function beginEdit(task: AutomaticTask) {
  resetDraft(task)
  editorDialog.show({ task })
}

function resetDraft(task?: AutomaticTask) {
  Object.assign(draft, {
    name: task?.name || '',
    instruction: task?.instruction || '',
    mode: 'every' as EditorMode,
    at: toLocalDateTime(new Date(Date.now() + 60 * 60 * 1000).toISOString()),
    everyValue: '24',
    everyUnit: 'hour' as const,
    calendarTime: '09:00',
    weeklyDays: [1],
    minimumMinutes: '30',
    defaultMinutes: '60',
    maximumMinutes: '1440',
    preserved: null,
  })
  if (!task) {
    formError.value = ''
    return
  }
  const definition = task.schedule
  if (definition.kind === 'at' && definition.spec.at) {
    draft.mode = 'at'
    draft.at = toLocalDateTime(definition.spec.at)
  } else if (definition.kind === 'every') {
    draft.mode = 'every'
    const normalized = intervalDraft(definition.spec.every_seconds || 0)
    draft.everyValue = normalized.value
    draft.everyUnit = normalized.unit
  } else if (definition.kind === 'adaptive') {
    draft.mode = 'adaptive'
    draft.minimumMinutes = secondsToMinutes(definition.spec.minimum_seconds)
    draft.defaultMinutes = secondsToMinutes(definition.spec.default_seconds)
    draft.maximumMinutes = secondsToMinutes(definition.spec.maximum_seconds)
  } else if (definition.kind === 'cron') {
    const parsed = parseCalendarSchedule(definition)
    if (parsed) {
      draft.mode = parsed.mode
      draft.calendarTime = parsed.time
      draft.weeklyDays = parsed.days
    } else {
      draft.mode = 'preserved'
      draft.preserved = { ...definition, spec: { ...definition.spec } }
    }
  }
  formError.value = ''
}

function updateEditorOpen(open: boolean) {
  editorDialog.updateOpen(open, !saving.value)
}

function completeEditorClose() {
  if (!editorDialog.clearAfterExit()) return
  formError.value = ''
  resetDraft()
}

function requestDelete(task: AutomaticTask) {
  deleteError.value = ''
  deleteDialog.show(task)
}

function updateDeleteOpen(open: boolean) {
  deleteDialog.updateOpen(open, !deleting.value)
}

async function saveTask() {
  if (saving.value) return
  const input = buildInput()
  if (!input) return
  saving.value = true
  formError.value = ''
  try {
    const task = editorTask.value
    if (task) await schedules.update(task.id, task.revision, input)
    else await schedules.create(input)
    editorDialog.close()
  } catch (cause) {
    formError.value = errorMessage(cause, '保存自动任务失败')
  } finally {
    saving.value = false
  }
}

function buildInput(): ScheduleInput | null {
  const name = draft.name.trim()
  const instruction = draft.instruction.trim()
  if (!name || Array.from(name).length > 200) {
    formError.value = '请输入不超过 200 个字符的任务名称'
    return null
  }
  if (!instruction || new TextEncoder().encode(instruction).byteLength > 64 * 1024) {
    formError.value = '请输入有效的任务内容'
    return null
  }
  const schedule = buildDefinition()
  if (!schedule) return null
  return { name, instruction, schedule }
}

function buildDefinition(): ScheduleDefinition | null {
  if (draft.mode === 'preserved' && draft.preserved) return { ...draft.preserved, spec: { ...draft.preserved.spec } }
  if (draft.mode === 'at') {
    const value = new Date(draft.at)
    if (!draft.at || Number.isNaN(value.getTime()) || value.getTime() <= Date.now()) {
      formError.value = '单次执行时间必须晚于现在'
      return null
    }
    return { kind: 'at', spec: { at: value.toISOString() } }
  }
  if (draft.mode === 'every') {
    const value = positiveNumber(draft.everyValue)
    const unit = draft.everyUnit === 'minute' ? 60 : draft.everyUnit === 'hour' ? 3600 : 86400
    const seconds = value * unit
    if (!Number.isInteger(seconds) || seconds < 60 || seconds > 315_360_000) {
      formError.value = '请输入 1 分钟到 10 年之间的固定间隔'
      return null
    }
    const anchor = editorTask.value?.schedule.kind === 'every' ? editorTask.value.schedule.spec.anchor : undefined
    return { kind: 'every', spec: { every_seconds: seconds, ...(anchor ? { anchor } : {}) } }
  }
  if (draft.mode === 'daily' || draft.mode === 'weekly') {
    const [hour, minute] = draft.calendarTime.split(':').map(Number)
    if (!Number.isInteger(hour) || !Number.isInteger(minute) || hour < 0 || hour > 23 || minute < 0 || minute > 59) {
      formError.value = '请选择有效的执行时间'
      return null
    }
    if (draft.mode === 'weekly' && draft.weeklyDays.length === 0) {
      formError.value = '请至少选择一个星期'
      return null
    }
    const day = draft.mode === 'daily' ? '*' : [...draft.weeklyDays].sort((a, b) => a - b).join(',')
    return {
      kind: 'cron',
      spec: { expression: `${minute} ${hour} * * ${day}` },
      timezone: editorTask.value?.schedule.kind === 'cron' ? editorTask.value.schedule.timezone : browserTimezone(),
    }
  }
  const minimum = positiveNumber(draft.minimumMinutes)
  const fallback = positiveNumber(draft.defaultMinutes)
  const maximum = positiveNumber(draft.maximumMinutes)
  if (!Number.isInteger(minimum) || !Number.isInteger(fallback) || !Number.isInteger(maximum)) {
    formError.value = '智能跟进时间需要填写完整的分钟数'
    return null
  }
  if (minimum < 1 || fallback < minimum || maximum < fallback || maximum * 60 > 315_360_000) {
    formError.value = '请确保最短时间不大于常用时间，常用时间不大于最长时间'
    return null
  }
  return {
    kind: 'adaptive',
    spec: {
      minimum_seconds: minimum * 60,
      default_seconds: fallback * 60,
      maximum_seconds: maximum * 60,
    },
  }
}

function positiveNumber(value: string) {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : Number.NaN
}

function toggleWeekday(day: number, checked: boolean | 'indeterminate') {
  if (checked === true) {
    if (!draft.weeklyDays.includes(day)) draft.weeklyDays = [...draft.weeklyDays, day]
  } else {
    draft.weeklyDays = draft.weeklyDays.filter((candidate) => candidate !== day)
  }
}

async function togglePaused(task: AutomaticTask) {
  await runAction(task, async () => {
    if (task.status === 'paused') await schedules.resume(task)
    else await schedules.pause(task)
  })
}

async function runNow(task: AutomaticTask) {
  await runAction(task, () => schedules.runNow(task))
}

async function stopRun(task: AutomaticTask) {
  await runAction(task, () => schedules.stop(task))
}

async function runAction(task: AutomaticTask, action: () => Promise<unknown>) {
  if (actionID.value) return
  actionID.value = task.id
  actionError.value = ''
  try {
    await action()
  } catch (cause) {
    actionError.value = errorMessage(cause, '自动任务操作失败')
    if ((cause as { status?: number })?.status === 409) await schedules.load(true).catch(() => undefined)
  } finally {
    actionID.value = ''
  }
}

async function confirmDelete() {
  const task = deleteDialog.payload.value
  if (!task || deleting.value) return
  deleting.value = true
  deleteError.value = ''
  try {
    await schedules.remove(task)
    deleteDialog.close()
  } catch (cause) {
    deleteError.value = errorMessage(cause, '删除自动任务失败')
  } finally {
    deleting.value = false
  }
}

async function toggleHistory(task: AutomaticTask) {
  if (historyID.value === task.id) {
    historyID.value = ''
    return
  }
  historyID.value = task.id
  historyLoadingID.value = task.id
  actionError.value = ''
  try {
    await schedules.loadRuns(task.id, true)
  } catch (cause) {
    actionError.value = errorMessage(cause, '运行记录加载失败')
  } finally {
    historyLoadingID.value = ''
  }
}

async function openConversation(task: AutomaticTask) {
  if (!task.session_id) return
  await runAction(task, async () => {
    agent.connect()
    await agent.loadSessions()
    if (!agent.allSessions.some((session) => session.id === task.session_id)) throw new Error('对应的智能体对话已删除')
    await agent.selectSession(task.session_id!)
    await router.push(workspaceRoute('/agent'))
  })
}

function taskScheduleText(task: AutomaticTask) {
  const definition = task.schedule
  if (definition.kind === 'at') return definition.spec.at ? `单次 · ${formatTime(definition.spec.at)}` : '单次执行'
  if (definition.kind === 'every') return `每 ${formatDuration(definition.spec.every_seconds || 0)}`
  if (definition.kind === 'adaptive') {
    return `智能跟进 · 通常 ${formatDuration(definition.spec.default_seconds || 0)}后再次检查`
  }
  const parsed = parseCalendarSchedule(definition)
  if (!parsed) return '按设定的日历时间执行'
  const timezone =
    definition.timezone && definition.timezone !== browserTimezone() ? ` · ${timezoneLabel(definition.timezone)}` : ''
  if (parsed.mode === 'daily') return `每天 ${parsed.time}${timezone}`
  const labels = parsed.days
    .map((day) => weekdays.find((weekday) => weekday.value === day)?.label)
    .filter(Boolean)
    .join('、')
  return `每周${labels} ${parsed.time}${timezone}`
}

function taskTimingText(task: AutomaticTask) {
  if (task.active_run?.status === 'running') return '智能体正在执行'
  if (task.active_run?.status === 'queued') return '等待执行'
  if (task.active_run?.status === 'waiting_retry') {
    return task.active_run.next_attempt_at ? `${formatTime(task.active_run.next_attempt_at)} 再试一次` : '稍后再试'
  }
  if (task.status === 'paused') {
    if (task.paused_reason === 'failure') return '执行失败，已暂停后续执行'
    return task.paused_reason || '已暂停后续执行'
  }
  if (task.status === 'completed') return '任务已结束'
  return task.next_run_at ? `下次 ${formatTime(task.next_run_at)}` : '等待安排下次执行'
}

function statusLabel(task: AutomaticTask) {
  if (task.active_run?.status === 'running') return '执行中'
  if (task.active_run?.status === 'queued') return '等待中'
  if (task.active_run?.status === 'waiting_retry') return '重试中'
  if (task.status === 'paused') return '已暂停'
  if (task.status === 'completed') return '已结束'
  return '已启用'
}

function runStatusLabel(run: ScheduleRun) {
  return (
    {
      queued: '等待执行',
      running: '执行中',
      waiting_retry: '等待重试',
      succeeded: '已完成',
      failed: '失败',
      timed_out: '超时',
      cancelled: '已停止',
    } as Record<string, string>
  )[run.status]
}

function parseCalendarSchedule(definition: ScheduleDefinition) {
  const match = /^(\d{1,2})\s+(\d{1,2})\s+\*\s+\*\s+(\*|[0-6](?:,[0-6])*)$/.exec(
    definition.spec.expression?.trim() || '',
  )
  if (!match) return null
  const minute = Number(match[1])
  const hour = Number(match[2])
  if (minute > 59 || hour > 23) return null
  const time = `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
  if (match[3] === '*') return { mode: 'daily' as const, time, days: [] as number[] }
  return { mode: 'weekly' as const, time, days: match[3].split(',').map(Number) }
}

function intervalDraft(seconds: number) {
  if (seconds > 0 && seconds % 86400 === 0) return { value: String(seconds / 86400), unit: 'day' as const }
  if (seconds > 0 && seconds % 3600 === 0) return { value: String(seconds / 3600), unit: 'hour' as const }
  return { value: String(Math.max(1, seconds / 60)), unit: 'minute' as const }
}

function secondsToMinutes(seconds?: number) {
  return String(Math.max(1, Math.round((seconds || 60) / 60)))
}

function formatDuration(seconds: number) {
  if (seconds >= 86400 && seconds % 86400 === 0) return `${seconds / 86400} 天`
  if (seconds >= 3600 && seconds % 3600 === 0) return `${seconds / 3600} 小时`
  return `${Math.max(1, Math.round(seconds / 60))} 分钟`
}

function formatTime(value?: string | null) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (part: number) => String(part).padStart(2, '0')
  return `${date.getFullYear()}.${pad(date.getMonth() + 1)}.${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function toLocalDateTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (part: number) => String(part).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function browserTimezone() {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
}

function timezoneLabel(timezone: string) {
  try {
    const parts = new Intl.DateTimeFormat('zh-CN', { timeZone: timezone, timeZoneName: 'longGeneric' }).formatToParts(
      new Date(),
    )
    return parts.find((part) => part.type === 'timeZoneName')?.value || timezone
  } catch {
    return timezone
  }
}

function errorMessage(cause: unknown, fallback: string) {
  return cause instanceof Error && cause.message ? cause.message : fallback
}

useAppBackHandler(() => {
  if (deleteDialog.open.value) {
    if (!deleting.value) deleteDialog.close()
    return true
  }
  if (editorDialog.open.value) {
    if (!saving.value) editorDialog.close()
    return true
  }
  return false
}, 70)
</script>

<template>
  <main class="schedules-page">
    <div class="schedules-content">
      <header class="schedules-heading">
        <div>
          <h1>自动任务</h1>
          <p>让智能体按时间持续研究、检查和推进工作。</p>
        </div>
        <div class="schedules-heading-actions">
          <WorkspaceActivityNav />
          <XButton variant="primary" @click="beginCreate"><PlusOutlined aria-hidden="true" />新建任务</XButton>
        </div>
      </header>

      <p v-if="actionError" class="schedules-error" role="alert">{{ actionError }}</p>

      <div v-if="schedules.loading && !schedules.loaded" class="schedules-loading" role="status">
        <span class="page-loading-spinner" />
        <span>正在加载自动任务…</span>
      </div>

      <section v-else-if="schedules.error && schedules.tasks.length === 0" class="schedules-load-error" role="alert">
        <strong>自动任务暂时无法加载</strong>
        <p>{{ schedules.error }}</p>
        <XButton @click="retry"><ReloadOutlined aria-hidden="true" />重试</XButton>
      </section>

      <XWelcome
        v-else-if="schedules.tasks.length === 0"
        class="schedules-empty"
        :icon="ClockCircleOutlined"
        title="还没有自动任务"
        description="你可以在这里创建，也可以直接让智能体为你安排。"
        variant="filled"
      >
        <template #extra>
          <XButton variant="primary" @click="beginCreate"><PlusOutlined aria-hidden="true" />新建任务</XButton>
        </template>
      </XWelcome>

      <div v-else class="schedules-list">
        <article v-for="task in schedules.tasks" :key="task.id" class="schedule-card">
          <header class="schedule-card-heading">
            <span class="schedule-card-icon" aria-hidden="true"><ClockCircleOutlined /></span>
            <div class="schedule-card-title">
              <div>
                <h2>{{ task.name }}</h2>
                <span :class="['schedule-status', `is-${task.active_run?.status || task.status}`]">
                  {{ statusLabel(task) }}
                </span>
              </div>
              <p>{{ taskScheduleText(task) }}</p>
            </div>
            <XButton size="small" variant="ghost" :disabled="!!actionID" @click="beginEdit(task)">
              <EditOutlined aria-hidden="true" />编辑
            </XButton>
          </header>

          <p class="schedule-instruction">{{ task.instruction }}</p>

          <div class="schedule-meta">
            <span><CalendarOutlined aria-hidden="true" />{{ taskTimingText(task) }}</span>
            <span v-if="task.last_run_at">上次 {{ formatTime(task.last_run_at) }}</span>
          </div>
          <p v-if="task.last_error" class="schedule-last-error" role="status">{{ task.last_error }}</p>

          <footer class="schedule-actions">
            <XButton v-if="task.active_run" size="small" variant="danger" :disabled="!!actionID" @click="stopRun(task)">
              <LoadingOutlined v-if="actionID === task.id" class="schedule-spin" aria-hidden="true" />
              <StopOutlined v-else aria-hidden="true" />停止本轮
            </XButton>
            <XButton v-else size="small" :disabled="!!actionID || !!task.active_run" @click="runNow(task)">
              <LoadingOutlined v-if="actionID === task.id" class="schedule-spin" aria-hidden="true" />
              <PlayCircleOutlined v-else aria-hidden="true" />立即执行
            </XButton>
            <XButton v-if="task.status !== 'completed'" size="small" :disabled="!!actionID" @click="togglePaused(task)">
              <PlayCircleOutlined v-if="task.status === 'paused'" aria-hidden="true" />
              <PauseCircleOutlined v-else aria-hidden="true" />{{ task.status === 'paused' ? '继续任务' : '暂停任务' }}
            </XButton>
            <XButton v-if="task.session_id" size="small" :disabled="!!actionID" @click="openConversation(task)">
              <MessageOutlined aria-hidden="true" />打开对话
            </XButton>
            <XButton size="small" :disabled="historyLoadingID === task.id" @click="toggleHistory(task)">
              <LoadingOutlined v-if="historyLoadingID === task.id" class="schedule-spin" aria-hidden="true" />
              <HistoryOutlined v-else aria-hidden="true" />{{ historyID === task.id ? '收起记录' : '运行记录' }}
            </XButton>
            <XButton
              size="small"
              variant="danger"
              :disabled="!!actionID || !!task.active_run"
              @click="requestDelete(task)"
            >
              <DeleteOutlined aria-hidden="true" />删除
            </XButton>
          </footer>

          <div v-if="historyID === task.id" class="schedule-history">
            <p v-if="!schedules.runs[task.id]?.length" class="schedule-history-empty">还没有运行记录。</p>
            <ol v-else>
              <li v-for="run in schedules.runs[task.id]" :key="run.id">
                <span :class="['schedule-run-status', `is-${run.status}`]">{{ runStatusLabel(run) }}</span>
                <span>{{
                  run.trigger === 'manual'
                    ? `手动执行 · ${formatTime(run.scheduled_for)}`
                    : formatTime(run.scheduled_for)
                }}</span>
                <span v-if="run.attempt > 1">尝试 {{ run.attempt }} 次</span>
                <p v-if="run.error">{{ run.error }}</p>
              </li>
            </ol>
          </div>
        </article>
      </div>
    </div>
  </main>

  <Dialog.Root
    :open="editorDialog.open.value"
    lazy-mount
    unmount-on-exit
    :close-on-escape="!saving"
    :close-on-interact-outside="!saving"
    @exit-complete="completeEditorClose"
    @update:open="updateEditorOpen"
  >
    <Teleport to="body">
      <Dialog.Backdrop class="dialog-backdrop" />
      <Dialog.Positioner class="dialog-positioner">
        <Dialog.Content class="dialog-panel schedule-editor-dialog">
          <div class="dialog-header">
            <div>
              <Dialog.Title>{{ editorTask ? '编辑自动任务' : '新建自动任务' }}</Dialog.Title>
              <Dialog.Description>智能体每次都会在同一对话中继续这项工作。</Dialog.Description>
            </div>
            <Dialog.CloseTrigger class="dialog-close" :disabled="saving" aria-label="关闭">
              <CloseOutlined aria-hidden="true" />
            </Dialog.CloseTrigger>
          </div>
          <div class="dialog-body schedule-editor-body">
            <Field.Root class="schedule-field" required>
              <Field.Label>任务名称</Field.Label>
              <Field.Input
                v-model="draft.name"
                maxlength="200"
                autocomplete="off"
                placeholder="例如：跟进量子计算新进展"
              />
            </Field.Root>

            <Field.Root class="schedule-field" required>
              <Field.Label>任务内容</Field.Label>
              <XFullscreenTextarea
                v-model="draft.instruction"
                title="编辑任务内容"
                maxlength="65536"
                placeholder="说明每次需要智能体检查、研究或推进什么，以及何时应该联系你。"
              />
            </Field.Root>

            <Field.Root class="schedule-field" required>
              <Field.Label>执行安排</Field.Label>
              <div class="schedule-mode-scroller">
                <SegmentGroup.Root v-model="draft.mode" class="schedule-mode-group">
                  <SegmentGroup.Indicator class="schedule-mode-indicator" />
                  <SegmentGroup.Item
                    v-for="option in [
                      { value: 'at', label: '单次' },
                      { value: 'every', label: '间隔' },
                      { value: 'daily', label: '每天' },
                      { value: 'weekly', label: '每周' },
                      { value: 'adaptive', label: '智能跟进' },
                      ...(draft.preserved ? [{ value: 'preserved', label: '原有安排' }] : []),
                    ]"
                    :key="option.value"
                    :value="option.value"
                    class="schedule-mode-item"
                  >
                    <SegmentGroup.ItemText>{{ option.label }}</SegmentGroup.ItemText>
                    <SegmentGroup.ItemHiddenInput />
                  </SegmentGroup.Item>
                </SegmentGroup.Root>
              </div>
            </Field.Root>

            <Field.Root v-if="draft.mode === 'at'" class="schedule-field" required>
              <Field.Label>执行时间</Field.Label>
              <Field.Input v-model="draft.at" type="datetime-local" />
            </Field.Root>

            <div v-else-if="draft.mode === 'every'" class="schedule-inline-fields">
              <Field.Root class="schedule-field" required>
                <Field.Label>每隔</Field.Label>
                <Field.Input v-model="draft.everyValue" type="number" min="1" step="1" inputmode="numeric" />
              </Field.Root>
              <SegmentGroup.Root v-model="draft.everyUnit" class="schedule-unit-group" aria-label="间隔单位">
                <SegmentGroup.Indicator class="schedule-mode-indicator" />
                <SegmentGroup.Item
                  v-for="unit in [
                    { value: 'minute', label: '分钟' },
                    { value: 'hour', label: '小时' },
                    { value: 'day', label: '天' },
                  ]"
                  :key="unit.value"
                  :value="unit.value"
                  class="schedule-mode-item"
                >
                  <SegmentGroup.ItemText>{{ unit.label }}</SegmentGroup.ItemText>
                  <SegmentGroup.ItemHiddenInput />
                </SegmentGroup.Item>
              </SegmentGroup.Root>
            </div>

            <template v-else-if="draft.mode === 'daily' || draft.mode === 'weekly'">
              <Field.Root class="schedule-field" required>
                <Field.Label>执行时间</Field.Label>
                <Field.Input v-model="draft.calendarTime" type="time" />
                <Field.HelperText>{{ calendarTimezoneText }}</Field.HelperText>
              </Field.Root>
              <Field.Root v-if="draft.mode === 'weekly'" class="schedule-field" required>
                <Field.Label>星期</Field.Label>
                <div class="schedule-weekdays">
                  <Checkbox.Root
                    v-for="weekday in weekdays"
                    :key="weekday.value"
                    :checked="draft.weeklyDays.includes(weekday.value)"
                    @update:checked="toggleWeekday(weekday.value, $event)"
                  >
                    <Checkbox.HiddenInput />
                    <Checkbox.Control
                      ><Checkbox.Indicator><CheckOutlined /></Checkbox.Indicator
                    ></Checkbox.Control>
                    <Checkbox.Label>周{{ weekday.label }}</Checkbox.Label>
                  </Checkbox.Root>
                </div>
              </Field.Root>
            </template>

            <div v-else-if="draft.mode === 'adaptive'" class="schedule-adaptive-fields">
              <Field.Root class="schedule-field" required>
                <Field.Label>最短等待</Field.Label>
                <Field.Input v-model="draft.minimumMinutes" type="number" min="1" step="1" inputmode="numeric" />
                <Field.HelperText>分钟</Field.HelperText>
              </Field.Root>
              <Field.Root class="schedule-field" required>
                <Field.Label>通常等待</Field.Label>
                <Field.Input v-model="draft.defaultMinutes" type="number" min="1" step="1" inputmode="numeric" />
                <Field.HelperText>分钟</Field.HelperText>
              </Field.Root>
              <Field.Root class="schedule-field" required>
                <Field.Label>最长等待</Field.Label>
                <Field.Input v-model="draft.maximumMinutes" type="number" min="1" step="1" inputmode="numeric" />
                <Field.HelperText>分钟</Field.HelperText>
              </Field.Root>
            </div>

            <p v-else class="schedule-preserved-copy">
              这项任务使用由智能体设置的日历安排。你可以保留它，或改用上面的常用安排。
            </p>

            <p v-if="formError" class="schedules-error" role="alert">{{ formError }}</p>
          </div>
          <div class="dialog-footer">
            <XButton :disabled="saving" @click="editorDialog.close"><CloseOutlined aria-hidden="true" />取消</XButton>
            <XButton variant="primary" :disabled="saving" @click="saveTask">
              <LoadingOutlined v-if="saving" class="schedule-spin" aria-hidden="true" />
              <SaveOutlined v-else aria-hidden="true" />{{ saving ? '保存中…' : '保存任务' }}
            </XButton>
          </div>
        </Dialog.Content>
      </Dialog.Positioner>
    </Teleport>
  </Dialog.Root>

  <Dialog.Root
    :open="deleteDialog.open.value"
    lazy-mount
    unmount-on-exit
    :close-on-escape="!deleting"
    :close-on-interact-outside="!deleting"
    @exit-complete="deleteDialog.clearAfterExit"
    @update:open="updateDeleteOpen"
  >
    <Teleport to="body">
      <Dialog.Backdrop class="dialog-backdrop" />
      <Dialog.Positioner class="dialog-positioner">
        <Dialog.Content class="dialog-panel schedule-delete-dialog">
          <div class="dialog-header">
            <Dialog.Title>删除自动任务</Dialog.Title>
            <Dialog.CloseTrigger class="dialog-close" :disabled="deleting" aria-label="关闭">
              <CloseOutlined aria-hidden="true" />
            </Dialog.CloseTrigger>
          </div>
          <div class="dialog-body">
            <p>
              「{{ deleteDialog.payload.value?.name }}」将连同运行记录永久删除。如有相关智能体对话，该对话不会被删除。
            </p>
            <p v-if="deleteError" class="schedules-error" role="alert">{{ deleteError }}</p>
          </div>
          <div class="dialog-footer">
            <XButton :disabled="deleting" @click="deleteDialog.close"><CloseOutlined aria-hidden="true" />取消</XButton>
            <XButton variant="danger" :disabled="deleting" @click="confirmDelete">
              <LoadingOutlined v-if="deleting" class="schedule-spin" aria-hidden="true" />
              <DeleteOutlined v-else aria-hidden="true" />{{ deleting ? '删除中…' : '确认删除' }}
            </XButton>
          </div>
        </Dialog.Content>
      </Dialog.Positioner>
    </Teleport>
  </Dialog.Root>
</template>

<style lang="scss" scoped>
.schedules-page {
  height: 100%;
  overflow-y: auto;
  padding: clamp(20px, 3vw, 38px) clamp(14px, 4vw, 44px);
  background: var(--bg-primary);
  color: var(--text-primary);
}

.schedules-content {
  width: min(100%, 1040px);
  margin: 0 auto;
  padding-bottom: 40px;
}

.schedules-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 24px;

  h1 {
    margin: 0;
    font-size: var(--marvo-type-24);
    line-height: 1.25;
  }

  p {
    margin: 7px 0 0;
    color: var(--text-tertiary);
    font-size: var(--marvo-type-13);
  }
}

.schedules-heading-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.schedules-list {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.schedule-card {
  padding: 20px;
  border: 1px solid var(--border-primary);
  border-radius: 16px;
  background: var(--bg-card);
  box-shadow: 0 5px 18px color-mix(in srgb, #000 5%, transparent);
}

.schedule-card-heading {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: 11px;
}

.schedule-card-icon {
  width: 34px;
  height: 34px;
  display: grid;
  place-items: center;
  border-radius: 10px;
  background: color-mix(in srgb, var(--marvo-accent-color) 10%, var(--bg-secondary));
  color: var(--text-accent);
  font-size: var(--marvo-type-16);
}

.schedule-card-title {
  min-width: 0;

  > div {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
  }

  h2 {
    min-width: 0;
    margin: 0;
    overflow-wrap: anywhere;
    font-size: var(--marvo-type-16);
    line-height: 1.4;
  }

  p {
    margin: 4px 0 0;
    color: var(--text-tertiary);
    font-size: var(--marvo-type-12);
  }
}

.schedule-status,
.schedule-run-status {
  display: inline-flex;
  align-items: center;
  padding: 2px 7px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--marvo-accent-color) 9%, var(--bg-secondary));
  color: var(--text-accent);
  font-size: var(--marvo-type-10);
  font-weight: 600;
  white-space: nowrap;

  &.is-paused,
  &.is-completed,
  &.is-cancelled {
    background: var(--bg-secondary);
    color: var(--text-tertiary);
  }

  &.is-failed,
  &.is-timed_out {
    background: color-mix(in srgb, var(--text-danger) 9%, var(--bg-secondary));
    color: var(--text-danger);
  }
}

.schedule-instruction {
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
  margin: 16px 0 0 45px;
  overflow: hidden;
  color: var(--text-secondary);
  font-size: var(--marvo-type-13);
  line-height: 1.65;
  white-space: pre-wrap;
}

.schedule-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px 18px;
  margin: 14px 0 0 45px;
  color: var(--text-tertiary);
  font-size: var(--marvo-type-11);

  span {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
}

.schedule-last-error {
  margin: 12px 0 0 45px;
  padding: 9px 11px;
  border-radius: 9px;
  background: color-mix(in srgb, var(--text-danger) 6%, var(--bg-secondary));
  color: var(--text-danger);
  font-size: var(--marvo-type-11);
  line-height: 1.5;
}

.schedule-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 7px;
  margin: 17px 0 0 45px;
}

.schedule-history {
  margin: 16px 0 0 45px;
  padding-top: 14px;
  border-top: 1px solid var(--border-primary);

  ol {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  li {
    display: grid;
    grid-template-columns: auto minmax(150px, 1fr) auto;
    align-items: center;
    gap: 9px;
    padding: 9px 4px;
    color: var(--text-tertiary);
    font-size: var(--marvo-type-11);

    + li {
      border-top: 1px solid var(--border-primary);
    }

    p {
      grid-column: 2 / -1;
      margin: 0;
      color: var(--text-danger);
      line-height: 1.5;
    }
  }
}

.schedule-history-empty {
  margin: 0;
  color: var(--text-muted);
  font-size: var(--marvo-type-12);
}

.schedules-loading {
  min-height: 220px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--text-tertiary);

  .page-loading-spinner {
    position: static;
  }
}

.schedules-empty {
  min-height: 220px;
  align-items: center;
  padding: 26px;
}

.schedules-error,
.schedules-load-error {
  border: 1px solid color-mix(in srgb, var(--text-danger) 30%, var(--border-primary));
  border-radius: 11px;
  background: color-mix(in srgb, var(--text-danger) 5%, var(--bg-card));
  color: var(--text-danger);
}

.schedules-error {
  margin: 0 0 14px;
  padding: 10px 13px;
  font-size: var(--marvo-type-12);
}

.schedules-load-error {
  padding: 22px;

  p {
    margin: 6px 0 16px;
    color: var(--text-secondary);
  }
}

.schedule-spin {
  animation: schedule-spin 0.8s linear infinite;
}

@keyframes schedule-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 760px) {
  .schedules-heading {
    align-items: stretch;
    flex-direction: column;
  }

  .schedules-heading-actions {
    justify-content: space-between;
  }

  .schedule-instruction,
  .schedule-meta,
  .schedule-last-error,
  .schedule-actions,
  .schedule-history {
    margin-left: 0;
  }
}

@media (max-width: 520px) {
  .schedules-page {
    padding: 16px 12px 28px;
  }

  .schedules-heading-actions {
    align-items: stretch;
    flex-direction: column;
  }

  .schedule-card {
    padding: 16px 13px;
    border-radius: 13px;
  }

  .schedule-card-heading {
    grid-template-columns: auto minmax(0, 1fr);

    > :last-child {
      grid-column: 2;
      justify-self: end;
    }
  }

  .schedule-actions :deep(.x-button) {
    flex: 1 1 calc(50% - 7px);
  }

  .schedule-history li {
    grid-template-columns: auto 1fr;

    > :nth-child(3),
    p {
      grid-column: 2;
    }
  }
}
</style>

<style lang="scss">
.schedule-editor-dialog {
  width: min(680px, calc(100vw - 28px));
  max-width: 680px;
}

.schedule-editor-dialog .dialog-header [data-part='description'] {
  margin: 5px 0 0;
  color: var(--text-muted);
  font-size: var(--marvo-type-11);
}

.schedule-editor-body {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.schedule-field {
  min-width: 0;

  > [data-part='label'] {
    display: block;
    margin-bottom: 7px;
    color: var(--text-secondary);
    font-size: var(--marvo-type-12);
    font-weight: 600;
  }

  > [data-part='input'] {
    box-sizing: border-box;
    width: 100%;
    min-height: 40px;
    padding: 8px 11px;
    border: 1px solid var(--border-light);
    border-radius: 8px;
    outline: none;
    background: var(--bg-primary);
    color: var(--text-primary);
    color-scheme: light dark;
    font: inherit;
    font-size: var(--marvo-type-13);

    &:focus {
      border-color: var(--text-accent);
      box-shadow: 0 0 0 3px color-mix(in srgb, var(--marvo-accent-color) 13%, transparent);
    }
  }

  > [data-part='helper-text'] {
    display: block;
    margin-top: 5px;
    color: var(--text-muted);
    font-size: var(--marvo-type-10);
  }
}

.schedule-editor-dialog .x-fullscreen-textarea-input {
  min-height: 138px;
}

.schedule-mode-scroller {
  max-width: 100%;
  overflow-x: auto;
  padding-bottom: 2px;
}

.schedule-mode-group,
.schedule-unit-group {
  position: relative;
  width: max-content;
  min-width: 100%;
  display: flex;
  align-items: stretch;
  gap: 3px;
  padding: 3px;
  border-radius: 9px;
  background: var(--bg-secondary);
}

.schedule-mode-indicator {
  border-radius: 7px;
  background: var(--bg-primary);
  box-shadow: 0 1px 4px color-mix(in srgb, #000 11%, transparent);
}

.schedule-mode-item {
  position: relative;
  z-index: 1;
  min-height: 34px;
  flex: 1 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0 12px;
  border-radius: 7px;
  color: var(--text-tertiary);
  cursor: pointer;
  font-size: var(--marvo-type-12);
  white-space: nowrap;

  &[data-state='checked'] {
    color: var(--text-primary);
    font-weight: 600;
  }
}

.schedule-inline-fields {
  display: grid;
  grid-template-columns: minmax(100px, 0.55fr) minmax(240px, 1fr);
  align-items: end;
  gap: 12px;
}

.schedule-unit-group {
  min-width: 0;
}

.schedule-adaptive-fields {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.schedule-weekdays {
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
  gap: 6px;

  [data-scope='checkbox'][data-part='root'] {
    min-height: 40px;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 5px;
    border: 1px solid var(--border-primary);
    border-radius: 8px;
    color: var(--text-tertiary);
    cursor: pointer;
    font-size: var(--marvo-type-11);

    &[data-state='checked'] {
      border-color: color-mix(in srgb, var(--marvo-accent-color) 50%, var(--border-primary));
      background: color-mix(in srgb, var(--marvo-accent-color) 8%, var(--bg-primary));
      color: var(--text-accent);
    }
  }

  [data-part='control'] {
    width: 14px;
    height: 14px;
    display: grid;
    place-items: center;
    border: 1px solid currentColor;
    border-radius: 4px;
    font-size: 9px;
  }
}

.schedule-preserved-copy {
  margin: 0;
  padding: 13px 14px;
  border-radius: 9px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: var(--marvo-type-12);
  line-height: 1.6;
}

.schedule-delete-dialog {
  max-width: 430px;

  .dialog-body p {
    margin: 0;
    color: var(--text-secondary);
    line-height: 1.65;
  }
}

@media (max-width: 600px) {
  .schedule-editor-dialog {
    width: calc(100vw - 20px);
  }

  .schedule-inline-fields,
  .schedule-adaptive-fields {
    grid-template-columns: 1fr;
  }

  .schedule-weekdays {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}
</style>
