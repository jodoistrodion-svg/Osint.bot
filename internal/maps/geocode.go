package maps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type GeocodeResult struct {
	DisplayName string  `json:"display_name"`
	Lat         float64 `json:"lat,string"`
	Lon         float64 `json:"lon,string"`
}

type Geocoder struct {
	client *http.Client
}

func NewGeocoder() *Geocoder {
	return &Geocoder{client: &http.Client{Timeout: 8 * time.Second}}
}

func (g *Geocoder) Lookup(ctx context.Context, query string) (*GeocodeResult, error) {
	u := "https://nominatim.openstreetmap.org/search?format=jsonv2&limit=1&q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "osint-bot/1.0")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geocode status: %s", resp.Status)
	}

	var arr []GeocodeResult
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		return nil, err
	}
	if len(arr) == 0 {
		return nil, fmt.Errorf("address not found")
	}
	return &arr[0], nil
}

func BuildMapLink(lat, lon float64) string {
	return fmt.Sprintf("https://www.openstreetmap.org/?mlat=%f&mlon=%f#map=17/%f/%f", lat, lon, lat, lon)
}
