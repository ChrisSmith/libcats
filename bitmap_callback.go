package libcats

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"runtime"
	"time"

	"github.com/nfnt/resize"
	"github.com/oliamb/cutter"
)

type ImageInfo struct {
	Id    string
	Image []byte
}

type ImageCallback interface {
	ImageReceived(image []byte, id string)
	ImageFailed(id string)
}

type ImageCallbackToken struct {
	done     chan struct{}
	callback ImageCallback
}

func CreateImageCallback(callback ImageCallback, width int, height int, id string, url string) *ImageCallbackToken {
	token := &ImageCallbackToken{
		callback: callback,
		done:     make(chan struct{}),
	}

	go token.downloadImage(id, url, width, height)

	return token
}

func (token *ImageCallbackToken) Close() {
	close(token.done)
}

func timeIt(start time.Time, description string) {
	elapsed := time.Since(start)
	loggerFunc("%s took %s", description, elapsed)
}

func (token *ImageCallbackToken) downloadImage(id string, url string, width int, height int) {
	loggerFunc("downloading %s %s %dx%d", id, url, width, height)

	boe, ok := <-downloadBytes(url, token.done)
	if !ok {
		return
	}

	if boe.Error != nil {
		loggerFunc("failed to download %s %s %dx%d %s", id, url, width, height, boe.Error.Error())
		token.callback.ImageFailed(id)
		return
	}

	imgBytes, ok := <-scaleImg(token.done, boe.Bytes, width, height)
	if !ok {
		return
	}

	if imgBytes.Error != nil {
		loggerFunc("failed to decode image %s %s %dx%d %s", id, url, width, height, imgBytes.Error.Error())
		token.callback.ImageFailed(id)
		return
	}

	select {
	case <-token.done:
		return
	default:
		start := time.Now()
		token.callback.ImageReceived(imgBytes.Bytes, id)
		// token.callback.ImageFailed(id)
		loggerFunc("%s took %s size %d", " <- ImageReceived", time.Since(start), len(imgBytes.Bytes))

		freeMemory()

		printHeap()
	}
}

func freeMemory() {
	defer timeIt(time.Now(), "freeMemory")

	runtime.GC()

	// This isn't doing anything, so don't bother
	// debug.FreeOSMemory()
}

func printHeap() {
	memstats := new(runtime.MemStats)
	runtime.ReadMemStats(memstats)
	loggerFunc("memstats: Alloc = %s Sys = %s", sizeof_fmt(memstats.Alloc), sizeof_fmt(memstats.Sys))
}

var byteSizes [8]string = [...]string{
	"", "K", "M", "G", "T", "P", "E", "Z",
}

func sizeof_fmt(num uint64) string {
	suffix := "b"

	for _, unit := range byteSizes {
		if num < 1024.0 {
			return fmt.Sprintf("%d %s%s", num, unit, suffix)
		}
		num /= 1024.0
	}
	return fmt.Sprintf("%d %s%s", num, "Y", suffix)
}

func scaleImg(done chan struct{}, imgBytes []byte, width int, height int) chan bytesOrError {
	resultChan := make(chan bytesOrError)

	go func() {
		defer close(resultChan)
		start := time.Now()
		orgStart := start

		result := bytesOrError{
			Bytes: nil,
			Error: nil,
		}

		errorOut := func(err error) {
			result.Error = err
			select {
			case <-done:
			case resultChan <- result:
			}
		}

		img, _, err := image.Decode(bytes.NewReader(imgBytes))
		if err != nil {
			errorOut(err)
			return
		}

		select {
		case <-done:
			return
		default:
		}

		decoding := time.Since(start)
		start = time.Now()

		uWidth := uint(width)
		uHight := uint(height)

		if uWidth != 0 && uHight != 0 {
			// full screen images!
			if uWidth > uHight {
				uHight = 0
			} else {
				uWidth = 0
			}

			img = resize.Resize(uWidth, uHight, img, resize.Bilinear)
		} else {
			loggerFunc("invalid width and height")
		}

		select {
		case <-done:
			return
		default:
		}

		resizing := time.Since(start)
		start = time.Now()

		img, err = cutter.Crop(img, cutter.Config{
			Width:  width,
			Height: height,
			Mode:   cutter.Centered,
		})
		if err != nil {
			errorOut(err)
			return
		}

		// loggerFunc("cutting image down from %+v to %+v", orgBounds, img.Bounds())

		buf := new(bytes.Buffer)
		e := png.Encoder{
			CompressionLevel: png.NoCompression,
		}
		if err := e.Encode(buf, img); err != nil {
			errorOut(err)
			return
		}

		loggerFunc("(decode/resize/encode) took %s/%s/%s total: %s", decoding, resizing, time.Since(start), time.Since(orgStart))

		result.Bytes = buf.Bytes()

		select {
		case <-done:
		case resultChan <- result:
		}
	}()
	return resultChan
}
