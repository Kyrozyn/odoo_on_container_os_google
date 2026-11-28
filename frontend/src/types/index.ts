export type OdooVersion = '17' | '18' | '19' | '20'

export interface OdooVersionInfo {
  version: OdooVersion
  label: string
  defaultImage: string
  status: string
}

export type DBMode = 'shared' | 'dedicated'

export interface DomainConfig {
  domain: string
  hasSsl: boolean
  forceHttps: boolean
}

export interface ResourceConfig {
  workers: number
  limitMemorySoft: number
  limitMemoryHard: number
  limitTimeCpu: number
  limitTimeReal: number
  maxCronThreads: number
}

export interface OdooInstance {
  id: string
  name: string
  version: OdooVersion
  image: string
  containerName: string
  httpPort: number
  longpollingPort: number
  exposePort: boolean
  domains: DomainConfig[]
  useRootSubdomain: boolean
  rootSubdomainSsl: boolean
  dbMode: DBMode
  dbContainerName: string
  dbHost: string
  dbPort: number
  dbUser: string
  dbPassword: string
  defaultDbName: string
  adminPassword: string
  addonRepoIds: string[]
  envVars: Record<string, string>
  customConf: string
  resources: ResourceConfig
  createdAt: string
  updatedAt: string
  status: string
  stateText: string
  dbStatus: string
  containerId: string
  primaryUrl: string
}

export interface AddonModule {
  technicalName: string
  name: string
  version: string
  summary: string
  author: string
  depends: string[]
  relPath: string
}

export interface AddonRepo {
  id: string
  name: string
  sourceType: 'git' | 'zip' | 'local'
  gitUrl?: string
  gitBranch?: string
  localPath: string
  modules: AddonModule[]
  updatedAt: string
  createdAt: string
}

export interface DatabaseInfo {
  name: string
  owner: string
  sizeBytes: number
  sizePretty: string
  instanceId?: string
  instanceName?: string
  containerName: string
}

export interface PostgresMetrics {
  containerName: string
  dbMode: DBMode
  postgresVersion: string
  cpuPercent: number
  memoryUsageMb: number
  memoryLimitMb: number
  memoryPercent: number
  activeConnections: number
  idleConnections: number
  totalConnections: number
  maxConnections: number
  cacheHitRatio: number
  totalSizeBytes: number
  totalSizePretty: string
  databaseCount: number
  uptime: string
}

export interface PostgresSetting {
  name: string
  setting: string
  unit: string
  shortDesc: string
  context: string
  pendingRestart: boolean
  source: string
}

export interface MonitoringConfig {
  installed: boolean
  prometheusContainer: string
  grafanaContainer: string
  pgExporterContainer: string
  exposePort: boolean
  httpPort: number
  useRootSubdomain: boolean
  rootSubdomainSsl: boolean
  rootSubdomainForceHttps: boolean
  domains: DomainConfig[]
  grafanaUser: string
  grafanaPassword: string
  allowAnonymous: boolean
}

export interface ScrapeTarget {
  job: string
  instance: string
  health: string
  lastError?: string
  scrapeUrl: string
}

export interface MonitoringStatus {
  config: MonitoringConfig
  prometheusStatus: string
  grafanaStatus: string
  pgExporterStatus: string
  primaryUrl: string
  rootSubdomainUrl: string
  targets: ScrapeTarget[]
}

export interface BackupRecord {
  id: string
  instanceId: string
  instanceName: string
  dbName: string
  fileName: string
  sizeBytes: number
  hasFilestore: boolean
  source: string
  createdAt: string
}

export interface BackupSchedule {
  id: string
  instanceId: string
  dbName: string
  cronExpression: string
  retentionCount: number
  enabled: boolean
  lastRunAt?: string
  lastStatus?: string
  createdAt: string
}

export interface BackupJob {
  id: string
  type: 'backup' | 'restore'
  source: string
  status: 'queued' | 'running' | 'completed' | 'failed'
  instanceId: string
  instanceName: string
  dbName: string
  backupId?: string
  fileName?: string
  step?: string
  error?: string
  scheduleId?: string
  createdAt: string
  startedAt?: string
  finishedAt?: string
}

export interface PanelSettings {
  rootDomain: string
  panelSubdomain: string
  panelHasSsl: boolean
  panelForceHttps: boolean
  disableDirectPort: boolean
  letsEncryptEmail: string
  panelPort?: string
  passwordChanged: boolean
  tokenVersion?: number
  totpEnabled: boolean
  loginRateLimitEnabled: boolean
  loginMaxAttempts: number
  loginLockoutMinutes: number
}

export interface SystemInfo {
  hostname: string
  osPretty: string
  isCos: boolean
  arch: string
  cpuCores: number
  totalRamMb: number
  availableRamMb: number
  diskTotalGb: number
  diskFreeGb: number
  dockerConnected: boolean
  dockerVersion: string
  panelPort: string
}

export interface ContainerSummary {
  Id: string
  Names: string[]
  Image: string
  State: string
  Status: string
  Created: number
  Labels: Record<string, string>
  Ports: { IP?: string; PrivatePort: number; PublicPort?: number; Type: string }[]
}

export interface ContainerStats {
  cpuPercent: number
  memoryUsageMb: number
  memoryLimitMb: number
  memoryPercent: number
}

export interface DeployProgress {
  instanceId: string
  status: 'idle' | 'deploying' | 'running' | 'failed'
  step: string
  percent: number
  logs: string[]
  error?: string
  startedAt?: string
  finishedAt?: string
}
