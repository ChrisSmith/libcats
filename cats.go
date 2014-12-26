package libcats

import (
	"fmt"
	"io/ioutil"

	"encoding/json"
	"errors"

	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/peterbourgon/diskv"
)

var transport *httpcache.Transport

func Init(cachePath string) {
	transport = newTransportWithDiskCache(cachePath)
}

func GetCats() (*RedditUrlCollection, error) {

	resp, err := transport.Client().Get("http://www.reddit.com/r/aww.json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	decoder := json.NewDecoder(resp.Body)

	response := redditResponseDto{}
	if err := decoder.Decode(&response); err != nil {
		return nil, err
	}

	urls := make([]string, 0)
	for _, v := range response.Data.Children {
		if v.Kind == "t3" && v.Data.Url != "" {
			urls = append(urls, v.Data.Url)
		}
	}

	return &RedditUrlCollection{
		urls: urls,
	}, nil
}

func (dto *RedditUrlCollection) GetUrl(index int) (string, error) {
	if index < len(dto.urls) && index >= 0 {
		return dto.urls[index], nil
	}
	return "", errors.New("invalid index")
}

type RedditUrlCollection struct {
	urls []string
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

func DownloadCat(url string) ([]byte, error) {

	resp, err := transport.Client().Get(url)
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
