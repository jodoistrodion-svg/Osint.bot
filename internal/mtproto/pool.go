package mtproto

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
)

type Pool struct {
	mu      sync.RWMutex
	clients []*telegram.Client
	apis    []*tg.Client
}

func NewPool() *Pool { return &Pool{} }

func (p *Pool) Start(ctx context.Context, appID int, appHash string, size int) {
	if appID == 0 || appHash == "" || size <= 0 {
		return
	}

	for i := 0; i < size; i++ {
		i := i
		go func() {
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
