import { useEffect, useMemo, useState, useSyncExternalStore } from 'react'
import type { Translate } from '../client/i18n'
import { DICTS } from '../client/i18n'
import { makeViewKit } from '../client/viewkit'
import { makeAgentHeads } from '../client/agentHeads'
import { createContextSettings } from '../client/settings'
import { makeContextView } from '../client/components/contextView'
import { makeFleetView } from '../client/components/fleetView'
import { makeOverviewPanel } from '../client/components/overviewPanel'
import { makeProjectionProps, type PortStore, type SessionTableRow } from '../port/store'
import type { ClientCtx } from '../client/services'
import { makeSessionsFace } from './sessions'

type Lang = keyof typeof DICTS

// The ported components reach for a handful of dsh services through the cordis
// context. CPA has none of them, so every lookup returns undefined and the
// components fall back to their own "not available" behaviour, which is what
// they already do when a dashboard runs without an optional service. The
// locale is the exception: several panels read ctx.locale directly rather than
// through a lookup, so it has to be a real face.
function makeServices(
  locale: ReturnType<typeof makeLocale>,
  store: PortStore,
): ClientCtx {
  const sessions = makeSessionsFace(store, () => {})
  return {
    get: (name: string) => (name === 'sessions' ? sessions : undefined),
    locale,
    slots: { inject: () => undefined, register: () => undefined },
  } as unknown as ClientCtx
}

function detectLang(): Lang {
  const raw = (navigator.languages?.[0] ?? navigator.language ?? 'en').toLowerCase()
  const short = raw.split('-')[0] as Lang
  return short in DICTS ? short : 'en'
}

function makeTranslate(lang: Lang): Translate {
  const dict = DICTS[lang] as Record<string, string>
  const fallback = DICTS.en as Record<string, string>
  return (key, params) => {
    let text = dict[key] ?? fallback[key] ?? key
    if (params !== undefined) {
      for (const [name, value] of Object.entries(params)) {
        text = text.split('{' + name + '}').join(String(value))
      }
    }
    return text
  }
}

const noopSubscribe = (): (() => void) => () => { }

function makeLocale(lang: Lang) {
  return {
    register: () => () => { },
    bind: () => makeTranslate(lang),
    getLocale: () => ({ active: lang }),
    subscribe: noopSubscribe,
  }
}

export interface AppProps {
  store: PortStore
}

const TABS = ['context', 'fleet', 'insights'] as const
type Tab = typeof TABS[number]

export function App({ store }: AppProps): JSX.Element {
  const [lang] = useState<Lang>(detectLang)
  const [tab, setTab] = useState<Tab>('context')

  const selected = useSyncExternalStore(store.subscribe, store.selected, store.selected)
  const sessions = useSyncExternalStore(store.subscribe, store.sessions, store.sessions)

  const locale = useMemo(() => makeLocale(lang), [lang])
  const t = useMemo(() => makeTranslate(lang), [lang])
  const kit = useMemo(() => makeViewKit(t, locale), [t, locale])
  const heads = useMemo(() => makeAgentHeads(), [])
  const settings = useMemo(() => createContextSettings(), [])

  const services = useMemo(() => makeServices(locale, store), [locale, store])
  const ContextView = useMemo(() => makeContextView(services, kit, settings, heads), [services, kit, settings, heads])
  const FleetView = useMemo(() => makeFleetView(services, kit, heads), [services, kit, heads])
  const OverviewPanel = useMemo(() => makeOverviewPanel(services, kit, settings), [services, kit, settings])

  // The management namespace is authenticated, so the first load waits for a
  // key. Refreshing before one is typed only produces a 401 in the console.
  useEffect(() => {
    if ((document.getElementById('key') as HTMLInputElement | null)?.value) void store.refresh()
  }, [store])

  useEffect(() => {
    if (sessions.length === 0) return
    if (sessions.some(row => row.id === selected)) return
    store.select(sessions[0].id)
  }, [store, sessions, selected])

  const props = useMemo(() => makeProjectionProps(store, selected), [store, selected])

  return (
    <div className="cpa-port">
      <nav className="cpa-port-tabs">
        {TABS.map(name => (
          <button key={name} type="button" data-active={name === tab}
            onClick={() => setTab(name)}>{name === 'insights' ? t('ov.title') : t('tab.' + name)}</button>
        ))}
      </nav>
      <div className="cpa-port-body">
        <SessionList sessions={sessions} selected={selected} onSelect={id => store.select(id)} />
        <div className="cpa-port-view">
          {tab === 'context' && selected !== '' && <ContextView {...props} />}
          {tab === 'fleet' && selected !== '' && <FleetView {...props} />}
          {tab === 'insights' && <OverviewPanel />}
        </div>
      </div>
    </div>
  )
}

function SessionList(props: {
  sessions: SessionTableRow[]
  selected: string
  onSelect(id: string): void
}): JSX.Element {
  const { sessions, selected, onSelect } = props
  if (sessions.length === 0) {
    return <div className="cpa-port-list"><p className="cpa-port-empty">No traffic observed yet.</p></div>
  }
  return (
    <div className="cpa-port-list">
      {sessions.map(row => (
        <button key={row.id} type="button" data-selected={row.id === selected}
          className="cpa-port-row" onClick={() => onSelect(row.id)}>
          <span className="cpa-port-row-title">{row.title ?? row.id}</span>
          <span className="cpa-port-row-meta">
            {row.model ?? 'unknown'} · {row.requests ?? 0} req
          </span>
        </button>
      ))}
    </div>
  )
}
