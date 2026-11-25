import type {
  AddonModule,
  AddonRepo,
  BackupJob,
  BackupRecord,
  BackupSchedule,
  ContainerStats,
  ContainerSummary,
  DatabaseInfo,
  DeployProgress,
  OdooInstance,
  OdooVersionInfo,
  PanelSettings,
  SystemInfo,
} from '../types'

const TOKEN_KEY = 'runodoo_token'

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const token = getToken()
  const headers: Record<string, string> = {
    ...(options.headers as Record<string, string>),
  }
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }
  if (!(options.body instanceof FormData) && !headers['Content-Type']) {
    headers['Content-Type'] = 'application/json'
  }

  const res = await fetch(path, { ...options, headers })
  if (res.status === 401) {
    clearToken()
    window.dispatchEvent(new Event('runodoo:unauthorized'))
    throw new Error('Unauthorized')
  }

  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error(data.error || `Request failed (${res.status})`)
  }
  return data as T
}

async function streamExecRequest(
  path: string,
  body: unknown,
  onChunk: (text: string) => void,
  options?: { signal?: AbortSignal; onPwd?: (pwd: string) => void }
): Promise<number> {
  const token = getToken()
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }
  const res = await fetch(path, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
    signal: options?.signal,
  })
  if (res.status === 401) {
    clearToken()
    window.dispatchEvent(new Event('runodoo:unauthorized'))
    throw new Error('Unauthorized')
  }
  if (!res.ok) {
    const data = await res.json().catch(() => ({}))
    throw new Error(data.error || `Request failed (${res.status})`)
  }
  if (!res.body) {
    throw new Error('Streaming not supported by browser')
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let full = ''
  let emittedLen = 0
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    full += decoder.decode(value, { stream: true })
    const markerIdx = full.search(/\n__RUNODOO_(EXIT|ERROR|PWD)__:/)
    const visibleEnd = markerIdx >= 0 ? markerIdx : full.length
    if (visibleEnd > emittedLen) {
      onChunk(full.slice(emittedLen, visibleEnd))
      emittedLen = visibleEnd
    }
  }
  full += decoder.decode()
  const pwdMatch = full.match(/\n__RUNODOO_PWD__:([^\n]*)/)
  if (pwdMatch && options?.onPwd) {
    options.onPwd(pwdMatch[1])
  }
  const errMatch = full.match(/\n__RUNODOO_ERROR__:([^\n]*)/)
  if (errMatch) {
    throw new Error(errMatch[1] || 'Command execution failed')
  }
  const exitMatch = full.match(/\n__RUNODOO_EXIT__:(-?\d+)/)
  return exitMatch ? parseInt(exitMatch[1], 10) : 0
}

export type CleanMissingMode = 'all' | 'custom' | 'stalled'

export interface StalledModuleInfo {
  name: string
  state: string
  onDisk: boolean
}

export interface CleanMissingDBResult {
  dbName: string
  status: string
  message?: string
  mode?: CleanMissingMode
  totalOnDisk: number
  totalInDb: number
  detectedMissing: string[]
  removed: string[]
  skippedInstalled: string[]
  stalledModules: StalledModuleInfo[]
}

export interface PythonPackageInfo {
  name: string
  version: string
  location?: string
  isCustom: boolean
}

export interface PythonEnvInfo {
  pythonVersion: string
  pipAvailable: boolean
  pipVersion?: string
  userSite: string
  packages: PythonPackageInfo[]
}

export const api = {
  // Auth
  login: (password: string) =>
    request<{ token: string; passwordChanged: boolean }>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ password }),
    }),
  me: () => request<{ user: string; passwordChanged: boolean; rootDomain: string }>('/api/auth/me'),
  changePassword: (currentPassword: string, newPassword: string) =>
    request<{ ok: boolean }>('/api/auth/change-password', {
      method: 'POST',
      body: JSON.stringify({ currentPassword, newPassword }),
    }),

  // System & Settings
  systemInfo: () => request<SystemInfo>('/api/system/info'),
  versions: () => request<OdooVersionInfo[]>('/api/system/versions'),
  getSettings: () => request<PanelSettings>('/api/settings'),
  updateSettings: (payload: Partial<PanelSettings>) =>
    request<PanelSettings>('/api/settings', {
      method: 'PUT',
      body: JSON.stringify(payload),
    }),
  changePanelPort: (port: number) =>
    request<{ ok: boolean; port: string; recreating: boolean }>('/api/settings/port', {
      method: 'POST',
      body: JSON.stringify({ port }),
    }),
  enablePanelSsl: () => request<{ ok: boolean; domain: string; logs: string }>('/api/settings/enable-ssl', { method: 'POST' }),
  syncProxy: () => request<{ ok: boolean }>('/api/proxy/sync', { method: 'POST' }),

  // Instances
  listInstances: () => request<OdooInstance[]>('/api/instances'),
  getInstance: (id: string) => request<OdooInstance>(`/api/instances/${id}`),
  createInstance: (payload: Record<string, unknown>) =>
    request<OdooInstance>('/api/instances', { method: 'POST', body: JSON.stringify(payload) }),
  updateInstance: (id: string, payload: Record<string, unknown>) =>
    request<OdooInstance>(`/api/instances/${id}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteInstance: (id: string, deleteData: boolean) =>
    request<{ ok: boolean }>(`/api/instances/${id}?deleteData=${deleteData}`, { method: 'DELETE' }),
  startInstance: (id: string) => request<{ ok: boolean }>(`/api/instances/${id}/start`, { method: 'POST' }),
  stopInstance: (id: string) => request<{ ok: boolean }>(`/api/instances/${id}/stop`, { method: 'POST' }),
  restartInstance: (id: string) => request<{ ok: boolean }>(`/api/instances/${id}/restart`, { method: 'POST' }),
  instanceLogs: (id: string, tail = 200) => request<{ logs: string }>(`/api/instances/${id}/logs?tail=${tail}`),
  deployStatus: (id: string) => request<DeployProgress>(`/api/instances/${id}/deploy-status`),
  instanceStats: (id: string) => request<ContainerStats>(`/api/instances/${id}/stats`),
  instanceConf: (id: string) =>
    request<{
      generatedConf: string
      customConf: string
      confOptions: Record<string, string>
      orderedKeys: string[]
      envVars: Record<string, string>
    }>(`/api/instances/${id}/conf`),
  issueInstanceSsl: (id: string, domain: string, email?: string) =>
    request<{ ok: boolean; logs: string }>(`/api/instances/${id}/ssl`, {
      method: 'POST',
      body: JSON.stringify({ domain, email }),
    }),
  updateAppsList: (id: string) =>
    request<{ ok: boolean; updated: string[]; output: string }>(`/api/instances/${id}/update-apps-list`, {
      method: 'POST',
    }),
  cleanMissingModules: (id: string, mode: CleanMissingMode = 'custom') =>
    request<{
      ok: boolean
      mode: CleanMissingMode
      results: CleanMissingDBResult[]
      output: string
    }>(`/api/instances/${id}/clean-missing-modules`, {
      method: 'POST',
      body: JSON.stringify({ mode }),
    }),
  upgradeModules: (id: string, dbName: string, modules: string[]) =>
    request<{ ok: boolean; output: string }>(`/api/instances/${id}/upgrade-modules`, {
      method: 'POST',
      body: JSON.stringify({ dbName, modules }),
    }),
  upgradeModulesStream: async (
    id: string,
    dbName: string,
    modules: string[],
    onChunk: (text: string) => void
  ): Promise<number> => {
    const exitCode = await streamExecRequest(
      `/api/instances/${id}/upgrade-modules/stream`,
      { dbName, modules },
      onChunk
    )
    if (exitCode !== 0) {
      throw new Error(`Module upgrade failed (exit ${exitCode})`)
    }
    return exitCode
  },
  execStream: (
    id: string,
    command: string,
    cwd: string,
    onChunk: (text: string) => void,
    options?: { signal?: AbortSignal; onPwd?: (pwd: string) => void }
  ) => streamExecRequest(`/api/instances/${id}/exec/stream`, { command, cwd }, onChunk, options),
  listPythonPackages: (id: string) =>
    request<PythonEnvInfo>(`/api/instances/${id}/python-packages`),
  installPythonPackages: (
    id: string,
    payload: { packages?: string[]; requirements?: string; upgrade?: boolean }
  ) =>
    request<{ ok: boolean; output: string }>(`/api/instances/${id}/python-packages/install`, {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  uninstallPythonPackages: (id: string, packages: string[]) =>
    request<{ ok: boolean; output: string }>(`/api/instances/${id}/python-packages/uninstall`, {
      method: 'POST',
      body: JSON.stringify({ packages }),
    }),

  // Databases
  listAllDatabases: () => request<DatabaseInfo[]>('/api/databases'),
  listInstanceDatabases: (id: string) => request<DatabaseInfo[]>(`/api/instances/${id}/databases`),
  createDatabase: (
    instanceId: string,
    payload: {
      name: string
      login: string
      password: string
      phone?: string
      lang?: string
      countryCode?: string
      demo?: boolean
    }
  ) =>
    request<{ ok: boolean }>(`/api/instances/${instanceId}/databases`, {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  duplicateDatabase: (instanceId: string, sourceName: string, targetName: string) =>
    request<{ ok: boolean }>(`/api/instances/${instanceId}/databases/duplicate`, {
      method: 'POST',
      body: JSON.stringify({ sourceName, targetName }),
    }),
  dropDatabase: (instanceId: string, dbName: string) =>
    request<{ ok: boolean }>(`/api/instances/${instanceId}/databases/${encodeURIComponent(dbName)}`, {
      method: 'DELETE',
    }),

  // Addons
  listAddons: () => request<AddonRepo[]>('/api/addons'),
  cloneAddon: (payload: { name: string; gitUrl: string; branch?: string; token?: string }) =>
    request<AddonRepo>('/api/addons/git', { method: 'POST', body: JSON.stringify(payload) }),
  uploadAddonZip: (name: string, file: File) => {
    const fd = new FormData()
    fd.append('name', name)
    fd.append('file', file)
    return request<AddonRepo>('/api/addons/upload', { method: 'POST', body: fd })
  },
  pullAddon: (id: string) => request<AddonRepo>(`/api/addons/${id}/pull`, { method: 'POST' }),
  rescanAddon: (id: string) => request<AddonRepo>(`/api/addons/${id}/rescan`, { method: 'POST' }),
  deleteAddon: (id: string) => request<{ ok: boolean }>(`/api/addons/${id}`, { method: 'DELETE' }),

  // Backups
  listBackups: () => request<BackupRecord[]>('/api/backups'),
  createBackup: (instanceId: string, dbName: string) =>
    request<BackupJob>('/api/backups', { method: 'POST', body: JSON.stringify({ instanceId, dbName }) }),
  listBackupJobs: () => request<BackupJob[]>('/api/backup-jobs'),
  clearBackupJobs: () => request<{ ok: boolean }>('/api/backup-jobs', { method: 'DELETE' }),
  downloadBackupUrl: (id: string) => `/api/backups/${id}/download?token=${encodeURIComponent(getToken() || '')}`,
  restoreBackup: (id: string, instanceId: string, targetDbName: string) =>
    request<BackupJob>(`/api/backups/${id}/restore`, {
      method: 'POST',
      body: JSON.stringify({ instanceId, targetDbName }),
    }),
  uploadRestoreBackup: (instanceId: string, targetDbName: string, file: File) => {
    const fd = new FormData()
    fd.append('instanceId', instanceId)
    fd.append('targetDbName', targetDbName)
    fd.append('file', file)
    return request<BackupJob>('/api/backups/upload-restore', { method: 'POST', body: fd })
  },
  deleteBackup: (id: string) => request<{ ok: boolean }>(`/api/backups/${id}`, { method: 'DELETE' }),
  listSchedules: () => request<BackupSchedule[]>('/api/backup-schedules'),
  createSchedule: (payload: { instanceId: string; dbName: string; cronExpression: string; retentionCount: number }) =>
    request<BackupSchedule>('/api/backup-schedules', { method: 'POST', body: JSON.stringify(payload) }),
  deleteSchedule: (id: string) => request<{ ok: boolean }>(`/api/backup-schedules/${id}`, { method: 'DELETE' }),

  // Containers
  listContainers: (managed = false) => request<ContainerSummary[]>(`/api/containers?managed=${managed}`),
  startContainer: (id: string) => request<{ ok: boolean }>(`/api/containers/${id}/start`, { method: 'POST' }),
  stopContainer: (id: string) => request<{ ok: boolean }>(`/api/containers/${id}/stop`, { method: 'POST' }),
  restartContainer: (id: string) => request<{ ok: boolean }>(`/api/containers/${id}/restart`, { method: 'POST' }),
  containerLogs: (id: string, tail = 200) => request<{ logs: string }>(`/api/containers/${id}/logs?tail=${tail}`),
  containerStats: (id: string) => request<ContainerStats>(`/api/containers/${id}/stats`),
}
