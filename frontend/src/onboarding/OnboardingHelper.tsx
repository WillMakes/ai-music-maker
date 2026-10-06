import {useState} from 'react'
import {ChevronLeft, ImageIcon, Link2} from 'lucide-react'
import {Modal} from '../components/Modal'
import {AIServices} from '../settings/AIServices'
import {AIServiceDetail} from '../settings/AIServiceDetail'
import {SETTING_KEYS, saveSetting} from '../lib/settings'

/**
 * The setup guide that opens at launch (#127): connect the AI services the
 * app's features run on, before the first feature that needs one refuses.
 *
 * The provider list and each provider's connect page are Settings → AI's own,
 * reused rather than redrawn: a second copy of the connect flow is a second
 * place for it to go stale. Every way out — Done, Skip, ×, Escape, the scrim —
 * turns the helper off, because a guide that comes back after being dismissed
 * is nagging. Preferences turns it back on.
 */
export function OnboardingHelper({onClose}: {onClose: () => void}) {
  const [serviceId, setServiceId] = useState('')

  const finish = () => {
    void saveSetting(SETTING_KEYS.onboardingHelper, 'off')
    onClose()
  }

  return (
    <Modal
      title="Set up AIMusicMaker"
      wide
      onClose={finish}
      footer={
        <>
          <button className="sm-button sm-button-quiet" type="button" onClick={finish}>
            Skip for now
          </button>
          <button className="sm-button sm-button-primary" type="button" onClick={finish}>
            Done
          </button>
        </>
      }
    >
      {serviceId ? (
        <div className="space-y-4">
          <button
            className="sm-button sm-button-quiet"
            type="button"
            onClick={() => setServiceId('')}
          >
            <ChevronLeft size={15} />
            All providers
          </button>
          <AIServiceDetail serviceId={serviceId} />
        </div>
      ) : (
        <div className="space-y-4">
          <p className="text-sm text-fg-muted">
            AIMusicMaker writes artists, albums, prompts and lyrics with the AI you already
            use. Connect at least one provider and star the one the interviews should use.
            This can all be changed later in Settings → AI.
          </p>
          {/* The two features tied to one provider, said up front: otherwise the
              producer learns it from an error the first time they try. Both
              match the backend's own gates (renderCover, AnalyzeMusicURL). */}
          <ul className="space-y-2">
            <li className="sm-panel flex items-start gap-3 p-3 text-sm">
              <ImageIcon size={16} className="mt-0.5 shrink-0 text-accent" aria-hidden />
              <span>
                <span className="font-medium">ChatGPT is required to generate artwork.</span>
                <span className="block text-xs text-fg-muted">
                  Album covers are drawn by ChatGPT, connected by API key or by the signed-in
                  Codex CLI.
                </span>
              </span>
            </li>
            <li className="sm-panel flex items-start gap-3 p-3 text-sm">
              <Link2 size={16} className="mt-0.5 shrink-0 text-accent" aria-hidden />
              <span>
                <span className="font-medium">
                  Antigravity is required for processing existing online music for inspiration.
                </span>
                <span className="block text-xs text-fg-muted">
                  Analysing a YouTube or YouTube Music link runs on Antigravity's signed-in
                  agy CLI.
                </span>
              </span>
            </li>
          </ul>
          <AIServices onOpen={setServiceId} />
        </div>
      )}
    </Modal>
  )
}
