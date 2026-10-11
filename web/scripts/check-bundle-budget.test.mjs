import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { gzipSync } from 'node:zlib'
import { measureBundle, checkBudgets, resolveChunk, validateManifest } from './check-bundle-budget.mjs'

const chunks = {
  entry: { facade: 'index.html', name: 'index' },
  application: { facade: 'src/app/ApplicationRoot.tsx', name: 'ApplicationRoot' },
  workspace: { facade: 'src/features/workspace/WorkspacePage.tsx', name: 'WorkspacePage', historicalParent: 'application' },
}
function fixture() {
  return {
    'index.html': { file: 'assets/entry.js', name: 'index', imports: ['shared'] },
    app: { file: 'assets/app.js', name: 'ApplicationRoot', imports: ['shared'], dynamicImports: ['workspace'] },
    workspace: { file: 'assets/workspace.js', name: 'WorkspacePage', imports: ['shared'], css: ['assets/style.css'] },
    shared: { file: 'assets/shared.js', imports: ['leaf'], css: ['assets/style.css'] },
    leaf: { file: 'assets/leaf.js' },
    duplicate: { file: 'assets/shared.js' },
    stylesheet: { file: 'assets/style.css' },
  }
}
const asset = file => Buffer.from(`/* ${file} */`)
const measure = (graph = fixture(), options = {}) => measureBundle(graph, asset, { chunks, ...options })
const report = (application = 100, closure = 150, first = 200) => ({ metrics: {
  application: { rawBytes: application }, applicationStaticClosure: { rawBytes: closure }, firstScreenJs: { rawBytes: first },
} })
const budget = () => ({ schemaVersion: 1, rationale: 'Test baseline',
  references: { phase4: { metrics: { application: { rawBytes: 1000 } } }, preP7: { metrics: { firstScreenJs: { rawBytes: 200 } } } },
  baseline: { metrics: { application: { rawBytes: 100 }, applicationStaticClosure: { rawBytes: 150 } } },
})

test('static closures include roots and deduplicate shared emitted files and CSS', () => {
  const graph = fixture()
  graph.app.imports.push('duplicate')
  let reads = 0
  const result = measureBundle(graph, file => { reads++; return asset(file) }, { chunks })
  assert.deepEqual(result.metrics.firstScreenJs.files, ['assets/app.js', 'assets/entry.js', 'assets/leaf.js', 'assets/shared.js', 'assets/workspace.js'])
  assert.deepEqual(result.metrics.applicationStaticClosure.files, ['assets/app.js', 'assets/leaf.js', 'assets/shared.js'])
  assert.equal(result.metrics.css.rawBytes, asset('assets/style.css').length)
  assert.equal(reads, 6)
  assert.equal(result.metrics.firstScreenJs.rawBytes, result.metrics.firstScreenJs.files.reduce((sum, file) => sum + asset(file).length, 0))
  assert.equal(result.metrics.firstScreenJs.gzipBytes, result.metrics.firstScreenJs.files.reduce((sum, file) => sum + gzipSync(asset(file), { level: 9 }).length, 0))
  assert.deepEqual(measureBundle(graph, asset, { chunks }), result)
})

test('source facade takes priority over a unique stable-name fallback', () => {
  const graph = fixture()
  assert.equal(resolveChunk(graph, chunks.application, 'application'), 'app')
  graph.app.src = chunks.application.facade
  graph.decoy = { file: 'assets/decoy.js', name: 'ApplicationRoot' }
  assert.equal(resolveChunk(graph, chunks.application, 'application'), 'app')
  delete graph.app.src
  assert.throws(() => measure(graph), /Ambiguous chunk for application/)
})

test('missing mandatory chunks fail, including report-only builds', () => {
  const graph = fixture()
  delete graph.app
  assert.throws(() => measure(graph), /Missing chunk for application/)
  assert.throws(() => measure(graph, { reportOnly: true }), /Missing chunk for application/)
})

test('historical unsplit features explicitly identify their parent', () => {
  const graph = fixture()
  delete graph.app.dynamicImports
  delete graph.workspace
  assert.throws(() => measure(graph), /Missing chunk for workspace/)
  const result = measure(graph, { reportOnly: true })
  assert.deepEqual(result.chunks.workspace, { includedIn: 'application' })
  assert.equal(result.metrics.firstScreenJs.files.includes('assets/workspace.js'), false)
})

test('malformed manifests, dangling static/dynamic imports and escaping files fail', () => {
  for (const graph of [null, [], {}, { bad: null }, { bad: { file: '../secret.js' } }, { bad: { file: '/secret.js' } }]) {
    assert.throws(() => validateManifest(graph))
  }
  for (const [field, value] of [['imports', 'shared'], ['imports', ['absent']], ['dynamicImports', ['absent']], ['css', ['../style.css']], ['css', [12]]]) {
    const graph = fixture()
    graph.app[field] = value
    assert.throws(() => measure(graph))
  }
})

test('valid cyclic static imports terminate and count each file once', () => {
  const graph = fixture()
  graph.leaf.imports = ['app']
  assert.equal(measure(graph).metrics.applicationStaticClosure.files.length, 3)
})

test('70% Phase 4 threshold uses exact integer arithmetic', () => {
  const config = budget()
  config.baseline.metrics.application.rawBytes = 1000
  const gate = bytes => checkBudgets(report(bytes), config)[0]
  assert.equal(gate(700).passed, true)
  assert.equal(gate(701).passed, false)
  config.references.phase4.metrics.application.rawBytes = 1001
  assert.equal(gate(700).maxRawBytes, 700)
  assert.equal(gate(701).passed, false)
})

test('exact 110% boundary passes; one extra byte and closure-only growth fail', () => {
  assert.equal(checkBudgets(report(110, 165), budget()).every(gate => gate.passed), true)
  assert.equal(checkBudgets(report(111), budget())[1].passed, false)
  const gates = checkBudgets(report(100, 166), budget())
  assert.equal(gates[1].passed, true)
  assert.equal(gates[2].passed, false)
  assert.equal(checkBudgets(report(100, 150, 201), budget())[3].passed, false)
})

test('invalid budget values and undocumented first-screen overrides fail', () => {
  for (const value of [undefined, 0, -1, 1.5, Number.MAX_SAFE_INTEGER + 1]) {
    const config = budget()
    config.baseline.metrics.application.rawBytes = value
    assert.throws(() => checkBudgets(report(), config), /Invalid positive integer budget/)
  }
  const config = budget()
  config.firstScreenDecision = { maxRawBytes: 201 }
  assert.throws(() => checkBudgets(report(), config), /needs a reason and sourceRevision/)
  Object.assign(config.firstScreenDecision, { reason: 'Explicit test decision', sourceRevision: 'test' })
  assert.equal(checkBudgets(report(100, 150, 201), config)[3].passed, true)
  config.rationale = ''
  assert.throws(() => checkBudgets(report(), config), /explicit rationale/)
})

test('CLI accepts a build directory, writes failure JSON, and reports historical builds without a budget', t => {
  const dir = mkdtempSync(join(tmpdir(), 'bonsai-budget-test-'))
  t.after(() => rmSync(dir, { recursive: true, force: true }))
  mkdirSync(join(dir, '.vite'))
  const graph = fixture()
  for (const id of ['pullRequests', 'xterm', 'addonFit', 'landing']) graph[id] = { file: `assets/${id}.js`, name: { pullRequests: 'PullRequestsRoute', xterm: 'xterm', addonFit: 'addon-fit', landing: 'LandingPage' }[id] }
  mkdirSync(join(dir, 'assets'))
  for (const chunk of Object.values(graph)) writeFileSync(join(dir, chunk.file), asset(chunk.file))
  writeFileSync(join(dir, '.vite/manifest.json'), JSON.stringify(graph))
  const script = new URL('./check-bundle-budget.mjs', import.meta.url)
  const output = join(dir, 'reports/result.json')
  const run = extra => spawnSync(process.execPath, [script.pathname, '--build-dir', dir, '--json', output, ...extra], { encoding: 'utf8' })
  const historical = run(['--report-only', '--source-revision', 'historical-test'])
  assert.equal(historical.status, 0, historical.stderr)
  assert.equal(JSON.parse(readFileSync(output)).sourceRevision, 'historical-test')
  assert.equal(JSON.parse(readFileSync(output)).chunks.filesPage.includedIn, 'application')
  const enforced = run(['--budget', join(dir, 'missing-budget.json')])
  assert.equal(enforced.status, 1)
  assert.equal(JSON.parse(readFileSync(output)).passed, false)
  assert.match(enforced.stderr, /Missing chunk for filesPage/)
  rmSync(join(dir, 'assets/app.js'))
  assert.equal(run(['--report-only']).status, 1)
  assert.match(JSON.parse(readFileSync(output)).error, /ENOENT/)
})
