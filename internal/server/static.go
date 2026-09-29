package server

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/gofiber/fiber/v2"
)

func staticApp(staticDirectory string) fiber.Handler {
	indexPath := filepath.Join(staticDirectory, "index.html")

	return func(c *fiber.Ctx) error {
		requestPath := c.Path()
		isApiPath := requestPath == "/api" || strings.HasPrefix(requestPath, "/api/")
		if isApiPath {
			return response.Error(c, fiber.StatusNotFound, "not_found", "No such API route")
		}

		cleanedPath := path.Clean("/" + requestPath)
		relativePath := filepath.FromSlash(strings.TrimPrefix(cleanedPath, "/"))
		hasBackslash := strings.Contains(requestPath, `\`)
		isInsideStaticDirectory := relativePath == "" || filepath.IsLocal(relativePath)
		if hasBackslash || !isInsideStaticDirectory {
			return response.Error(c, fiber.StatusNotFound, "not_found", "File not found")
		}
		candidatePath := filepath.Join(staticDirectory, relativePath)
		candidateInfo, statError := os.Stat(candidatePath)
		if statError == nil && candidateInfo.IsDir() {
			candidatePath = filepath.Join(candidatePath, "index.html")
			candidateInfo, statError = os.Stat(candidatePath)
		}
		if statError == nil && !candidateInfo.IsDir() {
			if strings.HasPrefix(cleanedPath, "/_nuxt/") {
				c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
			}
			return c.SendFile(candidatePath)
		}

		looksLikeAsset := path.Ext(cleanedPath) != ""
		if looksLikeAsset {
			return response.Error(c, fiber.StatusNotFound, "not_found", "File not found")
		}
		c.Set(fiber.HeaderCacheControl, "no-cache")
		return c.SendFile(indexPath)
	}
}
