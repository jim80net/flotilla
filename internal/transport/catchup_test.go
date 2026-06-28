package transport

import "testing"

// catchupTransport embeds the inert fakeTransport and adds the optional CatchUp
// capability, so the type-assertion contract (present ⇒ ok; absent ⇒ skip cleanly)
// is exercised directly — mirroring surface.ResultReader's optional-capability test.
type catchupTransport struct {
	fakeTransport
}

func (c *catchupTransport) MessagesAfter(Destination, string, int, int) ([]Message, bool, error) {
	return nil, false, nil
}
func (c *catchupTransport) Latest(Destination) (Message, bool, error) {
	return Message{}, false, nil
}

func TestCatchUp_PresentTypeAsserts(t *testing.T) {
	var tr Transport = &catchupTransport{fakeTransport{name: "with-catchup"}}
	if _, ok := tr.(CatchUp); !ok {
		t.Error("a transport implementing CatchUp must type-assert as CatchUp")
	}
}

func TestCatchUp_AbsentTypeAssertsFalse(t *testing.T) {
	// The plain fakeTransport does NOT implement CatchUp (its delivery cannot gap);
	// the assertion must fail so the caller skips the backstop cleanly.
	var tr Transport = &fakeTransport{name: "no-catchup"}
	if _, ok := tr.(CatchUp); ok {
		t.Error("a transport NOT implementing CatchUp must NOT type-assert as CatchUp")
	}
}
