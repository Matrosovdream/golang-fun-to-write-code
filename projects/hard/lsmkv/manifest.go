package lsmkv

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// manifest is the single source of truth for what is live: the SSTables
// (oldest first, so later means newer), the number of the current WAL, and
// the next unused file number. It is replaced as a whole by an atomic rename.
type manifest struct {
	Wal    uint64   `json:"wal"`
	Next   uint64   `json:"next"`
	Tables []uint64 `json:"tables"`
}

func (m manifest) encode() []byte {
	b, _ := json.Marshal(m)
	return append(b, '\n')
}

func decodeManifest(b []byte) (m manifest, err error) {
	if err := json.Unmarshal(b, &m); err != nil {
		return m, corruptf("manifest: %v", err)
	}
	if m.Wal == 0 || m.Next <= m.Wal {
		return m, corruptf("manifest: wal %d, next %d", m.Wal, m.Next)
	}
	return m, nil
}

// readManifest returns ok=false when the directory has none yet.
func readManifest(dir string) (m manifest, ok bool, err error) {
	b, err := os.ReadFile(filepath.Join(dir, "MANIFEST"))
	if errors.Is(err, fs.ErrNotExist) {
		return m, false, nil
	}
	if err != nil {
		return m, false, err
	}
	m, err = decodeManifest(b)
	return m, true, err
}

// writeManifest is the commit point of a flush or compaction: write a temp
// file, fsync it, rename over MANIFEST, fsync the directory. A crash leaves
// either the old manifest or the new one, never a mix.
func writeManifest(dir string, m manifest) error {
	tmp := filepath.Join(dir, "MANIFEST.tmp")
	if err := writeFileSync(tmp, m.encode()); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, "MANIFEST")); err != nil {
		return err
	}
	return syncDir(dir)
}

func writeFileSync(path string, b []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, werr := f.Write(b)
	serr := f.Sync()
	return errors.Join(werr, serr, f.Close())
}

// syncDir makes renames and creations inside dir durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}
