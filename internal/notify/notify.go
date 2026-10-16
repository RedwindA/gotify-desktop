// Package notify shows desktop notifications through each OS's native API
// and decides which Gotify messages deserve one.
package notify

import (
	"errors"
	"strings"
	"sync"
)

type Level int

const (
	LevelSilent Level = iota
	LevelNormal
	LevelHigh
)

type Notification struct {
	ID        string
	Title     string
	Body      string
	AppName   string
	IconPath  string
	ImagePath string
	Group     string
	Level     Level
	// Actions are the notification's buttons.
	Actions []Action
}

// Action is a button of a notification. Choosing it activates the notification
// as ActionArgs(id, action.ID).
type Action struct {
	ID    string
	Label string
}

const (
	// ActionRead marks the notification's messages read.
	ActionRead = "read"
	// ActionOpen opens the message's link.
	ActionOpen = "open"
)

// ActionArgs is what activating the action of a notification reports.
func ActionArgs(id, action string) string { return id + "#" + action }

// SplitActivation separates what an activation reports into the notification's
// id and the action chosen, "" for a click on the notification itself.
func SplitActivation(s string) (id, action string) {
	id, action, _ = strings.Cut(s, "#")
	return id, action
}

type Notifier interface {
	Supported() bool
	// Show is safe from any goroutine.
	Show(n Notification) error
	Remove(id string)
	// OnActivate fn runs on an arbitrary goroutine when the user clicks a notification.
	// Clicks that arrived before fn was set are replayed.
	OnActivate(fn func(id string))
}

var ErrUnsupported = errors.New("notify: notifications are not supported here")

// IsActivationLaunch reports whether the process was started by Windows to
// deliver a toast click (COM activation passes -Embedding).
func IsActivationLaunch(args []string) bool {
	for _, a := range args {
		if strings.EqualFold(strings.TrimLeft(a, "-/"), "embedding") {
			return true
		}
	}
	return false
}

const maxPendingActivations = 16

type activator struct {
	mu      sync.Mutex
	fn      func(string)
	pending []string
}

func (a *activator) set(fn func(string)) {
	a.mu.Lock()
	a.fn = fn
	pending := a.pending
	a.pending = nil
	a.mu.Unlock()
	if fn != nil {
		for _, id := range pending {
			fn(id)
		}
	}
}

func (a *activator) fire(id string) {
	if id == "" {
		return
	}
	a.mu.Lock()
	fn := a.fn
	if fn == nil && len(a.pending) < maxPendingActivations {
		a.pending = append(a.pending, id)
	}
	a.mu.Unlock()
	if fn != nil {
		fn(id)
	}
}

// Unsupported returns a notifier that shows nothing: Show fails with ErrUnsupported.
func Unsupported() Notifier { return unsupported{} }

type unsupported struct{}

func (unsupported) Supported() bool         { return false }
func (unsupported) Show(Notification) error { return ErrUnsupported }
func (unsupported) Remove(string)           {}
func (unsupported) OnActivate(func(string)) {}
