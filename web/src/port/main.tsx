import { createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
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

const store = makePortStore({
  apiBase: apiBase(),
  key: () => keyInput?.value ?? '',
  initialSession: new URLSearchParams(window.location.search).get('session') ?? '',
})

refreshButton?.addEventListener('click', () => { void store.refresh() })
keyInput?.addEventListener('keydown', event => {
  if (event.key === 'Enter') void store.refresh()
})
// The key survives the page reload so the dashboard is usable without retyping
// it, and the first load can run as soon as the page opens.
const STORED_KEY = 'cpa-context.management-key'
if (keyInput !== null) {
  const saved = sessionStorage.getItem(STORED_KEY)
  if (saved !== null && keyInput.value === '') {
    keyInput.value = saved
    void store.refresh()
  }
  keyInput.addEventListener('change', () => {
    sessionStorage.setItem(STORED_KEY, keyInput.value)
  })
}

const mount = document.getElementById('root')
if (mount !== null) {
  createRoot(mount).render(createElement(App, { store }))
}
