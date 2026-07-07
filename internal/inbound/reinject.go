package inbound

import "strings"

const reinjectClaimPrefix = "inbound-reinject:"

// ReinjectClaimKey keys a confirmed KindDetector reinject delivery for deferral counting.
func ReinjectClaimKey(recipient, entryID string) string {
	return reinjectClaimPrefix + recipient + "/" + entryID
}

// ParseReinjectClaimKey splits a reinject claim key into recipient and entry id.
func ParseReinjectClaimKey(key string) (recipient, entryID string, ok bool) {
	if !strings.HasPrefix(key, reinjectClaimPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, reinjectClaimPrefix)
	i := strings.Index(rest, "/")
	if i <= 0 || i >= len(rest)-1 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

// MarkReinjectDelivered records that the one-shot reinject wake confirmed (busy-dropped
// reinjects do NOT call this — escalation requires a confirmed reinject, not merely enqueued).
func MarkReinjectDelivered(rosterDir, recipient, entryID string) error {
	path, err := Path(rosterDir, recipient)
	if err != nil {
		return err
	}
	st := NewStore(path)
	return st.withLock(func() error {
		f, err := st.readFileForUpdate()
		if err != nil {
			return err
		}
		for i, p := range f.Pending {
			if p.ID == entryID {
				if p.Deferrals < 1 {
					p.Deferrals = 1
				}
				f.Pending[i] = p
				return st.save(f)
			}
		}
		return nil
	})
}
