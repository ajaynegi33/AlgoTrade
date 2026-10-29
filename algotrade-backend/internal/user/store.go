package user

import (
	"errors"
	"strings"
	"sync"

	"github.com/ajaynegi33/AlgoTrade/internal/auth"
	"github.com/ajaynegi33/AlgoTrade/internal/config"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidCredentials = errors.New("invalid email or password")
)

type User struct {
	ID           string
	Email        string
	PasswordHash string
	Name         string
	Role         string
}

type Store interface {
	Authenticate(email, password string) (*auth.User, error)
	GetByID(id string) (*User, error)
}

type MemoryStore struct {
	mu           sync.RWMutex
	usersByEmail map[string]*User
	usersByID    map[string]*User
}

func NewMemoryStore(cfg *config.Config) (*MemoryStore, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AuthUserPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	userRecord := &User{
		ID:           cfg.AuthUserID,
		Email:        strings.ToLower(strings.TrimSpace(cfg.AuthUserEmail)),
		PasswordHash: string(hash),
		Name:         cfg.AuthUserName,
		Role:         cfg.AuthUserRole,
	}

	store := &MemoryStore{
		usersByEmail: make(map[string]*User),
		usersByID:    make(map[string]*User),
	}
	store.usersByEmail[userRecord.Email] = userRecord
	store.usersByID[userRecord.ID] = userRecord

	return store, nil
}

func (store *MemoryStore) Authenticate(email, password string) (*auth.User, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	userRecord, exists := store.usersByEmail[normalizedEmail]
	if !exists {
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(userRecord.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	return &auth.User{
		ID:    userRecord.ID,
		Email: userRecord.Email,
		Name:  userRecord.Name,
		Role:  userRecord.Role,
	}, nil
}

func (store *MemoryStore) GetByID(id string) (*User, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()

	userRecord, exists := store.usersByID[id]
	if !exists {
		return nil, ErrUserNotFound
	}
	return userRecord, nil
}
