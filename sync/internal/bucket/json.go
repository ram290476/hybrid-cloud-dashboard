package bucket

import "encoding/json"

func unmarshalJSON(raw []byte, dest any) error {
	return json.Unmarshal(raw, dest)
}
