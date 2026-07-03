package sessionmirror

import (
	"bufio"
	"bytes"
)

// maxLineBytes is the bufio.Scanner token cap for jsonl ledger lines. DefaultVerboseCap
// runes can exceed the scanner's default 64KiB after JSON encoding and escaping.
const maxLineBytes = 4 << 20 // 4 MiB

func newLineScanner(data []byte) *bufio.Scanner {
	sc := bufio.NewScanner(bytes.NewReader(data))
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, maxLineBytes)
	return sc
}