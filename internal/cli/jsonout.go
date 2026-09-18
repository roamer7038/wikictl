package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"slices"
	"unicode/utf8"
)

// A value of --json that is not valid UTF-8 would lose its bytes:
// encoding/json replaces every byte it cannot decode with U+FFFD, so a path
// printed that way no longer names the file and content no longer holds what
// the file holds. Such a value is therefore left out and its bytes are
// printed in base64 under another key named after it, "path_base64" for
// "path"; a value that is valid UTF-8 keeps its own key alone, so the output
// of a wiki written in valid UTF-8 is unchanged. Readers take the base64 key
// when the plain one is absent.
//
// The name of the key differs per command, "from" and "to" for mv and
// "target" for links, and one value is an array of strings whose shape must
// not change, "paths" of rm, where no key can be put beside an element. The
// objects are therefore built key by key rather than described by struct
// tags; jsonObject keeps the keys in the order the struct fields are written
// in, so that the output of a valid value is the same as before.

// jsonField is one key of a jsonObject.
type jsonField struct {
	key   string
	value any
}

// jsonObject is a JSON object whose keys keep the order they were added in.
type jsonObject []jsonField

// add appends key with the value v, whatever its type.
func (o jsonObject) add(key string, v any) jsonObject {
	return append(o, jsonField{key, v})
}

// text appends key with s when s is valid UTF-8, and key+"_base64" with the
// bytes of s in base64 when it is not.
func (o jsonObject) text(key, s string) jsonObject {
	if utf8.ValidString(s) {
		return o.add(key, s)
	}
	return o.add(key+"_base64", encodeBase64(s))
}

// list appends key with ss, followed by key+"_base64" holding every element
// in base64, in the same order, when any element is not valid UTF-8. The
// array holds strings, so the second array keeps the shape of the first
// instead of a key beside the element.
func (o jsonObject) list(key string, ss []string) jsonObject {
	o = o.add(key, ss)
	if b64 := listBase64(ss); b64 != nil {
		o = o.add(key+"_base64", b64)
	}
	return o
}

// MarshalJSON writes the keys in the order they were added. HTML characters
// are not escaped, as emit does not escape them either: a value of --json is
// printed as stored.
func (o jsonObject) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := jsonValue(f.key)
		if err != nil {
			return nil, err
		}
		value, err := jsonValue(f.value)
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// jsonValue returns v as JSON without escaping HTML characters.
func jsonValue(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// setText is jsonObject.text for the commands whose whole output is one
// object built as a map, put and rm. JSON sorts the keys of a map, so adding
// a key leaves the order of the others alone.
func setText(m map[string]any, key, s string) {
	if utf8.ValidString(s) {
		m[key] = s
		return
	}
	m[key+"_base64"] = encodeBase64(s)
}

// setList is jsonObject.list for such a map.
func setList(m map[string]any, key string, ss []string) {
	m[key] = ss
	if b64 := listBase64(ss); b64 != nil {
		m[key+"_base64"] = b64
	}
}

// listBase64 returns every element of ss in base64, or nil when they are all
// valid UTF-8 and no second array is needed.
func listBase64(ss []string) []string {
	if !slices.ContainsFunc(ss, func(s string) bool { return !utf8.ValidString(s) }) {
		return nil
	}
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = encodeBase64(s)
	}
	return out
}

// encodeBase64 returns the bytes of s in standard base64 with padding.
func encodeBase64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
