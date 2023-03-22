package pods

import (
	"io"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// myReader is used to test the WatchPodStatus function.
type myReader struct {
	data []string
}

func (r *myReader) Read(buf []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(buf, ([]byte)(r.data[0]))
	if n < len(r.data[0]) {
		r.data[0] = r.data[0][n:]
	} else {
		r.data = r.data[1:]
	}
	return n, nil
}

func TestGetPodStatus(t *testing.T) {
	testData = consolidated
	got, err := GetPodStatus("")
	if err != nil {
		t.Fatal(err)
	}
	if s := cmp.Diff(got, wantStatus); s != "" {
		t.Errorf("%s\n", s)
	}
}

func TestBadJsonGet(t *testing.T) {
	testData = ([]byte)("bad json")

	s, err := GetPodStatus("multivendor")
	if err != nil {
		return
	}
	t.Errorf("Did not get error, got %#v", s)
}

func TestWatchPodStatus(t *testing.T) {
	var r myReader
	r.data = append([]string{}, rawStream...)
	testReader = &r

	ch, err := WatchPodStatus("multivendor", nil)
	if err != nil {
		t.Fatal(err)
	}
	i := -1
	for got := range ch {
		i++
		want := wantStatus[i]
		if s := cmp.Diff(got, want); s != "" {
			t.Errorf("slice %d\n%s\n", i, s)
		}
	}
}

func TestBadJsonWatch(t *testing.T) {
	var r myReader
	r.data = []string{
		"bad json",
	}
	testReader = &r

	ch, err := WatchPodStatus("multivendor", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := <-ch
	if got.Error == nil {
		t.Errorf("Did not get an error response")
	}
	select {
	case got, ok := <-ch:
		if ok {
			t.Errorf("Got more than 1 response: %#v", got)
		}
	default:
	}
}
