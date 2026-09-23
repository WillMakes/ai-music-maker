package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ---------------------------------------------------------------------------
// Bulk take import onto an album (#76)
//
// A finished Suno session ends with a folder of downloads, one per track,
// each named after the song it is a take of. Importing them one by one from
// each track's own panel is a dozen trips through the same dialog; here the
// whole batch lands on the album at once — picked or dropped — and each file
// is routed to its track by name.
//
// Matching is by name and refuses ambiguity rather than guessing: an import
// filed onto the wrong track is worse than one that asks to be dropped on
// the track itself.
// ---------------------------------------------------------------------------

// normalizeForMatch reduces a track name or file name to the letters and
// digits that survive renaming: extension off, case folded, the running-order
// numbering this app's own files carry stripped from the front.
func normalizeForMatch(s string) string {
	s = strings.TrimSuffix(s, filepath.Ext(s))
	return normalizeTitle(strings.TrimLeft(s, "0123456789 .-_"))
}

// normalizeTitle is normalizeForMatch for a name that is not a file name — a
// song title off Suno's player (#91). Neither of the file-name trims applies:
// "Mr. Blue" has no extension to lose, and "1999" is a title, not a running
// order, so trimming either would match the song as "mr" or as nothing.
func normalizeTitle(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// matchAlbumTrack finds the one track a file belongs to. Exact name matches
// win outright; containment handles the ordinary suffixes a download picks up
// ("Cold Open (v2).mp3"). Zero or several candidates is an error that names
// the way out.
func matchAlbumTrack(tracks []Track, fileName string) (Track, error) {
	base := filepath.Base(fileName)
	want := normalizeForMatch(base)
	if want == "" {
		return Track{}, fmt.Errorf("%s has no name to match a track by", base)
	}

	var exact, loose []Track
	for _, t := range tracks {
		have := normalizeForMatch(t.Name)
		if have == "" {
			continue
		}
		if have == want {
			exact = append(exact, t)
		} else if strings.Contains(want, have) || strings.Contains(have, want) {
			loose = append(loose, t)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) == 0 && len(loose) == 1 {
		return loose[0], nil
	}
	if len(exact) == 0 && len(loose) == 0 {
		return Track{}, fmt.Errorf(
			"%s does not match any track on this record — import it from the track itself", base)
	}
	return Track{}, fmt.Errorf(
		"%s matches more than one track — import it from the track itself", base)
}

// ImportAlbumTakeFile routes one picked file to its track and imports it.
func (a *App) ImportAlbumTakeFile(albumID, sourcePath string) (Track, error) {
	tracks, err := a.ListTracks(albumID)
	if err != nil {
		return Track{}, err
	}
	track, err := matchAlbumTrack(tracks, sourcePath)
	if err != nil {
		return Track{}, err
	}
	return a.ImportTrackFile(track.ID, sourcePath, "")
}

// ImportAlbumTakeDrop routes one dropped file — bytes and a name, the shape a
// browser drop arrives in (#57) — to its track and imports it.
func (a *App) ImportAlbumTakeDrop(albumID, name, dataURL string) (Track, error) {
	tracks, err := a.ListTracks(albumID)
	if err != nil {
		return Track{}, err
	}
	track, err := matchAlbumTrack(tracks, name)
	if err != nil {
		return Track{}, err
	}
	return a.ImportDroppedTrackFile(track.ID, name, dataURL, "")
}

// PickAudioFiles opens the OS file picker for a batch of takes, the multiple
// counterpart of PickImageFile (#39): the same headless refusal, filtered to
// the audio this app files. An empty answer is a cancelled dialog, not an
// error.
func (a *App) PickAudioFiles() ([]string, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("there is no window to open a file dialog from")
	}
	paths, err := wruntime.OpenMultipleFilesDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "Choose the takes to import",
		Filters: []wruntime.FileFilter{{
			DisplayName: "Audio (*.mp3, *.wav, *.flac, *.m4a, *.aac, *.ogg)",
			Pattern:     "*.mp3;*.wav;*.flac;*.m4a;*.aac;*.ogg",
		}},
	})
	if err != nil {
		return nil, err
	}
	if paths == nil {
		paths = []string{}
	}
	return paths, nil
}
