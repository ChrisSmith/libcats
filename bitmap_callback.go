package libcats

import (
	"bytes"
	"image"
	"image/png"
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

	if imgBytes, err := DownloadBytes(url); err == nil {
		imgBytes, err := scaleImg(imgBytes, width, height)
		if err != nil {
			loggerFunc("failed to decode image %s %s %dx%d %s", id, url, width, height, err.Error())
			token.callback.ImageFailed(id)
			return
		}

		select {
		case <-token.done:
			return
		default:
			start := time.Now()
			token.callback.ImageReceived(imgBytes, id)
			// token.callback.ImageFailed(id)
			loggerFunc("%s took %s size %d", " <- ImageReceived", time.Since(start), len(imgBytes))
		}
	} else {
		loggerFunc("failed to download %s %s %dx%d %s", id, url, width, height, err.Error())
		token.callback.ImageFailed(id)
	}
}

func scaleImg(imgBytes []byte, width int, height int) ([]byte, error) {
	start := time.Now()

	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return nil, err
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

	resizing := time.Since(start)
	start = time.Now()

	orgBounds := img.Bounds()

	img, err = cutter.Crop(img, cutter.Config{
		Width:  width,
		Height: height,
		Mode:   cutter.Centered,
	})
	if err != nil {
		return nil, err
	}

	loggerFunc("cutting image down from %+v to %+v", orgBounds, img.Bounds())

	cropping := time.Since(start)
	start = time.Now()

	buf := new(bytes.Buffer)
	e := png.Encoder{
		CompressionLevel: png.NoCompression,
	}
	if err := e.Encode(buf, img); err != nil {
		return nil, err
	}

	loggerFunc("(decode/resize/crop/encode) took %s/%s/%s/%s", decoding, resizing, cropping, time.Since(start))

	return buf.Bytes(), nil
}
