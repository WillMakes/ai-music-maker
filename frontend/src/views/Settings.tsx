import {PageHeader} from '../components/PageHeader'
import {TabStrip} from '../components/TabStrip'
import {SETTINGS_TABS, type ViewId} from '../navigation'
import {AIAssistants} from '../settings/AIAssistants'
import {AIServices} from '../settings/AIServices'
import {AboutPanel} from '../settings/AboutPanel'
import {BugsPanel} from '../settings/BugsPanel'
import {ExtensionPanel} from '../settings/ExtensionPanel'
import {SkillsPanel} from '../settings/SkillsPanel'

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
  onOpenSkill,
}: {
  view: ViewId
  onNavigate: (view: ViewId) => void
  onOpenService: (serviceId: string) => void
  onOpenSkill: (skillId: string) => void
}) {
  return (
    <div className="space-y-5">
      <PageHeader title="Settings" />
      <TabStrip tabs={SETTINGS_TABS} current={view} onSelect={onNavigate} />
      <div>
        {view === 'settings-ai' && <AIServices onOpen={onOpenService} />}
        {view === 'settings-assistants' && <AIAssistants />}
        {view === 'settings-skills' && <SkillsPanel onOpenSkill={onOpenSkill} />}
        {view === 'settings-extension' && <ExtensionPanel />}
        {view === 'settings-bugs' && <BugsPanel />}
        {view === 'settings-about' && <AboutPanel />}
      </div>
    </div>
  )
}
