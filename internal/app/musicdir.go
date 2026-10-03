package app

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Where the finished music goes
//
// Not ~/.aimusicmaker. Everything else this app owns lives there and is disposable
// with it, but the audio Suno gives back is the output — the thing that should
// still be there after the app is uninstalled, and the thing a music player
// should be able to find without being told where to look. So it goes in the
// operating system's own music folder, under one directory of ours:
//
//	Windows  %USERPROFILE%\Music\AIMusicMaker
//	macOS    ~/Music/AIMusicMaker
//	Linux    $XDG_MUSIC_DIR/AIMusicMaker, else ~/Music/AIMusicMaker
//
// Inside it, one folder per artist and album, named from their slugs, so the
// library reads the way any other music library does — Artist/Album/01 Track.mp3
// — rather than as a pile of ids only this app can decode.
//
// The producer may point it somewhere else (Settings → keyMusicRoot), which is
// what an external drive full of music is for. Moving the setting does not move
// the files already filed: every track_files row keeps an absolute path, so the
// old ones stay findable and the new ones land in the new place.
// ---------------------------------------------------------------------------

// musicFolderName is the one directory this app owns inside the music folder.
const musicFolderName = "AIMusicMaker"

// legacyMusicFolderName is the directory from before the rename (#103). Unlike
// the data folder it is never moved: it is the producer's music library, other
// players may have it indexed, and every take is stored by absolute path. A
// library that already exists under the old name stays the default, so new
// takes land beside the old ones rather than in a second, empty library.
const legacyMusicFolderName = "SunoMaker"

// osMusicDir is the operating system's music folder, without AIMusicMaker on the
// end. A variable so tests can point it at a temp directory — the alternative is
// a test suite that writes MP3s into the producer's real library.
var osMusicDir = realOSMusicDir

func realOSMusicDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	// XDG first, and only on Linux in practice: it is the one platform where the
	// music folder is genuinely configurable and often is — a localised desktop
	// calls it Musique or Musik, and ~/Music would be a second, empty folder
	// beside the real one. The file is read rather than the environment because
	// XDG_MUSIC_DIR is exported by the session, and this app can be launched from
	// a terminal that never saw it.
	if dir := xdgMusicDir(home); dir != "" {
		return dir, nil
	}
	return filepath.Join(home, "Music"), nil
}

// xdgMusicDir reads XDG_MUSIC_DIR out of ~/.config/user-dirs.dirs, answering ""
// when there is no such file or no such entry — which is every non-Linux machine
// and plenty of Linux ones.
//
// The file is shell syntax and this is not a shell, so only the one form
// xdg-user-dirs actually writes is understood:
// XDG_MUSIC_DIR="$HOME/Musique". Anything else is left to the ~/Music fallback,
// which is the right answer far more often than a half-parsed one.
func xdgMusicDir(home string) string {
	f, err := os.Open(filepath.Join(home, ".config", "user-dirs.dirs"))
	if err != nil {
		return ""
	}
	defer f.Close()

	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if !strings.HasPrefix(line, "XDG_MUSIC_DIR=") {
			continue
		}
		value := strings.Trim(strings.TrimPrefix(line, "XDG_MUSIC_DIR="), `"`)
		if value == "" {
			return ""
		}
		if strings.HasPrefix(value, "$HOME/") {
			return filepath.Join(home, strings.TrimPrefix(value, "$HOME/"))
		}
		if filepath.IsAbs(value) {
			return value
		}
		return ""
	}
	return ""
}

// musicRoot is where this app files audio: the producer's chosen folder, or the
// OS music folder's AIMusicMaker directory. Created (0755, not 0700) because it is
// a music library — other applications on this machine are meant to read it.
func (a *App) musicRoot() (string, error) {
	if a.store != nil {
		if chosen, err := a.store.setting(keyMusicRoot); err == nil && strings.TrimSpace(chosen) != "" {
			dir := strings.TrimSpace(chosen)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", err
			}
			return dir, nil
		}
	}
	base, err := osMusicDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, musicFolderName)
	if legacy := filepath.Join(base, legacyMusicFolderName); !fileExists(dir) && fileExists(legacy) {
		dir = legacy
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// MusicRoot reports where audio is filed, for Settings and for the empty state
// that tells somebody where their downloads should go.
func (a *App) MusicRoot() (string, error) {
	return a.musicRoot()
}

// SetMusicRoot points the library somewhere else; "" restores the OS default.
// The folder is created on the spot rather than at the next import, so a typo in
// a path is reported while the producer is still looking at the field.
func (a *App) SetMusicRoot(dir string) (string, error) {
	if a.store == nil {
		return "", errNoStore
	}
	dir = strings.TrimSpace(dir)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	if err := a.store.setSetting(keyMusicRoot, dir); err != nil {
		return "", err
	}
	return a.musicRoot()
}

// albumFolder is where one album's files are filed: <root>/<artist>/<album>. The
// slugs rather than the names, because a name may hold a slash, a colon or a
// character Windows refuses in a path, and slugify has already reduced it to
// what every filesystem here accepts.
func (a *App) albumFolder(artist Artist, album Album) (string, error) {
	root, err := a.musicRoot()
	if err != nil {
		return "", err
	}
	artistPart := artist.Slug
	if artistPart == "" {
		artistPart = "unknown-artist"
	}
	albumPart := album.Slug
	if albumPart == "" {
		albumPart = "unknown-album"
	}
	dir := filepath.Join(root, artistPart, albumPart)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// artworkDir is where generated and uploaded album covers are kept. This one
// *is* under ~/.aimusicmaker: a cover is a working file of the app's rather than
// something a music player reads, and the copy that matters ends up embedded in
// the released record elsewhere.
func artworkDir() (string, error) {
	base, err := dataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "artwork")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
