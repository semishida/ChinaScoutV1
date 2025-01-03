package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

// escapeMarkdownV2 экранирует спецсимволы для MarkdownV2
func escapeMarkdownV2(text string) string {
	replacer := strings.NewReplacer(
		`_`, `\_`,
		`*`, `\*`,
		`[`, `\[`,
		`]`, `\]`,
		`(`, `\(`,
		`)`, `\)`,
		`~`, `\~`,
		`>`, `\>`,
		`#`, `\#`,
		`+`, `\+`,
		`-`, `\-`,
		`=`, `\=`,
		`|`, `\|`,
		`{`, `\{`,
		`}`, `\}`,
		`.`, `\.`,
		`!`, `\!`,
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

	// Отправка сообщения с подписью
	if caption != "" {
		_, err = dg.ChannelMessageSend(channelID, caption)
		if err != nil {
			return fmt.Errorf("Failed to send message to Discord: %v", err)
		}
	}

	// Отправка файла
	_, err = dg.ChannelFileSend(channelID, filePath, file)
	if err != nil {
		return fmt.Errorf("Failed to send file to Discord: %v", err)
	}

	return nil
}

// parseChatID преобразует строковый Telegram Chat ID в int64
func parseChatID(chatID string) (int64, error) {
	var parsedChatID int64
	_, err := fmt.Sscanf(chatID, "%d", &parsedChatID)
	return parsedChatID, err
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
	adminFilePath := os.Getenv("ADMIN_FILE_PATH")

	// Инициализация рейтинга
	ranking, err := NewRanking(adminFilePath)
	if err != nil {
		log.Fatalf("Failed to initialize ranking: %v", err)
	}

	// Загрузка рейтингов из файла
	err = ranking.LoadFromFile("users.json")
	if err != nil {
		log.Printf("Failed to load users from file: %v", err)
	}

	// Запуск периодического сохранения в отдельной горутине
	go ranking.PeriodicSave("users.json")

	// Обработка сигналов завершения для корректного сохранения данных
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		log.Printf("Received signal %s. Saving users to file before shutdown...", sig)
		err := ranking.SaveToFile("users.json")
		if err != nil {
			log.Printf("Failed to save users to file on shutdown: %v", err)
		}
		os.Exit(0)
	}()

	// Проверка обязательных переменных
	if discordToken == "" || telegramToken == "" || telegramChatID == "" || discordChannelID == "" || adminFilePath == "" {
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
	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentMessageContent | discordgo.IntentsGuildVoiceStates

	// Отслеживание активности в голосовых каналах
	ranking.TrackVoiceActivity(dg)

	// Обработчик сообщений Discord
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		log.Println("Discord message handler triggered.")
		if m.Author.ID == s.State.User.ID {
			return
		}
		if m.ChannelID != discordChannelID {
			return
		}

		// Логирование полученного сообщения
		log.Printf("Received message: %s from %s", m.Content, m.Author.Username)

		// Обработка команд
		if strings.HasPrefix(m.Content, "!") {
			if strings.HasPrefix(m.Content, "!china") {
				ranking.HandleChinaCommand(s, m, m.Content)
				return
			}

			if m.Content == "!top5" {
				topUsers := ranking.GetTop5()
				if len(topUsers) == 0 {
					s.ChannelMessageSend(m.ChannelID, "Демография владельцев Социальных Кредитов пока пуста.")
					return
				}
				response := "Топ-5 жителей Китая:\n"
				for i, user := range topUsers {
					response += fmt.Sprintf("%d. <@%s> - %d очков\n", i+1, user.ID, user.Rating)
				}
				s.ChannelMessageSend(m.ChannelID, response)
				return
			}

			if strings.HasPrefix(m.Content, "!rating") {
				parts := strings.Fields(m.Content)
				if len(parts) < 2 {
					s.ChannelMessageSend(m.ChannelID, "❌ Глупый Китайский житель! Вводи данные из привелегии правильно! Пример: !rating @username")
					return
				}
				userID := strings.TrimPrefix(parts[1], "<@")
				userID = strings.TrimSuffix(userID, ">") // Удаление завершающего >
				// Удаление '!' из упоминания, если есть
				userID = strings.TrimPrefix(userID, "!")
				rating := ranking.GetRating(userID)
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Социальные кредиты жителя Китая <@%s>: %d баллов", userID, rating))
				return
			}
		}

		// Отправка сообщений и файлов в Telegram (если не команда)
		if m.Content != "" {
			escapedContent := escapeMarkdownV2(m.Content)
			escapedUsername := escapeMarkdownV2(m.Author.Username)
			telegramMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("🎧:\n*%s*: %s", escapedUsername, escapedContent))
			telegramMsg.ParseMode = "MarkdownV2"
			if _, err := tgBot.Send(telegramMsg); err != nil {
				log.Printf("Failed to send message to Telegram: %v", err)
			}
		}

		if len(m.Attachments) > 0 {
			for _, attachment := range m.Attachments {
				if strings.HasPrefix(attachment.ContentType, "image/") {
					photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(attachment.URL))
					photo.Caption = fmt.Sprintf("🎧:\n %s", m.Author.Username)
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

	// Обработчик сообщений Telegram (это уже работает)
	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60
	updates := tgBot.GetUpdatesChan(updateConfig)

	for update := range updates {
		if update.Message == nil || update.Message.Chat.ID != chatID {
			continue
		}

		// 1. Отправка текста в Discord
		if update.Message.Text != "" {
			telegramMsg := fmt.Sprintf("➤ \n**%s**: %s", update.Message.From.UserName, update.Message.Text)
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
			err = sendFileToDiscord(dg, discordChannelID, photoPath, fmt.Sprintf("➤ %s:", update.Message.From.UserName))
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
			err = sendFileToDiscord(dg, discordChannelID, videoPath, fmt.Sprintf("➤ %s:", update.Message.From.UserName))
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
			err = sendFileToDiscord(dg, discordChannelID, voicePath, fmt.Sprintf("➤ %s:", update.Message.From.UserName))
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
