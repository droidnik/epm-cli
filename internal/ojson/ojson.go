// Package ojson decodes JSON while preserving object key order.
//
// Decoded values are: *Object, []any, string, json.Number, bool, nil.
// *Object marshals back to JSON in its original key order.
package ojson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Object is a JSON object with ordered keys.
type Object struct {
	Keys   []string
	Values map[string]any
}

func NewObject() *Object { return &Object{Values: map[string]any{}} }

func (o *Object) Len() int { return len(o.Keys) }

func (o *Object) Get(key string) (any, bool) {
	v, ok := o.Values[key]
	return v, ok
}

// GetFold looks a key up case-insensitively (the EPM API is not always consistent).
func (o *Object) GetFold(key string) (any, bool) {
	if v, ok := o.Values[key]; ok {
		return v, true
	}
	for _, k := range o.Keys {
		if strings.EqualFold(k, key) {
			return o.Values[k], true
		}
	}
	return nil, false
}

func (o *Object) Set(key string, v any) {
	if _, ok := o.Values[key]; !ok {
		o.Keys = append(o.Keys, key)
	}
	o.Values[key] = v
}

func (o *Object) Delete(key string) {
	if _, ok := o.Values[key]; !ok {
		return
	}
	delete(o.Values, key)
	for i, k := range o.Keys {
		if k == key {
			o.Keys = append(o.Keys[:i], o.Keys[i+1:]...)
			break
		}
	}
}

func (o *Object) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.Keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := Marshal(o.Values[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// Marshal encodes v without escaping HTML characters.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// MarshalIndent encodes v with two-space indentation.
func MarshalIndent(v any) ([]byte, error) {
	b, err := Marshal(v)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Decode parses one JSON document, preserving key order.
func Decode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after JSON value")
	}
	return v, nil
}

// DecodeFrom parses the first JSON value from a decoder (used to scan embedded JSON).
func DecodeFrom(dec *json.Decoder) (any, error) { return decodeValue(dec) }

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := NewObject()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("invalid object key %v", kt)
				}
				val, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				obj.Set(key, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return obj, nil
		case '[':
			arr := []any{}
			for dec.More() {
				val, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("unexpected delimiter %v", t)
	default:
		return t, nil // string, json.Number, bool, nil
	}
}

// Path walks a dotted path ("a.b.0.c") through objects and arrays.
func Path(v any, path string) (any, bool) {
	cur := v
	for _, part := range strings.Split(path, ".") {
		switch c := cur.(type) {
		case *Object:
			nv, ok := c.GetFold(part)
			if !ok {
				return nil, false
			}
			cur = nv
		case []any:
			var idx int
			if _, err := fmt.Sscanf(part, "%d", &idx); err != nil || idx < 0 || idx >= len(c) {
				return nil, false
			}
			cur = c[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

// Obj returns v as *Object or nil.
func Obj(v any) *Object {
	o, _ := v.(*Object)
	return o
}

// Str returns v as string ("" if not a string).
func Str(v any) string {
	s, _ := v.(string)
	return s
}
