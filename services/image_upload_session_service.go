package services

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// ponytail: in-memory, single-process store — sessions are a throwaway QR
// handoff (few minutes, single use), losing them on a backend restart is fine.
const imageUploadSessionTTL = 5 * time.Minute

type imageUploadSession struct {
	imageDataURI string
	createdAt    time.Time
}

type ImageUploadSessionService struct {
	mutex    sync.Mutex
	sessions map[string]*imageUploadSession
}

func NewImageUploadSessionService() *ImageUploadSessionService {
	return &ImageUploadSessionService{sessions: make(map[string]*imageUploadSession)}
}

func (service *ImageUploadSessionService) CreateSession() (string, error) {
	tokenBytes := make([]byte, 16)
	if _, randomReadError := rand.Read(tokenBytes); randomReadError != nil {
		return "", randomReadError
	}
	token := hex.EncodeToString(tokenBytes)

	service.mutex.Lock()
	defer service.mutex.Unlock()
	service.evictExpiredLocked()
	service.sessions[token] = &imageUploadSession{createdAt: time.Now()}
	return token, nil
}

func (service *ImageUploadSessionService) SubmitImage(token string, imageDataURI string) error {
	service.mutex.Lock()
	defer service.mutex.Unlock()

	session, sessionExists := service.sessions[token]
	if !sessionExists || time.Since(session.createdAt) > imageUploadSessionTTL {
		return errors.New("upload session not found or expired")
	}
	session.imageDataURI = imageDataURI
	return nil
}

// GetStatus reports the uploaded image, if any, and whether the token is still a live session.
func (service *ImageUploadSessionService) GetStatus(token string) (imageDataURI string, sessionValid bool) {
	service.mutex.Lock()
	defer service.mutex.Unlock()

	session, sessionExists := service.sessions[token]
	if !sessionExists || time.Since(session.createdAt) > imageUploadSessionTTL {
		return "", false
	}
	return session.imageDataURI, true
}

func (service *ImageUploadSessionService) evictExpiredLocked() {
	for token, session := range service.sessions {
		if time.Since(session.createdAt) > imageUploadSessionTTL {
			delete(service.sessions, token)
		}
	}
}
