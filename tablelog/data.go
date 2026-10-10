// SPDX-License-Identifier: BSD-3-Clause

package tablelog

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/org-runink/runi/avro"
)

// row is one entry of a data file.
//
// ver/ord order rows for last-writer-wins. An appended file is written before
// its commit version is known (a lost race moves it to a later one), so its rows
// carry ver 0, meaning "the version of the commit that added this file".
// Compaction output carries each surviving row's original (ver, ord): a
// compaction can commit after a concurrent append, and stamping its rows with
// its own, higher version would let stale values beat newer ones.
type row struct {
	key     string
	del     bool
	ts      int64 // unix µs
	payload []byte
	ver     int64
	ord     int64
}

func (r row) newer(than row) bool {
	if r.ver != than.ver {
		return r.ver > than.ver
	}
	return r.ord > than.ord
}

func (r row) record() Record {
	return Record{Key: r.key, Payload: r.payload, Version: r.ver, TS: time.UnixMicro(r.ts).UTC()}
}

const rowSchema = `{"type":"record","name":"Row","namespace":"org.runink.store.tablelog","fields":[` +
	`{"name":"key","type":"string"},` +
	`{"name":"op","type":{"type":"enum","name":"Op","symbols":["put","del"]}},` +
	`{"name":"ts","type":{"type":"long","logicalType":"timestamp-micros"}},` +
	`{"name":"payload","type":"bytes"},` +
	`{"name":"ver","type":"long"},` +
	`{"name":"ord","type":"long"}]}`

func marshalRow(e *avro.Encoder, r row) {
	e.String(r.key)
	if r.del {
		e.Long(1)
	} else {
		e.Long(0)
	}
	e.Long(r.ts)
	e.Blob(r.payload)
	e.Long(r.ver)
	e.Long(r.ord)
}

func unmarshalRow(d *avro.Decoder) (row, error) {
	var r row
	var err error
	if r.key, err = d.String(); err != nil {
		return r, err
	}
	op, err := d.Long()
	if err != nil {
		return r, err
	}
	switch op {
	case 0:
	case 1:
		r.del = true
	default:
		return r, fmt.Errorf("tablelog: bad op %d", op)
	}
	if r.ts, err = d.Long(); err != nil {
		return r, err
	}
	if r.payload, err = d.Blob(); err != nil {
		return r, err
	}
	if r.ver, err = d.Long(); err != nil {
		return r, err
	}
	r.ord, err = d.Long()
	return r, err
}

// newDataPath names a data file: data/<16 hex µs creation time><32 hex random>.avro.
func (t *Table) newDataPath() string {
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(t.cfg.now().UnixMicro()))
	return "data/" + hex.EncodeToString(ts[:]) + randHex(16) + ".avro"
}

// dataFileCreated parses the creation time out of a data path; ok=false for a
// name this package did not write.
func dataFileCreated(path string) (time.Time, bool) {
	name, found := strings.CutPrefix(path, "data/")
	if !found {
		return time.Time{}, false
	}
	name, found = strings.CutSuffix(name, ".avro")
	if !found || len(name) != 48 {
		return time.Time{}, false
	}
	b, err := hex.DecodeString(name[:16])
	if err != nil {
		return time.Time{}, false
	}
	if _, err := hex.DecodeString(name[16:]); err != nil {
		return time.Time{}, false
	}
	return time.UnixMicro(int64(binary.BigEndian.Uint64(b))), true
}

// writeDataFile writes rows (in the given order) as one immutable OCF and
// returns its log entry. Nothing references it until a commit does.
func (t *Table) writeDataFile(ctx context.Context, rs []row) (fileEntry, error) {
	var buf bytes.Buffer
	if err := writeRowsOCF(&buf, rowSchema, avro.CodecDeflate, rs, marshalRow); err != nil {
		return fileEntry{}, fmt.Errorf("tablelog: encode: %w", err)
	}
	fe := fileEntry{Path: t.newDataPath(), Rows: int64(len(rs)), Bytes: int64(buf.Len())}
	for i, r := range rs {
		if i == 0 || r.key < fe.MinKey {
			fe.MinKey = r.key
		}
		if i == 0 || r.key > fe.MaxKey {
			fe.MaxKey = r.key
		}
		if r.del {
			fe.Dels++
		}
	}
	if err := t.st.PutIfAbsent(ctx, t.root+fe.Path, buf.Bytes()); err != nil {
		return fileEntry{}, fmt.Errorf("tablelog: write %s: %w", fe.Path, err)
	}
	return fe, nil
}

// bodies holds the buffers data-file bodies are read into. A scan reads every
// live file, and the rows it decodes are copies — no row points into the body
// it came from — so the buffer is free the moment the file is decoded, and
// reusing it keeps a scan's allocation proportional to the rows it returns
// rather than to the files it opened.
var bodies = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// readDataFile reads one file and resolves ver 0 to the file's add version.
func (t *Table) readDataFile(ctx context.Context, fe fileEntry) ([]row, error) {
	rc, err := t.st.Get(ctx, t.root+fe.Path)
	if err != nil {
		return nil, fmt.Errorf("tablelog: read %s: %w", fe.Path, err)
	}
	defer rc.Close()
	body := bodies.Get().(*bytes.Buffer)
	defer bodies.Put(body)
	body.Reset()
	if _, err := body.ReadFrom(rc); err != nil {
		return nil, fmt.Errorf("tablelog: read %s: %w", fe.Path, err)
	}
	_, rows, err := avro.ReadOCFBytes(body.Bytes(), unmarshalRow)
	if err != nil {
		return nil, fmt.Errorf("%w: decode %s: %v", ErrCorrupt, fe.Path, err)
	}
	for i := range rows {
		if rows[i].ver == 0 {
			rows[i].ver = fe.Version
		}
	}
	return rows, nil
}

// readFiles reads files with bounded parallelism, preserving order.
func (t *Table) readFiles(ctx context.Context, files []fileEntry) ([][]row, error) {
	out := make([][]row, len(files))
	errs := make([]error, len(files))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, f := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out[i], errs[i] = t.readDataFile(ctx, f)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// writeRowsOCF is avro.WriteOCF behind a variable. Encoding writes to an
// in-memory buffer, so the only way it fails is if the sync marker cannot be
// generated — real, but not something a caller of this package can arrange.
// The error is returned rather than ignored because a data file written
// without a usable marker cannot be read back, and an error that is returned
// but never exercised is not known to work.
var writeRowsOCF = avro.WriteOCF[row]
