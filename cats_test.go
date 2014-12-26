package libcats

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestReverse(t *testing.T) {
	dir, err := ioutil.TempDir(os.TempDir(), "libcats")
	defer os.RemoveAll(dir)
	Init(dir)
	urls, err := GetCats()

	if err != nil {
		t.Errorf("%s", err.Error())
	}

	if len(urls.urls) == 0 {
		t.Errorf("no urls", "")
	}
}
