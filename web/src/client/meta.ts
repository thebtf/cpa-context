// The name, version and repository come from plugin.go, so the number the page
// shows is the number the binary declares to the host. The generator runs before
// every build; see scripts/generate-version.mjs.
export { PLUGIN_NAME, PLUGIN_REPO, PLUGIN_VERSION } from './generated'

import { PLUGIN_REPO } from './generated'

export const PLUGIN_REPO_SHORT = PLUGIN_REPO.replace(/^https?:\/\/github\.com\//, '')
