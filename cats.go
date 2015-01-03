package libcats

import (
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
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

type ImageMetaData struct {
	Url       string
	Title     string
	Author    string
	Permalink string
	Id        string
}

type MetaDataCallback interface {
	MetaDataReceived(url string, title string, author string, permalink string, id string)
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
	isLoadingMux     *sync.Mutex
	wg               sync.WaitGroup
	done             chan struct{}
	nextBatch        chan struct{}
	isLoading        bool
	metaDataCallback MetaDataCallback
}

func (token *CallbackToken) LoadNextBatch() {
	token.isLoadingMux.Lock()

	if !token.isLoading {
		token.isLoading = true
		token.isLoadingMux.Unlock()

		token.nextBatch <- struct{}{}
	} else {
		token.isLoadingMux.Unlock()
	}
}

func (token *CallbackToken) Close() {
	close(token.done)
	token.wg.Wait()
	loggerFunc("callback closed.")
}

func CreateMetaDataCallback(callback MetaDataCallback, lastPost string) *CallbackToken {
	token := &CallbackToken{
		metaDataCallback: callback,
		done:             make(chan struct{}),
		nextBatch:        make(chan struct{}),
		wg:               sync.WaitGroup{},
		isLoading:        false,
		isLoadingMux:     &sync.Mutex{},
	}

	go produceCatUrls(token, lastPost)

	loggerFunc("started produceCatUrls, GOMAXPROCS %d", runtime.GOMAXPROCS(-1))

	return token
}

func produceCatUrls(token *CallbackToken, lastPost string) {
	token.wg.Add(1)
	defer token.wg.Done()

	loggerFunc("starting produceCatUrls")

	backoff := 100

	for {
		token.isLoadingMux.Lock()
		token.isLoading = true
		token.isLoadingMux.Unlock()

		response, err := getCats(lastPost)

		if err != nil {
			loggerFunc("failed to produceCatUrls. " + err.Error())

			select {
			case <-token.done:
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
				case <-token.done:
					return
				default:
					start := time.Now()
					permalink := "http://www.reddit.com/" + v.Data.Permalink
					token.metaDataCallback.MetaDataReceived(url, v.Data.Title, v.Data.Author, permalink, v.Data.Name)
					lastPost = v.Data.Name
					loggerFunc("%s took %s", " <- MetaDataReceived", time.Since(start))
				}
			}
		}

		token.isLoadingMux.Lock()
		token.isLoading = false
		token.isLoadingMux.Unlock()

		select {
		case <-token.done:
			return
		case <-token.nextBatch:
			continue
		}
	}
}

func getCats(afterPost string) (*redditResponseDto, error) {
	url := "http://www.reddit.com/r/aww.json"
	if afterPost != "" {
		url += "?after=" + afterPost
	}

	resp, err := DownloadBytes(url)
	if err != nil {
		return nil, err
	}

	redditResponse := redditResponseDto{}
	if err := json.Unmarshal(resp, &redditResponse); err != nil {
		return nil, err
	}

	return &redditResponse, nil
}

func DownloadBytes(url string) ([]byte, error) {
	defer timeIt(time.Now(), fmt.Sprintf("downloading %s", url))

	resp, err := getClient().Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.Header.Get("X-From-Cache") == "1" {
		loggerFunc("from cache: YES")
	} else {
		loggerFunc("from cache: NO")
	}

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
