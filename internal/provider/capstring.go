package provider

import (
	"bytes"
	"encoding/json"
)

// capString decodes a monetary cap that the gateway may serialise either as a
// JSON string ("5.00") or, under rust_decimal's `serde-float` build, as a bare
// JSON number (5.0). It always yields the decimal's textual form so downstream
// Terraform state stays a string and a `terraform plan`/refresh never fails with
// `json: cannot unmarshal number into ... of type string`.
//
// Only the decode (server → provider) side is tolerant; the provider always
// SENDS caps as strings (see subLimitCreateBody / costCenterUpdateBody).
type capString string

func (c *capString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*c = ""
		return nil
	}
	// JSON string: unwrap the quotes.
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*c = capString(s)
		return nil
	}
	// Bare JSON number: keep its literal text (preserves the scale as sent).
	*c = capString(b)
	return nil
}

// String returns the plain string form for building Terraform values.
func (c capString) String() string { return string(c) }
