package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	tgbot "github.com/go-telegram/bot"

	"github.com/Hristofor1234/AntiSpamBotTG/internal/config"
)

// shutdownTimeout — сколько ждём завершения in-flight HTTP-запросов при
// остановке webhook-сервера, прежде чем считать shutdown неудавшимся.
const shutdownTimeout = 10 * time.Second

// runWebhook регистрирует webhook в Telegram (setWebhook) и поднимает
// HTTP-сервер, который принимает обновления и передаёт их в b (через
// b.WebhookHandler → внутренний updates-канал → b.StartWebhook). Блокируется
// до отмены ctx, затем аккуратно останавливает сервер и снимает webhook.
//
// cfg.WebhookURL должен быть публичным https-адресом (обычно за reverse
// proxy с TLS — nginx/Caddy), который проксирует на cfg.WebhookListenAddr.
// Сам этот HTTP-сервер слушает обычный (не TLS) адрес внутри контейнера/сети.
func runWebhook(ctx context.Context, b *tgbot.Bot, cfg *config.Config, logger *slog.Logger) error {
	if _, err := b.SetWebhook(ctx, &tgbot.SetWebhookParams{
		URL:                cfg.WebhookURL,
		SecretToken:        cfg.WebhookSecretToken,
		DropPendingUpdates: true,
	}); err != nil {
		return fmt.Errorf("не удалось зарегистрировать webhook в Telegram: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc(cfg.WebhookPath, b.WebhookHandler())

	srv := &http.Server{
		Addr:              cfg.WebhookListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("webhook-сервер запущен",
			"listen_addr", cfg.WebhookListenAddr,
			"path", cfg.WebhookPath,
			"public_url", cfg.WebhookURL)

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("http-сервер webhook упал: %w", err)
			return
		}
		serveErr <- nil
	}()

	// StartWebhook запускает воркеров, разбирающих канал обновлений,
	// наполняемый b.WebhookHandler(); блокируется до отмены ctx.
	go b.StartWebhook(ctx)

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("ошибка при остановке webhook-сервера", "error", err)
	}

	if _, err := b.DeleteWebhook(shutdownCtx, &tgbot.DeleteWebhookParams{}); err != nil {
		logger.Warn("не удалось снять webhook при остановке (не критично)", "error", err)
	}

	// Дожидаемся, пока ListenAndServe действительно вернёт управление.
	if err := <-serveErr; err != nil {
		return err
	}
	return nil
}
