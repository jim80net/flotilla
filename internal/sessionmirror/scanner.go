package sessionmirror

import (
	"bufio"
	"bytes"
)

// scannerJSONOverhead is headroom for ts/agent/info/debug fields outside verbose.
const scannerJSONOverhead = 64 << 10 // 64 KiB

// maxLineBytes is the bufio.Scanner token cap for one marshaled ledger line.
// DefaultVerboseCap is in runes; UTF-8 needs up to 4 bytes/rune. JSON escaping
// and ANSI bytes from tmux can expand further — marshalLedgerLine shrinks verbose
// until len(line) ≤ maxLineBytes so the writer/reader contract stays airtight.
const maxLineBytes = DefaultVerboseCap*4 + scannerJSONOverhead

func newLineScanner(data []byte) *bufio.Scanner {
	sc := bufio.NewScanner(bytes.NewReader(data))
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, maxLineBytes)
	return sc
}
