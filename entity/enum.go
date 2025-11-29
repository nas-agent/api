package entity

type UserRole string

const (
	UserRoleUnknown UserRole = "unknown"
	UserRoleAdmin   UserRole = "admin"
	UserRoleUser    UserRole = "user"
)
