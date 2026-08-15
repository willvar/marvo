import { expect, test } from '@playwright/test'

test('根路由展示公开落地页且不会进入平台后台', async ({ page }) => {
  const requests: string[] = []
  page.on('request', (request) => requests.push(new URL(request.url()).pathname))

  await page.goto('/')

  await expect(page).toHaveURL('/')
  await expect(page.getByRole('heading', { name: /不只保存知识.*更让它参与工作/ })).toBeVisible()
  await expect(page.getByRole('link', { name: '平台管理' }).first()).toHaveAttribute('href', '/admin')
  expect(requests.some((path) => path.startsWith('/api/admin/'))).toBe(false)

  const firstFoldLayout = await page.evaluate(() => {
    const root = document.querySelector<HTMLElement>('.landing-page')!
    const headingLines = [...document.querySelectorAll<HTMLElement>('.landing-hero h1 span')]
    const actions = [...document.querySelectorAll<HTMLElement>('.landing-hero-actions > *')]
    return {
      headingLineCount: headingLines.length,
      headingOverflow: headingLines.some((line) => line.scrollWidth > line.clientWidth),
      actionOverflow: actions.some(
        (action) => action.scrollWidth > action.clientWidth || action.scrollHeight > action.clientHeight,
      ),
      horizontalOverflow: root.scrollWidth > root.clientWidth,
    }
  })
  expect(firstFoldLayout).toEqual({
    headingLineCount: 2,
    headingOverflow: false,
    actionOverflow: false,
    horizontalOverflow: false,
  })
})

test('落地页可以通过空间链接或 ID 打开用户空间', async ({ page }) => {
  await page.goto('/')
  const input = page.getByRole('textbox', { name: '用户空间链接或空间 ID' })

  await input.fill('not-a-space')
  await page.getByRole('button', { name: '打开空间' }).click()
  await expect(page.getByRole('alert')).toHaveText('请输入有效的空间链接或 20 位空间 ID')

  await input.fill('https://example.test/user/35720f590f5a31830136/admin')
  await page.getByRole('button', { name: '打开空间' }).click()
  await expect(page).toHaveURL('/user/35720f590f5a31830136')
})

test('未知地址显示 404 而不是平台后台', async ({ page }) => {
  await page.goto('/this-page-does-not-exist')

  await expect(page).toHaveURL('/this-page-does-not-exist')
  await expect(page.getByRole('heading', { name: '这里没有你要找的页面' })).toBeVisible()
  await expect(page).toHaveTitle('页面未找到 · Marvo')
})
