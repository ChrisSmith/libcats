package libcats

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io/ioutil"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"encoding/json"

	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/nfnt/resize"
	"github.com/peterbourgon/diskv"
)

type ImageInfo struct {
	Url       string
	Title     string
	Author    string
	Permalink string

	Image []byte
}

type ImageMetaData struct {
	Url       string
	Title     string
	Author    string
	Permalink string
}

type ImageCallback interface {
	ImageReceived(image []byte, url string, title string, author string, permalink string)
}

var transport *httpcache.Transport
var loggerFunc func(string, ...interface{}) = func(format string, args ...interface{}) {
	fmt.Printf("%s\n", fmt.Sprintf(format, args...))
}

func Init(cachePath string) {
	runtime.GOMAXPROCS(runtime.NumCPU())

	transport = newTransportWithDiskCache(cachePath)
}

type CallbackToken struct {
	done          chan struct{}
	imageCallback ImageCallback
	wg            sync.WaitGroup
}

func (token *CallbackToken) Close() {
	close(token.done)
	token.wg.Wait()
	loggerFunc("callback closed.")
}

func CreateImageCallback(callback ImageCallback) *CallbackToken {
	imageCallback := &CallbackToken{
		imageCallback: callback,
		done:          make(chan struct{}),
		wg:            sync.WaitGroup{},
	}

	loggerFunc("starting timer")

	go startTimer(imageCallback)

	loggerFunc("returning")

	return imageCallback
}

// Buffers 4 images in memory ready to be consumed by
// the timer when it ticks
func startTimer(token *CallbackToken) {
	loggerFunc("starting callback timer, GOMAXPROCS %d", runtime.GOMAXPROCS(-1))

	urls := produceCatUrls(token.done)

	var images = make(chan ImageInfo, 4)
	defer close(images)

	// need to wait for produces to exit before
	// closing the images channel
	token.wg.Add(4)

	for i := 0; i < 4; i++ {
		go consumeUrl(urls, images, token.done, &token.wg)
	}

	c := time.Tick(5 * time.Second)

	var img ImageInfo
	for {
		select {
		case <-token.done:
			token.wg.Wait()
			loggerFunc("receive on done channel")
			return
		case <-c:
			{
				select {
				case <-token.done:
					token.wg.Wait()
					loggerFunc("receive on done channel")
					return
				case img = <-images:
					loggerFunc("sending callback")
					token.imageCallback.ImageReceived(img.Image, img.Url, img.Title, img.Author, img.Permalink)
					loggerFunc("successfully sent callback")
				}
			}
		}
	}
}

func consumeUrl(urls chan *ImageMetaData, images chan ImageInfo, done chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	for url := range urls {
		loggerFunc("downloading %s", url.Url)
		if imgBytes, err := DownloadBytes(url.Url); err == nil {
			imgBytes, err := scaleImg(imgBytes)
			if err != nil {
				loggerFunc("failed to decode image %+v %s", url, err.Error())
				continue
			}

			imgInfo := ImageInfo{
				Url:       url.Url,
				Title:     url.Title,
				Author:    url.Author,
				Permalink: "http://www.reddit.com/" + url.Permalink,
				Image:     imgBytes,
			}

			select {
			case <-done:
				return
			case images <- imgInfo:
			}
		} else {
			loggerFunc("failed to download %+v %s", url, err.Error())
		}
	}
}

func scaleImg(imgBytes []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return nil, err
	}

	img = resize.Resize(1080, 0, img, resize.Bilinear)

	buf := new(bytes.Buffer)
	if err := png.Encode(buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func produceCatUrls(done chan struct{}) chan *ImageMetaData {
	var urls = make(chan *ImageMetaData)

	go func() {
		defer close(urls)
		lastPost := ""
		backoff := 100

		for {
			response, err := getCats(lastPost)

			if err != nil {
				loggerFunc("failed to produceCatUrls. " + err.Error())

				select {
				case <-done:
					return
				case <-time.After(time.Duration(backoff) * time.Millisecond):
					backoff *= 2
					loggerFunc("increased backoff to %d ms", backoff)
					continue
				}
			}

			backoff = 100

			for _, v := range response.Data.Children {
				if v.Kind == "t3" && v.Data.Url != "" {
					url := v.Data.Url

					// sometimes the url is missing an extension
					dotIndex := strings.LastIndex(url, ".")
					if dotIndex == -1 || dotIndex < strings.LastIndex(url, "/") {
						url = url + ".jpg"
					}

					metaData := &ImageMetaData{
						Url:       url,
						Title:     v.Data.Title,
						Author:    v.Data.Author,
						Permalink: v.Data.Permalink,
					}

					select {
					case <-done:
						return
					case urls <- metaData:
						lastPost = v.Data.Name
					}

				}
			}
		}
	}()

	return urls
}

func getCats(afterPost string) (*redditResponseDto, error) {

	url := "http://www.reddit.com/r/aww.json"
	if afterPost != "" {
		url += "?after=" + afterPost
	}

	resp, err := getClient().Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	decoder := json.NewDecoder(resp.Body)

	response := redditResponseDto{}
	if err := decoder.Decode(&response); err != nil {
		return nil, err
	}

	return &response, nil
}

type redditResponseDto struct {
	Kind string
	Data redditCollectionDto
}

type redditCollectionDto struct {
	Modhash  string
	Children []redditPostDto1
}

type redditPostDto1 struct {
	Kind string
	Data redditPostDto
}

type redditPostDto struct {
	Url       string
	Name      string
	Title     string
	Author    string
	Permalink string
}

func DownloadBytes(url string) ([]byte, error) {

	resp, err := getClient().Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	loggerFunc("from cache? %s", resp.Header.Get("X-From-Cache"))

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func newTransportWithDiskCache(basePath string) *httpcache.Transport {
	d := diskv.New(diskv.Options{
		BasePath:     basePath,
		CacheSizeMax: 100 * 1024 * 10, // 10MB
	})

	c := diskcache.NewWithDiskv(d)

	return httpcache.NewTransport(c)
}

func getClient() *http.Client {
	c := transport.Client()
	//c.Timeout = time.Duration(30 * time.Second) //TODO Client Transport of type *httpcache.Transport doesn't support CanelRequest; Timeout not supported
	return c
}
