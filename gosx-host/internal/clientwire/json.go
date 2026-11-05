// Package clientwire handles editor JSON without a reflective decoder.
// It preserves the server's encoding/json field types and names.
package clientwire

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/go-json-experiment/json/jsontext"
)

type SaveReceipt struct {
	Saved       bool
	Name, Error string
}

type RoomUpdate struct {
	Event               string
	Revision, Connected int
}

var legacySyntax = []jsontext.Options{
	jsontext.AllowDuplicateNames(true),
	jsontext.AllowInvalidUTF8(true),
}

// Valid checks syntax while keeping the input available verbatim.
func Valid(data []byte) bool {
	return jsontext.Value(data).IsValid(legacySyntax...)
}

func DecodeSave(data []byte) (SaveReceipt, bool) {
	var result SaveReceipt
	d := wireDecoder(bytes.NewReader(data))
	err := readObject(d, func(name string) error {
		switch {
		case strings.EqualFold(name, "saved"):
			return readScalar(d, &result.Saved)
		case strings.EqualFold(name, "name"):
			return readScalar(d, &result.Name)
		case strings.EqualFold(name, "error"):
			return readScalar(d, &result.Error)
		default:
			return d.SkipValue()
		}
	})
	return result, err == nil && atEnd(d)
}

func DecodeRoom(data string) (RoomUpdate, bool) {
	var result RoomUpdate
	d := wireDecoder(strings.NewReader(data))
	err := readObject(d, func(name string) error {
		switch {
		case strings.EqualFold(name, "event"):
			return readScalar(d, &result.Event)
		case strings.EqualFold(name, "data"):
			return readObject(d, func(name string) error {
				switch {
				case strings.EqualFold(name, "revision"):
					return readScalar(d, &result.Revision)
				case strings.EqualFold(name, "connected"):
					return readScalar(d, &result.Connected)
				default:
					return d.SkipValue()
				}
			})
		default:
			return d.SkipValue()
		}
	})
	return result, err == nil && atEnd(d)
}

var errWireType = errors.New("unexpected JSON field type")

func wireDecoder(r io.Reader) *jsontext.Decoder {
	return jsontext.NewDecoder(r, legacySyntax...)
}

func atEnd(d *jsontext.Decoder) bool {
	_, err := d.ReadToken()
	return err == io.EOF
}

func readObject(d *jsontext.Decoder, field func(string) error) error {
	token, err := d.ReadToken()
	if err != nil || token.Kind() == 'n' {
		return err
	}
	if token.Kind() != '{' {
		return errWireType
	}
	for d.PeekKind() != '}' {
		name, err := d.ReadToken()
		if err != nil {
			return err
		}
		if err = field(name.String()); err != nil {
			return err
		}
	}
	_, err = d.ReadToken()
	return err
}

func readScalar(d *jsontext.Decoder, dst any) error {
	token, err := d.ReadToken()
	if err != nil || token.Kind() == 'n' {
		return err
	}
	switch value := dst.(type) {
	case *string:
		if token.Kind() == '"' {
			*value = token.String()
			return nil
		}
	case *bool:
		if kind := token.Kind(); kind == 't' || kind == 'f' {
			*value = kind == 't'
			return nil
		}
	case *int:
		if token.Kind() == '0' {
			// Parse the original digits rather than rounding through float64.
			*value, err = strconv.Atoi(token.String())
			return err
		}
	}
	return errWireType
}
