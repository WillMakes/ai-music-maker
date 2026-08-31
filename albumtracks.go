package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Writing a track list for a record that has none (#71)
//
// Albums created before the interview's writing pass (#70) exist with a
// style and an empty track list, and the only way to fill one was a whole
// second conversation. This is the one-press repair: the record already
// carries everything a track list is written from — the artist's base style
// and its own technical style — so the model is asked for the list directly,
// the way EvolveAlbum (#42) writes a successor's.
//
// Lyrics are deliberately not generated, for EvolveAlbum's reason: the flow
// lives in the styles, lyrics are the record's meaning, and that stays a
// conversation.
// ---------------------------------------------------------------------------

// writtenTrack is one track of the list as the model proposes it.
type writtenTrack struct {
	Name          string `json:"name"`
	Prompt        string `json:"prompt"`
	TargetSeconds int    `json:"targetSeconds"`
}

// WriteAlbumTracks writes a full track list onto an album that has none and
// returns the stored album.
func (a *App) WriteAlbumTracks(albumID string) (Album, error) {
	if a.store == nil {
		return Album{}, errNoStore
	}
	album, err := a.GetAlbum(albumID)
	if err != nil {
		return Album{}, err
	}
	artist, err := a.GetArtist(album.ArtistID)
	if err != nil {
		return Album{}, err
	}
	existing, err := a.ListTracks(albumID)
	if err != nil {
		return Album{}, err
	}
	// Only an empty record: a list written over one that already has tracks
	// would either duplicate them or invent a second half nobody planned —
	// adding to a started record is the track interview's job.
	if len(existing) > 0 {
		return Album{}, fmt.Errorf(
			"this record already has tracks — Write tracks on the Tracks tab adds more")
	}

	var ground []string
	if s := strings.TrimSpace(artist.BaseStyle); s != "" {
		ground = append(ground, "The artist's base style:\n"+s)
	}
	if s := strings.TrimSpace(album.TechnicalStyle); s != "" {
		ground = append(ground, "The record's technical style:\n"+s)
	}
	// With no style anywhere there is nothing to write the tracks *from*, and
	// the list would be an unrelated record wearing the album's name.
	if len(ground) == 0 {
		return Album{}, fmt.Errorf(
			"this record has no technical style and the artist has no base style yet — write one first")
	}

	system := "You write the track list for a record from its style. Answer " +
		"with JSON and nothing else — no explanation, no markdown fence — in " +
		"this shape:\n" +
		`{"tracks": [{"name": "...", "prompt": "...", "targetSeconds": 210}]}` +
		"\n\nWrite eight to twelve tracks in running order, titles that " +
		"belong on this record, every prompt built on the style below — " +
		"comma-led, concrete, under about 200 words, most important terms " +
		"first, ending with what to avoid — and sequence the list so each " +
		"track hands off tempo, key and energy into the next. targetSeconds " +
		"may be 0 when unspecified."

	answer, err := a.askAI(a.interviewContext(), system, []chatMsg{{
		Role: "user",
		Text: "The record, called \"" + album.Name + "\" by " + artist.Name +
			":\n\n" + strings.Join(ground, "\n\n") +
			"\n\nReturn its track list.",
	}})
	if err != nil {
		return Album{}, err
	}
	var list struct {
		Tracks []writtenTrack `json:"tracks"`
	}
	if err := json.Unmarshal([]byte(unfence(answer)), &list); err != nil {
		return Album{}, fmt.Errorf("the track list did not come back as one: %w", err)
	}
	if len(list.Tracks) == 0 {
		return Album{}, fmt.Errorf("the track list came back empty — try again")
	}

	// Sequentially, with failures reported rather than swallowed — see
	// EvolveAlbum for both reasons.
	var problems []string
	for _, track := range list.Tracks {
		name := strings.TrimSpace(track.Name)
		if name == "" {
			name = "Untitled"
		}
		if _, err := a.SaveTrack(Track{
			AlbumID:       albumID,
			Name:          name,
			Prompt:        track.Prompt,
			TargetSeconds: track.TargetSeconds,
			Files:         []TrackFile{},
		}); err != nil {
			problems = append(problems, name+": "+err.Error())
		}
	}
	if len(problems) > 0 {
		return album, fmt.Errorf("some tracks were not created: %s",
			strings.Join(problems, "; "))
	}
	return a.GetAlbum(albumID)
}
