import { expect, test, type Route } from '@playwright/test'
import { approveDevice, workspacePath, workspaceURL } from './helpers'

const choiceID = '11111111111111111111111111111111'
const noticeID = '22222222222222222222222222222222'
const createdAt = '2026-08-16T08:06:06Z'

function activityItem(id: string, kind: 'notice' | 'choice') {
  return {
    id,
    kind,
    title: kind === 'choice' ? '请选择后续方向' : '资料整理完成',
    content: kind === 'choice' ? '研究已完成第一阶段，请确定下一步。' : '已将结果整理到对应笔记。',
    choices: kind === 'choice' ? ['继续深挖', '整理摘要'] : [],
    multiple: false,
    created_at: createdAt,
    read_at: null,
    responded_at: null,
    response_choices: [],
    replying: false,
  }
}

async function fulfillJSON(route: Route, body: unknown) {
  await route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(body),
  })
}

test('活动流展示主动消息并把用户回复发送到新会话', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name === 'webkit-portrait')

  const readRequests: string[][] = []
  const deletedRequests: string[] = []
  await page.route('**/api/user/*/activity**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (request.method() === 'DELETE' && url.pathname.includes('/activity/')) {
      deletedRequests.push(url.pathname.split('/').at(-1) || '')
      await route.fulfill({ status: 204 })
      return
    }
    if (request.method() === 'GET' && url.pathname.endsWith('/activity/counts')) {
      await fulfillJSON(route, { unread: 2, pending: 1 })
      return
    }
    if (request.method() === 'GET' && url.pathname.endsWith('/activity')) {
      await fulfillJSON(route, {
        activities: [activityItem(choiceID, 'choice'), activityItem(noticeID, 'notice')],
        unread: 2,
        pending: 1,
      })
      return
    }
    if (request.method() === 'POST' && url.pathname.endsWith('/activity/read')) {
      const payload = request.postDataJSON() as { ids?: string[] }
      readRequests.push(payload.ids || [])
      await fulfillJSON(route, { ok: true })
      return
    }
    await route.fallback()
  })

  await approveDevice(page, `Playwright Activity ${testInfo.project.name}`)
  await page.goto(workspacePath())
  await page.getByRole('button', { name: /^活动/ }).click()
  await expect(page).toHaveURL(workspaceURL('/activity'))
  await expect(page.getByRole('heading', { name: '活动', exact: true })).toBeVisible()
  await expect(page.getByText('请选择后续方向', { exact: true })).toBeVisible()
  await expect(page.getByText('资料整理完成', { exact: true })).toBeVisible()
  await expect.poll(() => readRequests.some((ids) => ids.includes(choiceID))).toBe(true)

  const noticeCard = page.locator('.activity-card').filter({ hasText: '资料整理完成' })
  await noticeCard.getByRole('button', { name: '删除', exact: true }).click()
  const deleteDialog = page.getByRole('dialog', { name: '删除活动' })
  await expect(deleteDialog).toContainText('如有相关智能体对话，该对话不会被删除')
  await deleteDialog.getByRole('button', { name: '确认删除' }).click()
  await expect(page.getByText('资料整理完成', { exact: true })).toHaveCount(0)
  expect(deletedRequests).toEqual([noticeID])

  const promptRequest = page.waitForRequest(
    (request) =>
      request.method() === 'POST' && /\/agent\/session\/[^/]+\/prompt_async$/.test(new URL(request.url()).pathname),
  )
  await page.route(/\/api\/user\/[^/]+\/agent\/session\/[^/]+\/prompt_async$/, (route) => fulfillJSON(route, {}))
  await page.getByRole('button', { name: '继续深挖', exact: true }).click()
  await page.getByPlaceholder('补充你的想法（可直接只选一项）').fill('优先核对原始来源。')
  await page.locator('.activity-card').first().getByRole('button', { name: '发送' }).click()

  const payload = (await promptRequest).postDataJSON() as {
    parts: Array<{ type: string; text?: string }>
    marvoContext?: { activity?: { id?: string; choices?: string[] } }
  }
  expect(payload.parts.find((part) => part.type === 'text')?.text).toBe('继续深挖\n\n优先核对原始来源。')
  expect(payload.marvoContext?.activity).toEqual({ id: choiceID, choices: ['继续深挖'] })
  await expect(page).toHaveURL(workspaceURL('/agent'))
})

test('响应会话已删除时留在活动页并禁用失效入口', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name === 'webkit-portrait')

  const deletedSessionID = 'ses_deleted_activity_reply'
  await page.route('**/api/user/*/activity**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (request.method() === 'GET' && url.pathname.endsWith('/activity/counts')) {
      await fulfillJSON(route, { unread: 0, pending: 0 })
      return
    }
    if (request.method() === 'GET' && url.pathname.endsWith('/activity')) {
      await fulfillJSON(route, {
        activities: [
          {
            ...activityItem(noticeID, 'notice'),
            read_at: createdAt,
            responded_at: createdAt,
            response_text: '知道了',
          },
          {
            ...activityItem(choiceID, 'choice'),
            read_at: createdAt,
            responded_at: createdAt,
            response_text: '继续深挖',
            reply_session_id: deletedSessionID,
          },
        ],
        unread: 0,
        pending: 0,
      })
      return
    }
    await route.fallback()
  })
  await page.route('**/api/user/*/agent/session', async (route) => {
    if (route.request().method() === 'GET') {
      await fulfillJSON(route, [])
      return
    }
    await route.fallback()
  })

  await approveDevice(page, `Playwright deleted Activity session ${testInfo.project.name}`)
  await page.goto(workspacePath('/activity'))
  await expect(page.getByText('对话已删除', { exact: true })).toBeVisible()
  const openButton = page.getByRole('button', { name: '打开对话' })
  await openButton.click()

  await expect(page).toHaveURL(workspaceURL('/activity'))
  await expect(page.getByRole('button', { name: '对话已删除' })).toBeDisabled()
  await expect(page.getByText(/Session not found/i)).toHaveCount(0)
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem('marvo.agent.currentSessionId')))
    .not.toBe(deletedSessionID)
})
