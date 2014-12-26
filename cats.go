package libcats

import (
	"fmt"
	"io/ioutil"

	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/peterbourgon/diskv"
)

var transport *httpcache.Transport

func Init(cachePath string) {
	transport = newTransportWithDiskCache(cachePath)
}

func GetCats(name string) string {
	return fmt.Sprintf("Meow, I'm %s!\n", name)
}

func DownloadCat() ([]byte, error) {

	resp, err := transport.Client().Get("http://i.imgur.com/3UD7Aqz.jpg")
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
	// return base64.StdEncoding.EncodeToString(body), nil
}

func newTransportWithDiskCache(basePath string) *httpcache.Transport {
	d := diskv.New(diskv.Options{
		BasePath:     basePath,
		CacheSizeMax: 100 * 1024 * 10, // 10MB
	})

	c := diskcache.NewWithDiskv(d)

	return httpcache.NewTransport(c)
}
