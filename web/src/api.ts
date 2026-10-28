import { invoke } from '@tauri-apps/api/core'
import type {
  AnalysisResult,
  Asset,
  ExportJob,
  ExportSettings,
  Font,
  Health,
  Project,
  TaskEvent,
  Timeline,
  Transcript,
} from './types'

let baseUrl = 'http://127.0.0.1:8080'
let token = ''

function isTauri(): boolean {
  return typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window
}

export async function initApi(): Promise<void> {
  if (isTauri()) {
    try {
      const info = await invoke<{ base_url: string; token: string }>('api_info')
      baseUrl = info.base_url
      token = info.token
      return
    } catch (err) {
      console.error('api_info failed, falling back to dev server', err)
      baseUrl = 'http://127.0.0.1:8137'
      return
    }
  }
  baseUrl = (import.meta.env.VITE_API_URL as string | undefined) ?? 'http://127.0.0.1:8080'
}

export function apiBaseUrl(): string {
  return baseUrl
}

export function apiToken(): string {
  return token
}

export async function waitForServer(timeoutMs = 25000): Promise<Health> {
  const deadline = Date.now() + timeoutMs
  let lastError: unknown = null
  while (Date.now() < deadline) {
    try {
      return await request<Health>('/api/health')
    } catch (err) {
      lastError = err
      await new Promise((resolve) => setTimeout(resolve, 350))
    }
  }
  throw lastError instanceof Error ? lastError : new Error('Backend tidak merespons')
}

function url(path: string, extra?: Record<string, string>): string {
  const u = new URL(path, baseUrl)
  if (token) u.searchParams.set('token', token)
  if (extra) {
    for (const [k, v] of Object.entries(extra)) u.searchParams.set(k, v)
  }
  return u.toString()
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers)
  if (token) headers.set('X-Autocut-Token', token)
  if (options.body && !(options.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const res = await fetch(url(path), { ...options, headers })
  if (!res.ok) {
    const text = (await res.text()).trim()
    throw new Error(text || `HTTP ${res.status}`)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export const api = {
  listProjects: () => request<Project[]>('/api/projects'),
  createProject: (name: string, width: number, height: number, fps: number) =>
    request<Project>('/api/projects', {
      method: 'POST',
      body: JSON.stringify({ name, width, height, fps }),
    }),
  getProject: (id: string) => request<Project>(`/api/projects/${id}`),
  renameProject: (id: string, name: string) =>
    request<Project>(`/api/projects/${id}`, { method: 'PATCH', body: JSON.stringify({ name }) }),
  deleteProject: (id: string) => request<void>(`/api/projects/${id}`, { method: 'DELETE' }),
  saveTimeline: (id: string, timeline: Timeline) =>
    request<Project>(`/api/projects/${id}/timeline`, {
      method: 'PUT',
      body: JSON.stringify(timeline),
    }),

  listAssets: (id: string) => request<Asset[]>(`/api/projects/${id}/assets`),
  importAsset: (id: string, file: File) => {
    const form = new FormData()
    form.append('file', file, file.name)
    return request<Asset>(`/api/projects/${id}/import`, { method: 'POST', body: form })
  },
  importAssetPath: (id: string, path: string) =>
    request<Asset>(`/api/projects/${id}/import-path`, {
      method: 'POST',
      body: JSON.stringify({ path }),
    }),
  deleteAsset: (id: string, assetId: string) =>
    request<void>(`/api/projects/${id}/assets/${assetId}`, { method: 'DELETE' }),

  transcript: (id: string) => request<Transcript>(`/api/projects/${id}/transcript`),
  latestAnalysis: (id: string) => request<AnalysisResult>(`/api/projects/${id}/analyze`),
  analyze: (id: string, assetId: string, useAI: boolean, useQA: boolean) =>
    request<{ status: string; task_id: string }>(`/api/projects/${id}/analyze`, {
      method: 'POST',
      body: JSON.stringify({ asset_id: assetId, use_ai: useAI, use_qa: useQA }),
    }),
  applyCuts: (id: string, assetId: string, cuts: unknown[], ripple = true) =>
    request<{ project: Project; removed_seconds: number; clips_affected: number }>(
      `/api/projects/${id}/autocut/apply`,
      { method: 'POST', body: JSON.stringify({ asset_id: assetId, cuts, ripple }) },
    ),
  generateCaptions: (id: string, replace = true) =>
    request<{ project: Project; caption_count: number }>(`/api/projects/${id}/captions`, {
      method: 'POST',
      body: JSON.stringify({ replace }),
    }),

  startExport: (id: string, settings: ExportSettings) =>
    request<ExportJob>(`/api/projects/${id}/export`, {
      method: 'POST',
      body: JSON.stringify({ settings }),
    }),
  latestExport: (id: string) => request<ExportJob>(`/api/projects/${id}/export/latest`),

  fonts: () => request<Font[]>('/api/fonts'),
}

export function assetUrl(projectId: string, assetId: string): string {
  return url(`/api/projects/${projectId}/assets/${assetId}/file`)
}

export function thumbUrl(projectId: string, assetId: string): string {
  return url(`/api/projects/${projectId}/assets/${assetId}/thumb`)
}

export function waveformUrl(projectId: string, assetId: string): string {
  return url(`/api/projects/${projectId}/assets/${assetId}/waveform`)
}

export function exportDownloadUrl(projectId: string, exportId: string, play = false): string {
  return url(`/api/projects/${projectId}/export/${exportId}/download`, play ? { play: '1' } : undefined)
}

export async function revealExport(projectId: string, outputName: string): Promise<boolean> {
  if (!isTauri()) return false
  await invoke('reveal_export', { projectId, outputName })
  return true
}

export function subscribeTask<T>(
  kind: 'analyze' | 'export',
  projectId: string,
  onEvent: (event: TaskEvent<T>) => void,
): () => void {
  const source = new EventSource(url(`/api/projects/${projectId}/${kind}/events`))
  let closed = false
  source.onmessage = (message) => {
    try {
      const data = JSON.parse(message.data) as TaskEvent<T>
      onEvent(data)
      if (data.status !== 'running') {
        closed = true
        source.close()
      }
    } catch {
      /* ignore malformed event */
    }
  }
  source.onerror = () => {
    if (closed) return
    closed = true
    source.close()
    onEvent({
      kind,
      task_id: '',
      project_id: projectId,
      status: 'failed',
      progress: 0,
      error: 'Koneksi progress terputus.',
    } as TaskEvent<T>)
  }
  return () => {
    closed = true
    source.close()
  }
}
