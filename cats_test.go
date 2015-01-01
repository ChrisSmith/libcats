package libcats

import (
	"io/ioutil"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"testing"
	"time"
)

func TestCreateImageCallback(t *testing.T) {
	dir, err := ioutil.TempDir(os.TempDir(), "libcats")
	defer os.RemoveAll(dir)
	Init(dir)
	SetThreadLogger()

	if err != nil {
		t.Errorf("%s", err.Error())
	}

	printHeap()

	callback := TestImageCallback{}
	token := CreateImageCallback(callback)

	// pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)

	toEnd := time.After(30 * time.Second)
	toPrint := time.Tick(1 * time.Second)
	for {
		select {
		case <-toEnd:

			token.Close()

			// All go routines should have stopped
			pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			return
		case <-toPrint:
			printHeap()
		}
	}
}

func printHeap() {
	runtime.GC()
	memstats := new(runtime.MemStats)
	runtime.ReadMemStats(memstats)
	log.Printf("memstats: bytes = %d footprint = %d", memstats.HeapAlloc, memstats.Sys)
}

func (callback TestImageCallback) ImageReceived(Image []byte, Url string, Title string, Author string, Permalink string) {
	callback.imagesReceived += 1
}

type TestImageCallback struct {
	imagesReceived int
}
