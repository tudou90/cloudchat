package models

import "time"

type Message struct {
	// ID uniquely identifies a published message (used to de-duplicate
	// history against live messages).
	ID string `json:"id,omitempty"`
	// Seq orders a room's messages (assigned by Redis, so it doesn't depend
	// on any server's clock).
	Seq      int64     `json:"seq,omitempty"`
	Type     string    `json:"type"`
	Sender   string    `json:"sender"`
	SenderID string    `json:"senderId"`
	Content  string    `json:"content"`
	Avatar   string    `json:"avatar"`
	Time     time.Time `json:"time"`
	// File is set on "file" messages.
	File *FileInfo `json:"file,omitempty"`
	// Token is sent only in the "welcome" message, to the client it
	// identifies; it authorizes HTTP actions (e.g. uploads) as that client.
	Token string `json:"token,omitempty"`
	// Messages carries the room's recent messages on a "history" message.
	Messages []Message `json:"messages,omitempty"`
}

// Member is one person in a room, as listed in "presence" messages.
type Member struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type FileInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Mime string `json:"mime"`
	URL  string `json:"url"`
}
