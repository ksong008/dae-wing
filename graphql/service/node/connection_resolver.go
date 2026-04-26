/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2023, daeuniverse Organization <team@v2raya.org>
 */

package node

import (
	"context"

	"github.com/daeuniverse/dae-wing/common"
	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/graphql/service"
	"github.com/graph-gophers/graphql-go"
	"gorm.io/gorm"
)

type ConnectionResolver struct {
	baseQuery func(context.Context) *gorm.DB

	models []db.Node
}

func NewConnectionResolver(ctx context.Context, _id *graphql.ID, _subscriptionId *graphql.ID, first *int32, _after *graphql.ID) (r *ConnectionResolver, err error) {
	var id uint
	var subscriptionId uint
	if _id != nil {
		id, err = common.DecodeCursor(*_id)
		if err != nil {
			return nil, err
		}
	}
	if _subscriptionId != nil {
		subscriptionId, err = common.DecodeCursor(*_subscriptionId)
		if err != nil {
			return nil, err
		}
	}
	baseQuery := func(ctx context.Context) *gorm.DB {
		q := db.DB(ctx).Model(&db.Node{})
		if _id != nil {
			q = q.Where("id = ?", id)
		}
		if _subscriptionId != nil {
			q = q.Where("subscription_id = ?", subscriptionId)
		} else {
			q = q.Where("subscription_id is null")
		}
		return q
	}

	q := baseQuery(ctx)
	if _after != nil {
		after, err := common.DecodeCursor(*_after)
		if err != nil {
			return nil, err
		}
		q = q.Where("id > ?", after)
	}
	if first != nil {
		q = q.Limit(int(*first))
	}
	var models []db.Node
	if err = q.Find(&models).Error; err != nil {
		return nil, err
	}
	return &ConnectionResolver{
		baseQuery: baseQuery,
		models:    models,
	}, nil
}

func (r *ConnectionResolver) TotalCount(ctx context.Context) (int32, error) {
	var count int64
	if err := r.baseQuery(ctx).Count(&count).Error; err != nil {
		return 0, err
	}
	return int32(count), nil
}
func (r *ConnectionResolver) Edges() (rs []*Resolver, err error) {
	for _, _m := range r.models {
		m := _m
		rs = append(rs, &Resolver{
			Node: &m,
		})
	}
	return rs, nil
}
func (r *ConnectionResolver) PageInfo(ctx context.Context) (pr *service.PageInfoResolver, err error) {
	if len(r.models) == 0 {
		return &service.PageInfoResolver{
			FStartCursor: nil,
			FEndCursor:   nil,
			FHasNextPage: false,
		}, nil
	}
	start := common.EncodeCursor(r.models[0].ID)
	end := common.EncodeCursor(r.models[len(r.models)-1].ID)
	// Get the last ID.
	var lastNode db.Node
	if err := r.baseQuery(ctx).Select("id").Order("id DESC").First(&lastNode).Error; err != nil {
		return nil, err
	}
	return &service.PageInfoResolver{
		FStartCursor: &start,
		FEndCursor:   &end,
		FHasNextPage: r.models[len(r.models)-1].ID < lastNode.ID,
	}, nil
}
