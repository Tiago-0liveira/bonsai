import { execFileSync } from 'node:child_process'
import { createReadStream, existsSync, mkdtempSync, statSync } from 'node:fs'
import { createServer } from 'node:https'
import { tmpdir } from 'node:os'
import { extname, join, normalize, resolve, sep } from 'node:path'

const dir = mkdtempSync(join(tmpdir(), 'bonsai-e2e-'))
const keyPath = join(dir, 'key.pem')
const certPath = join(dir, 'cert.pem')
execFileSync('openssl', [
  'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
  '-keyout', keyPath, '-out', certPath, '-days', '1',
  '-subj', '/CN=127.0.0.1',
  '-addext', 'subjectAltName=IP:127.0.0.1',
], { stdio: 'ignore' })

const dist = resolve('dist')
const securityHeaders = {
  'Content-Security-Policy': "default-src 'self'; script-src 'self'; style-src 'self'; style-src-elem 'self'; style-src-attr 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self' https://api.bonsai.dev http://127.0.0.1:7001 ws://127.0.0.1:7001; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' https://api.bonsai.dev",
  'Referrer-Policy': 'no-referrer',
  'X-Content-Type-Options': 'nosniff',
  'X-Frame-Options': 'DENY',
  'Permissions-Policy': 'local-network-access=(self)',
}
const types = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.webp': 'image/webp',
  '.woff2': 'font/woff2',
}

const server = createServer({
  key: await import('node:fs').then(fs => fs.readFileSync(keyPath)),
  cert: await import('node:fs').then(fs => fs.readFileSync(certPath)),
}, (request, response) => {
  for (const [name, value] of Object.entries(securityHeaders)) response.setHeader(name, value)
  response.setHeader('Cache-Control', 'no-store')

  const pathname = decodeURIComponent(new URL(request.url ?? '/', 'https://127.0.0.1').pathname)
  const candidate = resolve(dist, normalize(pathname).replace(/^[/\\]+/, ''))
  const insideDist = candidate === dist || candidate.startsWith(dist + sep)
  let file = insideDist && existsSync(candidate) && statSync(candidate).isFile()
    ? candidate
    : join(dist, 'index.html')

  if (!file.startsWith(dist + sep) || !existsSync(file)) {
    response.writeHead(404)
    response.end('Not found')
    return
  }
  response.setHeader('Content-Type', types[extname(file)] ?? 'application/octet-stream')
  createReadStream(file).pipe(response)
})

server.listen(4173, '127.0.0.1')
for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => server.close(() => process.exit(0)))
}
