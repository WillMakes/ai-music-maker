package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Importing an album that already exists (#36)
//
// The interview is how an album is invented; this is how one that was already
// made comes in. A producer with a finished record has a folder of audio —
// Suno downloads, a mastered export — and describing it to a model track by
// track would be typing out what the folder already says. So the import reads
// the folder: the album is named after it, every audio file becomes a track,
// and each file lands in the music library through the same ImportTrackFile
// every other route uses.
//
// Or no folder at all: a name alone makes an empty album, the "blank artist"
// of records — for content that exists somewhere this app cannot read yet.
// ---------------------------------------------------------------------------

// trackFilePattern reads a filename the way this app writes them ("01 Neon
// Rain.mp3") and the ways downloads commonly arrive ("01 - Neon Rain", "1.
// Neon Rain"): an optional leading number, an optional separator, the name.
var trackFilePattern = regexp.MustCompile(`^(\d{1,3})?[\s.\-_]*(.*)$`)

// ImportAlbum creates an album for an artist from a name, a folder of audio,
// or both. With a folder, the name may be blank — the folder's own name is
// what the record is called everywhere else already.
func (a *App) ImportAlbum(artistID, name, dir string) (Album, error) {
	if a.store == nil {
		return Album{}, errNoStore
	}
	name = strings.TrimSpace(name)
	dir = strings.TrimSpace(dir)
	if name == "" && dir == "" {
		return Album{}, fmt.Errorf("give the album a name, a folder, or both")
	}

	var files []string
	if dir != "" {
		info, err := os.Stat(dir)
		if err != nil {
			return Album{}, fmt.Errorf("could not read %s: %w", dir, err)
		}
		if !info.IsDir() {
			return Album{}, fmt.Errorf("%s is a file — give the album's folder", dir)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return Album{}, fmt.Errorf("could not read %s: %w", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if audioExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
				files = append(files, entry.Name())
			}
		}
		if len(files) == 0 {
			return Album{}, fmt.Errorf("%s holds no audio files this app knows", dir)
		}
		// Name order, which for numbered files is running order. The numbers
		// themselves are read per file below, so "1." and "01" sort the same
		// way they play.
		sort.Slice(files, func(i, j int) bool {
			return trackSortKey(files[i]) < trackSortKey(files[j])
		})
		if name == "" {
			name = filepath.Base(dir)
		}
	}

	album, err := a.SaveAlbum(Album{ArtistID: artistID, Name: name})
	if err != nil {
		return Album{}, err
	}

	// Tracks in folder order. A file that fails to import does not fail the
	// album — the producer asked for the record brought in, and nine tracks
	// with one to retry beats zero — but what happened is reported at the end
	// rather than swallowed.
	var problems []string
	for _, file := range files {
		track, err := a.SaveTrack(Track{
			AlbumID: album.ID,
			Name:    trackNameFromFile(file),
		})
		if err != nil {
			problems = append(problems, file+": "+err.Error())
			continue
		}
		if _, err := a.ImportTrackFile(track.ID, filepath.Join(dir, file), ""); err != nil {
			problems = append(problems, file+": "+err.Error())
		}
	}
	if len(problems) > 0 {
		return album, fmt.Errorf("the album was created but some files did not import: %s",
			strings.Join(problems, "; "))
	}
	return a.GetAlbum(album.ID)
}

// trackNameFromFile is what to call a track imported from a file: the filename
// without its running number, separators, or extension. "01 - Neon Rain.mp3"
// is a track called Neon Rain.
func trackNameFromFile(file string) string {
	base := strings.TrimSuffix(file, filepath.Ext(file))
	match := trackFilePattern.FindStringSubmatch(base)
	if match != nil && strings.TrimSpace(match[2]) != "" {
		return strings.TrimSpace(match[2])
	}
	// A file that is only a number keeps it — "07.mp3" is a track called 07,
	// which is more use than an empty name SaveTrack would refuse.
	return strings.TrimSpace(base)
}

// trackSortKey orders files by their leading number when they have one, and by
// name after it, so "2 - b.mp3" plays before "10 - a.mp3" the way the record
// does — a plain string sort would put 10 first.
func trackSortKey(file string) string {
	match := trackFilePattern.FindStringSubmatch(file)
	if match != nil && match[1] != "" {
		if n, err := strconv.Atoi(match[1]); err == nil {
			return fmt.Sprintf("%06d %s", n, file)
		}
	}
	return fmt.Sprintf("%06d %s", 999999, file)
}
