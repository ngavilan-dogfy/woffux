package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ngavilan-dogfy/woffuk-cli/internal/woffu"
)

type TelegramConfig struct {
	BotToken string
	ChatID   string
}

func (c TelegramConfig) IsConfigured() bool {
	return c.BotToken != "" && c.ChatID != ""
}

type telegramMessage struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

// SendSignedNotification sends a Telegram message confirming a successful sign.
func SendSignedNotification(cfg TelegramConfig, info *woffu.SignInfo) error {
	if !cfg.IsConfigured() {
		return nil
	}

	text := fmt.Sprintf("✅ Fichaje realizado correctamente\n📅 %s\n%s %s",
		info.Date,
		info.Mode.Emoji(),
		info.Mode.Label(),
	)

	return sendTelegram(cfg, text)
}

// SendSkippedNotification sends a Telegram message when signing is skipped.
func SendSkippedNotification(cfg TelegramConfig, date, reason string) error {
	if !cfg.IsConfigured() {
		return nil
	}

	text := fmt.Sprintf("⏭️ Fichaje omitido\n📅 %s\n📝 %s", date, reason)

	return sendTelegram(cfg, text)
}

func sendTelegram(cfg TelegramConfig, text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken)

	body, err := json.Marshal(telegramMessage{
		ChatID: cfg.ChatID,
		Text:   text,
	})
	if err != nil {
		return err
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram returned status %d", resp.StatusCode)
	}

	return nil
}
