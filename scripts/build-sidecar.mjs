#!/usr/bin/env node
// Builds the Go API server as a Tauri sidecar binary.
// Tauri expects external binaries named <name>-<target-triple>[.exe].

import { execFileSync } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')

function hostTriple() {
  if (process.env.TAURI_ENV_TARGET_TRIPLE) return process.env.TAURI_ENV_TARGET_TRIPLE
  if (process.env.CARGO_BUILD_TARGET) return process.env.CARGO_BUILD_TARGET
  try {
    const info = execFileSync('rustc', ['-vV'], { encoding: 'utf8' })
    const host = info.match(/^host:\s*(\S+)/m)
    if (host) return host[1]
  } catch {
    /* rustc not installed yet; fall back to the usual Windows host */
  }
  if (process.env.PROCESSOR_ARCHITECTURE === 'ARM64') return 'aarch64-pc-windows-msvc'
  return 'x86_64-pc-windows-msvc'
}

const triple = hostTriple()
const ext = process.platform === 'win32' ? '.exe' : ''
const outDir = join(root, 'src-tauri', 'binaries')
const outFile = join(outDir, `autocut-server-${triple}${ext}`)

mkdirSync(outDir, { recursive: true })

console.log(`[sidecar] building Go server -> ${outFile}`)
execFileSync('go', ['build', '-trimpath', '-ldflags', '-s -w', '-o', outFile, './cmd/server'], {
  cwd: root,
  stdio: 'inherit',
})
console.log('[sidecar] done')
