package follow

import (
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
)

type topologyFixture struct {
	calls []string
	fail  string
}

func (f *topologyFixture) DeclareExchange(name, kind string, durable bool) error {
	f.calls = append(f.calls, name+":"+kind)
	if !durable || f.fail == "exchange" {
		return errors.New("exchange failed")
	}
	return nil
}
func (f *topologyFixture) DeclareRelationQueue(name string) error {
	f.calls = append(f.calls, name)
	if f.fail == "queue" {
		return errors.New("queue failed")
	}
	return nil
}
func (f *topologyFixture) BindQueue(name, key, exchange string) error {
	f.calls = append(f.calls, name+":"+key+":"+exchange)
	if f.fail == key {
		return errors.New("binding failed")
	}
	return nil
}
func TestRelationTopologyDeclaresDurableQueueBeforeBindingsAndPropagatesFailure(t *testing.T) {
	f := &topologyFixture{}
	require.NoError(t, DeclareRelationTopology(f))
	require.Equal(t, []string{"twitter.events:topic", RelationQueue, RelationQueue + ":user.followed:twitter.events", RelationQueue + ":user.unfollowed:twitter.events"}, f.calls)
	for _, phase := range []string{"exchange", "queue", "user.followed", "user.unfollowed"} {
		f = &topologyFixture{fail: phase}
		require.Error(t, DeclareRelationTopology(f))
	}
}
