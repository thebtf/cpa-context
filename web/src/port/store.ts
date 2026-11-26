// The dsh build mounts these components through cordis slots and feeds them
// from dsh's own session projections. CPA has neither, so this module supplies
// the same props from the plugin's JSON API: useProjection is the single entry
// point every component uses to read its data, and the projection keys are the
// ones the ported client asks for.
import { useSyncExternalStore } from 'react'
import type { ContextTimeline } from '../shared/types'

export interface SessionTableRow {
  id: string
  title?: string
  model?: string
  cwd?: string
  parentId?: string
  origin?: 'root' | 'subagent' | 'fork'
  firstSeen?: string
  lastSeen?: string
  requests?: number
  failed?: number
  lastContextTokens?: number
  peakContextTokens?: number
  cacheHitRequests?: number
  cacheMissRequests?: number
  cacheReadTokens?: number
  inputTokens?: number
  outputTokens?: number
}

export interface ProjectionBundle {
  contextTimeline?: ContextTimeline | null
  contextPressure?: Record<string, unknown> | null
  contextBreakdown?: Record<string, unknown> | null
  contextHeaders?: Record<string, unknown> | null
  tokenUsage?: Record<string, unknown> | null
  agentTeam?: Record<string, unknown> | null
  subagentCatalog?: Record<string, unknown> | null
}

export interface PortStore {
  sessions(): SessionTableRow[]
  selected(): string
  select(id: string): void
  projections(id: string): ProjectionBundle
  /** Fetches the detail for one session. Resolves once it is in the store. */
  load(id: string): Promise<void>
  subscribe(listener: () => void): () => void
  refresh(): Promise<void>
}

export interface PortConfig {
  /** Base path of the management API, without a trailing slash. */
  apiBase: string
  /** The management key, read from the page's own input. */
  key(): string
  /** Which session the page opened with, if any. */
  initialSession?: string
}

const EMPTY_SESSIONS: SessionTableRow[] = []
const EMPTY_BUNDLE: ProjectionBundle = {}

async function readJSON(res: Response): Promise<unknown> {
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  const text = await res.text()
  return text === '' ? null : JSON.parse(text)
}

export function makePortStore(cfg: PortConfig): PortStore {
  let rows: SessionTableRow[] = EMPTY_SESSIONS
  let selected = cfg.initialSession ?? ''
  let bundle: ProjectionBundle = EMPTY_BUNDLE
  const cache = new Map<string, ProjectionBundle>()
  const inflight = new Map<string, Promise<void>>()
  const listeners = new Set<() => void>()

  const notify = (): void => {
    for (const listener of listeners) listener()
  }

  const setRows = (next: SessionTableRow[]): void => {
    rows = next
    notify()
  }

  const setBundle = (id: string, next: ProjectionBundle): void => {
    cache.set(id, next)
    if (id === selected) bundle = next
    notify()
  }

  const fetchSessions = async (): Promise<void> => {
    const body = await readJSON(await fetch(cfg.apiBase + `/cpa-context/sessions?_=${Date.now()}`, {
      headers: { Authorization: 'Bearer ' + cfg.key() },
    })) as { sessions?: SessionTableRow[] } | null
    setRows(body?.sessions ?? EMPTY_SESSIONS)
  }

  const fetchDetail = async (id: string): Promise<void> => {
    const existing = inflight.get(id)
    if (existing !== undefined) return existing
    const run = (async () => {
      const query = '?session=' + encodeURIComponent(id) + '&_=' + Date.now()
      const body = await readJSON(await fetch(cfg.apiBase + '/cpa-context/session' + query, {
        headers: { Authorization: 'Bearer ' + cfg.key() },
      })) as ProjectionBundle | null
      setBundle(id, body ?? EMPTY_BUNDLE)
    })()
      .catch((err: unknown) => {
        // A failed read must not wedge the page: the card falls back to its
        // own "no data" state, which is what the original does on a miss.
        console.warn('cpa-context: session detail failed', id, err)
      })
      .finally(() => { inflight.delete(id) })
    inflight.set(id, run)
    return run
  }

  return {
    sessions: () => rows,
    selected: () => selected,
    select(id) {
      if (selected === id && cache.has(id)) return
      selected = id
      bundle = cache.get(id) ?? EMPTY_BUNDLE
      notify()
      void fetchDetail(id)
    },
    projections: id => (id === selected ? bundle : cache.get(id) ?? EMPTY_BUNDLE),
    load: fetchDetail,
    subscribe(listener) {
      listeners.add(listener)
      return () => { listeners.delete(listener) }
    },
    async refresh() {
      await fetchSessions()
      if (selected !== '') await fetchDetail(selected)
    },
  }
}

export interface ProjectionProps {
  sessionId?: string
  useProjection?: (key: string) => unknown
}

export function makeProjectionProps(store: PortStore, sessionId: string): ProjectionProps {
  return {
    sessionId,
    useProjection(key: string) {
      const value = useSyncExternalStore(
        store.subscribe,
        () => store.projections(sessionId) as unknown,
        () => store.projections(sessionId) as unknown,
      )
      const all = (value ?? EMPTY_BUNDLE) as ProjectionBundle
      return (all as Record<string, unknown>)[key] ?? null
    },
  }
}
