import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],

  // Hot reload, told where its own socket is — and pinned so that stays true.
  //
  // `wails dev` does not serve this dev server to the webview directly: it
  // proxies it from its own asset server at wails.localhost:34115, and the page
  // therefore believes it was loaded from there. Vite's client builds its
  // websocket url from the page's origin, so left alone it tries
  // ws://wails.localhost:34115 — which macOS App Transport Security refuses,
  // because ATS exempts "localhost" and wails.localhost is a different name over
  // an insecure scheme.
  //
  // Naming the host and the client port sends the socket straight at Vite. That
  // needs three other things to be true:
  //
  //   strictPort  a client told to reach 5173 has to be talking to a server on
  //               5173. Vite quietly takes the next free port when its own is
  //               busy — a restart racing the previous instance is enough — and
  //               the socket then points at nothing.
  //   host        Vite binds to `localhost`, which Node resolves to ::1 alone.
  //               The webview resolves it to 127.0.0.1 and finds nothing there.
  //   clientPort  and not `port` for the socket: `port` is where Vite *listens*
  //               for HMR, and setting it to the dev server's own port stands a
  //               second, websocket-only server on top of the first.
  //
  // Ignored entirely by `npm run build`: `server` is dev-only.
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    hmr: {protocol: 'ws', host: 'localhost', clientPort: 5173},
  },
})
