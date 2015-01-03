package libcats

import (
	"io/ioutil"
	"os"
	"runtime/debug"
	"runtime/pprof"
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
		imagesReceived:  0,
		imgReceived:     make(chan ImageInfo),
		imgMetaReceived: make(chan ImageMetaData),
	}
}

func TestGetCatsCancelRequest(t *testing.T) {
	setup(t)
	done := make(chan struct{})
	close(done) // close it early

	response, ok := <-getCats("", done)
	if !ok {
		loggerFunc("channel was closed")
		return
	}

	loggerFunc("%+v", response)

	if response.Error != nil {
		t.Errorf("%s", response.Error.Error())
		return
	}

	if len(response.Response.Data.Children) == 0 {
		t.Errorf("no children")
		return
	}

	item := response.Response.Data.Children[0].Data
	if item.Url == "" {
		t.Errorf("no url %+v", item)
		return
	}
}

func TestGetCats(t *testing.T) {
	setup(t)
	done := make(chan struct{})
	defer close(done)

	response, ok := <-getCats("", done)
	if !ok {
		t.Errorf("channel was closed")
		return
	}

	loggerFunc("%+v", response)

	if response.Error != nil {
		t.Errorf("%s", response.Error.Error())
		return
	}

	if len(response.Response.Data.Children) == 0 {
		t.Errorf("no children")
		return
	}

	item := response.Response.Data.Children[0].Data
	if item.Url == "" {
		t.Errorf("no url %+v", item)
		return
	}
}

func TestCreateMetaDataCallback(t *testing.T) {
	setup(t)
	printHeap()

	<-time.After(5 * time.Second)

	callback := MakeTestCallback()
	token := CreateMetaDataCallback(callback, "")

	duration := 10 * time.Second

	go func() {
		for {
			select {
			case <-time.After(duration):
				return

			case meta, ok := <-callback.imgMetaReceived:
				if ok {
					CreateImageCallback(callback, 1024, 768, meta.Id, meta.Url)
					<-time.After(1 * time.Second)
				}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-time.After(duration):
				return
			case <-callback.imgReceived:
				// need a reader for the callback, otherwise it'll just block
			}
		}
	}()

	busyLoop(duration)

	token.Close()

	<-time.After(5 * time.Second)
	debug.FreeOSMemory()
	printHeap()

	<-time.After(5 * time.Hour)
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

func TestCreateImageCallbackCancelRequest(t *testing.T) {
	setup(t)

	callback := MakeTestCallback()
	token := CreateImageCallback(callback, 1080, 1776, "foobar", "http://imgur.com/0Hx3frP.jpg")
	token.Close()

	select {
	case <-time.After(10 * time.Second):
		// success

	case img := <-callback.imgReceived:
		if img.Image == nil {
			t.Errorf("img is nil")
		}
	}
}

func busyLoop(duration time.Duration) {
	toEnd := time.After(duration)
	toPrint := time.Tick(500 * time.Millisecond)

	for {
		select {
		case <-toEnd:
			printHeap()
			<-time.After(5 * time.Second)

			// dumpGoRoutines()
			printHeap()
			return
		case <-toPrint:
			printHeap()
		}
	}
}

func dumpGoRoutines() {
	pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
}

func (callback TestImageCallback) MetaDataReceived(url string, title string, author string, permalink string, id string) {
	loggerFunc("MetaDataReceived %s", url)

	meta := ImageMetaData{
		Url:       url,
		Title:     title,
		Author:    author,
		Permalink: permalink,
		Id:        id,
	}

	callback.imgMetaReceived <- meta
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
	imagesReceived  int
	imgReceived     chan ImageInfo
	imgMetaReceived chan ImageMetaData
}
