#!/usr/bin/env node
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { execFileSync } from 'node:child_process'
import { dirname, isAbsolute, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import { parseArgs } from 'node:util'
import { gzipSync } from 'node:zlib'

const web = resolve(dirname(fileURLToPath(import.meta.url)), '..')
export const selectors = {
  entry: { facade: 'index.html', name: 'index' },
  application: { facade: 'src/app/ApplicationRoot.tsx', name: 'ApplicationRoot' },
  workspace: { facade: 'src/features/workspace/WorkspacePage.tsx', name: 'WorkspacePage', historicalParent: 'application' },
  pullRequests: { facade: 'src/features/github/PullRequestsRoute.tsx', name: 'PullRequestsRoute' },
  xterm: { name: 'xterm' },
  addonFit: { name: 'addon-fit' },
  landing: { facade: 'src/marketing/LandingPage.tsx', name: 'LandingPage' },
  ...Object.fromEntries([
    ['filesPage', 'files/FilesPage'], ['filesPanel', 'files/FilesPanel'],
    ['pullRequestDetail', 'github/PullRequestDetail'], ['settings', 'settings/SettingsPage'],
    ['logs', 'logs/LogsPage'], ['agentTerminal', 'terminal/AgentTerminal'],
    ['processTerminal', 'terminal/ProcessTerminal'], ['commandPalette', 'command-palette/CommandPalette'],
    ['createWorktree', 'workspace/CreateWorktreeDialog'], ['deleteWorktree', 'workspace/DeleteWorktreeDialog'],
    ['startAgent', 'workspace/StartAgentDialog'], ['startProcess', 'workspace/StartProcessDialog'],
    ['envEditor', 'workspace/EnvEditor'],
  ].map(([id, source]) => [id, {
    facade: `src/features/${source}.tsx`, name: source.split('/').at(-1),
    historicalParent: source === 'github/PullRequestDetail' ? 'pullRequests' : 'application',
  }])),
}

function validFile(file) {
  return typeof file === 'string' && file.length > 0 && !isAbsolute(file)
    && !file.includes('\\') && !file.split('/').some(part => part === '..' || part === '.')
}

export function validateManifest(graph) {
  if (!graph || typeof graph !== 'object' || Array.isArray(graph) || !Object.keys(graph).length) {
    throw new Error('Manifest must be a nonempty object')
  }
  for (const [key, chunk] of Object.entries(graph)) {
    if (!chunk || typeof chunk !== 'object' || !validFile(chunk.file)) throw new Error(`Invalid file in manifest chunk ${key}`)
    for (const field of ['imports', 'dynamicImports', 'css']) {
      if (chunk[field] !== undefined && (!Array.isArray(chunk[field]) || chunk[field].some(value => typeof value !== 'string'))) {
        throw new Error(`Invalid ${field} in manifest chunk ${key}`)
      }
    }
    for (const dependency of [...(chunk.imports ?? []), ...(chunk.dynamicImports ?? [])]) {
      if (!Object.hasOwn(graph, dependency)) throw new Error(`Missing manifest dependency ${dependency} imported by ${key}`)
    }
    if ((chunk.css ?? []).some(file => !validFile(file) || !file.endsWith('.css'))) throw new Error(`Invalid CSS in manifest chunk ${key}`)
  }
}

export function resolveChunk(graph, selector, label) {
  const entries = Object.entries(graph).filter(([, chunk]) => chunk.file.endsWith('.js'))
  let matches = selector.facade ? entries.filter(([key, chunk]) => key === selector.facade || chunk.src === selector.facade) : []
  if (!matches.length) matches = entries.filter(([, chunk]) => chunk.name === selector.name)
  if (matches.length > 1) throw new Error(`Ambiguous chunk for ${label}: ${matches.map(([key]) => key).join(', ')}`)
  return matches[0]?.[0]
}

export function staticFiles(graph, roots) {
  const visited = new Set(), files = new Set()
  const visit = key => {
    if (visited.has(key)) return
    const chunk = graph[key]
    if (!chunk) throw new Error(`Missing manifest chunk ${key}`)
    visited.add(key)
    if (chunk.file.endsWith('.js')) files.add(chunk.file)
    for (const dependency of chunk.imports ?? []) visit(dependency)
  }
  roots.forEach(visit)
  return [...files].sort()
}

export function measureBundle(graph, readAsset, { reportOnly = false, chunks = selectors } = {}) {
  validateManifest(graph)
  const measured = new Map()
  const measure = files => {
    files = [...new Set(files)].sort()
    for (const file of files) {
      if (!measured.has(file)) {
        const bytes = readAsset(file)
        measured.set(file, { rawBytes: bytes.length, gzipBytes: gzipSync(bytes, { level: 9 }).length })
      }
    }
    return { files, rawBytes: files.reduce((total, file) => total + measured.get(file).rawBytes, 0), gzipBytes: files.reduce((total, file) => total + measured.get(file).gzipBytes, 0) }
  }
  const resolved = {}, output = {}
  for (const [id, selector] of Object.entries(chunks)) {
    const key = resolveChunk(graph, selector, id)
    if (!key) {
      if (reportOnly && selector.historicalParent) {
        output[id] = { includedIn: selector.historicalParent }
        continue
      }
      throw new Error(`Missing chunk for ${id} (${selector.facade ?? selector.name})`)
    }
    resolved[id] = key
    output[id] = { manifestKey: key, ...measure([graph[key].file]), staticClosure: measure(staticFiles(graph, [key])) }
  }
  // Include any additional dynamic entries so a newly introduced lazy feature
  // is visible in review even before it receives a named selector.
  const dynamicChunks = {}
  for (const [key, chunk] of Object.entries(graph)) {
    if (chunk.isDynamicEntry && chunk.file.endsWith('.js')) {
      dynamicChunks[key] = { ...measure([chunk.file]), staticClosure: measure(staticFiles(graph, [key])) }
    }
  }
  const css = new Set()
  for (const chunk of Object.values(graph)) {
    if (chunk.file.endsWith('.css')) css.add(chunk.file)
    for (const file of chunk.css ?? []) css.add(file)
  }
  return {
    schemaVersion: 1,
    chunks: output,
    dynamicChunks,
    metrics: {
      application: measure(output.application.files),
      applicationStaticClosure: output.application.staticClosure,
      entry: measure(output.entry.files),
      entryStaticClosure: output.entry.staticClosure,
      firstScreenJs: measure(staticFiles(graph, ['entry', 'application', 'workspace'].map(id => resolved[id]).filter(Boolean))),
      css: measure([...css]),
    },
  }
}

function positiveInteger(value, name) {
  if (!Number.isSafeInteger(value) || value <= 0) throw new Error(`Invalid positive integer budget: ${name}`)
  return BigInt(value)
}

export function checkBudgets(report, budget) {
  if (budget.schemaVersion !== 1 || !budget.rationale?.trim()) throw new Error('Budget needs schemaVersion 1 and an explicit rationale')
  const gates = [
    ['application vs Phase 4 (70%)', 'application', budget.references?.phase4?.metrics?.application?.rawBytes, 70, 100],
    ['application vs approved baseline (110%)', 'application', budget.baseline?.metrics?.application?.rawBytes, 110, 100],
    ['application static closure vs approved baseline (110%)', 'applicationStaticClosure', budget.baseline?.metrics?.applicationStaticClosure?.rawBytes, 110, 100],
    ['first-screen JS vs pre-P7', 'firstScreenJs', budget.references?.preP7?.metrics?.firstScreenJs?.rawBytes, 1, 1],
  ]
  if (budget.firstScreenDecision) {
    if (!budget.firstScreenDecision.reason?.trim() || !budget.firstScreenDecision.sourceRevision?.trim()) throw new Error('First-screen budget override needs a reason and sourceRevision')
    gates[3] = ['first-screen JS vs documented decision', 'firstScreenJs', budget.firstScreenDecision.maxRawBytes, 1, 1]
  }
  return gates.map(([name, metric, reference, numerator, denominator]) => {
    const base = positiveInteger(reference, name)
    const actual = positiveInteger(report.metrics[metric].rawBytes, metric)
    const maximum = base * BigInt(numerator) / BigInt(denominator)
    return { name, metric, rawBytes: Number(actual), maxRawBytes: Number(maximum), passed: actual * BigInt(denominator) <= base * BigInt(numerator) }
  })
}

export function main(args = process.argv.slice(2)) {
  const { values } = parseArgs({ args, options: {
    'build-dir': { type: 'string', default: resolve(web, 'dist') },
    budget: { type: 'string', default: resolve(web, 'bundle-budget.json') },
    json: { type: 'string' }, 'report-only': { type: 'boolean', default: false },
    'source-revision': { type: 'string' }, help: { type: 'boolean' },
  } })
  if (values.help) {
    console.log('Usage: node scripts/check-bundle-budget.mjs [--build-dir DIR] [--budget FILE] [--json FILE] [--report-only] [--source-revision SHA]')
    return 0
  }
  let report
  const writeReport = () => {
    if (values.json) {
      mkdirSync(dirname(resolve(values.json)), { recursive: true })
      writeFileSync(values.json, JSON.stringify(report, null, 2) + '\n')
    }
  }
  try {
    const buildDir = resolve(values['build-dir'])
    const graph = JSON.parse(readFileSync(resolve(buildDir, '.vite/manifest.json'), 'utf8'))
    report = measureBundle(graph, file => {
      const path = resolve(buildDir, file)
      if (!path.startsWith(buildDir + sep)) throw new Error(`Artifact escapes build directory: ${file}`)
      return readFileSync(path)
    }, { reportOnly: values['report-only'] })
    let sourceRevision = values['source-revision']
    if (!sourceRevision) {
      try { sourceRevision = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: web, encoding: 'utf8' }).trim() } catch { sourceRevision = null }
    }
    Object.assign(report, {
      sourceRevision, mode: values['report-only'] ? 'report-only' : 'enforced',
      toolchain: { node: process.version, pnpm: process.env.npm_config_user_agent?.match(/pnpm\/([^ ]+)/)?.[1] ?? null, zlib: process.versions.zlib, platform: process.platform, arch: process.arch },
      definitions: { staticClosure: 'Root chunk plus all static JS imports, deduplicated by emitted file.', firstScreenJs: 'Union of entry, application and default workspace static closures.', gzip: 'Sum of separately gzipped emitted files at level 9, with zero timestamp.' },
    })
    report.checks = values['report-only'] ? [] : checkBudgets(report, JSON.parse(readFileSync(values.budget, 'utf8')))
    report.passed = report.checks.every(check => check.passed)
    writeReport()
    for (const [name, metric] of Object.entries(report.metrics)) console.log(`${name}: ${metric.rawBytes} raw B / ${metric.gzipBytes} gzip B`)
    for (const check of report.checks) console.log(`${check.passed ? 'PASS' : 'FAIL'} ${check.name}: ${check.rawBytes} B <= ${check.maxRawBytes} B`)
    return report.passed ? 0 : 1
  } catch (error) {
    report = { ...report, passed: false, error: error.message }
    writeReport()
    console.error(`Bundle budget: ${error.message}`)
    return 1
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) process.exitCode = main()
