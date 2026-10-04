package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"aimusicmaker/skills"
)

// ---------------------------------------------------------------------------
// Skills
//
// A skill is a written instruction the app follows when it asks a model to do
// something: how to interview somebody about an artist, what a track prompt is
// meant to contain, what an album cover should look like. They are the app's
// own briefs, exposed so the producer can retune them without a rebuild — a
// person who makes one kind of record every week will want different questions
// from the defaults here.
//
// Two kinds, in one table:
//
//	Application skills  shipped with the build, listed below. A row under a
//	                    built-in's own id is an *override* of it. There is no row
//	                    for one left at its default, which is what makes
//	                    resetting a delete and lets a later build improve a
//	                    default nobody has edited.
//	Written skills      the producer's own, with ids of their own.
//
// The bodies of the built-ins are markdown files in skills/, embedded rather
// than written as Go string literals: they are documents, they are edited as
// documents, and a hundred-line prompt inside a const is a hundred lines nobody
// will read a diff of.
// ---------------------------------------------------------------------------

var builtInSkillFiles = skills.Files

// Built-in skill ids. Named constants because the code that runs each brief
// looks its skill up by id, and a typo in a string literal would silently fall
// back to no brief at all.
const (
	skillArtistInterview = "artist-interview"
	skillAlbumInterview  = "album-interview"
	skillTrackGeneration = "track-generation"
	skillAlbumArtwork    = "album-artwork"
	skillMusicVideo      = "music-video"
	skillBugReports      = "bug-reports"
)

// builtIn is one shipped skill: its identity here, its body in skills/.
type builtIn struct {
	id          string
	title       string
	description string
	tags        []string
	file        string
}

// builtInSkills is the whole catalogue, in the order Skills lists them — the
// order of the workflow they belong to, artist through to release, rather than
// alphabetical.
var builtInSkills = []builtIn{
	{
		id:          skillArtistInterview,
		title:       "Artist interview",
		description: "The questions asked when an artist is created, and how the base technical style is written from the answers.",
		tags:        []string{"interview", "artist"},
		file:        "artist-interview.md",
	},
	{
		id:          skillAlbumInterview,
		title:       "Album interview",
		description: "How an album is named and how its own technical style is decided against the artist's.",
		tags:        []string{"interview", "album"},
		file:        "album-interview.md",
	},
	{
		id:          skillTrackGeneration,
		title:       "Track generation",
		description: "How a track's Suno prompt and lyrics are written from the album's combined style and the target length.",
		tags:        []string{"interview", "track", "suno"},
		file:        "track-generation.md",
	},
	{
		id:          skillAlbumArtwork,
		title:       "Album artwork",
		description: "The creative brief the album cover is generated from.",
		tags:        []string{"album", "artwork"},
		file:        "album-artwork.md",
	},
	{
		id:          skillMusicVideo,
		title:       "Music video",
		description: "How an album's music video looks: the visualiser, its colour, the background and the layout, chosen from the record's sound.",
		tags:        []string{"album", "video"},
		file:        "music-video.md",
	},
	{
		id:          skillBugReports,
		title:       "Bug reports",
		description: "How a report filed from the app becomes an issue on the repository, and how an agent works the queue.",
		tags:        []string{"bugs", "workflow"},
		file:        "bug-reports.md",
	},
}

// Skill is one skill as the frontend sees it.
type Skill struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Body        string   `json:"body"`
	Tags        []string `json:"tags"`
	// BuiltIn marks one of the shipped skills, which may be edited and reset but
	// never deleted — deleting one would leave the feature that runs it with no
	// brief and no way to get one back.
	BuiltIn bool `json:"builtIn"`
	// Overridden marks a built-in the producer has edited, so the page can offer
	// Reset only where there is something to reset to.
	Overridden bool   `json:"overridden"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

// defaultSkillBody reads a built-in's shipped text.
func defaultSkillBody(file string) string {
	data, err := builtInSkillFiles.ReadFile(file)
	if err != nil {
		// Embedded at build time from a path in this file, so a failure here is
		// a build that shipped without its own briefs. Empty rather than a panic:
		// the feature degrades to no brief, which is bad, where a panic would
		// take the whole window with it.
		return ""
	}
	return string(data)
}

// ListSkills reports every skill: the built-ins with any override applied, then
// the producer's own, newest first.
func (a *App) ListSkills() ([]Skill, error) {
	stored := map[string]Skill{}
	if a.store != nil {
		rows, err := a.store.listSkills()
		if err != nil {
			return nil, err
		}
		for _, s := range rows {
			stored[s.ID] = s
		}
	}

	out := []Skill{}
	for _, b := range builtInSkills {
		skill := Skill{
			ID:          b.id,
			Title:       b.title,
			Description: b.description,
			Body:        defaultSkillBody(b.file),
			Tags:        b.tags,
			BuiltIn:     true,
		}
		if override, ok := stored[b.id]; ok {
			skill.Body = override.Body
			skill.Overridden = true
			skill.UpdatedAt = override.UpdatedAt
			// The title and description stay the build's. They name a slot the
			// app looks the skill up by, and a producer who renamed "Track
			// generation" to "My prompt" would be looking for a skill the app no
			// longer appears to have.
		}
		out = append(out, skill)
	}

	written := []Skill{}
	for id, s := range stored {
		if isBuiltInSkill(id) {
			continue
		}
		written = append(written, s)
	}
	sort.Slice(written, func(i, j int) bool { return written[i].CreatedAt > written[j].CreatedAt })
	return append(out, written...), nil
}

// GetSkill reads one skill by id, built-in or written.
func (a *App) GetSkill(id string) (Skill, error) {
	skills, err := a.ListSkills()
	if err != nil {
		return Skill{}, err
	}
	for _, s := range skills {
		if s.ID == id {
			return s, nil
		}
	}
	return Skill{}, fmt.Errorf("no skill %s", id)
}

// skillBody is the text to brief a model with, backend-side. Empty for a skill
// that does not exist, which every caller treats as "no extra brief" rather than
// as a failure — a missing skill must not stop an interview.
func (a *App) skillBody(id string) string {
	skill, err := a.GetSkill(id)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(skill.Body)
}

// SaveSkill writes a skill: an override of a built-in, or one of the producer's
// own.
func (a *App) SaveSkill(skill Skill) (Skill, error) {
	if a.store == nil {
		return Skill{}, errNoStore
	}
	skill.Title = strings.TrimSpace(skill.Title)
	if skill.ID == "" && skill.Title == "" {
		return Skill{}, fmt.Errorf("a skill needs a title")
	}
	if skill.ID == "" {
		skill.ID = newID("skill")
	}
	if skill.Tags == nil {
		skill.Tags = []string{}
	}
	if err := a.store.saveSkill(skill); err != nil {
		return Skill{}, err
	}
	a.store.changed(scopeSkills)
	return a.GetSkill(skill.ID)
}

// ResetSkill throws an override away, so the built-in goes back to the build's
// own text — and keeps going back to it as later builds improve the default.
func (a *App) ResetSkill(id string) (Skill, error) {
	if a.store == nil {
		return Skill{}, errNoStore
	}
	if !isBuiltInSkill(id) {
		return Skill{}, fmt.Errorf("%s is not a built-in skill — delete it instead", id)
	}
	if err := a.store.deleteSkill(id); err != nil {
		return Skill{}, err
	}
	a.store.changed(scopeSkills)
	return a.GetSkill(id)
}

// DeleteSkill removes one of the producer's own. A built-in is refused: there
// would be nothing left to run the feature that reads it.
func (a *App) DeleteSkill(id string) error {
	if a.store == nil {
		return errNoStore
	}
	if isBuiltInSkill(id) {
		return fmt.Errorf("%s is built in — reset it instead of deleting it", id)
	}
	if err := a.store.deleteSkill(id); err != nil {
		return err
	}
	a.store.changed(scopeSkills)
	return nil
}

func isBuiltInSkill(id string) bool {
	for _, b := range builtInSkills {
		if b.id == id {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

func (s *Store) listSkills() ([]Skill, error) {
	out := []Skill{}
	if s == nil || s.db == nil {
		return out, nil
	}
	rows, err := s.db.Query(
		`SELECT id, title, description, body, tags, created_at, updated_at FROM skills`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var skill Skill
		var tags string
		if err := rows.Scan(&skill.ID, &skill.Title, &skill.Description, &skill.Body,
			&tags, &skill.CreatedAt, &skill.UpdatedAt); err != nil {
			return out, err
		}
		skill.Tags = []string{}
		// A tag list this app cannot decode is a row it wrote wrong; the skill
		// with no tags is a better answer than no skill.
		_ = json.Unmarshal([]byte(tags), &skill.Tags)
		if skill.Tags == nil {
			skill.Tags = []string{}
		}
		out = append(out, skill)
	}
	return out, rows.Err()
}

func (s *Store) saveSkill(skill Skill) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	tags, err := json.Marshal(skill.Tags)
	if err != nil {
		return err
	}
	now := nowStamp()
	created := skill.CreatedAt
	if created == "" {
		// Read back rather than assumed new: this is also the write that turns a
		// built-in into an override, and an override edited twice should keep the
		// date it was first made.
		var existing string
		if err := s.db.QueryRow(`SELECT created_at FROM skills WHERE id = ?`, skill.ID).
			Scan(&existing); err != nil && err != sql.ErrNoRows {
			return err
		}
		created = existing
		if created == "" {
			created = now
		}
	}
	_, err = s.db.Exec(
		`INSERT INTO skills (id, title, description, body, tags, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   title = excluded.title, description = excluded.description,
		   body = excluded.body, tags = excluded.tags, updated_at = excluded.updated_at`,
		skill.ID, skill.Title, skill.Description, skill.Body, string(tags), created, now)
	return err
}

func (s *Store) deleteSkill(id string) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	_, err := s.db.Exec(`DELETE FROM skills WHERE id = ?`, id)
	return err
}
