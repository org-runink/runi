// SPDX-License-Identifier: BSD-3-Clause

package avro_test

import (
	"bytes"
	"fmt"

	"github.com/org-runink/runi/avro"
)

type order struct {
	ID    int64
	SKU   string
	Total float64
	Note  string // empty means no note
}

// The schema travels inside the file, so a reader needs only the decoder below.
const orderSchema = `{"type":"record","name":"Order","fields":[
	{"name":"id","type":"long"},
	{"name":"sku","type":"string"},
	{"name":"total","type":"double"},
	{"name":"note","type":["null","string"]}]}`

// Fields are written and read in schema order; the binary encoding carries no names.
func marshalOrder(e *avro.Encoder, o order) {
	e.Long(o.ID)
	e.String(o.SKU)
	e.Double(o.Total)
	e.OptionalString(o.Note, o.Note != "")
}

func unmarshalOrder(d *avro.Decoder) (order, error) {
	var o order
	var err error
	if o.ID, err = d.Long(); err != nil {
		return o, err
	}
	if o.SKU, err = d.String(); err != nil {
		return o, err
	}
	if o.Total, err = d.Double(); err != nil {
		return o, err
	}
	o.Note, _, err = d.OptionalString()
	return o, err
}

func Example() {
	orders := []order{
		{ID: 1001, SKU: "A-17", Total: 42.50},
		{ID: 1002, SKU: "B-03", Total: 9.99, Note: "gift wrap"},
	}

	var file bytes.Buffer
	if err := avro.WriteOCF(&file, orderSchema, avro.CodecDeflate, orders, marshalOrder); err != nil {
		panic(err)
	}

	// The bytes as an object store would hand them back.
	hdr, got, err := avro.ReadOCFBytes(file.Bytes(), unmarshalOrder)
	if err != nil {
		panic(err)
	}
	fmt.Println("codec:", hdr.Codec)
	for _, o := range got {
		fmt.Printf("%d %s %.2f %q\n", o.ID, o.SKU, o.Total, o.Note)
	}
	// Output:
	// codec: deflate
	// 1001 A-17 42.50 ""
	// 1002 B-03 9.99 "gift wrap"
}

func ExampleEncoder() {
	e := avro.NewEncoder()
	e.String("sensor-7")
	e.Long(-3) // zig-zag: small negatives stay one byte
	e.Float64Array([]float64{20.5, 21.0})

	d := avro.NewDecoder(bytes.NewReader(e.Bytes()))
	name, _ := d.String()
	delta, _ := d.Long()
	temps, _ := d.Float64Array()
	fmt.Println(name, delta, temps)
	// Output: sensor-7 -3 [20.5 21]
}
