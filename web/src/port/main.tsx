import { createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { installHistoryFace } from './historyFace'
import { makePortStore } from '../port/store'

import '../client/styles/tailwind.css'
import '../client/styles/base.css'
import '../client/styles/stats.css'
import '../client/styles/jump.css'
import '../client/styles/settings.css'
import '../client/styles/stackedBar.css'
import '../client/styles/timelineChart.css'
import '../client/styles/timelineMilestone.css'
import '../client/styles/requestDetail.css'
import '../client/styles/events.css'
import '../client/styles/fileCard.css'
import '../client/styles/commandPanel.css'
import '../client/styles/modal.css'
import '../client/styles/browser.css'
import '../client/styles/detailSections.css'
import '../client/styles/attachments.css'
import '../client/styles/agentGraph.css'
import '../client/styles/fleet.css'
import '../client/styles/pulse.css'
import '../client/styles/overview.css'
import '../client/styles/dateRange.css'
import '../client/styles/recom.css'
import './tokens.css'
import './port.css'

// The dashboard is served as a browser resource under
// /v0/resource/plugins/<plugin>/dashboard, and the management API sits under
// /v0/management. Deriving the base from the page URL keeps the build free of
// a hard-coded host and port.
function apiBase(): string {
  const here = window.location.pathname
  const marker = '/v0/resource/'
  const cut = here.indexOf(marker)
  return cut >= 0 ? here.slice(0, cut) + '/v0/management' : '/v0/management'
}

const keyInput = document.getElementById('key') as HTMLInputElement | null
const refreshButton = document.getElementById('refresh') as HTMLButtonElement | null

// forgetStoredKey drops the remembered key. It runs when the proxy refuses it,
// which happens after a rotation, a revoke, or a move to a host with a different
// secret. Keeping a rejected key would leave the page polling with it every few
// seconds, and CPA counts failed attempts toward an IP ban.
const STORED_KEY = 'cpa-context.management-key'

function forgetStoredKey(): void {
  try {
    localStorage.removeItem(STORED_KEY)
    sessionStorage.removeItem(STORED_KEY)
  } catch {
    // Nothing to do: the value stays for this view only.
  }
}

// requestKey clears the field and focuses it, so the next thing the operator does
// is type the new key. The page says why instead of silently showing no data.
function requestKey(): void {
  forgetStoredKey()
  if (keyInput === null) return
  keyInput.value = ''
  keyInput.placeholder = 'key refused — enter a new one'
  keyInput.style.borderColor = '#c2564b'
  keyInput.focus()
}

const store = makePortStore({
  apiBase: apiBase(),
  key: () => keyInput?.value ?? '',
  initialSession: new URLSearchParams(window.location.search).get('session') ?? '',
  onRejected: () => { requestKey() },
})

// The ported browser fetches one step's header and content through this face.
// Without it the panel reports that the environment cannot load a header.
installHistoryFace({ apiBase: apiBase(), key: () => keyInput?.value ?? '' })

refreshButton?.addEventListener('click', () => { void store.refresh() })
keyInput?.addEventListener('keydown', event => {
  if (event.key === 'Enter') void store.refresh()
})
// The key is remembered so the dashboard opens without typing it again.
//
// It goes in localStorage rather than sessionStorage: a session store dies with
// the tab, and the plugin opens in a fresh iframe or tab every time the operator
// comes back, which is exactly the retyping this is meant to end. The key is the
// same one the management panel needs, and the page it is stored for belongs to
// this proxy, so it never travels to another origin.
function readStoredKey(): string {
  try {
    return localStorage.getItem(STORED_KEY) ?? sessionStorage.getItem(STORED_KEY) ?? ''
  } catch {
    return ''
  }
}

function storeKey(value: string): void {
  try {
    localStorage.setItem(STORED_KEY, value)
    // The old session copy would shadow nothing but would linger after a clear.
    sessionStorage.removeItem(STORED_KEY)
  } catch {
    // A blocked storage must not break the page: the key still works for this
    // view, it just is not remembered.
  }
}

if (keyInput !== null) {
  const saved = readStoredKey()
  if (saved !== '' && keyInput.value === '') {
    keyInput.value = saved
    void store.refresh()
  }
  const remember = (): void => {
    const value = keyInput.value.trim()
    if (value === '') return
    storeKey(value)
    keyInput.placeholder = 'management key'
    keyInput.style.borderColor = ''
  }
  keyInput.addEventListener('change', remember)
  // Enter submits without leaving the field, and a change event does not fire in
  // that case, so the key would be forgotten by the next visit.
  keyInput.addEventListener('keydown', event => {
    if (event.key === 'Enter') remember()
  })
}

const mount = document.getElementById('root')
if (mount !== null) {
  createRoot(mount).render(createElement(App, { store }))
}
