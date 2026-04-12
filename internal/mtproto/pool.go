package mtproto

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
)

type Pool struct {
	mu      sync.RWMutex
	clients []*telegram.Client
	apis    []*tg.Client
	wg      sync.WaitGroup
	cancel  context.CancelFunc
	rr      uint64
}

func NewPool() *Pool { return &Pool{} }

func (p *Pool) Start(parent context.Context, appID int, appHash string, size int) {
	if appID == 0 || appHash == "" || size <= 0 {
		return
	}

	ctx, cancel := context.WithCancel(parent)
	p.cancel = cancel
	for i := 0; i < size; i++ {
		i := i
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("panic recovered in mtproto client %d: %v", i, rec)
				}
			}()
			client := telegram.NewClient(appID, appHash, telegram.Options{
				SessionStorage: &session.FileStorage{Path: fmt.Sprintf("session_%d.json", i)},
				UpdateHandler:  updates.New(updates.Config{}),
			})

			if err := client.Run(ctx, func(runCtx context.Context) error {
				api := client.API()
				p.mu.Lock()
				p.clients = append(p.clients, client)
				p.apis = append(p.apis, api)
				p.mu.Unlock()

				<-runCtx.Done()
				return nil
			}); err != nil {
				log.Printf("mtproto client %d failed: %v", i, err)
			}
		}()
	}
}

func (p *Pool) ReadyCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.apis)
}

func (p *Pool) WithClient(fn func(api *tg.Client) error) error {
	p.mu.RLock()
	if len(p.apis) == 0 {
		p.mu.RUnlock()
		return fmt.Errorf("no mtproto clients available")
	}
	idx := int(atomic.AddUint64(&p.rr, 1)-1) % len(p.apis)
	api := p.apis[idx]
	p.mu.RUnlock()
	return fn(api)
}

func (p *Pool) Close() {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
}
