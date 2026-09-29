package phoneupload

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	sessionLifetime  = 5 * time.Minute
	maximumImageSize = 2 * 1024 * 1024
)

var (
	ErrSessionNotFound = errors.New("this upload link has expired; scan a new code on the computer")
	ErrAlreadyUploaded = errors.New("a photo was already sent with this link")
	ErrNotAnImage      = errors.New("send a JPEG, PNG or WebP photo")
	ErrImageTooLarge   = errors.New("the photo must be 2 MB or smaller")
)

var allowedImageTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

type session struct {
	companyId   uuid.UUID
	createdAt   time.Time
	imageBytes  []byte
	contentType string
}

type Service struct {
	mutex    sync.Mutex
	sessions map[string]*session
	now      func() time.Time
}

func NewService() *Service {
	return &Service{
		sessions: map[string]*session{},
		now:      time.Now,
	}
}

func (service *Service) Create(companyId uuid.UUID) (string, time.Time, error) {
	tokenBytes := make([]byte, 16)
	_, randomError := rand.Read(tokenBytes)
	if randomError != nil {
		return "", time.Time{}, randomError
	}
	token := hex.EncodeToString(tokenBytes)

	service.mutex.Lock()
	defer service.mutex.Unlock()
	service.evictExpiredLocked()
	createdAt := service.now()
	service.sessions[token] = &session{companyId: companyId, createdAt: createdAt}
	return token, createdAt.Add(sessionLifetime).UTC(), nil
}

func (service *Service) Submit(token string, imageDataUri string) error {
	imageBytes, contentType, decodeError := decodeImage(imageDataUri)
	if decodeError != nil {
		return decodeError
	}

	service.mutex.Lock()
	defer service.mutex.Unlock()
	foundSession := service.liveSessionLocked(token)
	if foundSession == nil {
		return ErrSessionNotFound
	}
	if foundSession.imageBytes != nil {
		return ErrAlreadyUploaded
	}
	foundSession.imageBytes = imageBytes
	foundSession.contentType = contentType
	return nil
}

func (service *Service) Collect(token string, companyId uuid.UUID) ([]byte, string, bool, error) {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	foundSession := service.liveSessionLocked(token)
	if foundSession == nil || foundSession.companyId != companyId {
		return nil, "", false, ErrSessionNotFound
	}
	if foundSession.imageBytes == nil {
		return nil, "", false, nil
	}
	delete(service.sessions, token)
	return foundSession.imageBytes, foundSession.contentType, true, nil
}

func (service *Service) IsLive(token string) bool {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	foundSession := service.liveSessionLocked(token)
	return foundSession != nil && foundSession.imageBytes == nil
}

func (service *Service) liveSessionLocked(token string) *session {
	foundSession, isKnown := service.sessions[token]
	if !isKnown || service.now().Sub(foundSession.createdAt) > sessionLifetime {
		return nil
	}
	return foundSession
}

func (service *Service) evictExpiredLocked() {
	for token, foundSession := range service.sessions {
		if service.now().Sub(foundSession.createdAt) > sessionLifetime {
			delete(service.sessions, token)
		}
	}
}

func decodeImage(imageDataUri string) ([]byte, string, error) {
	header, encodedImage, hasComma := strings.Cut(imageDataUri, ",")
	isBase64Image := hasComma && strings.HasPrefix(header, "data:image/") && strings.HasSuffix(header, ";base64")
	if !isBase64Image {
		return nil, "", ErrNotAnImage
	}
	if base64.StdEncoding.DecodedLen(len(encodedImage)) > maximumImageSize+3 {
		return nil, "", ErrImageTooLarge
	}
	imageBytes, decodeError := base64.StdEncoding.DecodeString(encodedImage)
	if decodeError != nil {
		return nil, "", ErrNotAnImage
	}
	if len(imageBytes) > maximumImageSize {
		return nil, "", ErrImageTooLarge
	}
	sniffedType := http.DetectContentType(imageBytes)
	if !allowedImageTypes[sniffedType] {
		return nil, "", ErrNotAnImage
	}
	return imageBytes, sniffedType, nil
}
