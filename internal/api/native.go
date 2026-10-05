package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// The images a window of native UI shows, which a page loads from
// ImageHandler and AppImage instead. They are on the controller, so that the
// page cannot call them.

// AppImageData returns the image of an app, or nil when it has none.
func (c *Controller) AppImageData(serverID int64, appID uint) []byte {
	if !c.started.Load() {
		return nil
	}
	sv, ok := c.svc.be.Snapshot().Server(serverID)
	if !ok {
		return nil
	}
	a, ok := sv.App(appID)
	if !ok || !strings.HasPrefix(http.DetectContentType(a.Image), "image/") {
		return nil
	}
	return a.Image
}

// MessageImage downloads the image a message shows below its body
// (Message.ImageURL), or returns nil when it has none.
func (c *Controller) MessageImage(ctx context.Context, serverID int64, id uint) ([]byte, error) {
	if !c.started.Load() {
		return nil, nil
	}
	return c.svc.messageImage(ctx, serverID, id)
}

// FetchImage downloads an image of a message's Markdown body.
func (c *Controller) FetchImage(ctx context.Context, url string) ([]byte, error) {
	if !c.started.Load() {
		return nil, errors.New("not started")
	}
	ctx, cancel := context.WithTimeout(ctx, imageTimeout)
	defer cancel()
	data, err := c.svc.be.FetchImage(ctx, url)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(http.DetectContentType(data), "image/") {
		return nil, errors.New("not an image")
	}
	return data, nil
}
