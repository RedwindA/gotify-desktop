package gotify

import "time"

type Message struct {
	ID       uint           `json:"id"`
	AppID    uint           `json:"appid"`
	Message  string         `json:"message"`
	Title    string         `json:"title"`
	Priority int            `json:"priority"`
	Extras   map[string]any `json:"extras,omitempty"`
	Date     time.Time      `json:"date"`
}

type Application struct {
	ID              uint       `json:"id"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	Internal        bool       `json:"internal"`
	Image           string     `json:"image"`
	DefaultPriority int        `json:"defaultPriority"`
	CreatedAt       time.Time  `json:"createdAt"`
	LastUsed        *time.Time `json:"lastUsed"`
	SortKey         string     `json:"sortKey"`
}

type Paging struct {
	Next  string `json:"next,omitempty"`
	Size  int    `json:"size"`
	Since uint   `json:"since"`
	Limit int    `json:"limit"`
}

type PagedMessages struct {
	Paging   Paging    `json:"paging"`
	Messages []Message `json:"messages"`
}

type User struct {
	ID            uint       `json:"id"`
	Name          string     `json:"name"`
	Admin         bool       `json:"admin"`
	CreatedAt     time.Time  `json:"createdAt"`
	ClientID      uint       `json:"clientId,omitempty"`
	ElevatedUntil *time.Time `json:"elevatedUntil,omitempty"`
}

type VersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
}
