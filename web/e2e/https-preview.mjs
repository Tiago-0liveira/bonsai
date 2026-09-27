import { execFileSync, spawn } from 'node:child_process'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const dir = mkdtempSync(join(tmpdir(), 'bonsai-e2e-'))
const key = join(dir, 'key.pem')
const cert = join(dir, 'cert.pem')

execFileSync('openssl', [
  'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
  '-keyout', key, '-out', cert, '-days', '1',
  '-subj', '/CN=127.0.0.1',
  '-addext', 'subjectAltName=IP:127.0.0.1',
], { stdio: 'ignore' })

const command = process.platform === 'win32' ? 'pnpm.cmd' : 'pnpm'
const child = spawn(command, ['vite', 'preview', '--host', '127.0.0.1', '--port', '4173'], {
  stdio: 'inherit',
  env: {
    ...process.env,
    BONSAI_E2E_HTTPS_KEY: key,
    BONSAI_E2E_HTTPS_CERT: cert,
  },
})

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => child.kill(signal))
}
child.on('exit', code => process.exit(code ?? 0))
