/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package defaults

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae/pkg/config_parser"
	"gorm.io/gorm"
)

type EnsureInput struct {
	ConfigName   string
	Global       string
	DnsName      string
	Dns          string
	RoutingName  string
	Routing      string
	GroupName    string
	Policy       string
	PolicyParams []config_parser.Param
	Mode         string
}

type EnsureResult struct {
	DefaultConfigID  uint
	DefaultRoutingID uint
	DefaultDNSID     uint
	DefaultGroupID   uint
	Mode             string
}

var ensureDefaultsMu sync.Mutex

const (
	emptyGroupSection        = `group {}`
	emptySubscriptionSection = `subscription {}`
	emptyNodeSection         = `node {}`
	emptyRoutingSection      = `routing {}`
	emptyDNSSection          = `dns {}`
	emptyGlobalSection       = `global {}`
)

func Ensure(ctx context.Context, userID uint, input EnsureInput) (result *EnsureResult, err error) {
	ensureDefaultsMu.Lock()
	defer ensureDefaultsMu.Unlock()

	tx := db.BeginTx(ctx)
	if tx.Error != nil {
		return nil, tx.Error
	}
	defer func() {
		if finishErr := finishTx(tx, err); finishErr != nil {
			err = finishErr
		}
	}()

	var user db.User
	if err = tx.Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}

	storage, err := parseStorage(user.JsonStorage)
	if err != nil {
		return nil, err
	}

	configSelected, err := hasSelected(tx, &db.Config{})
	if err != nil {
		return nil, err
	}
	routingSelected, err := hasSelected(tx, &db.Routing{})
	if err != nil {
		return nil, err
	}
	dnsSelected, err := hasSelected(tx, &db.Dns{})
	if err != nil {
		return nil, err
	}

	configID, createdConfig, err := ensureConfig(tx, storageString(storage, "defaultConfigID"), input)
	if err != nil {
		return nil, err
	}
	if createdConfig && !configSelected {
		if err = replaceSelected(tx, &db.Config{}, configID); err != nil {
			return nil, err
		}
	}

	routingID, createdRouting, err := ensureRouting(tx, storageString(storage, "defaultRoutingID"), input)
	if err != nil {
		return nil, err
	}
	if createdRouting && !routingSelected {
		if err = replaceSelected(tx, &db.Routing{}, routingID); err != nil {
			return nil, err
		}
	}

	dnsID, createdDNS, err := ensureDNS(tx, storageString(storage, "defaultDNSID"), input)
	if err != nil {
		return nil, err
	}
	if createdDNS && !dnsSelected {
		if err = replaceSelected(tx, &db.Dns{}, dnsID); err != nil {
			return nil, err
		}
	}

	groupID, _, err := ensureGroup(tx, storageString(storage, "defaultGroupID"), input)
	if err != nil {
		return nil, err
	}

	storage["defaultConfigID"] = encodeCursor(configID)
	storage["defaultRoutingID"] = encodeCursor(routingID)
	storage["defaultDNSID"] = encodeCursor(dnsID)
	storage["defaultGroupID"] = encodeCursor(groupID)
	if strings.TrimSpace(storageString(storage, "mode")) == "" {
		storage["mode"] = input.Mode
	}

	marshaledStorage, err := json.Marshal(storage)
	if err != nil {
		return nil, err
	}
	user.JsonStorage = string(marshaledStorage)
	if err = tx.Model(&user).Update("json_storage", user.JsonStorage).Error; err != nil {
		return nil, err
	}

	return &EnsureResult{
		DefaultConfigID:  configID,
		DefaultRoutingID: routingID,
		DefaultDNSID:     dnsID,
		DefaultGroupID:   groupID,
		Mode:             storageString(storage, "mode"),
	}, nil
}

func parseStorage(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var storage map[string]any
	if err := json.Unmarshal([]byte(raw), &storage); err != nil {
		return nil, fmt.Errorf("parse json storage: %w", err)
	}
	if storage == nil {
		storage = map[string]any{}
	}
	return storage, nil
}

func storageString(storage map[string]any, key string) string {
	value, ok := storage[key]
	if !ok {
		return ""
	}
	strValue, ok := value.(string)
	if !ok {
		return ""
	}
	return strValue
}

func finishTx(tx *gorm.DB, currentErr error) error {
	if currentErr == nil {
		if commitErr := tx.Commit().Error; commitErr != nil {
			return commitErr
		}
		return nil
	}
	if rollbackErr := tx.Rollback().Error; rollbackErr != nil && !errors.Is(rollbackErr, gorm.ErrInvalidTransaction) {
		return fmt.Errorf("%w; rollback: %v", currentErr, rollbackErr)
	}
	return currentErr
}

func hasSelected(tx *gorm.DB, model any) (bool, error) {
	var count int64
	if err := tx.Model(model).Where("selected = ?", true).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func replaceSelected(tx *gorm.DB, model any, id uint) error {
	if err := tx.Model(model).Where("selected = ?", true).Update("selected", false).Error; err != nil {
		return err
	}
	return tx.Model(model).Where("id = ?", id).Update("selected", true).Error
}

func decodeExistingID(storedID string) (uint, bool) {
	if strings.TrimSpace(storedID) == "" {
		return 0, false
	}
	id, err := decodeCursor(storedID)
	if err != nil {
		return 0, false
	}
	return id, true
}

func loadExistingID(tx *gorm.DB, model any, storedID string) (uint, bool, error) {
	id, ok := decodeExistingID(storedID)
	if !ok {
		return 0, false, nil
	}
	var count int64
	if err := tx.Model(model).Where("id = ?", id).Count(&count).Error; err != nil {
		return 0, false, err
	}
	return id, count > 0, nil
}

func ensureConfig(tx *gorm.DB, storedID string, input EnsureInput) (id uint, created bool, err error) {
	if err = validateConfig(input.Global); err != nil {
		return 0, false, err
	}
	if id, ok, err := loadExistingID(tx, &db.Config{}, storedID); err != nil || ok {
		return id, false, err
	}
	var existing db.Config
	if err = tx.Where("name = ? AND global = ?", input.ConfigName, input.Global).First(&existing).Error; err == nil {
		return existing.ID, false, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, err
	}
	record := db.Config{
		Name:     input.ConfigName,
		Global:   input.Global,
		Selected: false,
	}
	if err = tx.Create(&record).Error; err != nil {
		return 0, false, err
	}
	return record.ID, true, nil
}

func ensureRouting(tx *gorm.DB, storedID string, input EnsureInput) (id uint, created bool, err error) {
	wrappedRouting := wrapSection("routing", input.Routing)
	if err = validateRouting(wrappedRouting); err != nil {
		return 0, false, err
	}
	if id, ok, err := loadExistingID(tx, &db.Routing{}, storedID); err != nil || ok {
		return id, false, err
	}
	var existing db.Routing
	if err = tx.Where("name = ? AND routing = ?", input.RoutingName, wrappedRouting).First(&existing).Error; err == nil {
		return existing.ID, false, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, err
	}
	record := db.Routing{
		Name:     input.RoutingName,
		Routing:  wrappedRouting,
		Selected: false,
	}
	if err = tx.Create(&record).Error; err != nil {
		return 0, false, err
	}
	return record.ID, true, nil
}

func ensureDNS(tx *gorm.DB, storedID string, input EnsureInput) (id uint, created bool, err error) {
	wrappedDNS := wrapSection("dns", input.Dns)
	if err = validateDNS(wrappedDNS); err != nil {
		return 0, false, err
	}
	if id, ok, err := loadExistingID(tx, &db.Dns{}, storedID); err != nil || ok {
		return id, false, err
	}
	var existing db.Dns
	if err = tx.Where("name = ? AND dns = ?", input.DnsName, wrappedDNS).First(&existing).Error; err == nil {
		return existing.ID, false, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, err
	}
	record := db.Dns{
		Name:     input.DnsName,
		Dns:      wrappedDNS,
		Selected: false,
	}
	if err = tx.Create(&record).Error; err != nil {
		return 0, false, err
	}
	return record.ID, true, nil
}

func ensureGroup(tx *gorm.DB, storedID string, input EnsureInput) (id uint, created bool, err error) {
	if err = validateID(input.GroupName); err != nil {
		return 0, false, err
	}
	if id, ok, err := loadExistingID(tx, &db.Group{}, storedID); err != nil || ok {
		return id, false, err
	}
	var existing db.Group
	if err = tx.Where("name = ?", input.GroupName).First(&existing).Error; err == nil {
		return existing.ID, false, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, err
	}
	params := make([]db.GroupPolicyParam, len(input.PolicyParams))
	for i := range input.PolicyParams {
		params[i].Unmarshal(&input.PolicyParams[i])
	}
	record := db.Group{
		Name:         input.GroupName,
		Policy:       input.Policy,
		PolicyParams: params,
	}
	if err = tx.Create(&record).Error; err != nil {
		return 0, false, err
	}
	return record.ID, true, nil
}

func validateConfig(globalSection string) error {
	return validateSyntax(globalSection, emptyDNSSection, emptyRoutingSection)
}

func validateDNS(dnsSection string) error {
	return validateSyntax(emptyGlobalSection, dnsSection, emptyRoutingSection)
}

func validateRouting(routingSection string) error {
	return validateSyntax(emptyGlobalSection, emptyDNSSection, routingSection)
}

func wrapSection(name string, body string) string {
	return name + " {\n" + body + "\n}"
}

func validateSyntax(globalSection string, dnsSection string, routingSection string) error {
	strConfig := strings.Join([]string{
		globalSection,
		dnsSection,
		routingSection,
		emptyGroupSection,
		emptySubscriptionSection,
		emptyNodeSection,
	}, "\n")
	sections, err := config_parser.Parse(strConfig)
	if err != nil {
		return err
	}
	if len(sections) == 0 {
		return fmt.Errorf("parsed config is empty")
	}
	return nil
}

func encodeCursor(id uint) string {
	return base64.StdEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(fmt.Sprintf("cursor%v", id)))
}

func decodeCursor(cursor string) (uint, error) {
	raw, err := base64.StdEncoding.WithPadding(base64.NoPadding).DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("failed to parse cursor")
	}
	intID, err := strconv.Atoi(strings.TrimPrefix(string(raw), "cursor"))
	if err != nil {
		return 0, fmt.Errorf("failed to parse cursor")
	}
	return uint(intID), nil
}

func validateID(id string) error {
	if !regexp.MustCompile(`^[a-zA-Z_][-a-zA-Z0-9_/\\^*+.=@$!#%]*$`).MatchString(id) {
		return fmt.Errorf("invalid id; only support numbers and letters")
	}
	return nil
}
