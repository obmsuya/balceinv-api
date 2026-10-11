package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/license"
)

var (
	ErrWapangajiNotConnected   = errors.New("sign in to Wapangaji first")
	ErrWapangajiUnreachable    = errors.New("Wapangaji could not be reached; try again")
	ErrWapangajiUpdateRequired = errors.New("Wapangaji does not support this yet")
)

type WapangajiError struct {
	StatusCode int
	Message    string
}

func (wapangajiError *WapangajiError) Error() string {
	return wapangajiError.Message
}

type wapangajiTokens struct {
	Access  string
	Refresh string
	Name    string
}

type wapangajiClient struct {
	httpClient *http.Client
}

func newWapangajiClient() *wapangajiClient {
	return &wapangajiClient{httpClient: &http.Client{Timeout: 20 * time.Second}}
}

func wapangajiApiRoot() string {
	return strings.TrimSuffix(license.DjangoBaseURL, "/payments")
}

func (client *wapangajiClient) signIn(ctx context.Context, phone string, password string) (wapangajiTokens, error) {
	requestBody := map[string]string{"phone_number": phone, "password": password}
	loginReply := struct {
		User struct {
			FullName string `json:"full_name"`
		} `json:"user"`
		Tokens struct {
			Access  string `json:"access"`
			Refresh string `json:"refresh"`
		} `json:"tokens"`
	}{}
	callError := client.call(ctx, http.MethodPost, "/auth/login/", "", requestBody, &loginReply)
	if callError != nil {
		return wapangajiTokens{}, callError
	}
	return wapangajiTokens{Access: loginReply.Tokens.Access, Refresh: loginReply.Tokens.Refresh, Name: loginReply.User.FullName}, nil
}

func (client *wapangajiClient) refresh(ctx context.Context, refreshToken string) (string, string, error) {
	refreshReply := struct {
		Access  string `json:"access"`
		Refresh string `json:"refresh"`
	}{}
	callError := client.call(ctx, http.MethodPost, "/auth/token/refresh/", "", map[string]string{"refresh": refreshToken}, &refreshReply)
	if callError != nil {
		return "", "", callError
	}
	nextRefresh := refreshReply.Refresh
	if nextRefresh == "" {
		nextRefresh = refreshToken
	}
	return refreshReply.Access, nextRefresh, nil
}

func (client *wapangajiClient) call(ctx context.Context, method string, path string, accessToken string, requestBody any, replyInto any) error {
	var bodyReader io.Reader
	if requestBody != nil {
		encodedBody, encodeError := json.Marshal(requestBody)
		if encodeError != nil {
			return fmt.Errorf("failed to encode the Wapangaji request: %w", encodeError)
		}
		bodyReader = bytes.NewReader(encodedBody)
	}

	outgoingRequest, requestError := http.NewRequestWithContext(ctx, method, wapangajiApiRoot()+path, bodyReader)
	if requestError != nil {
		return fmt.Errorf("failed to build the Wapangaji request: %w", requestError)
	}
	outgoingRequest.Header.Set("Content-Type", "application/json")
	outgoingRequest.Header.Set("Accept", "application/json")
	if accessToken != "" {
		outgoingRequest.Header.Set("Authorization", "Bearer "+accessToken)
	}

	reply, sendError := client.httpClient.Do(outgoingRequest)
	if sendError != nil {
		return ErrWapangajiUnreachable
	}
	defer reply.Body.Close()

	replyBytes, readError := io.ReadAll(io.LimitReader(reply.Body, 1<<20))
	if readError != nil {
		return ErrWapangajiUnreachable
	}

	isSuccess := reply.StatusCode >= 200 && reply.StatusCode < 300
	if !isSuccess {
		errorReply := struct {
			Error  string `json:"error"`
			Detail string `json:"detail"`
		}{}
		json.Unmarshal(replyBytes, &errorReply)
		message := errorReply.Error
		if message == "" {
			message = errorReply.Detail
		}
		if message == "" {
			message = fmt.Sprintf("Wapangaji answered %d", reply.StatusCode)
		}
		return &WapangajiError{StatusCode: reply.StatusCode, Message: message}
	}

	if replyInto == nil || len(replyBytes) == 0 {
		return nil
	}
	decodeError := json.Unmarshal(replyBytes, replyInto)
	if decodeError != nil {
		return fmt.Errorf("Wapangaji sent an unreadable reply: %w", decodeError)
	}
	return nil
}

func licenceQuery(hardwareId string, phone string) string {
	if hardwareId != "" {
		return "?hardware_id=" + url.QueryEscape(hardwareId)
	}
	return "?phone=" + url.QueryEscape(phone)
}
