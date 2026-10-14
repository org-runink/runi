package barn

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/org-runink/runi/avro"
)

// Until v0.8.0 this package was called tablelog, and the Avro schemas written into
// every data file and checkpoint said so: namespace org.runink.store.tablelog. The
// rename changed the namespace in the schemas it WRITES. Tables already on disk keep
// the old one, so a table written by v0.8.0 must still open, read and time-travel
// after the rename. It does because files are decoded by field position and the
// schema in a file's header is not compared; this pins that, by taking a table and
// rewriting every file in it exactly as v0.8.0 would have written it.
func TestTablesWrittenAsTablelogStillOpen(t *testing.T) {
	ctx := context.Background()
	st := NewMemStore()
	tb, err := Open(st, "acme", "events", WithCheckpointInterval(2))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []Row{{Key: "a", Payload: []byte("one")}, {Key: "b", Payload: []byte("two")}, {Key: "a", Payload: []byte("three")}} {
		if _, err := tb.Put(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	oldRow := strings.Replace(rowSchema, "org.runink.store.barn", "org.runink.store.tablelog", 1)
	oldCP := strings.Replace(checkpointSchema, "org.runink.store.barn", "org.runink.store.tablelog", 1)
	keys, err := st.List(ctx, "tables/acme/events/")
	if err != nil {
		t.Fatal(err)
	}
	rewritten := map[string]int{}
	for _, k := range keys {
		body, err := tb.readObject(ctx, k)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(body, []byte("Obj\x01")) {
			continue // the commit log entries and the hint are not Avro
		}
		var buf bytes.Buffer
		kind := "data"
		if strings.Contains(k, "_checkpoint/") {
			kind = "checkpoint"
			_, files, err := avro.ReadOCFBytes(body, unmarshalCP)
			if err != nil {
				t.Fatal(err)
			}
			err = writeCPOCF(&buf, oldCP, avro.CodecDeflate, files, marshalCP)
			if err != nil {
				t.Fatal(err)
			}
		} else {
			_, rows, err := avro.ReadOCFBytes(body, unmarshalRow)
			if err != nil {
				t.Fatal(err)
			}
			if err := writeRowsOCF(&buf, oldRow, avro.CodecDeflate, rows, marshalRow); err != nil {
				t.Fatal(err)
			}
		}
		h, _, err := avro.ReadOCFBytes(buf.Bytes(), unmarshalRow)
		if kind == "checkpoint" {
			h, _, err = avro.ReadOCFBytes(buf.Bytes(), unmarshalCP)
		}
		if err != nil || !strings.Contains(h.Schema, "org.runink.store.tablelog") {
			t.Fatalf("%s was not rewritten with the old schema: %v %q", k, err, h.Schema)
		}
		if err := st.Put(ctx, k, bytes.NewReader(buf.Bytes()), int64(buf.Len()), ""); err != nil {
			t.Fatal(err)
		}
		rewritten[kind]++
	}
	if rewritten["data"] == 0 || rewritten["checkpoint"] == 0 {
		t.Fatalf("rewrote %v; the test needs at least one data file and one checkpoint", rewritten)
	}

	// A fresh handle, so nothing is served from the first one's memory.
	again, err := Open(st, "acme", "events", WithCheckpointInterval(2))
	if err != nil {
		t.Fatal(err)
	}
	rec, ok, err := again.Get(ctx, "a")
	if err != nil || !ok || string(rec.Payload) != "three" {
		t.Fatalf("Get(a) = %q %v %v, want three", rec.Payload, ok, err)
	}
	snap, err := again.Snapshot(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rec, ok, err := snap.Get(ctx, "a"); err != nil || !ok || string(rec.Payload) != "one" {
		t.Fatalf("Snapshot(1).Get(a) = %q %v %v, want one", rec.Payload, ok, err)
	}
	if _, err := again.Put(ctx, Row{Key: "c", Payload: []byte("four")}); err != nil {
		t.Fatalf("writing to an old table: %v", err)
	}
}
