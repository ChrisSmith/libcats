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

		response, ok := <-getCats(lastPost, token.done)
		if !ok {
			// channel was closed
			return
		}

		if response.Error != nil {
			loggerFunc("failed to produceCatUrls. " + response.Error.Error())

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

		for _, v := range response.Response.Data.Children {
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

type redditResponseOrError struct {
	Response *redditResponseDto
	Error    error
}

func getCats(afterPost string, done chan struct{}) chan redditResponseOrError {
	returnChan := make(chan redditResponseOrError)

	go func() {
		defer close(returnChan)

		url := "http://www.reddit.com/r/aww.json"
		if afterPost != "" {
			url += "?after=" + afterPost
		}

		boe, ok := <-downloadBytes(url, done)
		if !ok {
			return
		}

		if boe.Error != nil {
			select {
			case <-done:
			case returnChan <- redditResponseOrError{Error: boe.Error}:
			}
			return
		}

		redditResponse := redditResponseDto{}
		if err := json.Unmarshal(boe.Bytes, &redditResponse); err != nil {
			select {
			case <-done:
			case returnChan <- redditResponseOrError{Error: boe.Error}:
			}
			return
		}

		select {
		case <-done:
		case returnChan <- redditResponseOrError{Response: &redditResponse}:
		}
	}()

	return returnChan
}

type bytesOrError struct {
	Bytes []byte
	Error error
}

func downloadBytes(url string, done chan struct{}) chan bytesOrError {
	returnChan := make(chan bytesOrError, 1)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		defer close(returnChan)
		returnChan <- bytesOrError{Error: err}
		return returnChan
	}

	go func() {
		defer timeIt(time.Now(), fmt.Sprintf("downloading %s", url))
		defer close(returnChan)

		result := bytesOrError{
			Bytes: nil,
			Error: nil,
		}

		resp, err := getClient().Do(req)
		if err != nil {
			result.Error = err
			returnChan <- result
			return
		}

		defer resp.Body.Close()

		if resp.Header.Get("X-From-Cache") == "1" {
			loggerFunc("from cache: YES")
		} else {
			loggerFunc("from cache: NO")
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			result.Error = err
		} else {
			result.Bytes = body
		}

		returnChan <- result
	}()

	// The http request funnels though the second goroutine
	// so we can return when the cancellation doesn't happen
	returnChan2 := make(chan bytesOrError, 1)

	go func() {
		defer close(returnChan2)

		select {
		case bytesOrError, ok := <-returnChan:
			if !ok {
				return
			} else {
				returnChan2 <- bytesOrError
			}
		case <-done:

			loggerFunc("trying to cancel request")
			if tr, ok := http.DefaultTransport.(*http.Transport); ok {
				loggerFunc("trying to cancel request 2")
				tr.CancelRequest(req)
				loggerFunc("successfully canceled request")
			}

		}
	}()

	return returnChan2
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
