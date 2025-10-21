package users

import "time"

type Role string

const (
	RoleMaster Role = "master"
	RoleAdmin  Role = "admin"
)

type User struct {
	ID         int64
	TelegramID int64
	Username   string
	FirstName  string
	LastName   string
	Role       Role
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Telegram struct {
	ID        int64
	Username  string
	FirstName string
	LastName  string
}
