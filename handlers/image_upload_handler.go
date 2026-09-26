package handlers

import (
	"fmt"
	"net"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

type ImageUploadHandler struct {
	sessionService *services.ImageUploadSessionService
	serverPort     string
}

func NewImageUploadHandler(sessionService *services.ImageUploadSessionService, serverPort string) *ImageUploadHandler {
	return &ImageUploadHandler{sessionService: sessionService, serverPort: serverPort}
}

// lanIPAddress returns the IP this machine actually routes outbound traffic
// through, so a phone on the same Wi-Fi can reach the POS machine directly.
// Machines commonly carry extra IPv4-capable interfaces (Docker/Internet
// Sharing bridges, VPN tunnels) that aren't reachable from the LAN; picking
// "the first non-loopback address" can land on one of those depending on
// interface enumeration order. Dialing UDP doesn't send a packet — it just
// asks the OS routing table which local address would be used, which is
// always the real LAN-facing interface.
func lanIPAddress() (string, error) {
	probeConnection, dialError := net.Dial("udp", "8.8.8.8:80")
	if dialError != nil {
		return "", fmt.Errorf("could not determine outbound network route: %w", dialError)
	}
	defer probeConnection.Close()

	localAddress, isUDPAddr := probeConnection.LocalAddr().(*net.UDPAddr)
	if !isUDPAddr {
		return "", fmt.Errorf("unexpected local address type %T", probeConnection.LocalAddr())
	}
	return localAddress.IP.String(), nil
}

// CreateSession is called by the desktop app to start a phone-upload handoff.
func (handler *ImageUploadHandler) CreateSession(context *fiber.Ctx) error {
	token, sessionCreateError := handler.sessionService.CreateSession()
	if sessionCreateError != nil {
		return utils.Error(context, fiber.StatusInternalServerError, "Could not start upload session")
	}

	ipAddress, ipLookupError := lanIPAddress()
	if ipLookupError != nil {
		return utils.Error(context, fiber.StatusInternalServerError, "Could not determine local network address")
	}

	uploadURL := fmt.Sprintf("http://%s:%s/upload/%s", ipAddress, handler.serverPort, token)
	return utils.Success(context, "Upload session created", fiber.Map{
		"token":      token,
		"upload_url": uploadURL,
	})
}

// GetSessionStatus is polled by the desktop app while the QR code is shown.
func (handler *ImageUploadHandler) GetSessionStatus(context *fiber.Ctx) error {
	token := context.Params("token")
	imageDataURI, sessionValid := handler.sessionService.GetStatus(token)
	if !sessionValid {
		return utils.Error(context, fiber.StatusNotFound, "Upload session expired")
	}
	if imageDataURI == "" {
		return utils.Success(context, "Waiting for upload", fiber.Map{"status": "pending"})
	}
	return utils.Success(context, "Image received", fiber.Map{"status": "done", "image": imageDataURI})
}

// SubmitImage receives the photo from the phone's browser.
func (handler *ImageUploadHandler) SubmitImage(context *fiber.Ctx) error {
	token := context.Params("token")

	var body struct {
		Image string `json:"image"`
	}
	if bodyParseError := context.BodyParser(&body); bodyParseError != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid request body")
	}
	if !strings.HasPrefix(body.Image, "data:image/") {
		return utils.Error(context, fiber.StatusBadRequest, "Image data is required")
	}

	if submitError := handler.sessionService.SubmitImage(token, body.Image); submitError != nil {
		return utils.Error(context, fiber.StatusNotFound, submitError.Error())
	}
	return utils.Success(context, "Image uploaded", nil)
}

// ServeUploadPage renders the small camera page the phone opens after scanning the QR code.
func (handler *ImageUploadHandler) ServeUploadPage(context *fiber.Ctx) error {
	token := context.Params("token")
	context.Set("Content-Type", "text/html; charset=utf-8")
	return context.SendString(uploadPageHTML(token))
}

func uploadPageHTML(token string) string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>POS — Product Photo</title>
<style>
  body { font-family: -apple-system, sans-serif; background: #0a0a0a; color: #fafafa; display: flex; flex-direction: column; align-items: center; justify-content: center; min-height: 100vh; margin: 0; padding: 24px; box-sizing: border-box; text-align: center; }
  h1 { font-size: 1.1rem; font-weight: 600; margin-bottom: 24px; }
  #preview { max-width: 100%; max-height: 50vh; border-radius: 12px; display: none; margin-bottom: 16px; }
  label, button { display: block; width: 100%; max-width: 320px; padding: 14px; border-radius: 10px; font-size: 1rem; font-weight: 600; border: none; margin-top: 12px; }
  label { background: #22c55e; color: #052e16; }
  button { background: #fafafa; color: #0a0a0a; }
  button:disabled { opacity: 0.5; }
  #status { margin-top: 16px; color: #a3a3a3; font-size: 0.9rem; }
  input { display: none; }
</style>
</head>
<body>
  <h1>Take a photo for this product</h1>
  <img id="preview" alt="Preview">
  <label for="file">Open Camera</label>
  <input type="file" id="file" accept="image/*" capture="environment">
  <button id="submit" disabled>Upload Photo</button>
  <p id="status"></p>
<script>
  var token = ` + fmt.Sprintf("%q", token) + `;
  var fileInput = document.getElementById('file');
  var preview = document.getElementById('preview');
  var submitButton = document.getElementById('submit');
  var statusText = document.getElementById('status');
  var compressedDataURL = null;

  fileInput.addEventListener('change', function () {
    var file = fileInput.files[0];
    if (!file) return;
    var reader = new FileReader();
    reader.onload = function () {
      var img = new Image();
      img.onload = function () {
        var maxDimension = 1200;
        var scale = Math.min(1, maxDimension / Math.max(img.width, img.height));
        var canvas = document.createElement('canvas');
        canvas.width = img.width * scale;
        canvas.height = img.height * scale;
        canvas.getContext('2d').drawImage(img, 0, 0, canvas.width, canvas.height);
        compressedDataURL = canvas.toDataURL('image/jpeg', 0.82);
        preview.src = compressedDataURL;
        preview.style.display = 'block';
        submitButton.disabled = false;
      };
      img.src = reader.result;
    };
    reader.readAsDataURL(file);
  });

  submitButton.addEventListener('click', function () {
    if (!compressedDataURL) return;
    submitButton.disabled = true;
    statusText.textContent = 'Uploading…';
    fetch('/upload/' + token, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ image: compressedDataURL }),
    })
      .then(function (response) {
        if (!response.ok) throw new Error('Upload failed');
        statusText.textContent = 'Done! You can close this page.';
      })
      .catch(function () {
        statusText.textContent = 'Upload failed. Please try again.';
        submitButton.disabled = false;
      });
  });
</script>
</body>
</html>`
}
