package main

import (
	"context"
	"testing"
)

func TestPoolSizeIsExplicitUnlessTheURLSetsIt(t *testing.T) {
	const base = "postgres://u:p@127.0.0.1:1/db?sslmode=disable"
	for url, want := range map[string]int32{
		base:                        20,
		base + "&pool_max_conns=7":  7,
		base + "&pool_max_conns=40": 40,
	} {
		pool, err := openPool(context.Background(), url)
		if err != nil {
			t.Fatal(err)
		}
		got := pool.Config().MaxConns
		pool.Close()
		if got != want {
			t.Errorf("%s: MaxConns=%d, want %d", url, got, want)
		}
	}
}
