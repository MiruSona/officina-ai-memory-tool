package install

import (
	"bytes"
	"encoding/json"
	"io"
)

// jsonObject is a JSON object that remembers the order its keys arrived in.
// settings.json belongs to the user: rewriting it must not shuffle their keys
// into alphabetical order.
type jsonObject struct {
	keys   []string
	values map[string]any
}

func newObject() *jsonObject {
	return &jsonObject{values: map[string]any{}}
}

func (o *jsonObject) get(key string) (any, bool) {
	value, ok := o.values[key]
	return value, ok
}

func (o *jsonObject) set(key string, value any) {
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o *jsonObject) remove(key string) {
	if _, ok := o.values[key]; !ok {
		return
	}
	delete(o.values, key)
	kept := make([]string, 0, len(o.keys))
	for _, name := range o.keys {
		if name != key {
			kept = append(kept, name)
		}
	}
	o.keys = kept
}

func (o *jsonObject) len() int {
	return len(o.keys)
}

// MarshalJSON writes the keys back in the order they were read. The outer
// encoder re-indents this, so only the order and the escaping matter here.
func (o *jsonObject) MarshalJSON() ([]byte, error) {
	out := bytes.Buffer{}
	out.WriteByte('{')
	for position, key := range o.keys {
		if position > 0 {
			out.WriteByte(',')
		}
		name, err := rawJSON(key)
		if err != nil {
			return nil, err
		}
		value, err := rawJSON(o.values[key])
		if err != nil {
			return nil, err
		}
		out.Write(name)
		out.WriteByte(':')
		out.Write(value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

// rawJSON encodes one value without turning < > & into escapes.
func rawJSON(value any) ([]byte, error) {
	buffer := bytes.Buffer{}
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buffer.Bytes(), "\n"), nil
}

// decodeJSON parses one document, turning every object into a jsonObject and
// every number into json.Number, so a rewrite gives the numbers back unchanged.
func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := decodeValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errBadSettings
	}
	return value, nil
}

func decodeValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	return decodeFrom(decoder, token)
}

func decodeFrom(decoder *json.Decoder, token json.Token) (any, error) {
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delim {
	case '{':
		return decodeObject(decoder)
	case '[':
		return decodeArray(decoder)
	}
	return nil, errBadSettings
}

func decodeObject(decoder *json.Decoder) (any, error) {
	object := newObject()
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if delim, ok := token.(json.Delim); ok && delim == '}' {
			return object, nil
		}
		key, ok := token.(string)
		if !ok {
			return nil, errBadSettings
		}
		value, err := decodeValue(decoder)
		if err != nil {
			return nil, err
		}
		object.set(key, value)
	}
}

func decodeArray(decoder *json.Decoder) (any, error) {
	list := []any{}
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if delim, ok := token.(json.Delim); ok && delim == ']' {
			return list, nil
		}
		value, err := decodeFrom(decoder, token)
		if err != nil {
			return nil, err
		}
		list = append(list, value)
	}
}
