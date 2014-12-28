package libcats

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
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

func OnStart() {
	fmt.Printf("onStart()")
}

func OnStop() {
	fmt.Printf("onStop()")
}

var transport *httpcache.Transport

func Init(cachePath string) {
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
	fmt.Printf("callback closed.\n")
}

func CreateImageCallback(callback ImageCallback) *CallbackToken {
	imageCallback := &CallbackToken{
		imageCallback: callback,
		done:          make(chan struct{}),
		wg:            sync.WaitGroup{},
	}

	go startTimer(imageCallback)

	return imageCallback
}

// Buffers 4 images in memory ready to be consumed by
// the timer when it ticks
func startTimer(token *CallbackToken) {
	fmt.Printf("starting callback timer\n")

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
			log.Printf("receive on done channel")
			return
		case <-c:
			{
				select {
				case <-token.done:
					token.wg.Wait()
					log.Printf("receive on done channel")
					return
				case img = <-images:
					fmt.Printf("sending callback\n")
					token.imageCallback.ImageReceived(img)
				}
			}
		}
	}
}

func consumeUrl(urls chan string, images chan []byte, done chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	for url := range urls {
		fmt.Printf("downloading %s\n", url)
		if bytes, err := DownloadBytes(url); err == nil {
			select {
			case <-done:
				return
			case images <- bytes:
			}
		}
	}
}

func produceCatUrls(done chan struct{}) chan string {
	var urls = make(chan string)

	go func() {
		defer close(urls)
		response, err := getCats()

		if err != nil {
			log.Printf("failed to produceCatUrls. " + err.Error())
			return
		}

		for _, v := range response.Data.Children {
			if v.Kind == "t3" && v.Data.Url != "" {
				select {
				case <-done:
					return
				case urls <- v.Data.Url:
				}

			}
		}
	}()

	return urls
}

func getCats() (*redditResponseDto, error) {

	resp, err := getClient().Get("http://www.reddit.com/r/aww.json")
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
	Url string
}

func DownloadBytes(url string) ([]byte, error) {

	resp, err := getClient().Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	fmt.Printf("from cache? %s\n", resp.Header.Get("X-From-Cache"))

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
