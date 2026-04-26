package group

import (
	"context"
	"regexp"
	"sync"

	"github.com/daeuniverse/dae-wing/common"
	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/graphql/internal"
	"github.com/daeuniverse/dae-wing/graphql/service/node"
	"github.com/daeuniverse/dae-wing/graphql/service/subscription"
	"github.com/graph-gophers/graphql-go"
)

type Resolver struct {
	*db.Group
	nodesLoaded         bool
	subscriptionsLoaded bool
	policyParamsLoaded  bool
}

type SubscriptionBindingResolver struct {
	*db.GroupSubscription
	matchedNodesOnce sync.Once
	matchedNodes     []*node.Resolver
	matchedCount     int32
	matchedErr       error
}

func NewResolver(group *db.Group) *Resolver {
	return &Resolver{Group: group}
}

func NewPreloadedResolver(group *db.Group) *Resolver {
	return &Resolver{
		Group:               group,
		nodesLoaded:         true,
		subscriptionsLoaded: true,
		policyParamsLoaded:  true,
	}
}

func (r *Resolver) ID() graphql.ID {
	return common.EncodeCursor(r.Group.ID)
}

func (r *Resolver) Name() string {
	return r.Group.Name
}

func (r *Resolver) Nodes() (rs []*node.Resolver, err error) {
	nodes := r.Group.Node
	if !r.nodesLoaded {
		if err = db.DB(context.TODO()).Model(r.Group).Association("Node").Find(&nodes); err != nil {
			return nil, err
		}
	}
	for i := range nodes {
		n := nodes[i]
		rs = append(rs, &node.Resolver{Node: &n})
	}
	return rs, nil
}

func matchedNodesForBinding(binding *db.GroupSubscription) ([]db.Node, error) {
	nodes := binding.Subscription.Node
	if binding.NameFilterRegex == nil || *binding.NameFilterRegex == "" {
		return nodes, nil
	}

	re, err := regexp.Compile(*binding.NameFilterRegex)
	if err != nil {
		return nil, err
	}

	var matched []db.Node
	for _, n := range nodes {
		if re.MatchString(n.Name) {
			matched = append(matched, n)
		}
	}
	return matched, nil
}

func (r *Resolver) Subscriptions() (rs []*SubscriptionBindingResolver, err error) {
	bindings := r.Group.SubscriptionBindings
	if !r.subscriptionsLoaded {
		if err = db.DB(context.TODO()).
			Where("group_id = ?", r.Group.ID).
			Preload("Subscription").
			Preload("Subscription.Node").
			Find(&bindings).Error; err != nil {
			return nil, err
		}
	}
	for i := range bindings {
		binding := bindings[i]
		rs = append(rs, &SubscriptionBindingResolver{GroupSubscription: &binding})
	}
	return rs, nil
}

func (r *Resolver) Policy() string {
	return r.Group.Policy
}

func (r *Resolver) PolicyParams() (rs []*internal.ParamResolver, err error) {
	params := r.Group.PolicyParams
	if !r.policyParamsLoaded {
		if err = db.DB(context.TODO()).Model(r.Group).Association("PolicyParams").Find(&params); err != nil {
			return nil, err
		}
	}
	for i := range params {
		rs = append(rs, &internal.ParamResolver{Param: params[i].Marshal()})
	}
	return rs, nil
}

func (r *SubscriptionBindingResolver) Subscription() *subscription.Resolver {
	sub := r.GroupSubscription.Subscription
	return &subscription.Resolver{Subscription: &sub}
}

func (r *SubscriptionBindingResolver) NameFilterRegex() *string {
	return r.GroupSubscription.NameFilterRegex
}

func (r *SubscriptionBindingResolver) loadMatchedNodes() error {
	r.matchedNodesOnce.Do(func() {
		matched, err := matchedNodesForBinding(r.GroupSubscription)
		if err != nil {
			r.matchedErr = err
			return
		}
		r.matchedCount = int32(len(matched))
		r.matchedNodes = make([]*node.Resolver, 0, len(matched))
		for i := range matched {
			n := matched[i]
			r.matchedNodes = append(r.matchedNodes, &node.Resolver{Node: &n})
		}
	})
	return r.matchedErr
}

func (r *SubscriptionBindingResolver) MatchedNodes() (rs []*node.Resolver, err error) {
	if err = r.loadMatchedNodes(); err != nil {
		return nil, err
	}
	return r.matchedNodes, nil
}

func (r *SubscriptionBindingResolver) MatchedCount() (int32, error) {
	if err := r.loadMatchedNodes(); err != nil {
		return 0, err
	}
	return r.matchedCount, nil
}
