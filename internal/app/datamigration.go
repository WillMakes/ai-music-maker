package app

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// The rename from SunoMaker (#103)
//
// The app was SunoMaker and kept everything in ~/.sunomaker, in a database
// called sunomaker.db. Renamed to AIMusicMaker, it moves both on first launch
// rather than starting empty beside them: a producer who updates should open
// the same library they closed.
//
// Moved rather than copied, so there is never a second library to drift from
// the first. And moved only when the new folder does not exist yet: if both are
// there, the new one is the one in use and the old one is left exactly as it
// is — merging two libraries by guesswork is how one of them gets lost.
//
// A move can fail. On Windows a folder with an open file in it cannot be
// renamed, and an old build still running (a debugger session, say) holds the
// database open. Then this launch carries on in the old folder, and the move is
// tried again next time. Nothing is half-moved: the folder rename is one
// operation, and it either happened or it did not.
//
// The music folder is not part of this. It lives in the OS music folder, every
// take is stored by absolute path, and it is the producer's library rather than
// the app's working files (see musicdir.go).
// ---------------------------------------------------------------------------

const (
	dataDirName       = ".aimusicmaker"
	legacyDataDirName = ".sunomaker"
	dbName            = "aimusicmaker.db"
	legacyDBName      = "sunomaker.db"
)

// dataDir returns the app's data directory (see settleDataDir), creating it
// (0700) if necessary. Everything the app owns — the database, the exported
// extension, generated artwork — hangs off this one place, so uninstalling is
// deleting a folder.
//
// A variable so a test can point it somewhere disposable. Not defensive
// plumbing: artwork lands under here, and a test suite that wrote into the
// producer's real directory is exactly what CLAUDE.md means by never touching
// the real data folder. newTestApp overrides it.
var dataDir = realDataDir

// settledDataDir is decided once per process. A move retried halfway through a
// run could succeed with the database open — and on macOS and Linux a rename
// does not care that a file is open — leaving this process writing to a folder
// whose name it no longer agrees on.
var settledDataDir struct {
	once sync.Once
	dir  string
	err  error
}

func realDataDir() (string, error) {
	settledDataDir.once.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			settledDataDir.err = err
			return
		}
		settledDataDir.dir, settledDataDir.err = settleDataDir(home)
	})
	return settledDataDir.dir, settledDataDir.err
}

// settleDataDir picks the data folder under home, moving the old one to the new
// name when only the old one exists.
func settleDataDir(home string) (string, error) {
	dir := filepath.Join(home, dataDirName)
	legacy := filepath.Join(home, legacyDataDirName)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if info, err := os.Stat(legacy); err == nil && info.IsDir() {
			if err := os.Rename(legacy, dir); err != nil {
				log.Printf("aimusicmaker: could not move %s to %s yet, using it where it is: %v", legacy, dir, err)
				return legacy, nil
			}
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// settleStorePath is the database inside dir, renamed from the old name when
// only the old one is there.
//
// The write-ahead log goes with it. It holds writes SQLite has not yet folded
// into the database, so a database renamed without its log has lost them. The
// database is renamed first, because if that fails nothing has changed; if the
// log will not follow, the database is put back and the old name is used for
// this launch. The -shm file is only an index over the log, which SQLite
// rebuilds, so it is moved when it can be and otherwise left.
func settleStorePath(dir string) string {
	path := filepath.Join(dir, dbName)
	legacy := filepath.Join(dir, legacyDBName)
	if fileExists(path) || !fileExists(legacy) {
		return path
	}
	if err := os.Rename(legacy, path); err != nil {
		log.Printf("aimusicmaker: could not rename %s yet, using it as it is: %v", legacy, err)
		return legacy
	}
	if fileExists(legacy + "-wal") {
		if err := os.Rename(legacy+"-wal", path+"-wal"); err != nil {
			_ = os.Rename(path, legacy)
			log.Printf("aimusicmaker: could not rename %s-wal yet, using the database as it is: %v", legacy, err)
			return legacy
		}
	}
	_ = os.Rename(legacy+"-shm", path+"-shm")
	return path
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// rewriteLegacyPaths points stored paths that name the old data folder at the
// new one. Two places record such paths: a bug report's attachments and the
// files sent on an interview, both as JSON — so the prefix is matched in its
// JSON form, where a Windows backslash is written twice.
//
// Run on every launch rather than once. It is two UPDATEs over small tables that
// match nothing after the first time, and an old build that
// wrote to the old folder in the meantime (the case settleDataDir allows for)
// has its rows caught up too.
func (s *Store) rewriteLegacyPaths(legacy, current string) error {
	from, err := jsonPathPrefix(legacy)
	if err != nil {
		return err
	}
	to, err := jsonPathPrefix(current)
	if err != nil {
		return err
	}
	for _, column := range []struct{ table, column string }{
		{"bug_reports", "attachments"},
		{"chats", "messages"},
	} {
		query := "UPDATE " + column.table + " SET " + column.column + " = REPLACE(" + column.column + ", ?, ?) WHERE instr(" + column.column + ", ?) > 0"
		if _, err := s.db.Exec(query, from, to, from); err != nil {
			return err
		}
	}
	return nil
}

// jsonPathPrefix is dir as it appears inside a JSON string, with a trailing
// separator so ".sunomaker" does not also match ".sunomaker-backup".
func jsonPathPrefix(dir string) (string, error) {
	encoded, err := json.Marshal(dir + string(filepath.Separator))
	if err != nil {
		return "", err
	}
	return strings.Trim(string(encoded), `"`), nil
}
