// Package sessionmirror is the watch-written, dash-read append ledger for tri-surface
// session mirroring (flotilla#267). Each non-suppressed desk mirror event is stored
// as one JSON line under <roster-dir>/session-mirror/<agent>.jsonl with verbose, info,
// and debug renderings derived from the turn-final read. Discord is an explicit
// parade-only egress layered after this durable append.
package sessionmirror
