# Music video

How an album's music video looks. Each song is shown over the album's cover
with its title, the artist and the album, and a visualisation of the audio;
the songs fade in and out with a silent gap between them. The record's name,
the artist, the combined technical style and the brief the cover was drawn
from are supplied below this. You choose the look from a fixed set of options
and answer with them as JSON.

## The choices

- **visualizer** — `waves`: the waveform as a line, drawn as the song plays.
  `bars`: an equaliser of frequency bars.
- **color** — the visualisation and the titles, as a six-digit hex colour
  (`7FE3FF`). It is drawn over the cover, so it has to stand clear of it.
- **background** — `blur`: the cover, blurred and darkened, filling the frame
  behind the sharp one. `black`: plain black around the cover.
- **layout** — `centered`: the cover in the middle, the titles under it, the
  visualisation across the foot of the frame. `split`: the cover on the left,
  the titles and the visualisation to its right.
- **fade** — how long each song's picture fades in and out, in seconds, from
  0.3 to 3.

## Choosing

Let the record decide, the way the cover's brief lets the production
character decide the medium:

- **Dense, driving, percussive** records — club music, drum and bass, synth
  pop with gated drums — read as `bars`. An equaliser moves with a kick drum.
- **Sparse, melodic, vocal-led** records — folk, ambient, piano, singer-
  songwriter — read as `waves`. A line moves with a voice.
- **color** comes from the cover. Where the cover's brief names a palette,
  take its brightest accent; otherwise white (`FFFFFF`) is never wrong over a
  darkened blur. Avoid mid-greys and anything close to the cover's dominant
  colour.
- **background** is `blur` unless the cover is mostly black or the record is
  austere — minimal techno, drone, a record described as cold or bare — where
  `black` says the same thing.
- **layout** is `centered` by default. `split` suits a record with long titles,
  which read better on a line of their own beside the cover, and records that
  want to feel like an editorial or a listening session rather than a show.
- **fade** around 1 second. Longer — 2 or so — for slow, ambient records;
  shorter — 0.5 — for fast ones, where a long fade drags.

## What not to do

There are no options beyond these. Do not invent another visualiser, a
second colour, text, effects or a layout not listed: anything outside the set
is ignored and the default is used instead.

## Answer

Only the JSON object, with every field:

```json
{"visualizer": "waves", "color": "FFFFFF", "background": "blur", "layout": "centered", "fade": 1}
```
