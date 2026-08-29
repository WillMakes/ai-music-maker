/**
 * The little file plumbing every attachment feature shares (#20, #24): the bug
 * form and the interview both take files from a clipboard, a drop, or a
 * picker, and both hand them to the backend as data URLs — a plain string
 * survives the JSON bridge where raw bytes would not.
 */

/** The last path segment, whichever slash the platform wrote it with. */
export const fileName = (path: string) => path.split(/[\\/]/).pop() ?? path

export const isImagePath = (path: string) => /\.(png|jpe?g|gif|webp)$/i.test(path)

export const isAudioPath = (path: string) => /\.(mp3|wav|flac|m4a|aac|ogg)$/i.test(path)

/**
 * Every attachment on a transcript, keyed by base name — how the track
 * extraction's file assignments (names) are resolved back to stored paths
 * (#24). A later attachment wins a name collision, matching how a
 * conversation corrects itself.
 */
export function attachmentPathsByName(
  messages: {attachments?: string[]}[],
): Record<string, string> {
  const out: Record<string, string> = {}
  for (const message of messages) {
    for (const path of message.attachments ?? []) {
      out[fileName(path)] = path
    }
  }
  return out
}

/** The file as a data URL, which is the shape the attachment bindings take. */
export const readAsDataURL = (file: File) =>
  new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = () => reject(new Error(`Could not read ${file.name}`))
    reader.readAsDataURL(file)
  })
