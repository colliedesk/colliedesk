//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
)

// End-to-end builds answer DNS questions from a local file instead of the network, so a journey can
// set a custom domain's records the way its owner would. The file is read on every question. Every
// custom domain then resolves to this machine: opening its address after a check reaches the local
// server on the address's port, the one loopback address a release build refuses to dial.
//
//	COLLIEDESK_E2E_DNS_FILE   JSON {"cname": {"<host>": "<end of the CNAME chain>"}, "txt": {"<name>": ["<value>"]}}
func init() {
	path := os.Getenv("COLLIEDESK_E2E_DNS_FILE")
	if path == "" {
		return
	}
	lookupCNAME = func(_ context.Context, host string) (string, error) {
		records := readE2EDNS(path)
		if target, ok := records.CNAME[strings.ToLower(host)]; ok {
			return target, nil
		}
		return "", &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	helpCenterAddressTransport = &http.Transport{Proxy: nil, DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
		}}
	lookupTXT = func(_ context.Context, name string) ([]string, error) {
		records := readE2EDNS(path)
		if values, ok := records.TXT[strings.ToLower(name)]; ok {
			return values, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
	}
}

type e2eDNS struct {
	CNAME map[string]string   `json:"cname"`
	TXT   map[string][]string `json:"txt"`
}

// readE2EDNS reads the stand-in records; a missing or broken file has none.
func readE2EDNS(path string) e2eDNS {
	var records e2eDNS
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &records)
	}
	return records
}
