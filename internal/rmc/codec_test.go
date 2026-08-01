package rmc

import (
	"bytes"
	"testing"
)

func TestRequestRoundTrip(t *testing.T) {
	want := Request{Protocol: 60, Method: 20, Call: 42, Params: []byte{1, 2, 3}}
	data, err := Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	gotMessage, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := gotMessage.(Request)
	if !ok {
		t.Fatalf("decoded %T, want Request", gotMessage)
	}
	if got.Protocol != want.Protocol || got.Method != want.Method || got.Call != want.Call || !bytes.Equal(got.Params, want.Params) {
		t.Fatalf("decoded %#v, want %#v", got, want)
	}
}

func TestResponseRoundTrip(t *testing.T) {
	want := ResponseOK{Protocol: 25, Method: 21, Call: 7, Return: []byte{9, 8}}
	data, err := Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	gotMessage, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	got := gotMessage.(ResponseOK)
	if got.Method != want.Method || !bytes.Equal(got.Return, want.Return) {
		t.Fatalf("decoded %#v, want %#v", got, want)
	}
}
