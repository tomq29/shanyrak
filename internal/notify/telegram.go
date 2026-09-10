package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const telegramAPI = "https://api.telegram.org"

type Telegram struct {
	Token   string
	ChatID  string
	BaseURL string
	Client  *http.Client
}

func NewTelegram(token, chatID string) *Telegram {
	return &Telegram{
		Token:   token,
		ChatID:  chatID,
		BaseURL: telegramAPI,
		Client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (t *Telegram) Send(ctx context.Context, text string) error {
	payload, err := json.Marshal(map[string]any{
		"chat_id":                  t.ChatID,
		"text":                     text,
		"disable_web_page_preview": true,
	})
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", t.BaseURL, t.Token)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := t.Client.Do(request)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("telegram answered %d: %s", response.StatusCode, bytes.TrimSpace(body))
	}
	return nil
}
