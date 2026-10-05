package markdown

import (
	"container/list"
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	maxImageBytes = 5 << 20
	maxCached     = 64
	fetchTimeout  = 10 * time.Second
)

type imageState int

const (
	imageLoading imageState = iota
	imageReady
	imageFailed
)

type imageEntry struct {
	url    string
	state  imageState
	bitmap *ui.Bitmap
	elem   *list.Element
}

// ImageCache fetches and decodes message images once, off the UI thread.
type ImageCache struct {
	http     *http.Client
	onLoaded func()

	mu      sync.Mutex
	entries map[string]*imageEntry
	lru     *list.List
}

// NewImageCache returns a cache whose onLoaded runs, on any goroutine, when an image finishes loading.
func NewImageCache(httpc *http.Client, onLoaded func()) *ImageCache {
	if httpc == nil {
		httpc = &http.Client{Timeout: fetchTimeout}
	}
	return &ImageCache{http: httpc, onLoaded: onLoaded, entries: map[string]*imageEntry{}, lru: list.New()}
}

// Get returns the image for a URL and starts loading it the first time.
func (c *ImageCache) Get(url string) (*ui.Bitmap, imageState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[url]; ok {
		c.lru.MoveToFront(e.elem)
		return e.bitmap, e.state
	}
	e := &imageEntry{url: url}
	e.elem = c.lru.PushFront(e)
	c.entries[url] = e
	for c.lru.Len() > maxCached {
		old := c.lru.Back()
		c.lru.Remove(old)
		delete(c.entries, old.Value.(*imageEntry).url)
	}
	go c.load(e)
	return nil, imageLoading
}

func (c *ImageCache) load(e *imageEntry) {
	bmp := c.fetch(e.url)
	c.mu.Lock()
	if bmp != nil {
		e.bitmap, e.state = bmp, imageReady
	} else {
		e.state = imageFailed
	}
	c.mu.Unlock()
	if c.onLoaded != nil {
		c.onLoaded()
	}
}

func (c *ImageCache) fetch(url string) *ui.Bitmap {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil || len(data) > maxImageBytes {
		return nil
	}
	bmp, err := ui.DecodeBitmap(data)
	if err != nil {
		return nil
	}
	return bmp
}
