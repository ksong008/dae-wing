/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package httpapi

import (
	"net/http"
	"strings"

	"github.com/daeuniverse/dae-wing/engine"
	daeCommon "github.com/daeuniverse/dae/common"
	daeConfig "github.com/daeuniverse/dae/config"
	"github.com/daeuniverse/dae/pkg/config_parser"
)

type parsedSectionRequest struct {
	Raw string `json:"raw"`
}

type parsedConfigResponse struct {
	Global       string         `json:"global"`
	ParsedGlobal map[string]any `json:"parsedGlobal,omitempty"`
}

type parsedDNSResponse struct {
	String   string                   `json:"string"`
	Upstream []parsedParamResponse    `json:"upstream"`
	Routing  parsedDNSRoutingResponse `json:"routing"`
}

type parsedDNSRoutingResponse struct {
	Request  parsedRoutingResponse `json:"request"`
	Response parsedRoutingResponse `json:"response"`
}

type parsedRoutingResponse struct {
	String   string                            `json:"string"`
	Rules    []parsedRoutingRuleResponse       `json:"rules"`
	Fallback parsedFunctionOrPlaintextResponse `json:"fallback"`
}

type parsedRoutingRuleResponse struct {
	Conditions []parsedFunctionResponse `json:"conditions"`
	Outbound   parsedFunctionResponse   `json:"outbound"`
}

type parsedFunctionOrPlaintextResponse struct {
	Type      string                   `json:"type,omitempty"`
	Plaintext string                   `json:"plaintext,omitempty"`
	Function  *parsedFunctionResponse  `json:"function,omitempty"`
	Functions []parsedFunctionResponse `json:"functions,omitempty"`
}

type parsedFunctionResponse struct {
	Name   string                `json:"name"`
	Not    bool                  `json:"not"`
	Params []parsedParamResponse `json:"params"`
}

type parsedParamResponse struct {
	Key          string                   `json:"key,omitempty"`
	Val          string                   `json:"val,omitempty"`
	AndFunctions []parsedFunctionResponse `json:"andFunctions,omitempty"`
}

func handleParsedDNS(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}
	var req parsedSectionRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	section := wrapRawSection("dns", req.Raw)
	conf, err := engine.Default().ParseConfig(nil, &section, nil)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, parsedDNSResponseFromModel(&conf.Dns, req.Raw))
}

func handleParsedConfig(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}
	var req configMutationRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	globalSection, err := buildConfigGlobalSectionForCreate(req.Global, req.ParsedGlobal)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	conf, err := engine.Default().ParseConfig(&globalSection, nil, nil)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, parsedConfigResponse{
		Global:       globalSection,
		ParsedGlobal: globalResourceFromModel(&conf.Global),
	})
}

func handleParsedRouting(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}
	var req parsedSectionRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	section := wrapRawSection("routing", req.Raw)
	conf, err := engine.Default().ParseConfig(nil, nil, &section)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, parsedRoutingResponseFromModel(conf.Routing.Rules, conf.Routing.Fallback, req.Raw))
}

func wrapRawSection(name string, raw string) string {
	if strings.TrimSpace(raw) == "" {
		return name + " {}"
	}
	return name + " {\n" + raw + "\n}"
}

func parsedDNSResponseFromModel(model *daeConfig.Dns, raw string) parsedDNSResponse {
	upstream := make([]parsedParamResponse, 0, len(model.Upstream))
	for _, item := range model.Upstream {
		tag, afterTag := daeCommon.GetTagFromLinkLikePlaintext(string(item))
		upstream = append(upstream, parsedParamResponse{
			Key: tag,
			Val: afterTag,
		})
	}
	return parsedDNSResponse{
		String:   strings.TrimSpace(raw),
		Upstream: upstream,
		Routing: parsedDNSRoutingResponse{
			Request:  parsedRoutingResponseFromModel(model.Routing.Request.Rules, model.Routing.Request.Fallback, ""),
			Response: parsedRoutingResponseFromModel(model.Routing.Response.Rules, model.Routing.Response.Fallback, ""),
		},
	}
}

func parsedRoutingResponseFromModel(rules []*config_parser.RoutingRule, fallback daeConfig.FunctionOrString, raw string) parsedRoutingResponse {
	items := make([]parsedRoutingRuleResponse, 0, len(rules))
	for _, rule := range rules {
		items = append(items, parsedRoutingRuleResponse{
			Conditions: parsedFunctionResponsesFromModel(rule.AndFunctions),
			Outbound:   parsedFunctionResponseFromModel(&rule.Outbound),
		})
	}
	fallbackResponse := parsedFunctionOrPlaintextResponseFromModel(fallback)
	return parsedRoutingResponse{
		String:   preferredPreviewString(raw, renderRoutingBody(rules, fallbackResponse)),
		Rules:    items,
		Fallback: fallbackResponse,
	}
}

func parsedFunctionOrPlaintextResponseFromModel(value daeConfig.FunctionOrString) parsedFunctionOrPlaintextResponse {
	switch typed := value.(type) {
	case nil:
		return parsedFunctionOrPlaintextResponse{}
	case string:
		return parsedFunctionOrPlaintextResponse{
			Type:      "plaintext",
			Plaintext: typed,
		}
	case *config_parser.Function:
		function := parsedFunctionResponseFromModel(typed)
		return parsedFunctionOrPlaintextResponse{
			Type:     "function",
			Function: &function,
		}
	case []*config_parser.Function:
		functions := parsedFunctionResponsesFromModel(typed)
		resp := parsedFunctionOrPlaintextResponse{
			Type:      "functions",
			Functions: functions,
		}
		if len(functions) == 1 {
			resp.Function = &functions[0]
		}
		return resp
	default:
		return parsedFunctionOrPlaintextResponse{}
	}
}

func parsedFunctionResponsesFromModel(functions []*config_parser.Function) []parsedFunctionResponse {
	items := make([]parsedFunctionResponse, 0, len(functions))
	for _, function := range functions {
		items = append(items, parsedFunctionResponseFromModel(function))
	}
	return items
}

func parsedFunctionResponseFromModel(function *config_parser.Function) parsedFunctionResponse {
	if function == nil {
		return parsedFunctionResponse{}
	}
	params := make([]parsedParamResponse, 0, len(function.Params))
	for _, param := range function.Params {
		params = append(params, parsedParamResponseFromModel(param))
	}
	return parsedFunctionResponse{
		Name:   function.Name,
		Not:    function.Not,
		Params: params,
	}
}

func parsedParamResponseFromModel(param *config_parser.Param) parsedParamResponse {
	if param == nil {
		return parsedParamResponse{}
	}
	resp := parsedParamResponse{
		Key: param.Key,
		Val: param.Val,
	}
	if len(param.AndFunctions) > 0 {
		resp.AndFunctions = parsedFunctionResponsesFromModel(param.AndFunctions)
	}
	return resp
}

func preferredPreviewString(raw string, fallback string) string {
	if strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}

func renderRoutingBody(rules []*config_parser.RoutingRule, fallback parsedFunctionOrPlaintextResponse) string {
	lines := make([]string, 0, len(rules)+1)
	for _, rule := range rules {
		lines = append(lines, rule.String(false, false, false))
	}
	if fallbackText := renderFunctionOrPlaintext(fallback); fallbackText != "" {
		lines = append(lines, "fallback: "+fallbackText)
	}
	return strings.Join(lines, "\n")
}

func renderFunctionOrPlaintext(value parsedFunctionOrPlaintextResponse) string {
	switch value.Type {
	case "plaintext":
		return value.Plaintext
	case "function":
		if value.Function == nil {
			return ""
		}
		return renderFunction(*value.Function)
	case "functions":
		rendered := make([]string, 0, len(value.Functions))
		for _, function := range value.Functions {
			rendered = append(rendered, renderFunction(function))
		}
		return strings.Join(rendered, " && ")
	default:
		return ""
	}
}

func renderFunction(function parsedFunctionResponse) string {
	var builder strings.Builder
	if function.Not {
		builder.WriteString("!")
	}
	builder.WriteString(function.Name)
	builder.WriteString("(")
	renderedParams := make([]string, 0, len(function.Params))
	for _, param := range function.Params {
		renderedParams = append(renderedParams, renderParam(param))
	}
	builder.WriteString(strings.Join(renderedParams, ", "))
	builder.WriteString(")")
	return builder.String()
}

func renderParam(param parsedParamResponse) string {
	if len(param.AndFunctions) > 0 {
		rendered := make([]string, 0, len(param.AndFunctions))
		for _, function := range param.AndFunctions {
			rendered = append(rendered, renderFunction(function))
		}
		if param.Key == "" {
			return strings.Join(rendered, " && ")
		}
		return param.Key + ": " + strings.Join(rendered, " && ")
	}
	if param.Key == "" {
		return param.Val
	}
	return param.Key + ": " + param.Val
}
