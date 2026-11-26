import { asPlainObject } from './decode'

export interface RecomText {
  en: string
  zh: string
}

export interface Recom {
  id: string
  name: string
  url: string
  icon: string
  slogan: RecomText
  promotion: RecomText
  tooltip: RecomText
}

export interface RecomRead {
  recom: Recom
  degraded: boolean
}

export interface RecomsRead {
  recoms: Recom[]
  degraded: boolean
}

export const RECOM_REPOSITORY_ROOT = ''
export const RECOM_DATA_URL = ''
export const RECOM_DATA_MIRROR_URL = ''

export const DEFAULT_RECOM: Recom = {
  id: 'cpa-context',
  name: 'cpa-context',
  url: 'https://github.com/thebtf/cpa-context',
  icon: 'https://cdn.simpleicons.org/anthropic',
  slogan: {
    zh: '理解你的上下文。',
    en: 'Know what your context carries.',
  },
  promotion: {
    zh: '把 dsh-context 的界面带到 CLIProxyAPI。',
    en: 'The dsh-context dashboard, ported to CLIProxyAPI.',
  },
  tooltip: {
    zh: '所有数字都由本插件在代理层观测得到。',
    en: 'Every number here is observed by this plugin at the proxy.',
  },
}

function textOf(value: unknown): RecomText | null {
  const record = asPlainObject(value)
  if (record === null || typeof record.en !== 'string' || record.en.trim() === ''
    || typeof record.zh !== 'string' || record.zh.trim() === '') return null
  return { en: record.en, zh: record.zh }
}

function httpsOf(value: unknown): string | null {
  if (typeof value !== 'string') return null
  try {
    const url = new URL(value)
    return url.protocol === 'https:' ? url.href : null
  } catch {
    return null
  }
}

function iconOf(value: unknown): string | null {
  if (typeof value !== 'string' || value.trim() === '') return null
  try {
    const url = new URL(value, RECOM_REPOSITORY_ROOT)
    return url.protocol === 'https:' ? url.href : null
  } catch {
    return null
  }
}

export function recomOf(value: unknown): Recom | null {
  const record = asPlainObject(value)
  if (record === null || typeof record.id !== 'string' || record.id.trim() === ''
    || typeof record.name !== 'string' || record.name.trim() === '') return null
  const url = httpsOf(record.url)
  const icon = iconOf(record.icon)
  const slogan = textOf(record.slogan)
  const promotion = textOf(record.promotion)
  const tooltip = textOf(record.tooltip)
  if (url === null || icon === null || slogan === null || promotion === null || tooltip === null) return null
  return { id: record.id, name: record.name, url, icon, slogan, promotion, tooltip }
}

export function recomsOfPayload(value: unknown): Recom[] {
  if (!Array.isArray(value)) return []
  const recoms: Recom[] = []
  for (const entry of value) {
    try {
      const recom = recomOf(entry)
      if (recom !== null) recoms.push(recom)
    } catch {
      continue
    }
  }
  return recoms
}
