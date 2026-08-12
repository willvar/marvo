import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { approvedDeviceContext, approveDevice, platformContext, workspaceAPI } from './helpers'

const isolationPassword = 'multiuser-e2e-password'

test('平台管理员可将旧版数据无覆盖迁移到指定用户', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  await page.setViewportSize({ width: 1366, height: 768 })
  await approveDevice(page, 'Playwright legacy migration')

  await page.goto('/admin/login')
  await page.getByPlaceholder('请输入密码').fill('e2e-admin-password')
  await page.getByRole('button', { name: '进入' }).click()
  await expect(page).toHaveURL(/\/admin$/)
  await expect(page.getByText('检测到可迁移的旧版单用户数据')).toBeVisible()

  const row = page.locator('tbody tr').filter({ hasText: 'Playwright 用户空间' })
  const actionBoxes = await row
    .locator('.platform-user-actions .admin-btn')
    .evaluateAll((buttons) =>
      buttons.map((button) => button.getBoundingClientRect()).map(({ top, bottom }) => ({ top, bottom })),
    )
  expect(actionBoxes.length).toBeGreaterThanOrEqual(5)
  expect(Math.max(...actionBoxes.map(({ top }) => top)) - Math.min(...actionBoxes.map(({ top }) => top))).toBeLessThan(
    2,
  )
  await row.getByRole('button', { name: '迁移旧数据' }).click()
  await expect(page.getByRole('heading', { name: '迁移旧版数据' })).toBeVisible()
  await page.getByRole('button', { name: '确认' }).click()
  await expect(page.getByText('旧版数据已经安全迁移')).toBeVisible()

  const migrated = await page.request.get(workspaceAPI('/api/notes/Legacy%20E2E%20note'))
  expect(migrated.ok()).toBeTruthy()
  expect((await migrated.json()).content).toContain('legacy migration content')
})

test('用户设备无法读取其他用户的内容与智能体状态', async ({ request: _request }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-landscape')
  const platform = await platformContext()
  const contexts = []
  try {
    const createUser = async (name: string) => {
      const response = await platform.post('/api/admin/users', {
        data: { name, password: isolationPassword },
      })
      expect(response.ok()).toBeTruthy()
      return (await response.json()).user as { id: string }
    }
    const userA = await createUser('隔离验证 A')
    const userB = await createUser('隔离验证 B')
    const deviceA = await approvedDeviceContext(platform, userA.id, isolationPassword, 'isolation-device-a')
    contexts.push(deviceA)
    const deviceB = await approvedDeviceContext(platform, userB.id, isolationPassword, 'isolation-device-b')
    contexts.push(deviceB)

    const api = (userID: string, path: string) => `/api/user/${userID}${path}`
    const noteTitle = '同名隔离笔记'
    const createA = await deviceA.post(api(userA.id, '/notes'), {
      data: { title: noteTitle, content: '只属于 A', tags: [] },
    })
    const createB = await deviceB.post(api(userB.id, '/notes'), {
      data: { title: noteTitle, content: '只属于 B', tags: [] },
    })
    expect(createA.ok()).toBeTruthy()
    expect(createB.ok()).toBeTruthy()
    const noteA = (await createA.json()) as { instance_token: string }

    expect((await deviceA.get(api(userB.id, '/notes'))).status()).toBe(401)
    const ownA = await deviceA.get(api(userA.id, `/notes/${encodeURIComponent(noteTitle)}`))
    const ownB = await deviceB.get(api(userB.id, `/notes/${encodeURIComponent(noteTitle)}`))
    expect((await ownA.json()).content).toBe('只属于 A')
    expect((await ownB.json()).content).toBe('只属于 B')

    const assetID = randomUUID()
    const reserved = await deviceA.post(api(userA.id, `/notes/${encodeURIComponent(noteTitle)}/assets/reserve`), {
      data: {
        asset_id: assetID,
        original_name: 'isolation.png',
        content_type: 'image/png',
        instance_token: noteA.instance_token,
      },
    })
    expect(reserved.ok()).toBeTruthy()
    const uploaded = await deviceA.put(
      api(userA.id, `/notes/${encodeURIComponent(noteTitle)}/assets/${assetID}/content`),
      {
        headers: {
          'Content-Type': 'image/png',
          'X-Marvo-Instance-Token': noteA.instance_token,
        },
        data: Buffer.from(
          'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=',
          'base64',
        ),
      },
    )
    expect(uploaded.ok()).toBeTruthy()
    let assetURL = ''
    await expect
      .poll(async () => {
        const status = await deviceA.get(
          api(
            userA.id,
            `/notes/${encodeURIComponent(noteTitle)}/assets/${assetID}/status?instance_token=${encodeURIComponent(noteA.instance_token)}`,
          ),
        )
        const asset = (await status.json()) as { state?: string; url?: string }
        assetURL = asset.url || ''
        return asset.state
      })
      .toBe('ready')
    expect(assetURL).toMatch(new RegExp(`^/api/user/${userA.id}/notes/`))
    expect((await deviceA.get(assetURL)).ok()).toBeTruthy()
    expect((await deviceB.get(assetURL)).status()).toBe(401)

    const sessionAResponse = await deviceA.post(api(userA.id, '/agent/session'))
    const sessionBResponse = await deviceB.post(api(userB.id, '/agent/session'))
    expect(sessionAResponse.ok()).toBeTruthy()
    expect(sessionBResponse.ok()).toBeTruthy()
    const sessionA = (await sessionAResponse.json()) as { id: string }
    const sessionB = (await sessionBResponse.json()) as { id: string }
    expect(
      (
        await deviceA.patch(api(userA.id, `/agent/session/${sessionA.id}`), {
          data: { title: '只属于 A 的会话' },
        })
      ).ok(),
    ).toBeTruthy()
    expect(
      (
        await deviceB.patch(api(userB.id, `/agent/session/${sessionB.id}`), {
          data: { title: '只属于 B 的会话' },
        })
      ).ok(),
    ).toBeTruthy()
    const sessionsA = (await (await deviceA.get(api(userA.id, '/agent/session'))).json()) as Array<{
      title: string
    }>
    const sessionsB = (await (await deviceB.get(api(userB.id, '/agent/session'))).json()) as Array<{
      title: string
    }>
    expect(sessionsA.map(({ title }) => title)).toContain('只属于 A 的会话')
    expect(sessionsA.map(({ title }) => title)).not.toContain('只属于 B 的会话')
    expect(sessionsB.map(({ title }) => title)).toContain('只属于 B 的会话')
    expect(sessionsB.map(({ title }) => title)).not.toContain('只属于 A 的会话')
    expect((await deviceA.get(api(userB.id, '/agent/session'))).status()).toBe(401)

    expect((await deviceB.get(api(userA.id, '/events?client_id=cross-user'))).status()).toBe(401)
    const trashed = await deviceA.delete(api(userA.id, `/notes/${encodeURIComponent(noteTitle)}`), {
      data: { instance_token: noteA.instance_token },
    })
    expect(trashed.ok()).toBeTruthy()
    const trashA = await deviceA.get(api(userA.id, '/trash'))
    expect((await trashA.json()).some((entry: { title?: string }) => entry.title === noteTitle)).toBeTruthy()
    expect((await deviceB.get(api(userA.id, '/trash'))).status()).toBe(401)
  } finally {
    for (const context of contexts) await context.dispose()
    await platform.dispose()
  }
})
