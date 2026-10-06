import React from 'react'
import {createRoot} from 'react-dom/client'
import App from './App'
import {ArtistsProvider} from './artists/ArtistsProvider'
import {LibraryProvider} from './library/LibraryProvider'
import {PlaylistsProvider} from './playlists/PlaylistsProvider'
import {PlayerProvider} from './player/PlayerProvider'
import {EvolutionsProvider} from './evolutions/EvolutionsProvider'
import {ArtworksProvider} from './artworks/ArtworksProvider'
import {VideosProvider} from './videos/VideosProvider'
import {BugsProvider} from './bugs/BugsProvider'
import {UpdatesProvider} from './updates/UpdatesProvider'
import {ThemeProvider} from './theme/ThemeProvider'
import './style.css'

/**
 * The providers, outermost first.
 *
 * Theme before everything, because it decides what the first paint looks like.
 * Bugs before the shell, because the top bar's badge reads it and the top bar is
 * on every page; updates likewise, for the top bar's update button (#132). Artists last of the three, since only the content region needs
 * it.
 *
 * StrictMode is on. It double-invokes effects in development, which is exactly
 * the pressure the data providers should be under: every one of them is written
 * so that loading twice is loading once with an extra request, never a duplicate
 * write.
 */
const container = document.getElementById('root')
if (!container) {
  throw new Error('index.html has no #root to mount into')
}

createRoot(container).render(
  <React.StrictMode>
    <ThemeProvider>
      <BugsProvider>
        <UpdatesProvider>
          <ArtistsProvider>
            <LibraryProvider>
              <PlaylistsProvider>
                <PlayerProvider>
                  {/* Above the shell so an evolution (#82), an artwork
                      generation (#83) and a music-video render (#118) outlive
                      the page that started them - the status bar and the album
                      page both read them. */}
                  <EvolutionsProvider>
                    <ArtworksProvider>
                      <VideosProvider>
                        <App />
                      </VideosProvider>
                    </ArtworksProvider>
                  </EvolutionsProvider>
                </PlayerProvider>
              </PlaylistsProvider>
            </LibraryProvider>
          </ArtistsProvider>
        </UpdatesProvider>
      </BugsProvider>
    </ThemeProvider>
  </React.StrictMode>,
)
