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
	mu      sync.RWMutex
	byEmail map[string]*User
	byID    map[string]*User
}

func NewMemoryStore(cfg *config.Config) (*MemoryStore, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AuthUserPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	u := &User{
		ID:           cfg.AuthUserID,
		Email:        strings.ToLower(strings.TrimSpace(cfg.AuthUserEmail)),
		PasswordHash: string(hash),
		Name:         cfg.AuthUserName,
		Role:         cfg.AuthUserRole,
	}

	store := &MemoryStore{
		byEmail: make(map[string]*User),
		byID:    make(map[string]*User),
	}
	store.byEmail[u.Email] = u
	store.byID[u.ID] = u

	return store, nil
}

func (s *MemoryStore) Authenticate(email, password string) (*auth.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	u, exists := s.byEmail[normalizedEmail]
	if !exists {
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	return &auth.User{
		ID:    u.ID,
		Email: u.Email,
		Name:  u.Name,
		Role:  u.Role,
	}, nil
}

func (s *MemoryStore) GetByID(id string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	u, exists := s.byID[id]
	if !exists {
		return nil, ErrUserNotFound
	}
	return u, nil
}
