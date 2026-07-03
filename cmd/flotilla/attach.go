package main

import (
	"fmt"
	"strings"

	"github.com/jim80net/flotilla/internal/transport"
)

// attachPathsFlag is a repeatable --attach flag value.
type attachPathsFlag []string

func (a *attachPathsFlag) String() string { return strings.Join(*a, ",") }

func (a *attachPathsFlag) Set(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("--attach requires a file path")
	}
	*a = append(*a, v)
	return nil
}

func postOutbound(tr transport.Transport, dest transport.Destination, username, content string, attachPaths []string) error {
	if len(attachPaths) > 0 {
		return tr.PostWithAttachments(dest, username, content, attachPaths)
	}
	return tr.Post(dest, username, content)
}
