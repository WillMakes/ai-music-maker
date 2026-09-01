package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Reading a take's length off the file (#85)
//
// The track's duration should say how long the song actually is once a real
// file exists, not how long it was aimed to be. Suno hands back MP3 and WAV,
// so those two are read here — headers only, no decoding, no dependency; a
// codec this cannot read answers 0 and the import carries on, because a take
// without a known length is still a take.
// ---------------------------------------------------------------------------

// audioDurationSeconds answers a file's playing time in whole seconds, or 0
// when the format is not one this reads or the file is malformed. Never an
// error: the length is a nicety on top of an import that has already worked.
func audioDurationSeconds(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	switch strings.ToLower(filepath.Ext(path)) {
	case ".wav":
		return wavDurationSeconds(f)
	case ".mp3":
		return mp3DurationSeconds(f)
	}
	return 0
}

// wavDurationSeconds is header arithmetic: the fmt chunk carries bytes per
// second, the data chunk carries how many bytes there are.
func wavDurationSeconds(f *os.File) int {
	var riff [12]byte
	if _, err := f.ReadAt(riff[:], 0); err != nil ||
		string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return 0
	}
	var byteRate uint32
	offset := int64(12)
	for {
		var head [8]byte
		if _, err := f.ReadAt(head[:], offset); err != nil {
			return 0
		}
		size := binary.LittleEndian.Uint32(head[4:8])
		switch string(head[0:4]) {
		case "fmt ":
			var fmtChunk [16]byte
			if size < 16 {
				return 0
			}
			if _, err := f.ReadAt(fmtChunk[:], offset+8); err != nil {
				return 0
			}
			byteRate = binary.LittleEndian.Uint32(fmtChunk[8:12])
		case "data":
			if byteRate == 0 {
				return 0
			}
			return int(int64(size) / int64(byteRate))
		}
		// Chunks are word-aligned; an odd size is padded by one byte.
		offset += 8 + int64(size) + int64(size%2)
	}
}

// mp3 frame tables, Layer III only — that is what an .mp3 is. Row 0 is MPEG1,
// row 1 is MPEG2 and 2.5; a 0 in the bitrate table is "free"/invalid.
var mp3Bitrates = [2][16]int{
	{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
	{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
}
var mp3SampleRates = [4][4]int{
	{11025, 12000, 8000, 0},  // MPEG2.5
	{0, 0, 0, 0},             // reserved
	{22050, 24000, 16000, 0}, // MPEG2
	{44100, 48000, 32000, 0}, // MPEG1
}

// mp3DurationSeconds finds the first frame, prefers the Xing/Info header a
// variable-bitrate encoder writes (frame count times samples per frame), and
// falls back to constant-bitrate arithmetic over the file size.
func mp3DurationSeconds(f *os.File) int {
	info, err := f.Stat()
	if err != nil {
		return 0
	}
	start := int64(0)
	// An ID3v2 tag sits before the audio; its size is syncsafe.
	var id3 [10]byte
	if _, err := f.ReadAt(id3[:], 0); err == nil && string(id3[0:3]) == "ID3" {
		size := int64(id3[6]&0x7f)<<21 | int64(id3[7]&0x7f)<<14 |
			int64(id3[8]&0x7f)<<7 | int64(id3[9]&0x7f)
		start = 10 + size
	}

	// The sync scan is bounded: a real MP3's first frame is at the front, and
	// a file where it is not is not worth reading to the end for.
	buf := make([]byte, 64*1024)
	n, _ := f.ReadAt(buf, start)
	for i := 0; i+4 <= n; i++ {
		if buf[i] != 0xFF || buf[i+1]&0xE0 != 0xE0 {
			continue
		}
		version := (buf[i+1] >> 3) & 0x3 // 0=2.5, 2=2, 3=1
		layer := (buf[i+1] >> 1) & 0x3   // 1 = Layer III
		if version == 1 || layer != 1 {
			continue
		}
		row := 0
		samplesPerFrame := 1152
		if version != 3 {
			row = 1
			samplesPerFrame = 576
		}
		bitrate := mp3Bitrates[row][(buf[i+2]>>4)&0xF]
		sampleRate := mp3SampleRates[version][(buf[i+2]>>2)&0x3]
		if bitrate == 0 || sampleRate == 0 {
			continue
		}

		// The Xing/Info header lives after the side information, whose size
		// depends on version and channel mode (mono is the smaller one).
		mono := (buf[i+3]>>6)&0x3 == 3
		side := 32
		if version == 3 && mono {
			side = 17
		} else if version != 3 {
			side = 17
			if mono {
				side = 9
			}
		}
		at := i + 4 + side
		if at+16 <= n && (string(buf[at:at+4]) == "Xing" || string(buf[at:at+4]) == "Info") {
			flags := binary.BigEndian.Uint32(buf[at+4 : at+8])
			if flags&0x1 != 0 {
				frames := binary.BigEndian.Uint32(buf[at+8 : at+12])
				return int(int64(frames) * int64(samplesPerFrame) / int64(sampleRate))
			}
		}
		return int((info.Size() - start - int64(i)) * 8 / int64(bitrate) / 1000)
	}
	return 0
}
