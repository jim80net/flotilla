package deliver

import (
	"reflect"
	"testing"
)

func TestPaneStartCommandArgs(t *testing.T) {
	want := []string{"display-message", "-p", "-t", "%7", "#{pane_start_command}"}
	if got := paneStartCommandArgs("%7"); !reflect.DeepEqual(got, want) {
		t.Fatalf("paneStartCommandArgs = %q, want %q", got, want)
	}
}
