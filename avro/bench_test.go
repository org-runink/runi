// SPDX-License-Identifier: BSD-3-Clause

package avro

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"testing"
)

// The shape the cross-library comparison measures: 20,000 records of a string
// and three doubles. Reading used to cost ten times writing, so the benchmark
// exists to keep the two in sight of each other -- and to be profiled, since
// the reason was never where guessing put it.
type benchRow struct {
	Name string
	Vals []float64
}

const benchSchema = `{"type":"record","name":"R","fields":[{"name":"name","type":"string"},` +
	`{"name":"vals","type":{"type":"array","items":"double"}}]}`

func benchMarshal(e *Encoder, v benchRow) { e.String(v.Name); e.Float64Array(v.Vals) }

func benchUnmarshal(d *Decoder) (benchRow, error) {
	var v benchRow
	var err error
	if v.Name, err = d.String(); err != nil {
		return v, err
	}
	v.Vals, err = d.Float64Array()
	return v, err
}

func benchRows(n int) []benchRow {
	r := rand.New(rand.NewPCG(7, 11))
	rows := make([]benchRow, n)
	for i := range rows {
		rows[i] = benchRow{
			Name: fmt.Sprintf("row-%06d", i),
			Vals: []float64{r.Float64(), r.Float64(), r.Float64()},
		}
	}
	return rows
}

func BenchmarkReadOCF(b *testing.B) {
	var buf bytes.Buffer
	if err := WriteOCF(&buf, benchSchema, CodecNull, benchRows(20000), benchMarshal); err != nil {
		b.Fatal(err)
	}
	body := buf.Bytes()
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := ReadOCF(bytes.NewReader(body), benchUnmarshal); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteOCF(b *testing.B) {
	rows := benchRows(20000)
	var buf bytes.Buffer
	b.ReportAllocs()
	for b.Loop() {
		buf.Reset()
		if err := WriteOCF(&buf, benchSchema, CodecNull, rows, benchMarshal); err != nil {
			b.Fatal(err)
		}
	}
}
