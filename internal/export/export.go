package export

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strconv"

	"osint.bot/internal/model"
)

func AsJSON(res *model.SearchResult) ([]byte, error) {
	return json.MarshalIndent(res, "", "  ")
}

func AsCSV(res *model.SearchResult) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	w := csv.NewWriter(buf)
	if err := w.Write([]string{"source", "title", "snippet", "query", "type", "duration_ms"}); err != nil {
		return nil, err
	}
	for _, h := range res.Hits {
		if err := w.Write([]string{h.Source, h.Title, h.Snippet, res.Query, string(res.Type), strconv.FormatInt(res.DurationMS, 10)}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}
