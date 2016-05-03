package upspin

import (
	"reflect"
	"testing"
)

var dirEnt = DirEntry{
	Name: "u@foo.com/a/directory", // so IsDir is not the zero value.
	Location: Location{
		Endpoint: Endpoint{
			Transport: GCP,
			NetAddr:   "foo.com:1234",
		},
		Reference: "Chubb",
	},
	Metadata: Metadata{
		Attr:     AttrDirectory,
		Sequence: 1234,
		Size:     12345,
		Time:     123456,
		Packdata: []byte{1, 2, 3},
	},
}

func TestDirEntMarshal(t *testing.T) {
	data, err := dirEnt.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var new DirEntry
	remaining, err := new.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("data remains after unmarshal")
	}
	if !reflect.DeepEqual(&dirEnt, &new) {
		t.Errorf("bad result. expected:")
		t.Errorf("%+v\n", &dirEnt)
		t.Errorf("got:")
		t.Errorf("%+v\n", &new)
	}
}

func TestDirEntMarshalAppendNoMalloc(t *testing.T) {
	// Marshal to see what length we need.
	data, err := dirEnt.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// Toss old data but keep length.
	data = make([]byte, len(data))
	p := &data[0]
	data, err = dirEnt.MarshalAppend(data[:0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if p != &data[0] {
		t.Fatalf("MarshalAppend allocated")
	}
}
