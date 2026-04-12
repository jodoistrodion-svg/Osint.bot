package search

import (
	"context"
	"fmt"
	"strings"

	"osint.bot/internal/model"
)

type StaticSource struct {
	name string
}

func NewStaticSource(name string) *StaticSource {
	return &StaticSource{name: name}
}

func (s *StaticSource) Name() string { return s.name }

func (s *StaticSource) Search(_ context.Context, query string, qType model.QueryType) ([]model.SearchHit, error) {
	clean := strings.TrimSpace(query)
	if clean == "" {
		return nil, nil
	}

	base := model.SearchHit{
		Source: s.name,
		Title:  fmt.Sprintf("%s match", strings.ToUpper(string(qType))),
		Fields: map[string]string{"query": clean},
	}

	switch qType {
	case model.QueryPhone:
		base.Snippet = "Найдены упоминания номера в объявлениях и мессенджерах"
		base.Fields["country"] = "RU"
		base.Fields["risk"] = "medium"
	case model.QueryEmail:
		base.Snippet = "Email встречался в открытых профилях и утечках"
		base.Fields["domains"] = "social, marketplaces"
	case model.QueryFIO:
		base.Snippet = "ФИО связано с несколькими профилями и юр.лицами"
		base.Fields["profiles"] = "3"
	case model.QueryAddress:
		base.Snippet = "По адресу есть кадастровые и бизнес-записи"
		base.Fields["objects"] = "2"
	case model.QueryCar:
		base.Snippet = "Авто встречается в объявлениях и штраф-базах"
		base.Fields["events"] = "5"
	default:
		base.Snippet = "Универсальный поиск отработал по нескольким индексам"
	}

	return []model.SearchHit{base}, nil
}
