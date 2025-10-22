package users

import "time"

type Role string

const (
	RoleMaster        Role = "master"
	RoleAdministrator Role = "administrator"
	RoleAdmin         Role = "admin"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

type User struct {
	ID         int64
	TelegramID int64
	Username   string
	Role       Role
	Status     Status
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
