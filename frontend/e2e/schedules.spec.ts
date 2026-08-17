import { expect, test, type Route } from '@playwright/test'
import { approveDevice, workspaceAPI, workspacePath, workspaceURL } from './helpers'

const taskID = '33333333333333333333333333333333'
const createdAt = '2026-08-17T01:00:00Z'

function automaticTask(overrides: Record<string, unknown> = {}) {
  return {
    id: taskID,
    name: '跟进量子计算新进展',
    instruction: '检查可信来源中的最新进展，有值得关注的变化时整理结论并发布活动。',
    schedule: {
      kind: 'cron',
      spec: { expression: '0 9 * * 1,3,5' },
      timezone: 'Asia/Hong_Kong',
    },
    status: 'active',
    next_run_at: '2026-08-19T01:00:00Z',
    revision: 1,
    consecutive_failures: 0,
    last_run_at: null,
    created_at: createdAt,
    updated_at: createdAt,
    ...overrides,
  }
}

async function fulfillJSON(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

test('自动任务在横竖屏均完整展示并提供运行记录', async ({ page }, testInfo) => {
  const task = automaticTask()
  await page.route('**/api/user/*/schedules**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (request.method() === 'GET' && path.endsWith(`/schedules/${taskID}/runs`)) {
      await fulfillJSON(route, {
        runs: [
          {
            id: '44444444444444444444444444444444',
            schedule_id: taskID,
            schedule_revision: 1,
            trigger: 'scheduled',
            scheduled_for: '2026-08-16T01:00:00Z',
            status: 'succeeded',
            attempt: 1,
            created_at: '2026-08-16T01:00:00Z',
            started_at: '2026-08-16T01:00:01Z',
            finished_at: '2026-08-16T01:02:00Z',
            updated_at: '2026-08-16T01:02:00Z',
          },
        ],
      })
      return
    }
    if (request.method() === 'GET' && path.endsWith('/schedules')) {
      await fulfillJSON(route, { tasks: [task] })
      return
    }
    await route.fallback()
  })

  await approveDevice(page, `Playwright schedules ${testInfo.project.name}`)
  await page.goto(workspacePath('/schedules'))

  await expect(page).toHaveURL(workspaceURL('/schedules'))
  await expect(page.getByRole('heading', { name: '自动任务', exact: true })).toBeVisible()
  await expect(page.getByText('跟进量子计算新进展', { exact: true })).toBeVisible()
  await expect(page.getByText(/^每周一、三、五 09:00/)).toBeVisible()
  await expect(page.getByText('已启用', { exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: '活动', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: '自动任务', exact: true })).toHaveAttribute('aria-current', 'page')

  await page.getByRole('button', { name: '运行记录', exact: true }).click()
  await expect(page.getByText('已完成', { exact: true })).toBeVisible()

  const viewport = page.viewportSize()!
  const bounds = await page.locator('.schedule-card').boundingBox()
  expect(bounds).not.toBeNull()
  expect(bounds!.x).toBeGreaterThanOrEqual(0)
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(viewport.width)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(viewport.width)

  if (process.env.MARVO_CAPTURE_UI === '1') {
    await page.screenshot({ path: testInfo.outputPath('automatic-tasks.png'), fullPage: true })
  }
})

test('用户可以用自然时间选项创建自动任务', async ({ page }, testInfo) => {
  const tasks: ReturnType<typeof automaticTask>[] = []
  let posted: Record<string, unknown> | null = null
  await page.route('**/api/user/*/schedules**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (request.method() === 'GET' && path.endsWith('/schedules')) {
      await fulfillJSON(route, { tasks })
      return
    }
    if (request.method() === 'POST' && path.endsWith('/schedules')) {
      posted = request.postDataJSON() as Record<string, unknown>
      const created = automaticTask({
        name: '每日整理行业动态',
        instruction: '阅读新资料并把重要变化发布到活动。',
        schedule: { kind: 'every', spec: { every_seconds: 86_400, anchor: createdAt } },
      })
      tasks.push(created)
      await fulfillJSON(route, created, 201)
      return
    }
    await route.fallback()
  })

  await approveDevice(page, `Playwright create schedule ${testInfo.project.name}`)
  await page.goto(workspacePath('/schedules'))
  await expect(page.getByText('还没有自动任务', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: '新建任务', exact: true }).first().click()
  const dialog = page.getByRole('dialog', { name: '新建自动任务' })
  await dialog.getByPlaceholder('例如：跟进量子计算新进展').fill('每日整理行业动态')
  await dialog
    .getByPlaceholder('说明每次需要智能体检查、研究或推进什么，以及何时应该联系你。')
    .fill('阅读新资料并把重要变化发布到活动。')
  await dialog.getByLabel('每隔').fill('1')
  await dialog.getByText('天', { exact: true }).click()
  if (process.env.MARVO_CAPTURE_UI === '1') {
    await page.screenshot({ path: testInfo.outputPath('automatic-task-editor.png'), fullPage: true })
  }
  await dialog.getByRole('button', { name: '保存任务', exact: true }).click()

  await expect(dialog).toBeHidden()
  await expect(page.getByText('每日整理行业动态', { exact: true })).toBeVisible()
  expect(posted).toMatchObject({
    name: '每日整理行业动态',
    instruction: '阅读新资料并把重要变化发布到活动。',
    schedule: { kind: 'every', spec: { every_seconds: 86_400 } },
  })
})

test('删除自动任务会携带当前版本并经过确认', async ({ page }, testInfo) => {
  const task = automaticTask()
  let deleted: Record<string, unknown> | null = null
  await page.route('**/api/user/*/schedules**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (request.method() === 'GET' && path.endsWith('/schedules')) {
      await fulfillJSON(route, { tasks: [task] })
      return
    }
    if (request.method() === 'DELETE' && path.endsWith(`/schedules/${taskID}`)) {
      deleted = request.postDataJSON() as Record<string, unknown>
      await route.fulfill({ status: 204 })
      return
    }
    await route.fallback()
  })

  await approveDevice(page, `Playwright delete schedule ${testInfo.project.name}`)
  await page.goto(workspacePath('/schedules'))
  const card = page.locator('.schedule-card').filter({ hasText: task.name })
  await card.getByRole('button', { name: '删除', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '删除自动任务' })
  await expect(dialog).toContainText('如有相关智能体对话，该对话不会被删除')
  await dialog.getByRole('button', { name: '确认删除', exact: true }).click()

  await expect(dialog).toBeHidden()
  await expect(card).toHaveCount(0)
  expect(deleted).toEqual({ revision: 1 })
})

test('停止操作只针对用户当前看到的本轮运行', async ({ page }, testInfo) => {
  const runID = '55555555555555555555555555555555'
  const task = automaticTask({
    active_run: {
      id: runID,
      schedule_id: taskID,
      schedule_revision: 1,
      trigger: 'manual',
      scheduled_for: createdAt,
      status: 'running',
      attempt: 1,
      created_at: createdAt,
      started_at: createdAt,
      updated_at: createdAt,
    },
  })
  let stopped: Record<string, unknown> | null = null
  await page.route('**/api/user/*/schedules**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (request.method() === 'GET' && path.endsWith('/schedules')) {
      await fulfillJSON(route, { tasks: [task] })
      return
    }
    if (request.method() === 'POST' && path.endsWith(`/schedules/${taskID}/stop`)) {
      stopped = request.postDataJSON() as Record<string, unknown>
      await fulfillJSON(route, { stopping: true }, 202)
      return
    }
    await route.fallback()
  })

  await approveDevice(page, `Playwright stop schedule ${testInfo.project.name}`)
  await page.goto(workspacePath('/schedules'))
  await page.getByRole('button', { name: '停止本轮', exact: true }).click()
  expect(stopped).toEqual({ run_id: runID })
})

test('立即执行会复用任务对话并把结果送到活动', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await approveDevice(page, 'Playwright automatic task execution')

  const create = await page.request.post(workspaceAPI('/api/schedules'), {
    data: {
      name: '自动检查集成测试',
      instruction: '完成一次自动检查并告知我结果。',
      schedule: { kind: 'every', spec: { every_seconds: 3600 } },
    },
  })
  expect(create.status()).toBe(201)
  const task = (await create.json()) as ReturnType<typeof automaticTask>
  const run = await page.request.post(workspaceAPI(`/api/schedules/${task.id}/run`))
  expect(run.status()).toBe(202)

  await expect
    .poll(
      async () => {
        const response = await page.request.get(workspaceAPI(`/api/schedules/${task.id}`))
        if (!response.ok()) return ''
        const current = (await response.json()) as ReturnType<typeof automaticTask> & { active_run?: unknown }
        return current.last_run_at && !current.active_run && current.session_id ? 'completed' : ''
      },
      { timeout: 12_000 },
    )
    .toBe('completed')

  await page.goto(workspacePath('/activity'))
  const activity = page.locator('.activity-card').filter({ hasText: '自动检查集成测试' })
  await expect(activity).toBeVisible()
  await expect(activity).toContainText('自动检查已完成，并整理了本轮结果。')

  await page.goto(workspacePath('/schedules'))
  const card = page.locator('.schedule-card').filter({ hasText: '自动检查集成测试' })
  await expect(card.getByRole('button', { name: '打开对话', exact: true })).toBeVisible()
})
