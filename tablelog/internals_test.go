package tablelog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/org-runink/runi/avro"
)

// Each field of a row is read in sequence, so a file truncated at any point
// must fail at the field it stopped in. Sweeping every prefix walks all of
// them. A half-read row that came back without an error would enter the table
// as real data with empty fields.
func TestUnmarshalRowTruncatedAtEveryField(t *testing.T) {
	e := avro.NewEncoder()
	marshalRow(e, row{key: "customer-42", ts: 1700000000000000, payload: []byte("hello"), ver: 7, ord: 3})
	b := e.Bytes()
	for n := 0; n < len(b); n++ {
		if _, err := unmarshalRow(avro.NewDecoder(bytes.NewReader(b[:n]))); err == nil {
			t.Errorf("row truncated to %d of %d bytes decoded without error", n, len(b))
		}
	}
	got, err := unmarshalRow(avro.NewDecoder(bytes.NewReader(b)))
	if err != nil {
		t.Fatalf("full row: %v", err)
	}
	if got.key != "customer-42" || got.ver != 7 || got.ord != 3 || string(got.payload) != "hello" {
		t.Errorf("round trip = %+v", got)
	}
	if got.del {
		t.Error("a put came back as a delete")
	}
}

func TestUnmarshalRowTombstoneAndBadOp(t *testing.T) {
	e := avro.NewEncoder()
	marshalRow(e, row{key: "k", del: true, ts: 1, ver: 2, ord: 0})
	got, err := unmarshalRow(avro.NewDecoder(bytes.NewReader(e.Bytes())))
	if err != nil || !got.del {
		t.Fatalf("tombstone round trip: %+v %v", got, err)
	}

	// An op that is neither put nor delete is corruption. Defaulting it to a
	// put would resurrect deleted rows.
	e.Reset()
	e.String("k")
	e.Long(99)
	e.Long(1)
	e.Blob(nil)
	e.Long(1)
	e.Long(0)
	if _, err := unmarshalRow(avro.NewDecoder(bytes.NewReader(e.Bytes()))); err == nil {
		t.Error("an unknown op decoded without error")
	}
}

func TestUnmarshalCheckpointTruncatedAtEveryField(t *testing.T) {
	e := avro.NewEncoder()
	marshalCP(e, fileEntry{
		Path: "data/x.avro", Rows: 10, MinKey: "a", MaxKey: "z",
		Bytes: 2048, Dels: 1, Version: 4,
	})
	b := e.Bytes()
	for n := 0; n < len(b); n++ {
		if _, err := unmarshalCP(avro.NewDecoder(bytes.NewReader(b[:n]))); err == nil {
			t.Errorf("checkpoint entry truncated to %d of %d bytes decoded without error", n, len(b))
		}
	}
	got, err := unmarshalCP(avro.NewDecoder(bytes.NewReader(b)))
	if err != nil {
		t.Fatalf("full entry: %v", err)
	}
	if got.Path != "data/x.avro" || got.Rows != 10 || got.Version != 4 || got.Dels != 1 {
		t.Errorf("round trip = %+v", got)
	}
}

// Only a name this package wrote carries a creation time. Everything else must
// be reported as unrecognised rather than guessed at, because the guess would
// decide whether someone's file gets deleted.
func TestDataFileCreatedRejectsForeignNames(t *testing.T) {
	good := fmt.Sprintf("data/%016x%032x.avro", time.Now().UnixMicro(), 0)
	if _, ok := dataFileCreated(good); !ok {
		t.Errorf("a well-formed name was rejected: %s", good)
	}
	for _, bad := range []string{
		"notdata/" + good[5:],                                   // wrong directory
		"data/" + good[5:len(good)-5] + ".json",                 // wrong extension
		"data/tooshort.avro",                                    // wrong length
		fmt.Sprintf("data/%048s.avro", "zz"),                    // not hex at all
		fmt.Sprintf("data/%016x%032s.avro", 1, "not-hex-here!"), // hex time, bad hash
		"data/.avro",
		"",
	} {
		if _, ok := dataFileCreated(bad); ok {
			t.Errorf("accepted a name it did not write: %q", bad)
		}
	}
}

// Two writes of the same key are ordered by version first and position within
// the commit second, so the later one always wins.
func TestRowNewer(t *testing.T) {
	cases := []struct {
		a, b row
		want bool
	}{
		{row{ver: 2, ord: 0}, row{ver: 1, ord: 9}, true},
		{row{ver: 1, ord: 9}, row{ver: 2, ord: 0}, false},
		{row{ver: 3, ord: 5}, row{ver: 3, ord: 4}, true},
		{row{ver: 3, ord: 4}, row{ver: 3, ord: 5}, false},
		{row{ver: 3, ord: 4}, row{ver: 3, ord: 4}, false},
	}
	for _, c := range cases {
		if got := c.a.newer(c.b); got != c.want {
			t.Errorf("%+v newer than %+v = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestRowRecord(t *testing.T) {
	r := row{key: "k", payload: []byte("v"), ver: 3, ts: 1700000000000000}
	rec := r.record()
	if rec.Key != "k" || string(rec.Payload) != "v" || rec.Version != 3 {
		t.Errorf("record = %+v", rec)
	}
	if rec.TS.Location() != time.UTC {
		t.Errorf("timestamp is not UTC: %v", rec.TS.Location())
	}
}

// Backoff waits, but never past the caller's deadline: a commit retry that
// ignores a cancelled context turns a client's timeout into a stuck request.
func TestBackoffHonoursContextAndClamps(t *testing.T) {
	tb, err := Open(NewMemStore(), "acme", "orders")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// A normal attempt waits briefly and returns.
	if err := tb.backoff(context.Background(), 0); err != nil {
		t.Errorf("attempt 0: %v", err)
	}

	// An already-cancelled context comes straight back with its own error,
	// including on a huge attempt number, which is also what exercises the
	// clamp: shifting the base left far enough would otherwise overflow to a
	// negative duration and wait forever.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, attempt := range []int{0, 17, 1000} {
		if err := tb.backoff(cancelled, attempt); !errors.Is(err, context.Canceled) {
			t.Errorf("attempt %d on a cancelled context: %v, want context.Canceled", attempt, err)
		}
	}
}
