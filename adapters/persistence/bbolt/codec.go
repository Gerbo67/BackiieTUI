package bbolt

import "encoding/json"

func encode(v any) ([]byte, error) {
	return json.Marshal(v)
}

func decode(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
