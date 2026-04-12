package formatter

import (
	"fmt"
	"sort"
	"strings"

	"osint.bot/internal/model"
)

const TelegramChunkSize = 3500

func RenderResult(res *model.SearchResult) []string {
	if res == nil {
		return []string{"Пустой ответ"}
	}

	var b strings.Builder
	b.WriteString("🔎 Результат поиска\n")
	b.WriteString(fmt.Sprintf("Запрос: %s\n", res.Query))
	b.WriteString(fmt.Sprintf("Тип: %s\n", res.Type))
	b.WriteString(fmt.Sprintf("Источников: %d\n", len(res.Hits)))
	b.WriteString(fmt.Sprintf("Время: %d ms\n\n", res.DurationMS))

	for i, h := range res.Hits {
		b.WriteString(fmt.Sprintf("%d) [%s] %s\n", i+1, h.Source, h.Title))
		b.WriteString(fmt.Sprintf("   %s\n", h.Snippet))
		if len(h.Fields) > 0 {
			keys := make([]string, 0, len(h.Fields))
			for k := range h.Fields {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				b.WriteString(fmt.Sprintf("   • %s: %s\n", k, h.Fields[k]))
			}
		}
		b.WriteString("\n")
	}

	return splitByLimit(b.String(), TelegramChunkSize)
}

func splitByLimit(text string, limit int) []string {
	if len(text) <= limit {
		return []string{text}
	}

	lines := strings.Split(text, "\n")
	chunks := make([]string, 0, len(lines)/5+1)
	var current strings.Builder

	for _, line := range lines {
		if current.Len()+len(line)+1 > limit {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}
