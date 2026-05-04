/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package httpapi

import (
	"reflect"
	"strings"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/engine"
	daeConfig "github.com/daeuniverse/dae/config"
)

func buildConfigResource(model *db.Config) (configResource, error) {
	resource := buildConfigResourceRaw(model)
	conf, err := engine.Default().ParseConfig(&model.Global, nil, nil)
	if err != nil {
		resource.ParseError = err.Error()
		return resource, nil
	}
	resource.ParsedGlobal = globalResourceFromModel(&conf.Global)
	return resource, nil
}

func buildConfigResourceRaw(model *db.Config) configResource {
	return configResource{
		ID:       model.ID,
		Name:     model.Name,
		Global:   model.Global,
		Selected: model.Selected,
		Version:  model.Version,
	}
}

func buildDNSResource(model *db.Dns) (dnsResource, error) {
	conf, err := engine.Default().ParseConfig(nil, &model.Dns, nil)
	if err != nil {
		return dnsResource{}, err
	}
	trimmed := trimSectionBody(model.Dns, "dns")
	parsed := parsedDNSResponseFromModel(&conf.Dns, trimmed)
	resource := buildDNSResourceRaw(model)
	resource.ParsedDNS = &parsed
	return resource, nil
}

func buildDNSResourceRaw(model *db.Dns) dnsResource {
	return dnsResource{
		ID:       model.ID,
		Name:     model.Name,
		DNS:      model.Dns,
		Selected: model.Selected,
		Version:  model.Version,
	}
}

func buildRoutingResource(model *db.Routing) (routingResource, error) {
	conf, err := engine.Default().ParseConfig(nil, nil, &model.Routing)
	if err != nil {
		return routingResource{}, err
	}
	trimmed := trimSectionBody(model.Routing, "routing")
	parsed := parsedRoutingResponseFromModel(conf.Routing.Rules, conf.Routing.Fallback, trimmed)
	resource := buildRoutingResourceRaw(model)
	resource.ParsedRouting = &parsed
	resource.ReferenceGroups = engine.Default().NecessaryOutbounds(&conf.Routing)
	return resource, nil
}

func buildRoutingResourceRaw(model *db.Routing) routingResource {
	return routingResource{
		ID:       model.ID,
		Name:     model.Name,
		Routing:  model.Routing,
		Selected: model.Selected,
		Version:  model.Version,
	}
}

func globalResourceFromModel(global *daeConfig.Global) map[string]any {
	if global == nil {
		return nil
	}
	value := reflect.ValueOf(*global)
	typ := value.Type()
	result := make(map[string]any, value.NumField())
	for i := 0; i < value.NumField(); i++ {
		field := typ.Field(i)
		name := field.Tag.Get("mapstructure")
		if name == "" {
			continue
		}
		result[snakeToLowerCamel(name)] = globalFieldValue(value.Field(i).Interface())
	}
	return result
}

func globalFieldValue(value any) any {
	switch typed := value.(type) {
	case time.Duration:
		return typed.String()
	case []string:
		items := make([]string, len(typed))
		copy(items, typed)
		return items
	default:
		return typed
	}
}

func snakeToLowerCamel(name string) string {
	parts := strings.Split(name, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

func trimSectionBody(section string, name string) string {
	trimmed := strings.TrimSpace(section)
	prefix := name + " {"
	trimmed = strings.TrimPrefix(trimmed, prefix)
	trimmed = strings.TrimSuffix(trimmed, "}")
	return strings.TrimSpace(trimmed)
}
