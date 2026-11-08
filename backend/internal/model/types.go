package model

import "time"

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type Session struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Title     string    `json:"title"`
	Provider  string    `json:"provider"`
	Mode      string    `json:"mode"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type Message struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionId"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

type TeamMember struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	CurrentTask string `json:"currentTask,omitempty"`
	SessionID   string `json:"sessionId,omitempty"`
}

type TeamRun struct {
	ID         string       `json:"id"`
	SessionID  string       `json:"sessionId"`
	TaskTitle  string       `json:"taskTitle"`
	TaskPrompt string       `json:"taskPrompt"`
	Status     string       `json:"status"`
	Members    []TeamMember `json:"members"`
	Demo       bool         `json:"demo"`
	CreatedAt  time.Time    `json:"createdAt"`
}

type TeamMessage struct {
	ID        string    `json:"id"`
	TeamRunID string    `json:"teamRunId"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

type Approval struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionId"`
	Source    string    `json:"source"`
	Command   string    `json:"command"`
	Risk      string    `json:"risk"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}
