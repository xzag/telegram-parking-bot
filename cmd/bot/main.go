package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"parking/internal/storage"
)

func main() {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatal("BOT_TOKEN is required")
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "parking.db"
	}

	store, err := storage.New(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		_ = store.Close()
	}()

	ctx := context.Background()

	spots := parseSpots(os.Getenv("PARKING_SPOTS"))
	if len(spots) == 0 {
		log.Fatal("PARKING_SPOTS is required")
	}

	if err := store.SyncSpots(ctx, spots); err != nil {
		log.Fatal(err)
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("bot started: @%s", bot.Self.UserName)

	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 30

	updates := bot.GetUpdatesChan(updateConfig)

	for update := range updates {
		if update.Message != nil {
			handleMessage(ctx, bot, store, update.Message)
			continue
		}

		if update.CallbackQuery != nil {
			handleCallback(ctx, bot, store, update.CallbackQuery)
			continue
		}
	}
}

func handleMessage(ctx context.Context, bot *tgbotapi.BotAPI, store *storage.Storage, msg *tgbotapi.Message) {
	switch msg.Command() {
	case "start", "parking":
		renderParking(ctx, bot, store, msg.Chat.ID, 0, msg.From.ID)

	default:
		sendText(bot, msg.Chat.ID, "Жми /parking — там список мест и бронирование")
	}
}

func handleCallback(ctx context.Context, bot *tgbotapi.BotAPI, store *storage.Storage, cb *tgbotapi.CallbackQuery) {
	data := cb.Data

	_, _ = bot.Request(tgbotapi.NewCallback(cb.ID, ""))

	switch {
	case strings.HasPrefix(data, "book:"):
		spot := strings.TrimPrefix(data, "book:")
		user := cb.From

		err := store.BookToday(
			ctx,
			spot,
			user.ID,
			user.UserName,
			userName(user),
		)

		switch {
		case err == nil:
			// молча перерисуем

		case errors.Is(err, storage.ErrBookingClosed):
			_, _ = bot.Request(tgbotapi.NewCallbackWithAlert(cb.ID, "Бронирование открывается в 06:00 по Новосибирску"))

		case errors.Is(err, storage.ErrSpotAlreadyBooked):
			_, _ = bot.Request(tgbotapi.NewCallbackWithAlert(cb.ID, "Место уже занято"))

		case errors.Is(err, storage.ErrUserAlreadyBooked):
			_, _ = bot.Request(tgbotapi.NewCallbackWithAlert(cb.ID, "У тебя уже есть бронь"))

		default:
			log.Println("book:", err)
			_, _ = bot.Request(tgbotapi.NewCallbackWithAlert(cb.ID, "Что-то пошло не так"))
		}

		renderParking(ctx, bot, store, cb.Message.Chat.ID, cb.Message.MessageID, user.ID)

	case strings.HasPrefix(data, "cancel:"):
		user := cb.From

		_, err := store.CancelToday(ctx, user.ID)
		switch {
		case err == nil:
			// молча перерисуем

		case errors.Is(err, storage.ErrBookingNotFound):
			_, _ = bot.Request(tgbotapi.NewCallbackWithAlert(cb.ID, "У тебя нет брони"))

		default:
			log.Println("cancel:", err)
			_, _ = bot.Request(tgbotapi.NewCallbackWithAlert(cb.ID, "Не смог отменить бронь"))
		}

		renderParking(ctx, bot, store, cb.Message.Chat.ID, cb.Message.MessageID, user.ID)

	case data == "noop":
		return

	case data == "refresh":
		renderParking(ctx, bot, store, cb.Message.Chat.ID, cb.Message.MessageID, cb.From.ID)

	case strings.HasPrefix(data, "busy:"):
		_, _ = bot.Request(tgbotapi.NewCallbackWithAlert(cb.ID, "Это место уже занято"))

	default:
		return
	}
}

func buildParkingView(ctx context.Context, store *storage.Storage, telegramID int64) (string, tgbotapi.InlineKeyboardMarkup, error) {
	state, err := store.TodayState(ctx)
	if err != nil {
		return "", tgbotapi.InlineKeyboardMarkup{}, err
	}

	userSpot, hasUserBooking, err := store.GetUserTodayBooking(ctx, telegramID)
	if err != nil {
		return "", tgbotapi.InlineKeyboardMarkup{}, err
	}

	bookingOpen := store.IsBookingOpenNow()

	text := fmt.Sprintf(
		"🚗 <b>Парковка на %s</b>\n\n",
		store.Now().Format("02.01"),
	)
	switch {
	case !bookingOpen:
		text += "Бронирование открывается в 06:00 по Новосибирску."

	case hasUserBooking:
		text += fmt.Sprintf(
			"Твое место: <b>%s</b>\nЧтобы выбрать другое место, сначала отмени текущую бронь.",
			htmlEscape(userSpot),
		)

	default:
		text += "Выбери свободное место:"
	}

	busyLines := make([]string, 0)

	for _, s := range state {
		if s.UserID.Valid {
			busyLines = append(busyLines, fmt.Sprintf(
				"%s — %s",
				htmlEscape(s.SpotNumber),
				htmlUserMention(s.UserID.Int64, s.UserName.String, s.Username),
			))
		}
	}

	if len(busyLines) > 0 {
		text += "\n\n<b>Занято:</b>\n" + strings.Join(busyLines, "\n")
	}

	const buttonsPerRow = 4

	var rows [][]tgbotapi.InlineKeyboardButton
	var currentRow []tgbotapi.InlineKeyboardButton

	flushRow := func() {
		if len(currentRow) == 0 {
			return
		}

		for len(currentRow) < buttonsPerRow {
			currentRow = append(currentRow, tgbotapi.NewInlineKeyboardButtonData(" ", "noop"))
		}

		rows = append(rows, currentRow)
		currentRow = nil
	}

	for _, s := range state {
		var btn tgbotapi.InlineKeyboardButton

		switch {
		case s.UserID.Valid && s.UserID.Int64 == telegramID:
			btn = tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🟡 %s ✕", s.SpotNumber),
				"cancel:"+s.SpotNumber,
			)

		case s.UserID.Valid:
			btn = tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🔴 %s", s.SpotNumber),
				"busy:"+s.SpotNumber,
			)

		case !bookingOpen:
			btn = tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("⚪ %s", s.SpotNumber),
				"noop:"+s.SpotNumber,
			)

		case hasUserBooking:
			btn = tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("⚪ %s", s.SpotNumber),
				"noop:"+s.SpotNumber,
			)

		default:
			btn = tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🟢 %s", s.SpotNumber),
				"book:"+s.SpotNumber,
			)
		}

		currentRow = append(currentRow, btn)

		if len(currentRow) == buttonsPerRow {
			flushRow()
		}
	}

	flushRow()

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔄 Обновить", "refresh"),
	))

	return text, tgbotapi.NewInlineKeyboardMarkup(rows...), nil
}

func renderParking(ctx context.Context, bot *tgbotapi.BotAPI, store *storage.Storage, chatID int64, messageID int, telegramID int64) {
	text, markup, err := buildParkingView(ctx, store, telegramID)
	if err != nil {
		log.Println("parking view:", err)
		sendText(bot, chatID, "Не смог получить список мест")
		return
	}

	if messageID > 0 {
		msg := tgbotapi.NewEditMessageText(chatID, messageID, text)
		msg.ParseMode = "HTML"
		msg.ReplyMarkup = &markup

		_, err := bot.Send(msg)
		if err == nil {
			return
		}

		if isTelegramMessageNotModified(err) {
			return
		}

		log.Println("edit parking:", err)
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = markup

	_, _ = bot.Send(msg)
}

func sendText(bot *tgbotapi.BotAPI, chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	_, _ = bot.Send(msg)
}

func userName(user *tgbotapi.User) string {
	name := strings.TrimSpace(user.FirstName + " " + user.LastName)
	if name != "" {
		return name
	}

	if user.UserName != "" {
		return "@" + user.UserName
	}

	return fmt.Sprintf("%d", user.ID)
}

func htmlUserMention(userID int64, name string, username sql.NullString) string {
	label := name
	if username.Valid && username.String != "" {
		label = fmt.Sprintf("%s (@%s)", name, username.String)
	}

	return fmt.Sprintf(
		`<a href="tg://user?id=%d">%s</a>`,
		userID,
		htmlEscape(label),
	)
}

func htmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	).Replace(s)
}

func isTelegramMessageNotModified(err error) bool {
	if err == nil {
		return false
	}

	return strings.Contains(
		err.Error(),
		"message is not modified",
	)
}

func parseSpots(raw string) []string {
	parts := strings.Split(raw, ",")

	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{})

	for _, part := range parts {
		spot := strings.TrimSpace(part)
		if spot == "" {
			continue
		}

		if _, ok := seen[spot]; ok {
			continue
		}

		seen[spot] = struct{}{}
		result = append(result, spot)
	}

	return result
}
