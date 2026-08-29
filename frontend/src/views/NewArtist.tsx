import {Sparkles, Trash2} from 'lucide-react'
import {ClearChat, ExtractArtist, SaveArtist} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {PageHeader} from '../components/PageHeader'
import {InterviewConversation, useInterview} from '../chat/Interview'
import {toMessage} from '../lib/errors'

/**
 * Creating an artist: a page, not a dialog (#17).
 *
 * This is the longest conversation in the app and the one somebody comes back to
 * across sittings — deciding what an artist sounds like is twenty minutes of
 * thinking, and the transcript is stored precisely because that is longer than
 * one uninterrupted go at it. A modal cannot hold that: it has no route, so it
 * cannot be linked to, cannot be what the app reopens on, and cannot be recorded
 * as the page a bug was filed from.
 *
 * The conversation is the same one an album or a track has (see chat/Interview);
 * only the shell around it differs.
 */
export function NewArtist({
  onCreated,
  onCancel,
}: {
  onCreated: (artistId: string) => void
  onCancel: () => void
}) {
  const interview = useInterview({
    subjectKind: 'artist',
    // "" — there is no artist yet. The thread is keyed to the artist that does
    // not exist, and cleared once one does.
    subjectId: '',
    onFinish: async (messages: main.InterviewMessage[]) => {
      try {
        const draft = await ExtractArtist(messages)
        const artist = await SaveArtist(
          main.Artist.createFrom({
            name: draft.name || 'New artist',
            summary: draft.summary,
            baseStyle: draft.baseStyle,
            inspirations: draft.inspirations ?? [],
          }),
        )
        // Cleared once the artist exists, or the next one would open on this
        // one's conversation.
        await ClearChat('artist', '').catch(() => {})
        onCreated(artist.id)
        return ''
      } catch (err) {
        return toMessage(err)
      }
    },
  })
  const {messages, busy, finishing, finish, clear} = interview

  return (
    // The page owns the height so the transcript takes the window rather than a
    // panel — the reason for having a page at all. min-h-0 is what lets the
    // scroller inside it actually scroll.
    <div className="flex h-[calc(100vh-9rem)] flex-col gap-4">
      <PageHeader
        title="Create an artist"
        breadcrumb="Artists"
        subtitle="Who is this artist and what do they sound like? The answers become their base technical style."
        actions={
          <>
            {messages.length > 0 && (
              <button
                className="sm-button sm-button-quiet"
                type="button"
                onClick={clear}
                disabled={busy || finishing}
              >
                <Trash2 size={15} />
                Start again
              </button>
            )}
            <button className="sm-button sm-button-quiet" type="button" onClick={onCancel}>
              Cancel
            </button>
            <button
              className="sm-button sm-button-primary"
              type="button"
              onClick={finish}
              disabled={messages.length === 0 || busy || finishing}
            >
              <Sparkles size={15} />
              {finishing ? 'Reading the conversation…' : 'Create artist'}
            </button>
          </>
        }
      />

      <div className="flex min-h-0 flex-1 flex-col gap-3">
        <InterviewConversation
          interview={interview}
          opening={
            "Let's work out who this artist is.\n\n" +
            'What kind of music are they making? A genre is a start — what I really ' +
            'want is what it sounds like: the instruments, whether there are vocals, ' +
            'how fast it moves, and what it is for.'
          }
        />
      </div>
    </div>
  )
}
