package sessionmirror

import (
	"bufio"
	"bytes"
)

// scannerJSONOverhead is headroom for ts/agent/info/debug fields outside verbose.
const scannerJSONOverhead = 64 << 10 // 64 KiB

// maxLineBytes is the bufio.Scanner token cap for one marshaled ledger line.
// DefaultVerboseCap is in runes; UTF-8 needs up to 4 bytes/rune before JSON escaping.
const maxLineBytes = DefaultVerboseCap*4 + scannerJSONOverhead

func newLineScanner(data []byte) *bufio.Scanner {
	sc := bufio.NewScanner(bytes.NewReader(data))
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, maxLineBytes)
	return sc
}
