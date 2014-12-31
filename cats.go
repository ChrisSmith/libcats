package libcats

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"encoding/json"

	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/peterbourgon/diskv"
)

type ImageCallback interface {
	ImageReceived(image []byte)
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

	var images = make(chan []byte, 4)
	defer close(images)

	// need to wait for produces to exit before
	// closing the images channel
	token.wg.Add(4)

	for i := 0; i < 4; i++ {
		go consumeUrl(urls, images, token.done, &token.wg)
	}

	c := time.Tick(5 * time.Second)

	var img []byte
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
					token.imageCallback.ImageReceived(img)
				}
			}
		}
	}
}

func consumeUrl(urls chan string, images chan []byte, done chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	for url := range urls {
		loggerFunc("downloading %s", url)
		if bytes, err := DownloadBytes(url); err == nil {
			select {
			case <-done:
				return
			case images <- bytes:
			}
		} else {
			loggerFunc("failed to download %s %s", url, err.Error())
		}
	}
}

func produceCatUrls(done chan struct{}) chan string {
	var urls = make(chan string)

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

					select {
					case <-done:
						return
					case urls <- url:
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
	Url  string
	Name string
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
