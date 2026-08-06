package messagebuffer

import (
	"fmt"
	"sort"

	"github.com/jim80net/flotilla/internal/outbox"
)

type MigrationResult struct {
	Migrated   int      `json:"migrated"`
	Recipients []string `json:"recipients"`
}

// MigrateOutboxes moves every legacy push-retry row into its recipient buffer.
// Buffer insert precedes outbox removal, and the original ID makes retries idempotent.
func MigrateOutboxes(rosterDir string) (MigrationResult, error) {
	var result MigrationResult
	recipients := make(map[string]bool)
	for _, legacy := range outbox.ListAll(rosterDir) {
		_, _, err := Enqueue(rosterDir, legacy.Sender, legacy.Recipient, legacy.Message, EnqueueOptions{
			ID: legacy.ID, EnqueuedAt: legacy.EnqueuedAt, MigratedFrom: "sender-outbox",
			LegacyDeferrals: legacy.Deferrals,
		})
		if err != nil {
			return result, fmt.Errorf("migrate outbox %s/%s: %w", legacy.Sender, legacy.ID, err)
		}
		path, err := outbox.Path(rosterDir, legacy.Sender)
		if err != nil {
			return result, err
		}
		outbox.NewStore(path).Remove(legacy.ID)
		result.Migrated++
		recipients[legacy.Recipient] = true
	}
	for recipient := range recipients {
		result.Recipients = append(result.Recipients, recipient)
	}
	sort.Strings(result.Recipients)
	return result, nil
}
