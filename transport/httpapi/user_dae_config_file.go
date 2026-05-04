/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/engine"
	"github.com/daeuniverse/dae-wing/orchestrator"
	daeCommon "github.com/daeuniverse/dae/common"
	"github.com/daeuniverse/dae/component/outbound"
	"github.com/daeuniverse/dae/component/outbound/dialer"
	daeConfig "github.com/daeuniverse/dae/config"
	"github.com/daeuniverse/dae/pkg/config_parser"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type daeConfigFileResponse struct {
	Filename string               `json:"filename"`
	Content  string               `json:"content"`
	Warnings []daeConfigFileIssue `json:"warnings,omitempty"`
}

type daeConfigFileImportRequest struct {
	Filename   string `json:"filename"`
	NamePrefix string `json:"namePrefix"`
	Content    string `json:"content"`
}

type daeConfigFileImportResponse struct {
	Imported bool                 `json:"imported"`
	Warnings []daeConfigFileIssue `json:"warnings,omitempty"`
}

type daeConfigFilePreviewResponse struct {
	Bundle   daeBundle            `json:"bundle"`
	Warnings []daeConfigFileIssue `json:"warnings,omitempty"`
}

type resolvedImportedDAEConfig struct {
	Warnings []daeConfigFileIssue

	ConfigName  string
	DNSName     string
	RoutingName string
	Global      string
	DNS         string
	Routing     string

	Subscriptions []resolvedImportedSubscription
	Nodes         []resolvedImportedNode
	Groups        []resolvedImportedGroup
}

type resolvedImportedSubscription struct {
	Tag        *string
	Link       string
	CronExp    string
	CronEnable bool
	Status     string
	Info       string
}

type resolvedImportedNode struct {
	Key             string
	SubscriptionTag string
	RawDialerLink   string
	Tag             *string
	Link            string
	Name            string
	Address         string
	Protocol        string
}

type resolvedImportedGroup struct {
	Name                 string
	Policy               string
	PolicyParams         []paramResource
	NodeKeys             []string
	SubscriptionBindings []importedSubscriptionBinding
}

type importedDAEConfigResources struct {
	Warnings []daeConfigFileIssue

	ConfigName  string
	DNSName     string
	RoutingName string
	Global      string
	DNS         string
	Routing     string

	Subscriptions []importedSubscription
	Nodes         []orchestrator.ImportArgument
	Groups        []importedGroup
}

type importedSubscription struct {
	Tag  *string
	Link string
}

type importedGroup struct {
	Name                 string
	Policy               string
	PolicyParams         []paramResource
	NodeRefs             []importedNodeRef
	SubscriptionBindings []importedSubscriptionBinding
}

type importedNodeRef struct {
	SubscriptionTag string
	Key             string
}

type importedSubscriptionBinding struct {
	SubscriptionTag string
	NameFilterRegex *string
}

type daeConfigFileIssueLevel string

const (
	daeConfigIssueInfo  daeConfigFileIssueLevel = "info"
	daeConfigIssueWarn  daeConfigFileIssueLevel = "warn"
	daeConfigIssueLossy daeConfigFileIssueLevel = "lossy"
)

type daeConfigFileIssue struct {
	Level   daeConfigFileIssueLevel `json:"level"`
	Code    string                  `json:"code"`
	Message string                  `json:"message"`
}

func handleCurrentUserDAEConfigFile(rw http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromContext(r)
	if !ok {
		writeError(rw, http.StatusUnauthorized, "authentication required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		resp, err := exportDAEConfigFile(r.Context(), user)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, resp)
	case http.MethodPut:
		var req daeConfigFileImportRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		resp, err := importDAEConfigFile(r.Context(), user, &req)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, resp)
	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPut)
	}
}

func handleCurrentUserDAEConfigFilePreview(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}
	user, ok := currentUserFromContext(r)
	if !ok {
		writeError(rw, http.StatusUnauthorized, "authentication required")
		return
	}

	var req daeConfigFileImportRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := previewDAEConfigFile(r.Context(), user, &req)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, resp)
}

func exportDAEConfigFile(ctx context.Context, _ *db.User) (*daeConfigFileResponse, error) {
	tx := db.BeginReadOnlyTx(ctx)
	defer tx.Commit()

	selectedConfig, selectedDNS, selectedRouting, groups, subscriptions, independentNodes, err := loadSelectedRuntimeResources(tx)
	if err != nil {
		return nil, err
	}

	conf, warnings, err := buildNativeDAEConfig(selectedConfig, selectedDNS, selectedRouting, groups, subscriptions, independentNodes)
	if err != nil {
		return nil, err
	}

	contentBytes, err := conf.Marshal(2)
	if err != nil {
		return nil, err
	}

	filenameBase := selectedConfig.Name
	if filenameBase == "" {
		filenameBase = "dae"
	}
	filenameBase = sanitizeExportFileStem(filenameBase)
	return &daeConfigFileResponse{
		Filename: filenameBase + ".dae",
		Content:  string(contentBytes),
		Warnings: warnings,
	}, nil
}

func importDAEConfigFile(ctx context.Context, user *db.User, req *daeConfigFileImportRequest) (*daeConfigFileImportResponse, error) {
	if strings.TrimSpace(req.Content) == "" {
		return nil, fmt.Errorf("content is required")
	}

	resources, err := parseImportedDAEConfig(req)
	if err != nil {
		return nil, err
	}

	oldSubscriptions, err := listSubscriptionIDs(ctx)
	if err != nil {
		return nil, err
	}

	tx := db.BeginTx(ctx)
	newSubscriptionIDs, err := replaceImportedDAEConfigResources(ctx, tx, user, resources)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	for _, oldID := range oldSubscriptions {
		orchestrator.RemoveSubscriptionUpdateScheduler(oldID)
	}
	for _, newID := range newSubscriptionIDs {
		orchestrator.AddSubscriptionUpdateScheduler(ctx, newID)
	}

	return &daeConfigFileImportResponse{
		Imported: true,
		Warnings: resources.Warnings,
	}, nil
}

func previewDAEConfigFile(ctx context.Context, user *db.User, req *daeConfigFileImportRequest) (*daeConfigFilePreviewResponse, error) {
	if strings.TrimSpace(req.Content) == "" {
		return nil, fmt.Errorf("content is required")
	}

	resources, err := parseImportedDAEConfig(req)
	if err != nil {
		return nil, err
	}
	resolved, err := resolveImportedDAEConfig(resources)
	if err != nil {
		return nil, err
	}

	mode := "rule"
	if values := orchestrator.QueryJSONStorage(user, []string{"mode"}); len(values) > 0 && values[0] != "" {
		mode = values[0]
	}

	return &daeConfigFilePreviewResponse{
		Bundle:   daeBundleFromResolvedImportedDAEConfig(mode, resolved),
		Warnings: resolved.Warnings,
	}, nil
}

func loadSelectedRuntimeResources(d *gorm.DB) (*db.Config, *db.Dns, *db.Routing, []db.Group, []db.Subscription, []db.Node, error) {
	var selectedConfig db.Config
	if err := d.Model(&db.Config{}).Where("selected = ?", true).First(&selectedConfig).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil, nil, nil, nil, nil, fmt.Errorf("please select a config")
		}
		return nil, nil, nil, nil, nil, nil, err
	}
	var selectedDNS db.Dns
	if err := d.Model(&db.Dns{}).Where("selected = ?", true).First(&selectedDNS).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil, nil, nil, nil, nil, fmt.Errorf("please select a dns")
		}
		return nil, nil, nil, nil, nil, nil, err
	}
	var selectedRouting db.Routing
	if err := d.Model(&db.Routing{}).Where("selected = ?", true).First(&selectedRouting).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil, nil, nil, nil, nil, fmt.Errorf("please select a routing")
		}
		return nil, nil, nil, nil, nil, nil, err
	}

	parsedConfig, err := engine.Default().ParseConfig(&selectedConfig.Global, &selectedDNS.Dns, &selectedRouting.Routing)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}
	groupNames := engine.Default().NecessaryOutbounds(&parsedConfig.Routing)
	groupNames = filterReferencedGroupNames(groupNames)

	var groups []db.Group
	if err := d.Model(&db.Group{}).
		Preload("Node").
		Preload("PolicyParams").
		Preload("SubscriptionBindings").
		Preload("SubscriptionBindings.Subscription").
		Preload("SubscriptionBindings.Subscription.Node").
		Find(&groups).Error; err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	missing := map[string]struct{}{}
	for _, name := range groupNames {
		missing[name] = struct{}{}
	}
	for _, group := range groups {
		delete(missing, group.Name)
	}
	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for name := range missing {
			names = append(names, name)
		}
		sort.Strings(names)
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("groups not defined but referenced by routing: %s", strings.Join(names, ", "))
	}

	var subscriptions []db.Subscription
	if err := d.Model(&db.Subscription{}).Preload("Node").Find(&subscriptions).Error; err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	var independentNodes []db.Node
	if err := d.Model(&db.Node{}).Where("subscription_id IS NULL").Find(&independentNodes).Error; err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	return &selectedConfig, &selectedDNS, &selectedRouting, groups, subscriptions, independentNodes, nil
}

func resolveImportedDAEConfig(resources *importedDAEConfigResources) (*resolvedImportedDAEConfig, error) {
	resolved := &resolvedImportedDAEConfig{
		Warnings:      append([]daeConfigFileIssue(nil), resources.Warnings...),
		ConfigName:    resources.ConfigName,
		DNSName:       resources.DNSName,
		RoutingName:   resources.RoutingName,
		Global:        resources.Global,
		DNS:           resources.DNS,
		Routing:       resources.Routing,
		Subscriptions: make([]resolvedImportedSubscription, 0, len(resources.Subscriptions)),
		Nodes:         make([]resolvedImportedNode, 0),
		Groups:        make([]resolvedImportedGroup, 0, len(resources.Groups)),
	}

	tagToNodeList := map[string][]string{}
	nodeKeyByDialer := map[string]string{}
	manualNodeByName := map[string]string{}

	for _, item := range resources.Subscriptions {
		subTag := ""
		if item.Tag != nil {
			subTag = *item.Tag
		}
		resolved.Subscriptions = append(resolved.Subscriptions, resolvedImportedSubscription{
			Tag:        item.Tag,
			Link:       item.Link,
			CronExp:    "10 */6 * * *",
			CronEnable: true,
			Status:     "",
			Info:       "",
		})

		links, err := orchestrator.FetchSubscriptionLinks(item.Link)
		if err != nil {
			return nil, fmt.Errorf("fetch subscription %q: %w", item.Link, err)
		}
		for _, link := range links {
			model, err := db.NewNodeModel(link, nil, nil)
			if err != nil {
				return nil, err
			}
			key := resolvedNodeKey(subTag, link)
			resolved.Nodes = append(resolved.Nodes, resolvedImportedNode{
				Key:             key,
				SubscriptionTag: subTag,
				RawDialerLink:   link,
				Tag:             nil,
				Link:            link,
				Name:            model.Name,
				Address:         model.Address,
				Protocol:        model.Protocol,
			})
			tagToNodeList[subTag] = append(tagToNodeList[subTag], link)
			nodeKeyByDialer[dialerNodeKey(subTag, link)] = key
		}
	}

	for _, item := range resources.Nodes {
		model, err := db.NewNodeModel(item.Link, item.Tag, nil)
		if err != nil {
			return nil, err
		}
		nameKey := model.Name
		rawDialerLink := item.Link
		if item.Tag != nil && *item.Tag != "" {
			nameKey = *item.Tag
			rawDialerLink = *item.Tag + ":" + item.Link
		}
		key := resolvedNodeKey("", rawDialerLink)
		resolved.Nodes = append(resolved.Nodes, resolvedImportedNode{
			Key:             key,
			SubscriptionTag: "",
			RawDialerLink:   rawDialerLink,
			Tag:             item.Tag,
			Link:            item.Link,
			Name:            model.Name,
			Address:         model.Address,
			Protocol:        model.Protocol,
		})
		manualNodeByName[nameKey] = key
		tagToNodeList[""] = append(tagToNodeList[""], rawDialerLink)
		nodeKeyByDialer[dialerNodeKey("", rawDialerLink)] = key
	}

	dialerSet := outbound.NewDialerSetFromLinks(&dialer.GlobalOption{Log: logrus.New()}, tagToNodeList)
	defer dialerSet.Close()

	for _, group := range resources.Groups {
		resolvedGroup := resolvedImportedGroup{
			Name:                 group.Name,
			Policy:               group.Policy,
			PolicyParams:         append([]paramResource(nil), group.PolicyParams...),
			SubscriptionBindings: append([]importedSubscriptionBinding(nil), group.SubscriptionBindings...),
		}
		nodeKeys := map[string]struct{}{}
		for _, ref := range group.NodeRefs {
			switch {
			case ref.SubscriptionTag == "" && ref.Key != "":
				key, ok := manualNodeByName[ref.Key]
				if !ok {
					return nil, fmt.Errorf("group %q references unknown node %q", group.Name, ref.Key)
				}
				nodeKeys[key] = struct{}{}
			case ref.SubscriptionTag == "*" && ref.Key == "*":
				for _, node := range resolved.Nodes {
					nodeKeys[node.Key] = struct{}{}
				}
			case ref.SubscriptionTag == "*" && ref.Key != "":
				parsedFilters, err := parseFilterExpression(ref.Key)
				if err != nil {
					return nil, fmt.Errorf("group %q filter %q: %w", group.Name, ref.Key, err)
				}
				dialers, _, err := dialerSet.FilterAndAnnotate([][]*config_parser.Function{parsedFilters}, [][]*config_parser.Param{{}})
				if err != nil {
					return nil, fmt.Errorf("group %q filter %q: %w", group.Name, ref.Key, err)
				}
				for _, matched := range dialers {
					if key, ok := nodeKeyByDialer[dialerNodeKey(matched.Property().SubscriptionTag, matched.Property().Link)]; ok {
						nodeKeys[key] = struct{}{}
					}
				}
			default:
				return nil, fmt.Errorf("unsupported resolved node ref in group %q", group.Name)
			}
		}

		keys := make([]string, 0, len(nodeKeys))
		for key := range nodeKeys {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		resolvedGroup.NodeKeys = keys
		resolved.Groups = append(resolved.Groups, resolvedGroup)
	}

	return resolved, nil
}

func buildNativeDAEConfig(selectedConfig *db.Config, selectedDNS *db.Dns, selectedRouting *db.Routing, groups []db.Group, subscriptions []db.Subscription, independentNodes []db.Node) (*daeConfig.Config, []daeConfigFileIssue, error) {
	conf, err := engine.Default().ParseConfig(&selectedConfig.Global, &selectedDNS.Dns, &selectedRouting.Routing)
	if err != nil {
		return nil, nil, err
	}

	exportState := newNativeExportState(groups, subscriptions, independentNodes)
	warnings := exportState.populate(conf)
	return conf, warnings, nil
}

func daeBundleFromResolvedImportedDAEConfig(mode string, resolved *resolvedImportedDAEConfig) daeBundle {
	bundle := daeBundle{
		SchemaVersion: daeBundleSchemaVersion,
		ExportedAt:    time.Now().Format(time.RFC3339Nano),
		Mode:          mode,
	}
	bundle.Configs = []daeBundleConfig{{
		ID:     1,
		Name:   resolved.ConfigName,
		Global: resolved.Global,
	}}
	bundle.DNSS = []daeBundleDNS{{
		ID:   1,
		Name: resolved.DNSName,
		DNS:  resolved.DNS,
	}}
	bundle.Routings = []daeBundleRouting{{
		ID:      1,
		Name:    resolved.RoutingName,
		Routing: resolved.Routing,
	}}
	bundle.Selected.ConfigID = uintPtr(1)
	bundle.Selected.DNSID = uintPtr(1)
	bundle.Selected.RoutingID = uintPtr(1)
	bundle.Defaults.ConfigID = uintPtr(1)
	bundle.Defaults.DNSID = uintPtr(1)
	bundle.Defaults.RoutingID = uintPtr(1)

	subscriptionIDByTag := map[string]uint{}
	for i, subscription := range resolved.Subscriptions {
		id := uint(i + 1)
		bundle.Subscriptions = append(bundle.Subscriptions, daeBundleSub{
			ID:         id,
			UpdatedAt:  time.Now().Format(time.RFC3339Nano),
			Link:       subscription.Link,
			CronExp:    subscription.CronExp,
			CronEnable: subscription.CronEnable,
			Status:     subscription.Status,
			Info:       subscription.Info,
			Tag:        subscription.Tag,
		})
		if subscription.Tag != nil {
			subscriptionIDByTag[*subscription.Tag] = id
		}
	}

	nodeIDByKey := map[string]uint{}
	for i, node := range resolved.Nodes {
		id := uint(i + 1)
		nodeIDByKey[node.Key] = id
		var subscriptionID *uint
		if node.SubscriptionTag != "" {
			if id, ok := subscriptionIDByTag[node.SubscriptionTag]; ok {
				subscriptionID = &id
			}
		}
		bundle.Nodes = append(bundle.Nodes, daeBundleNode{
			ID:             id,
			Link:           node.Link,
			Name:           node.Name,
			Address:        node.Address,
			Protocol:       node.Protocol,
			Tag:            node.Tag,
			SubscriptionID: subscriptionID,
		})
	}

	for i, group := range resolved.Groups {
		id := uint(i + 1)
		nodeIDs := make([]uint, 0, len(group.NodeKeys))
		for _, key := range group.NodeKeys {
			if nodeID, ok := nodeIDByKey[key]; ok {
				nodeIDs = append(nodeIDs, nodeID)
			}
		}
		bindings := make([]daeBundleGroupSubscription, 0, len(group.SubscriptionBindings))
		for _, binding := range group.SubscriptionBindings {
			if subscriptionID, ok := subscriptionIDByTag[binding.SubscriptionTag]; ok {
				bindings = append(bindings, daeBundleGroupSubscription{
					SubscriptionID:  subscriptionID,
					NameFilterRegex: binding.NameFilterRegex,
				})
			}
		}
		bundle.Groups = append(bundle.Groups, daeBundleGroup{
			ID:                   id,
			Name:                 group.Name,
			Policy:               group.Policy,
			PolicyParams:         group.PolicyParams,
			NodeIDs:              nodeIDs,
			SubscriptionBindings: bindings,
		})
	}
	if len(bundle.Groups) > 0 {
		bundle.Defaults.GroupID = uintPtr(bundle.Groups[0].ID)
	}
	return bundle
}

type nativeExportState struct {
	groups           []db.Group
	subscriptions    []db.Subscription
	independentNodes []db.Node
	used             map[string]struct{}
}

func newNativeExportState(groups []db.Group, subscriptions []db.Subscription, independentNodes []db.Node) *nativeExportState {
	return &nativeExportState{
		groups:           groups,
		subscriptions:    subscriptions,
		independentNodes: independentNodes,
		used:             make(map[string]struct{}),
	}
}

func (s *nativeExportState) populate(conf *daeConfig.Config) []daeConfigFileIssue {
	var warnings []daeConfigFileIssue
	conf.Subscription = nil
	conf.Node = nil
	conf.Group = nil

	subTagByID := make(map[uint]string)
	manualNodeNameByID := make(map[uint]string)

	seenSubscriptions := map[uint]*db.Subscription{}
	seenIndependentNodes := map[uint]*db.Node{}
	for i := range s.subscriptions {
		subscription := s.subscriptions[i]
		seenSubscriptions[subscription.ID] = &subscription
	}
	for i := range s.independentNodes {
		node := s.independentNodes[i]
		seenIndependentNodes[node.ID] = &node
	}
	for i := range s.groups {
		for _, binding := range s.groups[i].SubscriptionBindings {
			sub := binding.Subscription
			seenSubscriptions[sub.ID] = &sub
		}
		for _, node := range s.groups[i].Node {
			if node.SubscriptionID == nil {
				n := node
				seenIndependentNodes[node.ID] = &n
			}
		}
	}

	subIDs := make([]uint, 0, len(seenSubscriptions))
	for id := range seenSubscriptions {
		subIDs = append(subIDs, id)
	}
	sort.Slice(subIDs, func(i, j int) bool { return subIDs[i] < subIDs[j] })
	for _, id := range subIDs {
		sub := seenSubscriptions[id]
		tag := exportTag(sub.Tag, "sub", sub.ID, s.used)
		subTagByID[sub.ID] = tag
		if sub.Tag == nil || *sub.Tag == "" {
			warnings = append(warnings, daeConfigFileIssue{
				Level:   daeConfigIssueWarn,
				Code:    "subscription_tag_synthesized",
				Message: fmt.Sprintf("subscription %q exported with synthesized tag %q", sub.Link, tag),
			})
		}
		conf.Subscription = append(conf.Subscription, daeConfig.KeyableString(fmt.Sprintf("%s:%s", tag, sub.Link)))
	}

	nodeIDs := make([]uint, 0, len(seenIndependentNodes))
	for id := range seenIndependentNodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Slice(nodeIDs, func(i, j int) bool { return nodeIDs[i] < nodeIDs[j] })
	for _, id := range nodeIDs {
		node := seenIndependentNodes[id]
		tag := exportTag(node.Tag, "node", node.ID, s.used)
		manualNodeNameByID[node.ID] = tag
		if node.Tag == nil || *node.Tag == "" {
			warnings = append(warnings, daeConfigFileIssue{
				Level:   daeConfigIssueWarn,
				Code:    "node_tag_synthesized",
				Message: fmt.Sprintf("node %q exported with synthesized tag %q", node.Name, tag),
			})
		}
		conf.Node = append(conf.Node, daeConfig.KeyableString(fmt.Sprintf("%s:%s", tag, node.Link)))
	}

	for i := range s.groups {
		group := s.groups[i]
		exported := daeConfig.Group{
			Name:   group.Name,
			Policy: exportPolicy(group.Policy, group.PolicyParams),
		}

		appendFilter := func(filters []*config_parser.Function) {
			exported.Filter = append(exported.Filter, filters)
			exported.FilterAnnotation = append(exported.FilterAnnotation, []*config_parser.Param{})
		}

		manualNames := make([]*config_parser.Param, 0)
		for _, node := range group.Node {
			if node.SubscriptionID == nil {
				if name, ok := manualNodeNameByID[node.ID]; ok {
					manualNames = append(manualNames, &config_parser.Param{Val: name})
				}
				continue
			}
			warnings = append(warnings, daeConfigFileIssue{
				Level:   daeConfigIssueLossy,
				Code:    "group_subscription_node_flattened_on_export",
				Message: fmt.Sprintf("group %q contains subscription-backed manual node %q; exported as name filter against node name", group.Name, node.Name),
			})
			appendFilter([]*config_parser.Function{{
				Name: "name",
				Params: []*config_parser.Param{
					{Val: node.Name},
				},
			}})
		}
		if len(manualNames) > 0 {
			appendFilter([]*config_parser.Function{{
				Name:   "name",
				Params: manualNames,
			}})
		}

		for _, binding := range group.SubscriptionBindings {
			subTag := subTagByID[binding.SubscriptionID]
			filter := []*config_parser.Function{{
				Name: "subtag",
				Params: []*config_parser.Param{
					{Val: subTag},
				},
			}}
			if binding.NameFilterRegex != nil && *binding.NameFilterRegex != "" {
				filter = append(filter, &config_parser.Function{
					Name: "name",
					Params: []*config_parser.Param{
						{Key: "regex", Val: *binding.NameFilterRegex},
					},
				})
			}
			appendFilter(filter)
		}

		conf.Group = append(conf.Group, exported)
	}

	return warnings
}

func exportPolicy(policy string, params []db.GroupPolicyParam) daeConfig.FunctionListOrString {
	if len(params) == 0 {
		return policy
	}
	items := make([]*config_parser.Param, 0, len(params))
	for _, param := range params {
		items = append(items, &config_parser.Param{
			Key: param.Key,
			Val: param.Value,
		})
	}
	return &config_parser.Function{
		Name:   policy,
		Params: items,
	}
}

func exportTag(existing *string, prefix string, id uint, used map[string]struct{}) string {
	if existing != nil && *existing != "" {
		used[*existing] = struct{}{}
		return *existing
	}
	base := sanitizeExportIdentifier(fmt.Sprintf("%s_%d", prefix, id))
	tag := base
	for i := 1; ; i++ {
		if _, ok := used[tag]; !ok {
			used[tag] = struct{}{}
			return tag
		}
		tag = fmt.Sprintf("%s_%d", base, i)
	}
}

func sanitizeExportIdentifier(raw string) string {
	var builder strings.Builder
	for i, r := range raw {
		valid := r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9')
		if valid {
			builder.WriteRune(r)
			continue
		}
		builder.WriteRune('_')
	}
	sanitized := builder.String()
	if sanitized == "" || !regexp.MustCompile(`^[A-Za-z_]`).MatchString(string(sanitized[0])) {
		sanitized = "_" + sanitized
	}
	return sanitized
}

func sanitizeExportFileStem(raw string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", " ", "_", ":", "_")
	stem := replacer.Replace(strings.TrimSpace(raw))
	stem = strings.Trim(stem, "._-")
	if stem == "" {
		return "dae"
	}
	return stem
}

func resolvedNodeKey(subscriptionTag string, rawDialerLink string) string {
	return subscriptionTag + "\x00" + rawDialerLink
}

func dialerNodeKey(subscriptionTag string, rawDialerLink string) string {
	return subscriptionTag + "\x00" + rawDialerLink
}

func uintPtr(value uint) *uint {
	return &value
}

func parseImportedDAEConfig(req *daeConfigFileImportRequest) (*importedDAEConfigResources, error) {
	sections, err := config_parser.Parse(req.Content)
	if err != nil {
		return nil, err
	}
	for _, section := range sections {
		if section.Name == "include" {
			return nil, fmt.Errorf("include section is not supported when importing a single dae config file")
		}
	}
	conf, err := daeConfig.New(sections)
	if err != nil {
		return nil, err
	}

	namePrefix := strings.TrimSpace(req.NamePrefix)
	if namePrefix == "" {
		namePrefix = strings.TrimSuffix(filepath.Base(req.Filename), filepath.Ext(req.Filename))
	}
	if namePrefix == "" {
		namePrefix = "imported"
	}
	namePrefix = sanitizeExportIdentifier(namePrefix)

	globalSection, err := marshalDAESection("global", conf.Global)
	if err != nil {
		return nil, err
	}
	dnsSection, err := marshalDAESection("dns", conf.Dns)
	if err != nil {
		return nil, err
	}
	routingSection, err := marshalDAESection("routing", conf.Routing)
	if err != nil {
		return nil, err
	}

	resources := &importedDAEConfigResources{
		ConfigName:  namePrefix,
		DNSName:     namePrefix + "_dns",
		RoutingName: namePrefix + "_routing",
		Global:      globalSection,
		DNS:         dnsSection,
		Routing:     routingSection,
	}

	manualNodeByName := map[string]importedNodeRef{}

	for _, raw := range conf.Subscription {
		tag, link := daeCommon.GetTagFromLinkLikePlaintext(string(raw))
		var tagPtr *string
		if tag != "" {
			tagCopy := tag
			tagPtr = &tagCopy
		}
		resources.Subscriptions = append(resources.Subscriptions, importedSubscription{
			Tag:  tagPtr,
			Link: link,
		})
	}

	for _, raw := range conf.Node {
		tag, link := daeCommon.GetTagFromLinkLikePlaintext(string(raw))
		var tagPtr *string
		if tag != "" {
			tagCopy := tag
			tagPtr = &tagCopy
		}
		arg := orchestrator.ImportArgument{
			Link: link,
			Tag:  tagPtr,
		}
		resources.Nodes = append(resources.Nodes, arg)
		nameKey := link
		if tag != "" {
			nameKey = tag
		} else {
			model, err := db.NewNodeModel(link, nil, nil)
			if err != nil {
				return nil, err
			}
			nameKey = model.Name
		}
		manualNodeByName[nameKey] = importedNodeRef{
			SubscriptionTag: "",
			Key:             nameKey,
		}
	}

	for _, group := range conf.Group {
		policy, params, err := importGroupPolicy(group.Policy)
		if err != nil {
			return nil, err
		}
		imported := importedGroup{
			Name:         group.Name,
			Policy:       policy,
			PolicyParams: params,
		}
		if len(group.Filter) == 0 {
			imported.NodeRefs = append(imported.NodeRefs, importedNodeRef{SubscriptionTag: "*", Key: "*"})
			resources.Warnings = append(resources.Warnings, daeConfigFileIssue{
				Level:   daeConfigIssueLossy,
				Code:    "group_empty_filter_flattened",
				Message: fmt.Sprintf("group %q has no filter; import will flatten it to all currently imported nodes", group.Name),
			})
			resources.Groups = append(resources.Groups, imported)
			continue
		}
		for _, alternative := range group.Filter {
			if binding, ok, err := supportedSubscriptionBinding(alternative); err != nil {
				return nil, err
			} else if ok {
				imported.SubscriptionBindings = append(imported.SubscriptionBindings, *binding)
				continue
			}

			nodeRefs, err := supportedManualNodeRefs(alternative, manualNodeByName)
			if err != nil {
				return nil, err
			}
			if len(nodeRefs) > 0 {
				imported.NodeRefs = append(imported.NodeRefs, nodeRefs...)
				continue
			}

			imported.NodeRefs = append(imported.NodeRefs, importedNodeRef{SubscriptionTag: "*", Key: renderImportedFilter(alternative)})
			resources.Warnings = append(resources.Warnings, daeConfigFileIssue{
				Level:   daeConfigIssueLossy,
				Code:    "group_filter_flattened",
				Message: fmt.Sprintf("group %q uses filter %q which cannot be represented natively in daed; it will be flattened to explicit node membership during import", group.Name, renderImportedFilter(alternative)),
			})
		}
		resources.Groups = append(resources.Groups, imported)
	}

	return resources, nil
}

func marshalDAESection(name string, value any) (string, error) {
	m := daeConfig.Marshaller{IndentSpace: 2}
	if err := m.MarshalSection(name, reflect.ValueOf(value), 0); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(m.Bytes())), nil
}

func importGroupPolicy(value daeConfig.FunctionListOrString) (string, []paramResource, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil, nil
	case *config_parser.Function:
		return typed.Name, paramsFromParser(typed.Params), nil
	case []*config_parser.Function:
		if len(typed) != 1 {
			return "", nil, fmt.Errorf("unsupported group policy expression")
		}
		return typed[0].Name, paramsFromParser(typed[0].Params), nil
	default:
		return "", nil, fmt.Errorf("unsupported group policy type %T", value)
	}
}

func paramsFromParser(params []*config_parser.Param) []paramResource {
	items := make([]paramResource, 0, len(params))
	for _, param := range params {
		items = append(items, paramResource{
			Key: param.Key,
			Val: param.Val,
		})
	}
	return items
}

func supportedSubscriptionBinding(filters []*config_parser.Function) (*importedSubscriptionBinding, bool, error) {
	var subtag *config_parser.Function
	var nameFilter *config_parser.Function
	for _, filter := range filters {
		switch filter.Name {
		case "subtag":
			subtag = filter
		case "name":
			nameFilter = filter
		default:
			return nil, false, nil
		}
		if filter.Not {
			return nil, false, nil
		}
	}
	if subtag == nil || len(subtag.Params) != 1 || subtag.Params[0].Key != "" {
		return nil, false, nil
	}
	binding := &importedSubscriptionBinding{
		SubscriptionTag: subtag.Params[0].Val,
	}
	if nameFilter == nil {
		return binding, true, nil
	}
	regex, ok := convertNameFilterToRegex(nameFilter)
	if !ok {
		return nil, false, nil
	}
	binding.NameFilterRegex = regex
	return binding, true, nil
}

func supportedManualNodeRefs(filters []*config_parser.Function, manualNodeByName map[string]importedNodeRef) ([]importedNodeRef, error) {
	if len(filters) != 1 {
		return nil, nil
	}
	filter := filters[0]
	if filter.Name != "name" || filter.Not {
		return nil, nil
	}
	refs := make([]importedNodeRef, 0, len(filter.Params))
	for _, param := range filter.Params {
		if param.Key != "" {
			return nil, nil
		}
		ref, ok := manualNodeByName[param.Val]
		if !ok {
			return nil, nil
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func convertNameFilterToRegex(filter *config_parser.Function) (*string, bool) {
	if filter == nil || filter.Name != "name" || filter.Not {
		return nil, false
	}
	if len(filter.Params) == 0 {
		return nil, false
	}

	exacts := make([]string, 0)
	keywords := make([]string, 0)
	regexes := make([]string, 0)
	for _, param := range filter.Params {
		switch param.Key {
		case "":
			exacts = append(exacts, regexp.QuoteMeta(param.Val))
		case "keyword":
			keywords = append(keywords, regexp.QuoteMeta(param.Val))
		case "regex":
			regexes = append(regexes, param.Val)
		default:
			return nil, false
		}
	}
	if len(regexes) > 1 {
		return nil, false
	}
	if len(regexes) == 1 && len(exacts) == 0 && len(keywords) == 0 {
		regex := regexes[0]
		return &regex, true
	}
	if len(regexes) > 0 {
		return nil, false
	}
	parts := append(exacts, keywords...)
	if len(parts) == 0 {
		return nil, false
	}
	regex := strings.Join(parts, "|")
	return &regex, true
}

func renderImportedFilter(filters []*config_parser.Function) string {
	parts := make([]string, 0, len(filters))
	for _, filter := range filters {
		parts = append(parts, filter.String(false, true, false))
	}
	return strings.Join(parts, " && ")
}

func replaceImportedDAEConfigResources(ctx context.Context, tx *gorm.DB, user *db.User, resources *importedDAEConfigResources) ([]uint, error) {
	if err := tx.Exec("DELETE FROM group_nodes").Error; err != nil {
		return nil, err
	}
	if err := tx.Exec("DELETE FROM group_subscriptions").Error; err != nil {
		return nil, err
	}
	if err := tx.Exec("DELETE FROM group_policy_params").Error; err != nil {
		return nil, err
	}
	if err := tx.Where("1 = 1").Delete(&db.Group{}).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("1 = 1").Delete(&db.Node{}).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("1 = 1").Delete(&db.Subscription{}).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("1 = 1").Delete(&db.Config{}).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("1 = 1").Delete(&db.Dns{}).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("1 = 1").Delete(&db.Routing{}).Error; err != nil {
		return nil, err
	}

	config := db.Config{Name: resources.ConfigName, Global: resources.Global, Selected: true}
	if _, err := engine.Default().ParseConfig(&config.Global, nil, nil); err != nil {
		return nil, err
	}
	if err := tx.Create(&config).Error; err != nil {
		return nil, err
	}

	dns := db.Dns{Name: resources.DNSName, Dns: resources.DNS, Selected: true}
	if _, err := engine.Default().ParseConfig(nil, &dns.Dns, nil); err != nil {
		return nil, err
	}
	if err := tx.Create(&dns).Error; err != nil {
		return nil, err
	}

	routing := db.Routing{Name: resources.RoutingName, Routing: resources.Routing, Selected: true}
	if _, err := engine.Default().ParseConfig(nil, nil, &routing.Routing); err != nil {
		return nil, err
	}
	if err := tx.Create(&routing).Error; err != nil {
		return nil, err
	}

	manualNodeMap := map[string]*db.Node{}
	tagToNodeList := map[string][]string{}
	subTagToSubscriptionID := map[string]uint{}
	newSubscriptionIDs := make([]uint, 0, len(resources.Subscriptions))

	for _, item := range resources.Subscriptions {
		model := db.Subscription{
			UpdatedAt:  time.Now(),
			Tag:        item.Tag,
			Link:       item.Link,
			Status:     "",
			Info:       "",
			CronExp:    "10 */6 * * *",
			CronEnable: true,
		}
		if err := tx.Create(&model).Error; err != nil {
			return nil, err
		}
		newSubscriptionIDs = append(newSubscriptionIDs, model.ID)
		if item.Tag != nil {
			subTagToSubscriptionID[*item.Tag] = model.ID
		}
		links, err := orchestrator.FetchSubscriptionLinks(item.Link)
		if err != nil {
			return nil, fmt.Errorf("fetch subscription %q: %w", item.Link, err)
		}
		args := make([]orchestrator.ImportArgument, 0, len(links))
		for _, link := range links {
			args = append(args, orchestrator.ImportArgument{Link: link})
			if item.Tag != nil {
				tagToNodeList[*item.Tag] = append(tagToNodeList[*item.Tag], link)
			}
		}
		results, err := orchestrator.ImportNodes(tx, true, &model.ID, args)
		if err != nil {
			return nil, err
		}
		if !hasAnyImportedNodeResults(results) {
			return nil, fmt.Errorf("subscription %q imported no usable nodes", item.Link)
		}
	}

	for _, item := range resources.Nodes {
		results, err := orchestrator.ImportNodes(tx, true, nil, []orchestrator.ImportArgument{item})
		if err != nil {
			return nil, err
		}
		if len(results) != 1 || results[0].Node == nil {
			return nil, fmt.Errorf("node %q imported no usable model", item.Link)
		}
		node := results[0].Node
		nameKey := node.Name
		if item.Tag != nil && *item.Tag != "" {
			nameKey = *item.Tag
		}
		manualNodeMap[nameKey] = node
		rawKey := item.Link
		if item.Tag != nil && *item.Tag != "" {
			rawKey = *item.Tag + ":" + item.Link
		}
		tagToNodeList[""] = append(tagToNodeList[""], rawKey)
	}

	dialerSet := outbound.NewDialerSetFromLinks(&dialer.GlobalOption{Log: logrus.New()}, tagToNodeList)
	defer dialerSet.Close()

	for _, item := range resources.Groups {
		params := make([]db.GroupPolicyParam, len(item.PolicyParams))
		for i := range params {
			params[i] = db.GroupPolicyParam{
				Key:   item.PolicyParams[i].Key,
				Value: item.PolicyParams[i].Val,
			}
		}
		model := db.Group{
			Name:         item.Name,
			Policy:       item.Policy,
			PolicyParams: params,
		}
		if err := tx.Create(&model).Error; err != nil {
			return nil, err
		}

		for _, binding := range item.SubscriptionBindings {
			subID, ok := subTagToSubscriptionID[binding.SubscriptionTag]
			if !ok {
				return nil, fmt.Errorf("group %q references unknown subscription tag %q", item.Name, binding.SubscriptionTag)
			}
			if err := tx.Create(&db.GroupSubscription{
				GroupID:         model.ID,
				SubscriptionID:  subID,
				NameFilterRegex: binding.NameFilterRegex,
			}).Error; err != nil {
				return nil, err
			}
		}

		nodeIDs := map[uint]struct{}{}
		for _, ref := range item.NodeRefs {
			if ref.SubscriptionTag == "" {
				node, ok := manualNodeMap[ref.Key]
				if !ok {
					return nil, fmt.Errorf("group %q references unknown node %q", item.Name, ref.Key)
				}
				nodeIDs[node.ID] = struct{}{}
				continue
			}

			if ref.SubscriptionTag == "*" && ref.Key == "*" {
				dialers, _, err := dialerSet.FilterAndAnnotate(nil, nil)
				if err != nil {
					return nil, err
				}
				for _, matched := range dialers {
					node, ok := importedNodeByDialer(tx, matched.Property().SubscriptionTag, matched.Property().Link)
					if !ok {
						continue
					}
					nodeIDs[node.ID] = struct{}{}
				}
				continue
			}

			filterExpr := ref.Key
			parsedFilters, err := parseFilterExpression(filterExpr)
			if err != nil {
				return nil, fmt.Errorf("group %q filter %q: %w", item.Name, filterExpr, err)
			}
			dialers, _, err := dialerSet.FilterAndAnnotate([][]*config_parser.Function{parsedFilters}, [][]*config_parser.Param{{}})
			if err != nil {
				return nil, fmt.Errorf("group %q filter %q: %w", item.Name, filterExpr, err)
			}
			for _, matched := range dialers {
				node, ok := importedNodeByDialer(tx, matched.Property().SubscriptionTag, matched.Property().Link)
				if !ok {
					continue
				}
				nodeIDs[node.ID] = struct{}{}
			}
		}

		nodes := make([]db.Node, 0, len(nodeIDs))
		for id := range nodeIDs {
			nodes = append(nodes, db.Node{ID: id})
		}
		if len(nodes) > 0 {
			if err := tx.Model(&model).Association("Node").Append(nodes); err != nil {
				return nil, err
			}
		}
	}

	currentMode := "rule"
	if values := orchestrator.QueryJSONStorage(user, []string{"mode"}); len(values) > 0 && values[0] != "" {
		currentMode = values[0]
	}
	var defaultGroupID string
	if len(resources.Groups) > 0 {
		var firstGroup db.Group
		if err := tx.Where("name = ?", resources.Groups[0].Name).First(&firstGroup).Error; err == nil {
			defaultGroupID = fmt.Sprintf("%d", firstGroup.ID)
		}
	}
	if err := setJSONStorageWithTx(tx, user, []string{"defaultConfigID", "defaultRoutingID", "defaultDNSID", "defaultGroupID", "mode"}, []string{
		fmt.Sprintf("%d", config.ID),
		fmt.Sprintf("%d", routing.ID),
		fmt.Sprintf("%d", dns.ID),
		defaultGroupID,
		currentMode,
	}); err != nil {
		return nil, err
	}

	return newSubscriptionIDs, nil
}

func hasAnyImportedNodeResults(results []*orchestrator.NodeImportResult) bool {
	for _, result := range results {
		if result != nil && result.Node != nil {
			return true
		}
	}
	return false
}

func parseFilterExpression(expr string) ([]*config_parser.Function, error) {
	sections, err := config_parser.Parse("group {\n  temp {\n    filter: " + expr + "\n    policy: random\n  }\n}")
	if err != nil {
		return nil, err
	}
	conf, err := daeConfig.New(sections)
	if err != nil {
		return nil, err
	}
	if len(conf.Group) == 0 || len(conf.Group[0].Filter) == 0 {
		return nil, fmt.Errorf("no filter parsed")
	}
	return conf.Group[0].Filter[0], nil
}

func importedNodeByDialer(d *gorm.DB, subscriptionTag string, link string) (*db.Node, bool) {
	query := d.Model(&db.Node{}).Where("link = ?", link)
	if subscriptionTag == "" {
		tag, rawLink := daeCommon.GetTagFromLinkLikePlaintext(link)
		if tag != "" {
			query = d.Model(&db.Node{}).
				Where("subscription_id is null").
				Where("link = ?", rawLink).
				Where("tag = ?", tag)
		} else {
			query = query.Where("subscription_id is null")
		}
	} else {
		query = query.Joins("inner join subscriptions on subscriptions.id = nodes.subscription_id").
			Where("subscriptions.tag = ?", subscriptionTag)
	}
	var node db.Node
	if err := query.First(&node).Error; err != nil {
		return nil, false
	}
	return &node, true
}

func filterReferencedGroupNames(names []string) []string {
	result := make([]string, 0, len(names))
	for _, name := range names {
		switch name {
		case "direct", "block", "must_rules":
			continue
		default:
			result = append(result, strings.TrimPrefix(name, "must_"))
		}
	}
	return daeCommon.Deduplicate(result)
}

func listSubscriptionIDs(ctx context.Context) ([]uint, error) {
	var subs []db.Subscription
	if err := db.DB(ctx).Select("id").Find(&subs).Error; err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(subs))
	for _, sub := range subs {
		ids = append(ids, sub.ID)
	}
	return ids, nil
}
