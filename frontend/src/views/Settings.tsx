import {PageHeader} from '../components/PageHeader'
import {TabStrip} from '../components/TabStrip'
import {SETTINGS_TABS, type ViewId} from '../navigation'
import {AIAssistants} from '../settings/AIAssistants'
import {AIServices} from '../settings/AIServices'
import {AboutPanel} from '../settings/AboutPanel'
import {BugsPanel} from '../settings/BugsPanel'
import {ExtensionPanel} from '../settings/ExtensionPanel'
import {LibraryPanel} from '../settings/LibraryPanel'
import {PreferencesPanel} from '../settings/PreferencesPanel'

/**
 * The settings shell.
 *
 * Each tab is a real view id rather than local state (see TabStrip), so the
 * route the app reopens on and the route a bug report records name the tab that
 * was open — not "Settings". The service detail page is a separate view for the
 * same reason.
 */
export function Settings({
  view,
  onNavigate,
  onOpenService,
}: {
  view: ViewId
  onNavigate: (view: ViewId) => void
  onOpenService: (serviceId: string) => void
}) {
  return (
    <div className="space-y-5">
      <PageHeader title="Settings" />
      <TabStrip tabs={SETTINGS_TABS} current={view} onSelect={onNavigate} />
      <div>
        {/* Providers and assistants together (#25): both answer "what AI can
            reach this app", and two tabs about the same configuration were one
            tab cut in half. */}
        {view === 'settings-ai' && (
          <div className="space-y-8">
            <AIServices onOpen={onOpenService} />
            <AIAssistants />
          </div>
        )}
        {view === 'settings-library' && <LibraryPanel />}
        {view === 'settings-extension' && <ExtensionPanel />}
        {view === 'settings-bugs' && <BugsPanel />}
        {view === 'settings-preferences' && <PreferencesPanel />}
        {view === 'settings-about' && <AboutPanel />}
      </div>
    </div>
  )
}
