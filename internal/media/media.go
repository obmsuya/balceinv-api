package media

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const (
	FolderLogos     = "logos"
	FolderProducts  = "products"
	FolderPurchases = "purchases"
)

var (
	ErrEmptyImage       = errors.New("the image file is empty")
	ErrImageTooLarge    = errors.New("the image is too large")
	ErrUnsupportedImage = errors.New("the image must be a PNG, JPEG or WebP file")
)

var publicFolders = map[string]bool{
	FolderLogos:     true,
	FolderProducts:  true,
	FolderPurchases: true,
}

var mediaFileNamePattern = regexp.MustCompile(`^[0-9a-f-]{36}\.(png|jpg|webp)$`)

var extensionByContentType = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/webp": "webp",
}

func PublicUrl(objectKey *string) *string {
	if objectKey == nil {
		return nil
	}
	publicPath := "/api/media/" + *objectKey
	return &publicPath
}

func StoreImage(ctx context.Context, objectStore storage.Store, folder string, companyId uuid.UUID, imageBytes []byte, maximumBytes int) (string, error) {
	isEmpty := len(imageBytes) == 0
	if isEmpty {
		return "", ErrEmptyImage
	}
	isTooLarge := len(imageBytes) > maximumBytes
	if isTooLarge {
		return "", ErrImageTooLarge
	}

	detectedContentType := http.DetectContentType(imageBytes)
	imageExtension, isSupported := extensionByContentType[detectedContentType]
	if !isSupported {
		return "", ErrUnsupportedImage
	}

	objectKey := fmt.Sprintf("%s/%s/%s.%s", folder, companyId, uuid.Must(uuid.NewV7()), imageExtension)
	putError := objectStore.Put(ctx, objectKey, detectedContentType, imageBytes)
	if putError != nil {
		return "", fmt.Errorf("failed to store image: %w", putError)
	}

	return objectKey, nil
}

type Handler struct {
	objectStore storage.Store
}

func NewHandler(objectStore storage.Store) *Handler {
	return &Handler{
		objectStore: objectStore,
	}
}

func (handler *Handler) Serve(c *fiber.Ctx) error {
	folder := c.Params("folder")
	companyId, parseError := uuid.Parse(c.Params("companyId"))
	fileName := c.Params("fileName")

	isPublicFolder := publicFolders[folder]
	isValidFileName := mediaFileNamePattern.MatchString(fileName)
	if !isPublicFolder || parseError != nil || !isValidFileName {
		return response.Error(c, fiber.StatusNotFound, "not_found", "File not found")
	}

	storedObject, getError := handler.objectStore.Get(c.UserContext(), folder+"/"+companyId.String()+"/"+fileName)
	if errors.Is(getError, storage.ErrObjectNotFound) {
		return response.Error(c, fiber.StatusNotFound, "not_found", "File not found")
	}
	if getError != nil {
		return getError
	}

	c.Set(fiber.HeaderContentType, storedObject.ContentType)
	c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
	c.Set("Cross-Origin-Resource-Policy", "cross-origin")
	return c.Send(storedObject.Body)
}
