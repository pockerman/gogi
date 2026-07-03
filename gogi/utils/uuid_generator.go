package utils

import "github.com/google/uuid"

func NewUUIDString() string {
	newUUID := uuid.New().String()
	return newUUID
}
