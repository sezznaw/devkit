package takeover

import "encoding/json"

func decodeJSON(b []byte, out any) error { return json.Unmarshal(b, out) }
