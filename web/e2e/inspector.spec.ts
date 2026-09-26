import { expect, test } from '@playwright/test'

test('inspector leads from branch blockers to the matching review and agent session', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto('/')
  const inspector = page.getByRole('complementary', { name: 'Inspector' })
  await inspector.getByRole('button', { name: /fix\/daemon-lifecycle/ }).click()
  await expect(inspector.getByText('Resolve merge conflicts', { exact: true })).toBeVisible()
  await expect(inspector.getByText('go test ./...', { exact: true })).toBeVisible()
  await expect(inspector.getByText('1 commit behind main', { exact: true })).toBeVisible()

  // A review link must reveal the selected PR even after closing the entire dock.
  await page.getByTitle('Minimize workspace').click()
  await inspector.getByRole('button', { name: /#23 fix\(daemon\)/ }).click()
  await expect(page.getByTitle('Minimize workspace')).toBeVisible()
  await expect(page.getByText('test: reproduce interrupted shutdown', { exact: true })).toBeVisible()
  await page.getByRole('textbox', { name: 'Search pull requests' }).fill('no matching review')
  await expect(page.getByText('test: reproduce interrupted shutdown', { exact: true })).toHaveCount(0)
  await inspector.getByRole('button', { name: /#23 fix\(daemon\)/ }).click()
  await expect(page.getByRole('textbox', { name: 'Search pull requests' })).toHaveValue('')
  await expect(page.getByText('test: reproduce interrupted shutdown', { exact: true })).toBeVisible()

  await inspector.getByRole('button', { name: /Lifecycle fix/ }).click()
  await expect(inspector.getByRole('heading', { name: 'Latest terminal output' })).toBeVisible()
  await expect(inspector.locator('pre')).toContainText('[bonsaid] tracing process lifecycle')
  await inspector.getByRole('button', { name: 'Open terminal', exact: true }).click()
  await expect(page.locator('.runtime-tile-active')).toContainText('Lifecycle fix')

  await inspector.getByRole('button', { name: 'Stop agent', exact: true }).click()
  await expect(inspector.getByRole('button', { name: 'Restart', exact: true })).toBeVisible()
  await expect(inspector.getByRole('button', { name: 'Move to history', exact: true })).toBeVisible()
  expect(errors).toEqual([])
})

test('branch settings remain editable and project overview stays scoped', async ({ page }) => {
  await page.goto('/')
  const inspector = page.getByRole('complementary', { name: 'Inspector' })
  await inspector.getByRole('button', { name: /fix\/daemon-lifecycle/ }).click()
  await inspector.getByText('Branch settings', { exact: true }).click()
  await inspector.getByRole('textbox', { name: 'Tag name' }).fill('needs-review')
  await inspector.getByRole('button', { name: 'Save tag' }).click()
  await page.reload()
  await inspector.getByText('Branch settings', { exact: true }).click()
  await expect(inspector.getByRole('textbox', { name: 'Tag name' })).toHaveValue('needs-review')
  await expect(inspector.getByRole('button', { name: 'Merge target', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Project', exact: true }).click()
  await page.getByRole('option', { name: 'sprout-lab', exact: true }).click()
  await expect(inspector.getByRole('heading', { name: 'sprout-lab', exact: true })).toBeVisible()
  await expect(inspector.getByText('No branch blockers reported.')).toBeVisible()
  await expect(inspector.getByText('fix/daemon-lifecycle', { exact: true })).toHaveCount(0)
})
