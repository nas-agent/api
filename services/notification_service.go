package services

import (
	"fmt"
	"sync"

	"github.com/gofiber/fiber/v2"
)

// NotificationEvent represents the structure of a notification
type NotificationEvent struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Filename string `json:"filename"`
	Folder   string `json:"folder"`
}

var (
	pendingNotifications []NotificationEvent
	notifMutex           sync.Mutex
)

// PollNotifications serves pending notifications and clears the queue
func PollNotifications(c *fiber.Ctx) error {
	notifMutex.Lock()
	defer notifMutex.Unlock()

	// Return an empty array instead of null if empty
	toSend := pendingNotifications
	if toSend == nil {
		toSend = []NotificationEvent{}
	}

	// Clear the queue after sending
	pendingNotifications = nil

	return c.JSON(toSend)
}

// NotifyFileMoved adds a notification to the pending queue
func NotifyFileMoved(filename, folder string) {
	notifMutex.Lock()
	defer notifMutex.Unlock()

	event := NotificationEvent{
		Type:     "file_moved",
		Title:    "AI Assistant",
		Body:     fmt.Sprintf("Moved '%s' to folder '%s'", filename, folder),
		Filename: filename,
		Folder:   folder,
	}
	pendingNotifications = append(pendingNotifications, event)
}
