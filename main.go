package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

// escapeMarkdownV2 экранирует спецсимволы для MarkdownV2
func escapeMarkdownV2(text string) string {
	replacer := strings.NewReplacer(
		`_`, `\_`, `*`, `\*`, `[`, `\[`, `]`, `\]`,
		`(`, `\(`, `)`, `\)`, `~`, `\~`, `>`, `\>`,
		`#`, `\#`, `+`, `\+`, `-`, `\-`, `=`, `\=`,
		`|`, `\|`, `{`, `\{`, `}`, `\}`, `.`, `\.`, `!`, `\!`,
	)
	return replacer.Replace(text)
}

// downloadFile скачивает файл по URL и сохраняет его по указанному пути
func downloadFile(url, filepath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// sendFileToDiscord отправка файла в Discord
func sendFileToDiscord(dg *discordgo.Session, channelID string, filePath string, caption string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("Failed to open file: %v", err)
	}
	defer file.Close()

	_, err = dg.ChannelMessageSend(channelID, caption)
	if err != nil {
		return fmt.Errorf("Failed to send message to Discord: %v", err)
	}

	_, err = dg.ChannelFileSend(channelID, filePath, file)
	if err != nil {
		return fmt.Errorf("Failed to send file to Discord: %v", err)
	}

	return nil
}

func main() {
	// Загрузка переменных окружения из файла .env
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}

	discordToken := os.Getenv("DISCORD_TOKEN")
	telegramToken := os.Getenv("TELEGRAM_TOKEN")
	telegramChatID := os.Getenv("TELEGRAM_CHAT_ID")
	discordChannelID := os.Getenv("DISCORD_CHANNEL_ID")

	// Проверка обязательных переменных
	if discordToken == "" || telegramToken == "" || telegramChatID == "" || discordChannelID == "" {
		log.Fatal("Missing required environment variables")
	}

	chatID, err := parseChatID(telegramChatID)
	if err != nil {
		log.Fatalf("Invalid Telegram Chat ID: %v", err)
	}

	// Инициализация Telegram бота
	tgBot, err := tgbotapi.NewBotAPI(telegramToken)
	if err != nil {
		log.Fatalf("Failed to initialize Telegram bot: %v", err)
	}
	tgBot.Debug = true
	log.Printf("Authorized on Telegram account %s", tgBot.Self.UserName)

	// Инициализация Discord бота
	dg, err := discordgo.New("Bot " + discordToken)
	if err != nil {
		log.Fatalf("Failed to initialize Discord bot: %v", err)
	}
	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentMessageContent

	// Обработчик сообщений Discord
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		log.Println("Discord message handler triggered.")
		if m.Author.ID == s.State.User.ID {
			return
		}
		if m.ChannelID != discordChannelID {
			return
		}

		if m.Content != "" {
			escapedContent := escapeMarkdownV2(m.Content)
			escapedUsername := escapeMarkdownV2(m.Author.Username)
			telegramMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Из ДС:\n*%s*: %s", escapedUsername, escapedContent))
			telegramMsg.ParseMode = "MarkdownV2"
			if _, err := tgBot.Send(telegramMsg); err != nil {
				log.Printf("Failed to send message to Telegram: %v", err)
			}
		}

		if len(m.Attachments) > 0 {
			for _, attachment := range m.Attachments {
				if strings.HasPrefix(attachment.ContentType, "image/") {
					photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(attachment.URL))
					photo.Caption = fmt.Sprintf("ДС:\n %s", m.Author.Username)
					if _, err := tgBot.Send(photo); err != nil {
						log.Printf("Failed to send image to Telegram: %v", err)
					}
				}
			}
		}
	})

	// Запуск Discord бота
	if err := dg.Open(); err != nil {
		log.Fatalf("Failed to open Discord session: %v", err)
	}
	defer dg.Close()
	log.Println("Discord bot is running.")

	// Обработчик сообщений Telegram
	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60
	updates := tgBot.GetUpdatesChan(updateConfig)

	for update := range updates {
		if update.Message == nil || update.Message.Chat.ID != chatID {
			continue
		}

		// 1. Отправка текста в Discord (если есть)
		if update.Message.Text != "" {
			telegramMsg := fmt.Sprintf("ТГ:\n**%s**: %s", update.Message.From.UserName, update.Message.Text)
			_, err := dg.ChannelMessageSend(discordChannelID, telegramMsg)
			if err != nil {
				log.Printf("Failed to send text message to Discord: %v", err)
			}
		}

		// 2. Обработка фото (если есть)
		if len(update.Message.Photo) > 0 {
			photoFileID := update.Message.Photo[len(update.Message.Photo)-1].FileID
			fileURL, err := tgBot.GetFileDirectURL(photoFileID)
			if err != nil {
				log.Printf("Failed to get photo URL: %v", err)
				continue
			}

			photoPath := fmt.Sprintf("content/photo_%d.jpg", time.Now().UnixNano())

			// Скачиваем фото
			err = downloadFile(fileURL, photoPath)
			if err != nil {
				log.Printf("Failed to download photo: %v", err)
				continue
			}

			// Отправка фото в Discord
			err = sendFileToDiscord(dg, discordChannelID, photoPath, fmt.Sprintf("ТГ Фото от %s:", update.Message.From.UserName))
			if err != nil {
				log.Printf("Failed to send photo to Discord: %v", err)
			}

			// Удаление фото после отправки
			err = os.Remove(photoPath)
			if err != nil {
				log.Printf("Failed to remove photo file: %v", err)
			}
		}

		// 3. Обработка видеосообщений (если есть)
		if update.Message.VideoNote != nil {
			// Получаем ID видеосообщения
			videoFileID := update.Message.VideoNote.FileID
			fileURL, err := tgBot.GetFileDirectURL(videoFileID)
			if err != nil {
				log.Printf("Failed to get video URL: %v", err)
				continue
			}

			// Создаем уникальное имя для видео
			videoPath := fmt.Sprintf("content/video_%d.mp4", time.Now().UnixNano())

			// Скачиваем видео
			err = downloadFile(fileURL, videoPath)
			if err != nil {
				log.Printf("Failed to download video: %v", err)
				continue
			}

			// Отправка видео в Discord
			err = sendFileToDiscord(dg, discordChannelID, videoPath, fmt.Sprintf("ТГ Видео от %s:", update.Message.From.UserName))
			if err != nil {
				log.Printf("Failed to send video to Discord: %v", err)
			}

			// Удаление видео после отправки
			err = os.Remove(videoPath)
			if err != nil {
				log.Printf("Failed to remove video file: %v", err)
			}
		}

		// 4. Обработка голосовых сообщений (если есть)
		if update.Message.Voice != nil {
			voiceFileID := update.Message.Voice.FileID
			fileURL, err := tgBot.GetFileDirectURL(voiceFileID)
			if err != nil {
				log.Printf("Failed to get voice message URL: %v", err)
				continue
			}

			// Создаем уникальное имя для голосового сообщения
			voicePath := fmt.Sprintf("content/voice_%d.ogg", time.Now().UnixNano())

			// Скачиваем голосовое сообщение
			err = downloadFile(fileURL, voicePath)
			if err != nil {
				log.Printf("Failed to download voice message: %v", err)
				continue
			}

			// Отправка голосового сообщения в Discord
			err = sendFileToDiscord(dg, discordChannelID, voicePath, fmt.Sprintf("ТГ Голос от %s:", update.Message.From.UserName))
			if err != nil {
				log.Printf("Failed to send voice to Discord: %v", err)
			}

			// Удаление голосового сообщения после отправки
			err = os.Remove(voicePath)
			if err != nil {
				log.Printf("Failed to remove voice file: %v", err)
			}
		}
	}
}

// parseChatID преобразует строковый Telegram Chat ID в int64
func parseChatID(chatID string) (int64, error) {
	var parsedChatID int64
	_, err := fmt.Sscanf(chatID, "%d", &parsedChatID)
	return parsedChatID, err
}
