package services

import (
	"bytes"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

type WatcherService struct {
	Watcher *fsnotify.Watcher
}

func NewWatcherService() (*WatcherService, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &WatcherService{Watcher: watcher}, nil
}

func (ws *WatcherService) StartWatcher(dir string, apiURL string) {
	defer ws.Watcher.Close()

	// Ensure directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			log.Printf("Error creating directory %s: %v", dir, err)
			return
		}
	}

	err := ws.Watcher.Add(dir)
	if err != nil {
		log.Printf("Error adding directory to watcher: %v", err)
		return
	}

	log.Printf("Watching directory: %s", dir)

	for {
		select {
		case event, ok := <-ws.Watcher.Events:
			if !ok {
				return
			}
			// We only care about CREATE events
			if event.Has(fsnotify.Create) {
				log.Printf("New file detected: %s", event.Name)
				
				// CRITICAL: File writes are not instantaneous. 
				// We sleep briefly to ensure the OS releases the file lock 
				// and data is actually written before we try to read it.
				time.Sleep(500 * time.Millisecond)

				go ws.transferFile(event.Name, apiURL)
			}
		case err, ok := <-ws.Watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

func (ws *WatcherService) transferFile(filePath string, apiURL string) {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("Error opening file: %v", err)
		return
	}
	defer file.Close()

	// Prepare multipart form data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	
	// "file" is the form key the external API expects
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		log.Printf("Error creating form file: %v", err)
		return
	}
	
	_, err = io.Copy(part, file)
	if err != nil {
		log.Printf("Error copying file content: %v", err)
		return
	}

	err = writer.Close()
	if err != nil {
		log.Printf("Error closing writer: %v", err)
		return
	}

	// Send the request
	req, err := http.NewRequest("POST", apiURL, body)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{
		Timeout: 30 * time.Second, // Good practice to add a timeout
	}
	
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error calling external API: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 || resp.StatusCode == 201 {
		log.Printf("SUCCESS: Transferred %s to %s", filePath, apiURL)
		
		// OPTIONAL: If you want to delete the file from 'downloads' 
		// after a successful transfer, uncomment the line below:
		// os.Remove(filePath)
	} else {
		log.Printf("FAILED: External API returned status %s for %s", resp.Status, filePath)
	}
}