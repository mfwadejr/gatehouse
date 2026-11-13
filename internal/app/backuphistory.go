package app

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Backup history: one line for every backup made, restored, uploaded, downloaded or deleted, failed ones
// included, with who did it. It is kept beside the backups, not inside the settings, so a restore or a
// reset does not rewind or erase it. The newest 500 lines are kept.

const backupHistoryMax = 500

type BackupEvent struct {
	At     int64  `json:"at"`
	Action string `json:"action"` // created, failed, restored, restore-failed, uploaded, downloaded, deleted
	Kind   string `json:"kind"`   // manual, auto, before-reset, ...
	Name   string `json:"name"`
	Actor  string `json:"actor"` // a user name, or system
	Detail string `json:"detail,omitempty"`
}

var bkHistMu sync.Mutex

func (a *App) backupHistoryPath() string { return filepath.Join(a.backupDir(), "history.jsonl") }

// backupLog adds one line to the history. A failure to write it never stops the backup itself.
func (a *App) backupLog(actor, action, kind, name, detail string) {
	if actor == "" {
		actor = "system"
	}
	bkHistMu.Lock()
	defer bkHistMu.Unlock()
	os.MkdirAll(a.backupDir(), 0o700)
	lines := a.readBackupHistoryLocked()
	lines = append(lines, BackupEvent{At: time.Now().Unix(), Action: action, Kind: kind, Name: filepath.Base(name), Actor: actor, Detail: detail})
	if len(lines) > backupHistoryMax {
		lines = lines[len(lines)-backupHistoryMax:]
	}
	tmp := a.backupHistoryPath() + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	w := bufio.NewWriter(f)
	for _, e := range lines {
		b, _ := json.Marshal(e)
		w.Write(b)
		w.WriteByte('\n')
	}
	w.Flush()
	f.Close()
	os.Rename(tmp, a.backupHistoryPath())
}

func (a *App) readBackupHistoryLocked() []BackupEvent {
	f, err := os.Open(a.backupHistoryPath())
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []BackupEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e BackupEvent
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.At > 0 {
			out = append(out, e)
		}
	}
	return out
}

// backupHistory returns the history, newest first.
func (a *App) backupHistory() []BackupEvent {
	bkHistMu.Lock()
	defer bkHistMu.Unlock()
	l := a.readBackupHistoryLocked()
	for i, j := 0, len(l)-1; i < j; i, j = i+1, j-1 {
		l[i], l[j] = l[j], l[i]
	}
	if l == nil {
		l = []BackupEvent{}
	}
	return l
}
