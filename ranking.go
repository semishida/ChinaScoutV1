package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Структура для администраторов
type Admins struct {
	IDs []string `json:"admin_ids"`
}

// Структура для пользователей и их рейтинга
type User struct {
	ID     string `json:"id"`
	Rating int    `json:"rating"`
}

// Структура для хранения рейтинга
type Ranking struct {
	mu         sync.Mutex
	users      map[string]*User
	admins     map[string]bool
	isModified bool // Флаг, который указывает на изменения
}

// Создание нового рейтинга
func NewRanking(adminFilePath string) (*Ranking, error) {
	ranking := &Ranking{
		users:  make(map[string]*User),
		admins: make(map[string]bool),
	}

	// Загрузка администраторов из JSON файла
	file, err := os.Open(adminFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open admin file: %v", err)
	}
	defer file.Close()

	var admins Admins
	if err := json.NewDecoder(file).Decode(&admins); err != nil {
		return nil, fmt.Errorf("failed to parse admin file: %v", err)
	}

	for _, id := range admins.IDs {
		ranking.admins[id] = true
	}

	// Добавляем отладочный вывод
	log.Printf("Loaded admins: %v", ranking.admins)

	return ranking, nil
}

// Добавление пользователя в рейтинг
func (r *Ranking) AddUser(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.users[id]; !exists {
		r.users[id] = &User{ID: id, Rating: 0}
		r.isModified = true
	}
}

// Обновление рейтинга пользователя
func (r *Ranking) UpdateRating(id string, points int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if user, exists := r.users[id]; exists {
		user.Rating += points
	} else {
		// Если пользователь не найден, добавляем его
		r.users[id] = &User{ID: id, Rating: points}
	}

	// Устанавливаем флаг, что данные были изменены
	r.isModified = true
}

// Сохранение рейтингов в файл
func (r *Ranking) SaveToFile(filepath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Если изменений не было, не сохраняем файл
	if !r.isModified {
		return nil
	}

	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ") // Для читабельности JSON
	err = encoder.Encode(r.users)
	if err != nil {
		return fmt.Errorf("failed to encode users: %v", err)
	}

	// После сохранения данных сбрасываем флаг
	r.isModified = false
	return nil
}

// Загрузка рейтингов из файла
func (r *Ranking) LoadFromFile(filepath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	file, err := os.Open(filepath)
	if err != nil {
		// Если файл не существует, не считаем это ошибкой
		if os.IsNotExist(err) {
			log.Printf("File %s does not exist. Starting with empty users.", filepath)
			return nil
		}
		return fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&r.users)
	if err != nil {
		return fmt.Errorf("failed to decode users: %v", err)
	}

	log.Printf("Loaded %d users from %s", len(r.users), filepath)
	return nil
}

// Получить рейтинг пользователя
func (r *Ranking) GetRating(id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	if user, exists := r.users[id]; exists {
		return user.Rating
	}
	return 0
}

// Просмотр топ-5 пользователей по рейтингу
func (r *Ranking) GetTop5() []User {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Сортировка пользователей по рейтингу
	users := make([]User, 0, len(r.users))
	for _, user := range r.users {
		users = append(users, *user)
	}

	// Сортировка по убыванию рейтинга
	sort.Slice(users, func(i, j int) bool {
		return users[i].Rating > users[j].Rating
	})

	if len(users) > 5 {
		users = users[:5]
	}
	return users
}

// Проверка, является ли пользователь администратором
func (r *Ranking) IsAdmin(userID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, isAdmin := r.admins[userID]
	return isAdmin
}

// Обработка команды !china
func (r *Ranking) HandleChinaCommand(s *discordgo.Session, m *discordgo.MessageCreate, command string) {
	// Отладочный вывод ID
	log.Printf("Checking if user %s is an admin", m.Author.ID)

	// Очистка ID перед проверкой
	userID := strings.TrimPrefix(m.Author.ID, "<@")
	userID = strings.TrimSuffix(userID, ">")
	userID = strings.TrimPrefix(userID, "!")

	if !r.IsAdmin(userID) {
		s.ChannelMessageSend(m.ChannelID, "❌ Глупый Китайский мальчик хочет использовать привелегии Китай-Партии.")
		return
	}

	// Пример команды: !china @id +10
	parts := strings.Fields(command)
	if len(parts) < 3 {
		s.ChannelMessageSend(m.ChannelID, "❌ Глупый Китайский брат. Используй привелегии правильно: !china @id +10 или !china @id -10.")
		return
	}

	targetID := strings.TrimPrefix(parts[1], "<@")
	targetID = strings.TrimSuffix(targetID, ">")
	targetID = strings.TrimPrefix(targetID, "!")

	points, err := strconv.Atoi(parts[2])
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, "❌ Глупый количество очков. Используй целое число.")
		return
	}

	// Обновление рейтинга
	r.UpdateRating(targetID, points)

	// Сохранение после изменения рейтинга
	err = r.SaveToFile("users.json")
	if err != nil {
		log.Printf("Failed to save users to file after !china command: %v", err)
		s.ChannelMessageSend(m.ChannelID, "❌ Ничего не сохранилось.")
		return
	}

	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("✅ Социальные кредиты пользователя <@%s> изменились на %d баллов.", targetID, points))
}

// Функция для отслеживания активности в голосовых каналах
func (r *Ranking) TrackVoiceActivity(s *discordgo.Session) {
	s.AddHandler(func(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
		// Пропускаем, если это обновление голосового состояния бота
		if v.UserID == s.State.User.ID {
			return
		}

		// Если пользователь присоединился к голосовому каналу
		if v.ChannelID != "" {
			log.Printf("User %s joined voice channel %s", v.UserID, v.ChannelID)
			r.AddUser(v.UserID)
			go r.trackUser(s, v.UserID, v.ChannelID)
		}
	})
}

// Функция для отслеживания времени в голосовом канале с начислением баллов каждые 5 секунд
func (r *Ranking) trackUser(s *discordgo.Session, userID string, channelID string) {
	// Используем Ticker для выполнения задачи каждые 5 секунд
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Проверяем, находится ли пользователь в том же голосовом канале
			inChannel := false

			// Получаем состояние голосовых каналов
			guilds, err := s.UserGuilds(100, "", "")
			if err != nil {
				log.Printf("Failed to get user guilds: %v", err)
				return
			}

			for _, guild := range guilds {
				guildState, err := s.State.Guild(guild.ID)
				if err != nil {
					log.Printf("Failed to get guild state for guild %s: %v", guild.ID, err)
					continue
				}

				// Проверяем, есть ли пользователь в этом канале
				for _, vs := range guildState.VoiceStates {
					if vs.UserID == userID && vs.ChannelID == channelID {
						inChannel = true
						break
					}
				}
				if inChannel {
					break
				}
			}

			if inChannel {
				// Увеличиваем рейтинг на 1 очко (0.1 балла)
				r.UpdateRating(userID, 1)
				log.Printf("User %s has been in voice channel %s for 30 seconds. Rating increased by 0.1.", userID, channelID)
			} else {
				// Пользователь покинул канал, завершаем отслеживание
				log.Printf("User %s left voice channel %s", userID, channelID)
				return
			}
		}
	}
}

// Функция для периодического сохранения файла каждую секунду
func (r *Ranking) PeriodicSave(filepath string) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			err := r.SaveToFile(filepath)
			if err != nil {
				log.Printf("Failed to save users to file: %v", err)
			}
		}
	}
}
