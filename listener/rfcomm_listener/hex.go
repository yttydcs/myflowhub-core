package rfcomm_listener

import "fmt"

func strconvHexByte(twoHex string) (byte, error) {
	if len(twoHex) != 2 {
		return 0, fmt.Errorf("invalid hex byte: %q", twoHex)
	}
	hi, ok := fromHex(twoHex[0])
	if !ok {
		return 0, fmt.Errorf("invalid hex: %q", twoHex)
	}
	lo, ok := fromHex(twoHex[1])
	if !ok {
		return 0, fmt.Errorf("invalid hex: %q", twoHex)
	}
	return (hi << 4) | lo, nil
}

func fromHex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}
