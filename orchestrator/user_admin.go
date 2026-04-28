/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"time"
	"unicode"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/golang-jwt/jwt/v5"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"golang.org/x/crypto/sha3"
)

type UserUpdateInput struct {
	Username    *string
	Name        *string
	ClearName   bool
	Avatar      *string
	ClearAvatar bool
}

type PasswordUpdateInput struct {
	CurrentPassword string
	NewPassword     string
}

func IssueToken(ctx context.Context, username string, password string) (string, error) {
	var user db.User
	q := db.DB(ctx).Model(&db.User{}).Where("username = ?", username).First(&user)
	if q.Error != nil || q.RowsAffected == 0 {
		return "", fmt.Errorf("incorrect username or password")
	}

	hashedPassword, err := hashPassword([]byte(user.JwtSecret), password)
	if err != nil {
		return "", err
	}
	if hashedPassword != user.PasswordHash {
		return "", fmt.Errorf("incorrect username or password")
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"role": "admin",
		"sub":  user.Username,
		"exp":  time.Now().Add(30 * time.Hour * 24).UTC().Unix(),
	})
	return token.SignedString([]byte(user.JwtSecret))
}

func NumberUsers(ctx context.Context) (int32, error) {
	var count int64
	if err := db.DB(ctx).Model(&db.User{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return int32(count), nil
}

func CreateUser(ctx context.Context, username string, password string) (string, error) {
	if len(password) < 6 || !containsLetter(password) || !containsDigit(password) {
		return "", fmt.Errorf("too weak password; should contain numbers and letters, and no less than 6 in length")
	}

	tx := db.BeginTx(ctx)
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()

	var count int64
	if err := tx.Model(&db.User{}).Count(&count).Error; err != nil {
		tx.Rollback()
		return "", err
	}
	if count > 0 {
		tx.Rollback()
		return "", fmt.Errorf("a user already exists")
	}

	var secretRaw [32]byte
	if _, err := io.ReadFull(rand.Reader, secretRaw[:]); err != nil {
		tx.Rollback()
		return "", err
	}
	secret := hex.EncodeToString(secretRaw[:])
	hashedPassword, err := hashPassword([]byte(secret), password)
	if err != nil {
		tx.Rollback()
		return "", err
	}
	if err = tx.Model(&db.User{}).Create(&db.User{
		Username:     username,
		PasswordHash: hashedPassword,
		JwtSecret:    secret,
	}).Error; err != nil {
		tx.Rollback()
		return "", err
	}
	if err = tx.Commit().Error; err != nil {
		return "", err
	}
	return IssueToken(context.Background(), username, password)
}

func UpdateUser(ctx context.Context, user *db.User, input UserUpdateInput) (int32, error) {
	updates := map[string]any{}
	if input.Username != nil {
		updates["username"] = *input.Username
	}
	if input.Name != nil {
		updates["name"] = input.Name
	} else if input.ClearName {
		updates["name"] = nil
	}
	if input.Avatar != nil {
		updates["avatar"] = input.Avatar
	} else if input.ClearAvatar {
		updates["avatar"] = nil
	}
	if len(updates) == 0 {
		return 0, nil
	}

	q := db.DB(ctx).Model(user).Updates(updates)
	if q.Error != nil {
		return 0, q.Error
	}
	if err := db.DB(ctx).First(user, user.ID).Error; err != nil {
		return 0, err
	}
	return int32(q.RowsAffected), nil
}

func UpdatePassword(ctx context.Context, user *db.User, input PasswordUpdateInput, skipVerify bool) (string, error) {
	if !skipVerify {
		hashedPassword, err := hashPassword([]byte(user.JwtSecret), input.CurrentPassword)
		if err != nil {
			return "", err
		}
		if hashedPassword != user.PasswordHash {
			return "", fmt.Errorf("incorrect password")
		}
	}

	var secretRaw [32]byte
	if _, err := io.ReadFull(rand.Reader, secretRaw[:]); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(secretRaw[:])
	hashedPassword, err := hashPassword([]byte(secret), input.NewPassword)
	if err != nil {
		return "", err
	}

	tx := db.BeginTx(ctx)
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()

	q := tx.Model(user).Updates(db.User{
		PasswordHash: hashedPassword,
		JwtSecret:    secret,
	})
	if q.Error != nil {
		tx.Rollback()
		return "", q.Error
	}
	if err := tx.Commit().Error; err != nil {
		return "", err
	}
	user.PasswordHash = hashedPassword
	user.JwtSecret = secret
	return IssueToken(context.Background(), user.Username, input.NewPassword)
}

func QueryJSONStorage(user *db.User, paths []string) []string {
	if len(paths) == 0 {
		return []string{user.JsonStorage}
	}
	results := gjson.GetMany(user.JsonStorage, paths...)
	values := make([]string, 0, len(results))
	for _, result := range results {
		values = append(values, result.String())
	}
	return values
}

func SetJSONStorage(ctx context.Context, user *db.User, paths []string, values []string) (int32, error) {
	if len(paths) != len(values) {
		return 0, fmt.Errorf("len(paths) != len(values)")
	}

	var err error
	for i := range paths {
		user.JsonStorage, err = sjson.Set(user.JsonStorage, paths[i], values[i])
		if err != nil {
			return 0, err
		}
	}
	if err = db.DB(ctx).Model(user).Update("json_storage", user.JsonStorage).Error; err != nil {
		return 0, err
	}
	return int32(len(paths)), nil
}

func RemoveJSONStorage(ctx context.Context, user *db.User, paths []string) (int32, error) {
	var err error
	if len(paths) == 0 {
		user.JsonStorage = "{}"
		if err = db.DB(ctx).Model(user).Update("json_storage", user.JsonStorage).Error; err != nil {
			return 0, err
		}
		return 1, nil
	}

	for _, path := range paths {
		user.JsonStorage, err = sjson.Delete(user.JsonStorage, path)
		if err != nil {
			return 0, err
		}
	}
	if err = db.DB(ctx).Model(user).Update("json_storage", user.JsonStorage).Error; err != nil {
		return 0, err
	}
	return int32(len(paths)), nil
}

func hashPassword(salt []byte, password string) (string, error) {
	h := sha3.NewShake256()
	if _, err := h.Write(salt); err != nil {
		return "", err
	}
	if _, err := h.Write([]byte(password)); err != nil {
		return "", err
	}
	var hash [32]byte
	if _, err := io.ReadFull(h, hash[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash[:]), nil
}

func containsLetter(value string) bool {
	for _, r := range value {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func containsDigit(value string) bool {
	for _, r := range value {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
