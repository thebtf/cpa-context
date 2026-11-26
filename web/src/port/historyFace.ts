// The ported client asks for one step's own content through a session page
// face: the header of a request (its system prompt and tool schemas) and the
// nodes of a step. The dsh build supplies that face through a cordis inject;
// CPA has no cordis, so the face is handed to historyPage directly.
//
// This module owns that handoff. It is called once at startup, and the face it
// installs answers from the plugin's own history route, which serves one request
// at a time.
import { setPageFace } from '../client/historyPage'
import type { SessionAddressLike } from '../client/dshFaces'

export interface HistoryRow {
  seq: number
  type: string
  data: Record<string, unknown>
}

interface PageRequest {
  address: SessionAddressLike
  throughSeq: number
  beforeSeq?: number
  maxMessages?: number
}

export interface HistoryConfig {
  apiBase: string
  key(): string
}

function sessionIdOf(address: SessionAddressLike): string {
  return address.kind === 'session' ? address.sessionId : address.childSessionId
}

// rowsOf reads the plugin's answer into the envelope the client recognises:
// each entry is either the event itself or a wrapper with an "event" field, and
// the client accepts both.
function rowsOf(value: unknown): HistoryRow[] {
  if (value === null || typeof value !== 'object') return []
  if (!('ok' in value) && !('value' in value) && !('rows' in value)) return []
  const body = value as Record<string, unknown>
  const payload: unknown = body.ok === false ? null : body.value ?? value
  if (payload === null || typeof payload !== 'object') return []
  if (!('rows' in payload)) return []
  const rows: unknown = payload.rows
  if (!Array.isArray(rows)) return []
  return rows.filter((row): row is HistoryRow => {
    if (row === null || typeof row !== 'object') return false
    return 'seq' in row && typeof row.seq === 'number'
      && 'type' in row && typeof row.type === 'string'
  })
}

export function installHistoryFace(cfg: HistoryConfig): void {
  setPageFace(async (request: PageRequest): Promise<unknown> => {
    const target = request.beforeSeq ?? request.throughSeq + 1
    const body = {
      sessionId: sessionIdOf(request.address),
      throughSeq: target,
      limit: request.maxMessages ?? 0,
    }
    const res = await fetch(`${cfg.apiBase}/cpa-context/api/dsh-context/history`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + cfg.key() },
      body: JSON.stringify(body),
    })
    if (!res.ok) return { ok: false, error: `${res.status}` }
    return res.json()
  })
}

export { rowsOf }
