package search

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/gotd/td/tg"
	"osint.bot/internal/model"
	"osint.bot/internal/mtproto"
)

type TelegramMTSource struct {
	pool *mtproto.Pool
}

func NewTelegramMTSource(pool *mtproto.Pool) Source {
	return &TelegramMTSource{pool: pool}
}

func (s *TelegramMTSource) Name() string { return "telegram_mtproto" }

func (s *TelegramMTSource) Search(ctx context.Context, query string, qType model.QueryType) ([]model.SearchHit, error) {
	if s.pool == nil || s.pool.ReadyCount() == 0 {
		return nil, nil
	}

	var hits []model.SearchHit
	err := s.pool.WithClient(func(api *tg.Client) error {
		switch {
		case qType == model.QueryPhone || looksLikePhone(query):
			h, err := searchByPhone(ctx, api, query)
			if err == nil && h != nil {
				hits = append(hits, *h)
			}
		case strings.HasPrefix(query, "@") || qType == model.QueryUniversal:
			h, err := searchByUsername(ctx, api, strings.TrimPrefix(query, "@"))
			if err == nil && h != nil {
				hits = append(hits, *h)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

func searchByPhone(ctx context.Context, api *tg.Client, query string) (*model.SearchHit, error) {
	res, err := api.ContactsImportContacts(ctx, []tg.InputContactClass{
		&tg.InputPhoneContact{
			ClientID:  1,
			Phone:     query,
			FirstName: "Unknown",
			LastName:  "User",
		},
	})
	if err != nil {
		return nil, err
	}

	users, ok := res.(*tg.ContactsImportedContacts)
	if !ok || len(users.Users) == 0 {
		return nil, nil
	}
	u := users.Users[0]
	user, ok := u.(*tg.User)
	if !ok {
		return nil, nil
	}
	fullBio := ""
	full, err := api.UsersGetFullUser(ctx, &tg.InputUserObj{UserID: user.ID, AccessHash: user.AccessHash})
	if err == nil {
		if fu, ok := full.FullUser.(*tg.UserFull); ok {
			fullBio = fu.About
		}
	}

	return &model.SearchHit{
		Source:  "telegram_mtproto",
		Title:   fmt.Sprintf("Telegram user by phone: %s %s", user.FirstName, user.LastName),
		Snippet: "Данные получены через MTProto",
		Fields: map[string]string{
			"id":       fmt.Sprintf("%d", user.ID),
			"username": user.Username,
			"phone":    user.Phone,
			"bio":      fullBio,
		},
	}, nil
}

func searchByUsername(ctx context.Context, api *tg.Client, username string) (*model.SearchHit, error) {
	if username == "" {
		return nil, nil
	}
	res, err := api.ContactsResolveUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	peerUser, ok := res.Peer.(*tg.PeerUser)
	if !ok {
		return nil, nil
	}
	var found *tg.User
	for _, u := range res.Users {
		if usr, ok := u.(*tg.User); ok && usr.ID == peerUser.UserID {
			found = usr
			break
		}
	}
	if found == nil {
		return nil, nil
	}

	fullBio := ""
	full, err := api.UsersGetFullUser(ctx, &tg.InputUserObj{UserID: found.ID, AccessHash: found.AccessHash})
	if err == nil {
		if fu, ok := full.FullUser.(*tg.UserFull); ok {
			fullBio = fu.About
		}
	}

	return &model.SearchHit{
		Source:  "telegram_mtproto",
		Title:   fmt.Sprintf("Telegram user @%s", found.Username),
		Snippet: "Данные получены через MTProto",
		Fields: map[string]string{
			"id":       fmt.Sprintf("%d", found.ID),
			"username": found.Username,
			"phone":    found.Phone,
			"bio":      fullBio,
		},
	}, nil
}

func looksLikePhone(v string) bool {
	return regexp.MustCompile(`^\+?[0-9\-\s()]{7,}$`).MatchString(strings.TrimSpace(v))
}
