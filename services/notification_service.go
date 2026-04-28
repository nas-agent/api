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
	UserID   string `json:"user_id"`
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
func NotifyFileMoved(filename, folder string, userID string) {
	notifMutex.Lock()
	defer notifMutex.Unlock()

	translator := NewPathTranslator()
	winFolder := translator.ToWindowsPath(userID, folder)

	event := NotificationEvent{
		Type:     "file_moved",
		Title:    "AI Assistant",
		Body:     fmt.Sprintf("Moved '%s' to folder '%s'", filename, folder),
		Filename: filename,
		Folder:   winFolder,
		UserID:   userID,
	}

	// Broadcast via WebSocket if available
	if broadcaster != nil {
		broadcaster.Broadcast(map[string]interface{}{
			"type":     event.Type,
			"title":    event.Title,
			"body":     event.Body,
			"filename": event.Filename,
			"folder":   event.Folder,
			"user_id":   event.UserID,
		})
	}

	// Also keep in pending queue for polling fallback
	pendingNotifications = append(pendingNotifications, event)
}

// NotifyApprovalNeeded broadcasts a notification when a file requires manual review.
func NotifyApprovalNeeded(filename, suggestedFolder string, userID string) {
	notifMutex.Lock()
	defer notifMutex.Unlock()

	translator := NewPathTranslator()
	winFolder := translator.ToWindowsPath(userID, suggestedFolder)

	event := NotificationEvent{
		Type:     "approval_needed",
		Title:    "AI Assistant",
		Body:     fmt.Sprintf("Review needed for '%s' -> suggested folder '%s'", filename, suggestedFolder),
		Filename: filename,
		Folder:   winFolder,
		UserID:   userID,
	}

	// Broadcast via WebSocket if available
	if broadcaster != nil {
		broadcaster.Broadcast(map[string]interface{}{
			"type":     event.Type,
			"title":    event.Title,
			"body":     event.Body,
			"filename": event.Filename,
			"folder":   event.Folder,
			"user_id":   event.UserID,
		})
	}

	// Also keep in pending queue for polling fallback
	pendingNotifications = append(pendingNotifications, event)
}
// NotifyQuotaExceeded broadcasts a notification when AI quota is reached.
func NotifyQuotaExceeded(filename, folder string, userID string) {
	notifMutex.Lock()
	defer notifMutex.Unlock()

	translator := NewPathTranslator()
	winFolder := translator.ToWindowsPath(userID, folder)

	event := NotificationEvent{
		Type:     "quota_exceeded",
		Title:    "AI Quota Reached",
		Body:     fmt.Sprintf("Daily limit reached. Manual action required for '%s'. Click to open folder.", filename),
		Filename: filename,
		Folder:   winFolder,
		UserID:   userID,
	}

	// Broadcast via WebSocket if available
	if broadcaster != nil {
		broadcaster.Broadcast(map[string]interface{}{
			"type":     event.Type,
			"title":    event.Title,
			"body":     event.Body,
			"filename": event.Filename,
			"folder":   event.Folder,
			"user_id":   event.UserID,
		})
	}

	// Also keep in pending queue for polling fallback
	pendingNotifications = append(pendingNotifications, event)
}

// NotifyTaskCompleted broadcasts a notification when a background task (like batch scan) completes.
func NotifyTaskCompleted(title, body string, userID string) {
	notifMutex.Lock()
	defer notifMutex.Unlock()

	event := NotificationEvent{
		Type:   "task_completed",
		Title:  title,
		Body:   body,
		UserID: userID,
	}

	// Broadcast via WebSocket if available
	if broadcaster != nil {
		broadcaster.Broadcast(map[string]interface{}{
			"type":    event.Type,
			"title":   event.Title,
			"body":    event.Body,
			"user_id": event.UserID,
		})
	}

	// Also keep in pending queue for polling fallback
	pendingNotifications = append(pendingNotifications, event)
}
