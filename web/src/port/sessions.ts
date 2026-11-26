// The fleet view reads dsh's sessions service: a list snapshot of every session
// with its projection values, plus a subscription. CPA has no such service, so
// this builds the same face from the plugin's own API.
import { asRecord } from '../client/wire'
import type { PortStore } from '../port/store'

export interface SessionsListFace {
  getSnapshot(): unknown
  subscribe(listener: () => void): () => void
}

export interface SessionsFace {
  list: SessionsListFace
  scope?(id: string): unknown
  subagentAddress?(id: string): unknown
}

// snapshotOf renders the list the fleet expects: an object with byId, where each
// row carries the projection values the family graph reads.
export function snapshotFromRows(rows: readonly unknown[], store: PortStore): unknown {
  const byId: Record<string, unknown> = {}
  for (const entry of rows) {
    const row = asRecord(entry)
    if (row === null) continue
    const id = typeof row.id === 'string' ? row.id : ''
    if (id === '') continue
    byId[id] = {
      displayTitle: row.title,
      title: row.title,
      parentId: row.parentId,
      origin: row.origin === 'subagent' ? 'subagent' : 'root',
      running: false,
      blank: false,
      updatedAt: row.lastSeen,
      // The family graph reads the same projection keys the session view does,
      // so a member that has never been opened simply has none yet.
      projectionValues: store.projections(id),
    }
  }
  return { byId, ids: Object.keys(byId) }
}

// sessionsFaceOf builds the service the ported client looks up by name. The
// store already notifies on every change, so its subscription is reused.
export function makeSessionsFace(store: PortStore, loadAll: () => void): SessionsFace {
  let cachedRows: readonly unknown[] | null = null
  let cachedSnapshot: unknown = null
  return {
    list: {
      getSnapshot() {
        // useSyncExternalStore compares snapshots by identity, so the value is
        // rebuilt only when the session list actually changed. Returning a fresh
        // object every call would loop; returning a stale one would freeze the
        // family at whatever it was on first render.
        const rows = store.sessions()
        if (cachedRows === rows) return cachedSnapshot
        cachedRows = rows
        cachedSnapshot = snapshotFromRows(rows, store)
        return cachedSnapshot
      },
      subscribe: store.subscribe,
    },
    scope() {
      return undefined
    },
    subagentAddress(id: string) {
      const row = store.sessions().find(entry => entry.id === id)
      if (row === undefined || row.parentId === undefined) return undefined
      return { parentSessionId: row.parentId, childSessionId: id, mode: 'unknown' }
    },
  }
}
