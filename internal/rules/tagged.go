package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

// Params are the parameters of a fault or a behavior, keyed as they appear in
// the API. The domain type keeps them as they arrived; the type named alongside
// them is the only thing that knows which keys it wants, and the API boundary
// rejects the rest.
type Params map[string]any

// typeKey is the one key in a fault or behavior object that is not a parameter.
const typeKey = "type"

// decodeTagged reads the flat wire shape both faults and behaviors use, a type
// alongside that type's own parameters and no nesting.
func decodeTagged(data []byte) (string, Params, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", nil, err
	}

	name, _ := raw[typeKey].(string)
	delete(raw, typeKey)
	if len(raw) == 0 {
		return name, nil, nil
	}
	return name, raw, nil
}

// encodeTagged writes the flat wire shape back, type first and parameters after
// it in a stable order. What names the value, "fault" or "behavior", appears
// only in the error a parameter that cannot be encoded produces.
func encodeTagged(what, name string, params Params) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`{"` + typeKey + `":`)

	encoded, err := json.Marshal(name)
	if err != nil {
		return nil, fmt.Errorf("rules: %s type: %w", what, err)
	}
	b.Write(encoded)

	for _, key := range slices.Sorted(maps.Keys(params)) {
		value, err := json.Marshal(params[key])
		if err != nil {
			return nil, fmt.Errorf("rules: %s parameter %q: %w", what, key, err)
		}
		b.WriteString(",")
		b.Write(quoted(key))
		b.WriteString(":")
		b.Write(value)
	}

	b.WriteString("}")
	return b.Bytes(), nil
}

// quoted encodes a parameter name. Names come from a JSON object, so they are
// valid strings and cannot fail to encode.
func quoted(key string) []byte {
	out, err := json.Marshal(key)
	if err != nil {
		return []byte(`""`)
	}
	return out
}
