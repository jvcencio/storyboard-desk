package clientwire

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidMatchesLegacy(t *testing.T) {
	for _, input := range []string{
		`{"x":9007199254740993,"x":1e999999,"\u0078":"\uDEAD"}`,
		"\"\xff\"",
		`null`, `true`, `0`, `"雪"`, `{}`, `[]`, `[1,{"x":[2]}]`,
		`{"x":01}`, `{"x":1.}`, `{"x":"\x00"}`, `{"x":1,}`, `{"x":[]}` + "true",
		"\ufeff{}", "", " ",
		strings.Repeat("[", 10000) + strings.Repeat("]", 10000),
		strings.Repeat("[", 10001) + strings.Repeat("]", 10001),
	} {
		data := []byte(input)
		if got, want := Valid(data), json.Valid(data); got != want {
			t.Errorf("input starting with %.64q (%d bytes): got %v, want %v", input, len(data), got, want)
		}
	}
}

func TestDecodeSaveMatchesLegacy(t *testing.T) {
	for _, input := range []string{
		`{"saved":true,"name":"Review 雪.zip"}`,
		`{"saved":false,"name":"."}`,
		`{"error":"save the file with its .zip extension"}`,
		`{"ſaved":true,"NAME":"first","name":null,"ignored":{"saved":false}}`,
		`{"name":"first","name":"second","saved":true,"saved":false}`,
		`{"name":"\uD83C\uDFAC","error":"\uDEAD"}`,
		"{\"name\":\"\xff\"}",
		`{}`, `null`, `[]`, `{"saved":"true"}`, `{"saved":1}`,
		`{"name":false}`, `{"error":{}}`, `{"unknown":[1,]}`,
		`{"saved":true`, `{"saved":true} {}`, `{"saved":true} !`,
	} {
		var want SaveReceipt
		valid := json.Unmarshal([]byte(input), &want) == nil
		got, ok := DecodeSave([]byte(input))
		if ok != valid || ok && got != want {
			t.Errorf("%q: got (%+v, %v), want (%+v, %v)", input, got, ok, want, valid)
		}
	}
}

func TestDecodeRoomMatchesLegacy(t *testing.T) {
	for _, input := range []string{
		`{"event":"board","data":{"revision":9007199254740993,"connected":2}}`,
		`{"EVENT":"board","DATA":{"REVISION":1},"data":{"connected":2}}`,
		`{"data":{"revision":4,"revision":null},"data":null}`,
		`{"event":"board","event":"other","data":{"ignored":[1,2]}}`,
		`{"data":{"revision":-9223372036854775808}}`,
		`{"data":{"revision":9223372036854775807}}`,
		`{"data":{"revision":9223372036854775808}}`,
		`{"data":{"revision":1e2}}`, `{"data":{"revision":1.0}}`,
		`{"data":{"revision":"1"}}`, `{"data":false}`, `{"event":42}`,
		`{"event":"board","data":[{"revision":1}]}`,
		`{}`, `null`, `[]`, `{"data":{"revision":1`, `{"data":{}}null`,
	} {
		var legacy struct {
			Event string
			Data  struct {
				Revision, Connected int
			}
		}
		valid := json.Unmarshal([]byte(input), &legacy) == nil
		want := RoomUpdate{legacy.Event, legacy.Data.Revision, legacy.Data.Connected}
		got, ok := DecodeRoom(input)
		if ok != valid || ok && got != want {
			t.Errorf("%q: got (%+v, %v), want (%+v, %v)", input, got, ok, want, valid)
		}
	}
}
