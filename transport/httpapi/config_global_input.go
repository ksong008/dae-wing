/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package httpapi

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/daeuniverse/dae-wing/engine"
	daeConfig "github.com/daeuniverse/dae/config"
)

func buildConfigGlobalSectionForCreate(raw *string, parsed map[string]any) (string, error) {
	if raw != nil && parsed != nil {
		return "", fmt.Errorf("only one of global or parsedGlobal may be provided")
	}
	if parsed != nil {
		return marshalGlobalInput(parsed, nil, true)
	}
	return normalizeSection(raw, engine.Default().EmptyGlobalSection()), nil
}

func buildConfigGlobalSectionForUpdate(current string, raw *string, parsed map[string]any) (string, error) {
	if raw != nil && parsed != nil {
		return "", fmt.Errorf("only one of global or parsedGlobal may be provided")
	}
	if parsed != nil {
		conf, err := engine.Default().ParseConfig(&current, nil, nil)
		if err != nil {
			return "", err
		}
		return marshalGlobalInput(parsed, &conf.Global, false)
	}
	return normalizeSection(raw, engine.Default().EmptyGlobalSection()), nil
}

func marshalGlobalInput(input map[string]any, base *daeConfig.Global, ignoreZero bool) (string, error) {
	var global daeConfig.Global
	if base != nil {
		global = *base
	}
	if err := assignParsedGlobalInput(&global, input); err != nil {
		return "", err
	}
	marshaller := daeConfig.Marshaller{
		IndentSpace: 2,
		IgnoreZero:  ignoreZero,
	}
	if err := marshaller.MarshalSection("global", reflect.ValueOf(global), 0); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(marshaller.Bytes())), nil
}

func assignParsedGlobalInput(global *daeConfig.Global, input map[string]any) error {
	if global == nil {
		return fmt.Errorf("global is nil")
	}
	if input == nil {
		return nil
	}

	value := reflect.ValueOf(global).Elem()
	typ := value.Type()
	fields := make(map[string]reflect.StructField, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		mapping := field.Tag.Get("mapstructure")
		if mapping == "" {
			continue
		}
		fields[snakeToLowerCamel(mapping)] = field
	}

	for key, raw := range input {
		field, ok := fields[key]
		if !ok {
			return fmt.Errorf("unknown parsedGlobal field: %s", key)
		}
		if raw == nil {
			continue
		}
		converted, err := convertGlobalInputValue(raw, field.Type)
		if err != nil {
			return fmt.Errorf("invalid parsedGlobal.%s: %w", key, err)
		}
		value.FieldByName(field.Name).Set(converted)
	}
	return nil
}

func convertGlobalInputValue(raw any, targetType reflect.Type) (reflect.Value, error) {
	if targetType == reflect.TypeOf(time.Duration(0)) {
		text, ok := raw.(string)
		if !ok {
			return reflect.Value{}, fmt.Errorf("expected duration string")
		}
		duration, err := time.ParseDuration(text)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(duration), nil
	}

	switch targetType.Kind() {
	case reflect.String:
		text, ok := raw.(string)
		if !ok {
			return reflect.Value{}, fmt.Errorf("expected string")
		}
		return reflect.ValueOf(text).Convert(targetType), nil
	case reflect.Bool:
		flag, ok := raw.(bool)
		if !ok {
			return reflect.Value{}, fmt.Errorf("expected boolean")
		}
		return reflect.ValueOf(flag).Convert(targetType), nil
	case reflect.Slice:
		if targetType.Elem().Kind() != reflect.String {
			return reflect.Value{}, fmt.Errorf("unsupported slice type")
		}
		switch typed := raw.(type) {
		case []string:
			return reflect.ValueOf(typed).Convert(targetType), nil
		case []any:
			items := make([]string, 0, len(typed))
			for _, item := range typed {
				text, ok := item.(string)
				if !ok {
					return reflect.Value{}, fmt.Errorf("expected string array")
				}
				items = append(items, text)
			}
			return reflect.ValueOf(items).Convert(targetType), nil
		default:
			return reflect.Value{}, fmt.Errorf("expected string array")
		}
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint:
		number, err := parseJSONInteger(raw)
		if err != nil {
			return reflect.Value{}, err
		}
		if number < 0 {
			return reflect.Value{}, fmt.Errorf("expected unsigned integer")
		}
		value := reflect.New(targetType).Elem()
		if value.OverflowUint(uint64(number)) {
			return reflect.Value{}, fmt.Errorf("value out of range")
		}
		value.SetUint(uint64(number))
		return value, nil
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
		number, err := parseJSONInteger(raw)
		if err != nil {
			return reflect.Value{}, err
		}
		value := reflect.New(targetType).Elem()
		if value.OverflowInt(number) {
			return reflect.Value{}, fmt.Errorf("value out of range")
		}
		value.SetInt(number)
		return value, nil
	default:
		return reflect.Value{}, fmt.Errorf("unsupported type %s", targetType)
	}
}

func parseJSONInteger(raw any) (int64, error) {
	switch typed := raw.(type) {
	case float64:
		if math.Trunc(typed) != typed {
			return 0, fmt.Errorf("expected integer")
		}
		return int64(typed), nil
	case int:
		return int64(typed), nil
	case int32:
		return int64(typed), nil
	case int64:
		return typed, nil
	default:
		return 0, fmt.Errorf("expected number")
	}
}
