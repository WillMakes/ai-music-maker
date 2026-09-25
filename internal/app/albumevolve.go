package app

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Evolving an album into its successor (#42)
//
// A variation album is not a copy with a new name: the ask is the next record
// this artist would make. So the model is handed everything that made the
// source sound like itself — the base style, the record's technical style,
// the tracks' own prompts — and asked for one step of evolution: a successor
// style that is recognizably the same artist moved somewhere new, and a whole
// new track list written as a sequence, each prompt handing off into the next.
//
// One structured call rather than an interview: there is no conversation to
// have. The producer's judgement comes afterwards, on the record this creates,
// with every editing tool the album page already has.
// ---------------------------------------------------------------------------

// evolvedTrack is one track of the successor as the model proposes it.
type evolvedTrack struct {
	Name          string `json:"name"`
	Prompt        string `json:"prompt"`
	TargetSeconds int    `json:"targetSeconds"`
}

// evolvedAlbum is the successor record as the model proposes it.
type evolvedAlbum struct {
	Name           string         `json:"name"`
	TechnicalStyle string         `json:"technicalStyle"`
	Tracks         []evolvedTrack `json:"tracks"`
}

// EvolveAlbum creates a new album under the same artist: the source's
// technical style evolved one step, and a fresh track list sequenced so each
// song flows into the next. It returns the stored successor.
//
// Lyrics are deliberately not generated. The flow the report asks for lives in
// the styles; lyrics are the record's meaning, and that stays a conversation —
// the track interview knows the new album the moment this returns.
func (a *App) EvolveAlbum(albumID string) (Album, error) {
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
	tracks, err := a.ListTracks(albumID)
	if err != nil {
		return Album{}, err
	}

	var source []string
	if s := strings.TrimSpace(artist.BaseStyle); s != "" {
		source = append(source, "The artist's base style:\n"+s)
	}
	if s := strings.TrimSpace(album.TechnicalStyle); s != "" {
		source = append(source, "The record's technical style:\n"+s)
	}
	prompts := 0
	for _, track := range tracks {
		if prompt := strings.TrimSpace(track.Prompt); prompt != "" {
			source = append(source, track.Name+":\n"+prompt)
			prompts++
		}
	}
	// A record with no styles anywhere has nothing to evolve *from* — the
	// answer would be an unrelated album wearing the artist's name.
	if len(source) == 0 {
		return Album{}, fmt.Errorf(
			"this record has no technical style and no track prompts yet — there is nothing to evolve from")
	}

	count := len(tracks)
	if count == 0 {
		count = 8
	}
	system := "You evolve a finished record into its successor. Answer with JSON " +
		"and nothing else — no explanation, no markdown fence — in this shape:\n" +
		`{"name": "...", "technicalStyle": "...", "tracks": [{"name": "...", ` +
		`"prompt": "...", "targetSeconds": 210}]}` + "\n\n" +
		"The technicalStyle is the next step in this sound's evolution: " +
		"recognizably the same artist, moved somewhere new. Advance the " +
		"production, tempo range and palette rather than restating them — " +
		"dense, comma-led, concrete. Write a whole new track list of " +
		fmt.Sprintf("%d", count) + " tracks with new names, every prompt built " +
		"on the evolved style, and sequence it so each track flows into the " +
		"next: hand off tempo, key and energy, and say so in the prompts — the " +
		"end of one sets up the start of the one after. targetSeconds may be 0 " +
		"when unspecified. The album name is new, not the source's."

	answer, err := a.askAI(a.interviewContext(), system, []chatMsg{{
		Role: "user",
		Text: "The record to evolve, called \"" + album.Name + "\" by " +
			artist.Name + ":\n\n" + strings.Join(source, "\n\n") +
			"\n\nReturn its successor.",
	}})
	if err != nil {
		return Album{}, err
	}
	var next evolvedAlbum
	if err := json.Unmarshal([]byte(unfence(answer)), &next); err != nil {
		return Album{}, fmt.Errorf("the evolution did not come back as a record: %w", err)
	}
	if strings.TrimSpace(next.Name) == "" || len(next.Tracks) == 0 {
		return Album{}, fmt.Errorf("the evolution came back incomplete — try again")
	}

	created, err := a.SaveAlbum(Album{
		ArtistID:       album.ArtistID,
		Name:           next.Name,
		TechnicalStyle: next.TechnicalStyle,
	})
	if err != nil {
		return Album{}, err
	}
	// Sequentially for the same reason the interview saves that way: each save
	// appends after the highest position, and parallel saves would all read
	// the same one. A track that fails does not lose the record — what
	// happened is reported at the end rather than swallowed (see ImportAlbum).
	var problems []string
	for _, track := range next.Tracks {
		name := strings.TrimSpace(track.Name)
		if name == "" {
			name = "Untitled"
		}
		if _, err := a.SaveTrack(Track{
			AlbumID:       created.ID,
			Name:          name,
			Prompt:        track.Prompt,
			TargetSeconds: track.TargetSeconds,
			Files:         []TrackFile{},
		}); err != nil {
			problems = append(problems, name+": "+err.Error())
		}
	}
	if len(problems) > 0 {
		return created, fmt.Errorf("the album was created but some tracks were not: %s",
			strings.Join(problems, "; "))
	}
	return a.GetAlbum(created.ID)
}
