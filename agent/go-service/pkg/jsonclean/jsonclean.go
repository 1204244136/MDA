// Package jsonclean sanitizes JSONC into strict JSON so it can be fed to a
// plain JSON parser. It strips comments (// and /* */), trailing commas and
// a leading UTF-8 BOM while keeping string literals intact and preserving
// newlines so line numbers stay stable.
package jsonclean

import "bytes"

// Clean removes JSONC-only constructs (comments, trailing commas) and a
// leading UTF-8 BOM. The result is parseable by a strict JSON parser.
//
// The input must be valid UTF-8. Everything else is passed through
// byte-for-byte so the result stays equivalent to the original JSON.
func Clean(raw []byte) []byte {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if len(raw) == 0 {
		return raw
	}

	out := make([]byte, 0, len(raw))
	// State: 0 = normal, 1 = inside string, 2 = inside block comment.
	state := 0
	escaped := false
	blockStartRaw, blockStartOut := -1, -1

	for i := 0; i < len(raw); {
		c := raw[i]
		switch state {
		case 0:
			if c == '"' {
				out = append(out, c)
				state, escaped = 1, false
				i++
				continue
			}
			if c == '/' && i+1 < len(raw) {
				switch raw[i+1] {
				case '/':
					for i < len(raw) && raw[i] != '\n' {
						i++
					}
					if i < len(raw) {
						out = append(out, '\n')
						i++
					}
					continue
				case '*':
					blockStartRaw, blockStartOut = i, len(out)
					state = 2
					i += 2
					continue
				}
			}
			if c == ',' {
				j := i + 1
				for j < len(raw) && isJSONSpace(raw[j]) {
					j++
				}
				if j < len(raw) && (raw[j] == '}' || raw[j] == ']') {
					i++
					continue
				}
			}
			out = append(out, c)
			i++
		case 1:
			out = append(out, c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				state = 0
			}
			i++
		case 2:
			if c == '*' && i+1 < len(raw) && raw[i+1] == '/' {
				state = 0
				blockStartRaw, blockStartOut = -1, -1
				i += 2
				continue
			}
			if c == '\n' {
				out = append(out, '\n')
			}
			i++
		}
	}

	// Preserve an unterminated block comment so strict parsing still fails.
	if state == 2 && blockStartRaw >= 0 {
		out = append(out[:blockStartOut], raw[blockStartRaw:]...)
	}
	if len(out) > 0 && out[len(out)-1] == ',' {
		out = out[:len(out)-1]
	}
	return out
}

func isJSONSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}
