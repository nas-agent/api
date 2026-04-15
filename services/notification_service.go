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
	broadcaster          Broadcaster
)

// Broadcaster interface allows services to broadcast notifications
type Broadcaster interface {
	Broadcast(notif interface{})
}

// SetNotificationBroadcaster sets the broadcaster instance
func SetNotificationBroadcaster(b Broadcaster) {
	broadcaster = b
}

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

// NotifyFileMoved broadcasts a file moved notification and adds to pending queue for polling fallback
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

	// Broadcast via WebSocket if available
	if broadcaster != nil {
		broadcaster.Broadcast(map[string]interface{}{
			"type":     event.Type,
			"title":    event.Title,
			"body":     event.Body,
			"filename": event.Filename,
			"folder":   event.Folder,
		})
	}

	// Also keep in pending queue for polling fallback
	pendingNotifications = append(pendingNotifications, event)
}

// NotifyApprovalNeeded broadcasts a notification when a file requires manual review.
func NotifyApprovalNeeded(filename, suggestedFolder string) {
	notifMutex.Lock()
	defer notifMutex.Unlock()

	event := NotificationEvent{
		Type:     "approval_needed",
		Title:    "AI Assistant",
		Body:     fmt.Sprintf("Review needed for '%s' -> suggested folder '%s'", filename, suggestedFolder),
		Filename: filename,
		Folder:   suggestedFolder,
	}

	// Broadcast via WebSocket if available
	if broadcaster != nil {
		broadcaster.Broadcast(map[string]interface{}{
			"type":     event.Type,
			"title":    event.Title,
			"body":     event.Body,
			"filename": event.Filename,
			"folder":   event.Folder,
		})
	}

	// Also keep in pending queue for polling fallback
	pendingNotifications = append(pendingNotifications, event)
}
