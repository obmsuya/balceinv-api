package phoneupload

import (
	"encoding/base64"
	"errors"
	"regexp"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/lan"
	"github.com/gofiber/fiber/v2"
)

const unreachableReason = "Turn on \"Let other devices use Balce\" in Settings → Network so your phone can reach this computer."

var tokenPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type Handler struct {
	service *Service
	network *lan.Controller
	isCloud bool
}

func NewHandler(service *Service, network *lan.Controller, isCloud bool) *Handler {
	return &Handler{
		service: service,
		network: network,
		isCloud: isCloud,
	}
}

type submitRequest struct {
	Image string `json:"image" validate:"required"`
}

func (handler *Handler) Create(c *fiber.Ctx) error {
	token, expiresAt, createError := handler.service.Create(httpx.CurrentPrincipal(c).CompanyId)
	if createError != nil {
		return createError
	}

	uploadUrls := []string{}
	reason := ""
	switch {
	case handler.isCloud:
		uploadUrls = append(uploadUrls, c.BaseURL()+"/upload/"+token)
	case handler.network != nil:
		for _, networkUrl := range handler.network.Status().LanUrls {
			uploadUrls = append(uploadUrls, networkUrl+"/upload/"+token)
		}
	}
	if len(uploadUrls) == 0 {
		reason = unreachableReason
	}

	return response.Created(c, "Upload link ready", fiber.Map{
		"token":       token,
		"upload_urls": uploadUrls,
		"reachable":   len(uploadUrls) > 0,
		"reason":      reason,
		"expires_at":  expiresAt,
	})
}

func (handler *Handler) Collect(c *fiber.Ctx) error {
	token := c.Params("token")
	if !tokenPattern.MatchString(token) {
		return response.Error(c, fiber.StatusNotFound, "not_found", ErrSessionNotFound.Error())
	}
	imageBytes, contentType, isDone, collectError := handler.service.Collect(token, httpx.CurrentPrincipal(c).CompanyId)
	if collectError != nil {
		return response.Error(c, fiber.StatusNotFound, "expired", collectError.Error())
	}
	if !isDone {
		return response.Success(c, "Waiting for the phone", fiber.Map{"status": "pending"})
	}
	return response.Success(c, "Photo received", fiber.Map{
		"status":       "done",
		"content_type": contentType,
		"image":        "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(imageBytes),
	})
}

func (handler *Handler) Page(c *fiber.Ctx) error {
	token := c.Params("token")
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	c.Set(fiber.HeaderCacheControl, "no-store")
	isLive := tokenPattern.MatchString(token) && handler.service.IsLive(token)
	if !isLive {
		c.Status(fiber.StatusNotFound)
		return c.SendString(expiredPageHtml)
	}
	return c.SendString(uploadPageHtml)
}

func (handler *Handler) Submit(c *fiber.Ctx) error {
	token := c.Params("token")
	if !tokenPattern.MatchString(token) {
		return response.Error(c, fiber.StatusNotFound, "not_found", ErrSessionNotFound.Error())
	}
	request := submitRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	submitError := handler.service.Submit(token, request.Image)
	switch {
	case errors.Is(submitError, ErrSessionNotFound):
		return response.Error(c, fiber.StatusNotFound, "expired", submitError.Error())
	case errors.Is(submitError, ErrAlreadyUploaded):
		return response.Error(c, fiber.StatusConflict, "already_uploaded", submitError.Error())
	case errors.Is(submitError, ErrNotAnImage), errors.Is(submitError, ErrImageTooLarge):
		return response.Error(c, fiber.StatusBadRequest, "invalid_image", submitError.Error())
	case submitError != nil:
		return submitError
	}
	return response.Success(c, "Photo sent", nil)
}

const expiredPageHtml = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Balce photo</title>
<style>body{font-family:system-ui,sans-serif;background:#0a0a0a;color:#fafafa;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;padding:24px;text-align:center}</style>
</head><body><div><h1 style="font-size:1.2rem">This link has expired</h1><p style="color:#a3a3a3">Scan a new code on the computer.<br>Kiungo hiki kimekwisha muda. Changanua msimbo mpya kwenye kompyuta.</p></div></body></html>`

const uploadPageHtml = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Balce photo</title>
<style>
  body { font-family: system-ui, sans-serif; background: #0a0a0a; color: #fafafa; display: flex; flex-direction: column; align-items: center; justify-content: center; min-height: 100vh; margin: 0; padding: 24px; box-sizing: border-box; text-align: center; }
  h1 { font-size: 1.15rem; margin: 0 0 4px; }
  .hint { color: #a3a3a3; font-size: 0.9rem; margin: 0 0 20px; }
  #preview { max-width: 100%; max-height: 45vh; border-radius: 12px; display: none; margin-bottom: 16px; }
  label, button { display: block; width: 100%; max-width: 320px; padding: 14px; border-radius: 10px; font-size: 1rem; font-weight: 600; border: none; margin-top: 12px; box-sizing: border-box; }
  label { background: #22c55e; color: #052e16; }
  button { background: #fafafa; color: #0a0a0a; }
  button:disabled { opacity: 0.4; }
  #status { margin-top: 16px; color: #a3a3a3; min-height: 1.4em; }
  input { display: none; }
</style>
</head>
<body>
  <h1>Take a photo for this product</h1>
  <p class="hint">Piga picha ya bidhaa hii</p>
  <img id="preview" alt="">
  <label for="file">Open camera · Fungua kamera</label>
  <input type="file" id="file" accept="image/*" capture="environment">
  <button id="send" disabled>Send photo · Tuma picha</button>
  <p id="status"></p>
<script>
  var fileInput = document.getElementById('file');
  var preview = document.getElementById('preview');
  var sendButton = document.getElementById('send');
  var statusText = document.getElementById('status');
  var photo = null;
  fileInput.addEventListener('change', function () {
    var file = fileInput.files[0];
    if (!file) return;
    var reader = new FileReader();
    reader.onload = function () {
      var image = new Image();
      image.onload = function () {
        var scale = Math.min(1, 1200 / Math.max(image.width, image.height));
        var canvas = document.createElement('canvas');
        canvas.width = Math.round(image.width * scale);
        canvas.height = Math.round(image.height * scale);
        canvas.getContext('2d').drawImage(image, 0, 0, canvas.width, canvas.height);
        photo = canvas.toDataURL('image/jpeg', 0.82);
        preview.src = photo;
        preview.style.display = 'block';
        sendButton.disabled = false;
        statusText.textContent = '';
      };
      image.src = reader.result;
    };
    reader.readAsDataURL(file);
  });
  sendButton.addEventListener('click', function () {
    if (!photo) return;
    sendButton.disabled = true;
    statusText.textContent = 'Sending… · Inatuma…';
    fetch(window.location.pathname, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ image: photo }) })
      .then(function (answer) { return answer.json().then(function (body) { return { ok: answer.ok, body: body }; }); })
      .then(function (result) {
        if (!result.ok) throw new Error(result.body.message || 'Upload failed');
        statusText.textContent = 'Done! Check the computer. · Imekamilika! Angalia kompyuta.';
      })
      .catch(function (problem) {
        statusText.textContent = problem.message;
        sendButton.disabled = false;
      });
  });
</script>
</body>
</html>`
