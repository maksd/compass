package gardener

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestController(t *testing.T) {
	defaultLogger := zap.NewNop().Sugar()
	clusterChannel := make(chan *Cluster, 1)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	ctrl, err := NewController(newFakeClient(t), "az", clusterChannel, defaultLogger)
	if err != nil {
		t.Errorf("NewController() error = %v", err)
	}

	wg := sync.WaitGroup{}
	ctrl.Run(ctx, &wg)

	assert.EqualErrorf(t, ctx.Err(), context.DeadlineExceeded.Error(), "should got error %s but got %s", context.DeadlineExceeded.Error(), ctx.Err().Error())
}
