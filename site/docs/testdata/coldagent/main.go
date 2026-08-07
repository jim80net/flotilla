// Command coldagent is a network-free terminal fixture used only by the
// public quickstart cold test. Build it with the output name "claude" so tmux
// exposes the quickstart's default supported surface while the fixture accepts
// one line and returns to a cleared Claude Code-style composer.
package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("Claude Code cold-test fixture\n")
	renderComposer()
	for scanner.Scan() {
		fmt.Printf("\r\naccepted: %s\n", scanner.Text())
		renderComposer()
	}
}

func renderComposer() {
	// The real driver locates Claude Code's composer at the terminal cursor.
	// Rendering that prompt makes this fixture exercise the supported surface
	// probe instead of weakening the cold test to a shell prompt.
	fmt.Print("❯ ")
}
