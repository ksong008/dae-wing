/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2023, daeuniverse Organization <team@v2raya.org>
 */

package graphql

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/daeuniverse/dae-wing/common"
	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/defaults"
	"github.com/daeuniverse/dae-wing/graphql/internal"
	"github.com/daeuniverse/dae-wing/graphql/service/config"
	"github.com/daeuniverse/dae-wing/graphql/service/config/global"
	"github.com/daeuniverse/dae-wing/graphql/service/dns"
	"github.com/daeuniverse/dae-wing/graphql/service/group"
	"github.com/daeuniverse/dae-wing/graphql/service/node"
	"github.com/daeuniverse/dae-wing/graphql/service/routing"
	"github.com/daeuniverse/dae-wing/graphql/service/subscription"
	"github.com/daeuniverse/dae/pkg/config_parser"
	"github.com/graph-gophers/graphql-go"
	"github.com/tidwall/sjson"
)

type MutationResolver struct{}

type DefaultResourcesResolver struct {
	DefaultConfigIDV  uint
	DefaultRoutingIDV uint
	DefaultDNSIDV     uint
	DefaultGroupIDV   uint
	ModeV             string
}

func (r *DefaultResourcesResolver) DefaultConfigID() graphql.ID {
	return common.EncodeCursor(r.DefaultConfigIDV)
}

func (r *DefaultResourcesResolver) DefaultRoutingID() graphql.ID {
	return common.EncodeCursor(r.DefaultRoutingIDV)
}

func (r *DefaultResourcesResolver) DefaultDNSID() graphql.ID {
	return common.EncodeCursor(r.DefaultDNSIDV)
}

func (r *DefaultResourcesResolver) DefaultGroupID() graphql.ID {
	return common.EncodeCursor(r.DefaultGroupIDV)
}

func (r *DefaultResourcesResolver) Mode() string {
	return r.ModeV
}

func (r *MutationResolver) CreateUser(ctx context.Context, args *struct {
	Username string
	Password string
}) (token string, err error) {
	if len(args.Password) < 6 || strings.IndexFunc(args.Password, unicode.IsLetter) < 0 || strings.IndexFunc(args.Password, unicode.IsNumber) < 0 {
		return "", fmt.Errorf("too weak password; should contain numbers and letters, and no less than 6 in length")
	}
	tx := db.BeginTx(ctx)
	defer func() {
		if finishErr := db.FinishTx(tx, err); finishErr != nil {
			err = finishErr
		}
	}()
	// Check if there is already a user.
	n, err := numberUsers(tx)
	if err != nil {
		return "", err
	}
	if n > 0 {
		return "", fmt.Errorf("a user already exists")
	}
	// Hash password.
	var sec [32]byte
	if _, err = io.ReadFull(rand.Reader, sec[:]); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(sec[:])
	hashedPassword, err := hashPassword([]byte(secret), args.Password)
	if err != nil {
		return "", err
	}
	// Create user.
	if err = tx.Model(&db.User{}).Create(&db.User{
		Username:     args.Username,
		PasswordHash: hashedPassword,
		JwtSecret:    secret,
	}).Error; err != nil {
		return "", err
	}
	// Return token.
	return getToken(tx, args.Username, args.Password)
}
func (r *MutationResolver) SetJsonStorage(ctx context.Context, args *struct {
	Paths  []string
	Values []string
}) (int32, error) {
	u, err := userFromContext(ctx)
	if err != nil {
		return 0, err
	}
	if len(args.Paths) != len(args.Values) {
		return 0, fmt.Errorf("len(paths) != len(values)")
	}
	for i := range args.Paths {
		u.JsonStorage, err = sjson.Set(u.JsonStorage, args.Paths[i], args.Values[i])
		if err != nil {
			return 0, err
		}
	}
	if err = db.DB(ctx).Model(&u).Update("json_storage", u.JsonStorage).Error; err != nil {
		return 0, err
	}
	return int32(len(args.Paths)), nil
}
func (r *MutationResolver) RemoveJsonStorage(ctx context.Context, args *struct {
	Paths *[]string
}) (n int32, err error) {
	u, err := userFromContext(ctx)
	if err != nil {
		return 0, err
	}
	if args.Paths == nil {
		u.JsonStorage = "{}"
		n = 1
	} else {
		for i := range *args.Paths {
			u.JsonStorage, err = sjson.Delete(u.JsonStorage, (*args.Paths)[i])
			if err != nil {
				return 0, err
			}
		}
		n = int32(len(*args.Paths))
	}
	if err = db.DB(ctx).Model(&u).Update("json_storage", u.JsonStorage).Error; err != nil {
		return 0, err
	}
	return n, nil
}
func (r *MutationResolver) UpdateAvatar(ctx context.Context, args *struct {
	Avatar *string
}) (int32, error) {
	u, err := userFromContext(ctx)
	if err != nil {
		return 0, err
	}
	q := db.DB(ctx).Model(&u).Update("avatar", args.Avatar)
	if err = q.Error; err != nil {
		return 0, err
	}
	return int32(q.RowsAffected), nil
}
func (r *MutationResolver) UpdateName(ctx context.Context, args *struct {
	Name *string
}) (int32, error) {
	u, err := userFromContext(ctx)
	if err != nil {
		return 0, err
	}
	q := db.DB(ctx).Model(&u).Update("name", args.Name)
	if err = q.Error; err != nil {
		return 0, err
	}
	return int32(q.RowsAffected), nil
}
func (r *MutationResolver) UpdateUsername(ctx context.Context, args *struct {
	Username string
}) (int32, error) {
	u, err := userFromContext(ctx)
	if err != nil {
		return 0, err
	}
	q := db.DB(ctx).Model(&u).Update("username", args.Username)
	if err = q.Error; err != nil {
		return 0, err
	}
	return int32(q.RowsAffected), nil
}

func UpdatePassword(ctx context.Context, args *struct {
	CurrentPassword string
	NewPassword     string
}, u *db.User, skipVerify bool) (token string, err error) {
	// Check password.
	if !skipVerify {
		hashedPassword, err := hashPassword([]byte(u.JwtSecret), args.CurrentPassword)
		if err != nil {
			return "", err
		}
		if hashedPassword != u.PasswordHash {
			return "", fmt.Errorf("incorrect password")
		}
	}

	// Generate new jwt secret (to log out others) and password hash.
	var sec [32]byte
	if _, err = io.ReadFull(rand.Reader, sec[:]); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(sec[:])
	hashedPassword, err := hashPassword([]byte(secret), args.NewPassword)
	if err != nil {
		return "", err
	}
	tx := db.BeginTx(ctx)
	defer func() {
		if finishErr := db.FinishTx(tx, err); finishErr != nil {
			err = finishErr
		}
	}()
	q := tx.Model(u).Updates(db.User{
		PasswordHash: hashedPassword,
		JwtSecret:    secret,
	})
	if q.Error != nil {
		return "", q.Error
	}

	// Return token.
	return getToken(tx, u.Username, args.NewPassword)
}

func (r *MutationResolver) UpdatePassword(ctx context.Context, args *struct {
	CurrentPassword string
	NewPassword     string
}) (string, error) {
	u, err := userFromContext(ctx)
	if err != nil {
		return "", err
	}
	return UpdatePassword(ctx, args, u, false)
}
func (r *MutationResolver) CreateConfig(ctx context.Context, args *struct {
	Name   *string
	Global *global.Input
}) (c *config.Resolver, err error) {
	var strName string
	if args.Name != nil {
		strName = *args.Name
	}
	return config.Create(ctx, strName, args.Global)
}

func (r *MutationResolver) UpdateConfig(ctx context.Context, args *struct {
	ID     graphql.ID
	Global global.Input
}) (*config.Resolver, error) {
	return config.Update(ctx, args.ID, args.Global)
}

func (r *MutationResolver) RenameConfig(ctx context.Context, args *struct {
	ID   graphql.ID
	Name string
}) (int32, error) {
	return config.Rename(ctx, args.ID, args.Name)
}

func (r *MutationResolver) RemoveConfig(ctx context.Context, args *struct {
	ID graphql.ID
}) (int32, error) {
	return config.Remove(ctx, args.ID)
}

func (r *MutationResolver) SelectConfig(ctx context.Context, args *struct {
	ID graphql.ID
}) (int32, error) {
	return config.Select(ctx, args.ID)
}

func (r *MutationResolver) Run(ctx context.Context, args *struct {
	Dry bool
}) (int32, error) {
	return config.Run(ctx, args.Dry)
}

func (r *MutationResolver) CreateDns(ctx context.Context, args *struct {
	Name *string
	Dns  *string
}) (c *dns.Resolver, err error) {
	var strDns, strName string
	if args.Dns != nil {
		strDns = *args.Dns
	}
	if args.Name != nil {
		strName = *args.Name
	}
	return dns.Create(ctx, strName, strDns)
}

func (r *MutationResolver) UpdateDns(ctx context.Context, args *struct {
	ID  graphql.ID
	Dns string
}) (*dns.Resolver, error) {
	return dns.Update(ctx, args.ID, args.Dns)
}

func (r *MutationResolver) RenameDns(ctx context.Context, args *struct {
	ID   graphql.ID
	Name string
}) (int32, error) {
	return dns.Rename(ctx, args.ID, args.Name)
}

func (r *MutationResolver) RemoveDns(ctx context.Context, args *struct {
	ID graphql.ID
}) (int32, error) {
	return dns.Remove(ctx, args.ID)
}

func (r *MutationResolver) SelectDns(ctx context.Context, args *struct {
	ID graphql.ID
}) (int32, error) {
	return dns.Select(ctx, args.ID)
}

func (r *MutationResolver) CreateRouting(ctx context.Context, args *struct {
	Name    *string
	Routing *string
}) (c *routing.Resolver, err error) {
	var strRouting, strName string
	if args.Routing != nil {
		strRouting = *args.Routing
	}
	if args.Name != nil {
		strName = *args.Name
	}
	return routing.Create(ctx, strName, strRouting)
}

func (r *MutationResolver) UpdateRouting(ctx context.Context, args *struct {
	ID      graphql.ID
	Routing string
}) (*routing.Resolver, error) {
	return routing.Update(ctx, args.ID, args.Routing)
}

func (r *MutationResolver) RenameRouting(ctx context.Context, args *struct {
	ID   graphql.ID
	Name string
}) (int32, error) {
	return routing.Rename(ctx, args.ID, args.Name)
}

func (r *MutationResolver) RemoveRouting(ctx context.Context, args *struct {
	ID graphql.ID
}) (int32, error) {
	return routing.Remove(ctx, args.ID)
}

func (r *MutationResolver) SelectRouting(ctx context.Context, args *struct {
	ID graphql.ID
}) (int32, error) {
	return routing.Select(ctx, args.ID)
}

func (r *MutationResolver) ImportNodes(ctx context.Context, args *struct {
	RollbackError bool
	Args          []*internal.ImportArgument
}) ([]*node.ImportResult, error) {
	tx := db.BeginTx(ctx)
	result, err := node.Import(tx, args.RollbackError, nil, args.Args)
	if finishErr := db.FinishTx(tx, err); finishErr != nil {
		return nil, finishErr
	}
	return result, nil
}

func (r *MutationResolver) UpdateNode(ctx context.Context, args *struct {
	ID      graphql.ID
	NewLink string
}) (*node.Resolver, error) {
	tx := db.BeginTx(ctx)
	result, err := node.Update(tx, args.ID, args.NewLink)
	if finishErr := db.FinishTx(tx, err); finishErr != nil {
		return nil, finishErr
	}
	return result, nil
}

func (r *MutationResolver) TestNodeLatencies(ctx context.Context, args *struct {
	IDs *[]graphql.ID
}) ([]*node.LatencyResolver, error) {
	return node.TestLatencies(ctx, args.IDs)
}

func (r *MutationResolver) RemoveNodes(ctx context.Context, args *struct {
	IDs []graphql.ID
}) (int32, error) {
	return node.Remove(ctx, args.IDs)
}

func (r *MutationResolver) TagNode(ctx context.Context, args *struct {
	ID  graphql.ID
	Tag string
}) (int32, error) {
	return node.Tag(ctx, args.ID, args.Tag)
}

func (r *MutationResolver) ImportSubscription(ctx context.Context, args *struct {
	RollbackError bool
	Arg           internal.ImportArgument
}) (*subscription.ImportResult, error) {
	tx := db.BeginTx(ctx)
	result, err := subscription.Import(tx, args.RollbackError, &args.Arg)
	if finishErr := db.FinishTx(tx, err); finishErr != nil {
		return nil, finishErr
	}
	schedulerCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	subscription.UpdateAll(schedulerCtx)
	return result, nil
}

func (r *MutationResolver) UpdateSubscription(ctx context.Context, args *struct {
	ID graphql.ID
}) (*subscription.Resolver, error) {
	return subscription.Update(ctx, args.ID)
}

func (r *MutationResolver) UpdateSubscriptionLink(ctx context.Context, args *struct {
	ID   graphql.ID
	Link string
}) (*subscription.Resolver, error) {
	return subscription.UpdateLink(ctx, args.ID, args.Link)
}

func (r *MutationResolver) UpdateSubscriptionCron(ctx context.Context, args *struct {
	ID         graphql.ID
	CronExp    string
	CronEnable bool
}) (*subscription.Resolver, error) {
	return subscription.UpdateCron(ctx, args.ID, args.CronExp, args.CronEnable)
}

func (r *MutationResolver) RemoveSubscriptions(ctx context.Context, args *struct {
	IDs []graphql.ID
}) (int32, error) {
	return subscription.Remove(ctx, args.IDs)
}

func (r *MutationResolver) TagSubscription(ctx context.Context, args *struct {
	ID  graphql.ID
	Tag string
}) (int32, error) {
	return subscription.Tag(ctx, args.ID, args.Tag)
}

func (r *MutationResolver) CreateGroup(ctx context.Context, args *struct {
	Name         string
	Policy       string
	PolicyParams *[]struct {
		Key *string
		Val string
	}
}) (*group.Resolver, error) {
	var policyParams []config_parser.Param
	if args.PolicyParams != nil {
		// Convert.
		var params []config_parser.Param
		for _, p := range *args.PolicyParams {
			var k string
			if p.Key != nil {
				k = *p.Key
			}
			params = append(params, config_parser.Param{
				Key: k,
				Val: p.Val,
			})
		}
		policyParams = params
	}
	return group.Create(ctx, args.Name, args.Policy, policyParams)
}

func (r *MutationResolver) EnsureDefaultResources(ctx context.Context, args *struct {
	ConfigName   string
	Global       global.Input
	DnsName      string
	Dns          string
	RoutingName  string
	Routing      string
	GroupName    string
	Policy       string
	PolicyParams *[]struct {
		Key *string
		Val string
	}
	Mode string
}) (*DefaultResourcesResolver, error) {
	u, err := userFromContext(ctx)
	if err != nil {
		return nil, err
	}

	globalString, err := args.Global.Marshal()
	if err != nil {
		return nil, err
	}

	var policyParams []config_parser.Param
	if args.PolicyParams != nil {
		for _, p := range *args.PolicyParams {
			var k string
			if p.Key != nil {
				k = *p.Key
			}
			policyParams = append(policyParams, config_parser.Param{
				Key: k,
				Val: p.Val,
			})
		}
	}

	result, err := defaults.Ensure(ctx, u.ID, defaults.EnsureInput{
		ConfigName:   args.ConfigName,
		Global:       globalString,
		DnsName:      args.DnsName,
		Dns:          args.Dns,
		RoutingName:  args.RoutingName,
		Routing:      args.Routing,
		GroupName:    args.GroupName,
		Policy:       args.Policy,
		PolicyParams: policyParams,
		Mode:         args.Mode,
	})
	if err != nil {
		return nil, err
	}

	return &DefaultResourcesResolver{
		DefaultConfigIDV:  result.DefaultConfigID,
		DefaultRoutingIDV: result.DefaultRoutingID,
		DefaultDNSIDV:     result.DefaultDNSID,
		DefaultGroupIDV:   result.DefaultGroupID,
		ModeV:             result.Mode,
	}, nil
}

func (r *MutationResolver) GroupSetPolicy(ctx context.Context, args *struct {
	ID           graphql.ID
	Policy       string
	PolicyParams *[]struct {
		Key *string
		Val string
	}
}) (int32, error) {
	var policyParams []config_parser.Param
	if args.PolicyParams != nil {
		// Convert.
		var params []config_parser.Param
		for _, p := range *args.PolicyParams {
			var k string
			if p.Key != nil {
				k = *p.Key
			}
			params = append(params, config_parser.Param{
				Key: k,
				Val: p.Val,
			})
		}
		policyParams = params
	}
	return group.SetPolicy(ctx, args.ID, args.Policy, policyParams)
}

func (r *MutationResolver) RemoveGroup(ctx context.Context, args *struct {
	ID graphql.ID
}) (int32, error) {
	return group.Remove(ctx, args.ID)
}

func (r *MutationResolver) RenameGroup(ctx context.Context, args *struct {
	ID   graphql.ID
	Name string
}) (int32, error) {
	return group.Rename(ctx, args.ID, args.Name)
}

func (r *MutationResolver) GroupAddSubscriptions(ctx context.Context, args *struct {
	ID              graphql.ID
	SubscriptionIDs []graphql.ID
	NameFilterRegex *string
}) (int32, error) {
	return group.AddSubscriptions(ctx, args.ID, args.SubscriptionIDs, args.NameFilterRegex)
}

func (r *MutationResolver) GroupDelSubscriptions(ctx context.Context, args *struct {
	ID              graphql.ID
	SubscriptionIDs []graphql.ID
}) (int32, error) {
	return group.DelSubscriptions(ctx, args.ID, args.SubscriptionIDs)
}

func (r *MutationResolver) GroupAddNodes(ctx context.Context, args *struct {
	ID      graphql.ID
	NodeIDs []graphql.ID
}) (int32, error) {
	return group.AddNodes(ctx, args.ID, args.NodeIDs)
}

func (r *MutationResolver) GroupDelNodes(ctx context.Context, args *struct {
	ID      graphql.ID
	NodeIDs []graphql.ID
}) (int32, error) {
	return group.DelNodes(ctx, args.ID, args.NodeIDs)
}
