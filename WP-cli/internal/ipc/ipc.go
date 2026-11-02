package ipc

import (
	"time"

	"wp/internal/store"
)

type Request struct {
	Cmd   string   `json:"cmd"`
	Query string   `json:"query,omitempty"`
	Text  string   `json:"text,omitempty"`
	Args  []string `json:"args,omitempty"`
	Limit int      `json:"limit,omitempty"`
	Reply string   `json:"reply,omitempty"`
	File  string   `json:"file,omitempty"`
}

type Status struct {
	Linked    bool   `json:"linked"`
	JID       string `json:"jid,omitempty"`
	PushName  string `json:"pushName,omitempty"`
	Connected bool   `json:"connected"`
	Pairing   bool   `json:"pairing"`
	Code      string `json:"code,omitempty"`
	Chats     int    `json:"chats"`
}

type Response struct {
	OK       bool            `json:"ok"`
	Error    string          `json:"error,omitempty"`
	Status   *Status         `json:"status,omitempty"`
	Chats    []store.Chat    `json:"chats,omitempty"`
	Messages []store.Message `json:"messages,omitempty"`
	Chat     *store.Chat     `json:"chat,omitempty"`
	Sent     *store.Message  `json:"sent,omitempty"`
	Hits     []store.Hit     `json:"hits,omitempty"`
	Path     string          `json:"path,omitempty"`
}

type Live struct {
	ID       string    `json:"id,omitempty"`
	Chat     string    `json:"chat"`
	Name     string    `json:"name"`
	FromMe   bool      `json:"fromMe"`
	Text     string    `json:"text"`
	At       time.Time `json:"at"`
	Receipt  string    `json:"receipt,omitempty"`
	Refresh  bool      `json:"refresh,omitempty"`
	Kind     string    `json:"kind,omitempty"`
	Path     string    `json:"path,omitempty"`
	Deleted  bool      `json:"deleted,omitempty"`
	Edited   bool      `json:"edited,omitempty"`
	Presence string    `json:"presence,omitempty"`
}
