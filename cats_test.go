package libcats

import (
	"io/ioutil"
	"log"
	"os"
	"runtime"
	"testing"
	"time"
)

func setup(t *testing.T) {
	dir, err := ioutil.TempDir(os.TempDir(), "libcats")
	defer os.RemoveAll(dir)
	Init(dir)
	SetThreadLogger()

	if err != nil {
		t.Errorf("%s", err.Error())
	}
}

func MakeTestCallback() TestImageCallback {
	return TestImageCallback{
		imagesReceived: 0,
		imgReceived:    make(chan ImageInfo),
	}
}

func TestGetCats(t *testing.T) {
	setup(t)
	response, err := getCats("")

	if err != nil {
		t.Errorf("%s", err.Error())
	}

	if len(response.Data.Children) == 0 {
		t.Errorf("no children")
	}

	item := response.Data.Children[0].Data
	if item.Url == "" {
		t.Errorf("no url %+v", item)
	}
}

func TestCreateMetaDataCallback(t *testing.T) {
	setup(t)
	printHeap()

	callback := MakeTestCallback()
	token := CreateMetaDataCallback(callback, "")

	token.Close()

	// toEnd := time.After(30 * time.Second)
	// toPrint := time.Tick(1 * time.Second)

	// All go routines should have stopped
	// pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
}

func TestCreateImageCallback(t *testing.T) {
	setup(t)

	callback := MakeTestCallback()
	CreateImageCallback(callback, 1080, 1776, "foobar", "http://imgur.com/0Hx3frP.jpg")

	select {
	case <-time.After(10 * time.Second):
		t.Errorf("failed to download image")

	case img := <-callback.imgReceived:
		if img.Image == nil {
			t.Errorf("img is nil")
		}
	}
}

func printHeap() {
	runtime.GC()
	memstats := new(runtime.MemStats)
	runtime.ReadMemStats(memstats)
	log.Printf("memstats: bytes = %d footprint = %d", memstats.HeapAlloc, memstats.Sys)
}

func (callback TestImageCallback) MetaDataReceived(url string, title string, author string, permalink string, id string) {
	loggerFunc("MetaDataReceived %s", url)
}

func (callback TestImageCallback) ImageFailed(id string) {
	loggerFunc("ImageFailed %s", id)
}

func (callback TestImageCallback) ImageReceived(image []byte, id string) {
	loggerFunc("ImageReceived %s", id)

	info := ImageInfo{
		Image: image,
		Id:    id,
	}

	callback.imgReceived <- info
}

type TestImageCallback struct {
	imagesReceived int
	imgReceived    chan ImageInfo
}
