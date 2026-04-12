package search

import (
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

func NewHTTPClient(proxyURL string) *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	proxyURL = strings.TrimSpace(proxyURL)
	if strings.HasPrefix(strings.ToLower(proxyURL), "socks5://") {
		u, err := url.Parse(proxyURL)
		if err != nil {
			log.Printf("invalid socks5 proxy url %q: %v", proxyURL, err)
		} else {
			dialer, derr := proxy.FromURL(u, proxy.Direct)
			if derr != nil {
				log.Printf("failed to configure socks5 proxy %q: %v", proxyURL, derr)
			} else {
				transport.DialContext = nil
				transport.Dial = dialer.Dial
				transport.Proxy = nil
			}
		}
	}

	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}
}
