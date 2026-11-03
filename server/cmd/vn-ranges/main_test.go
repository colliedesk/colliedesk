package main

import (
	"strings"
	"testing"
)

func TestVietnameseReadsDelegations(t *testing.T) {
	input := strings.Join([]string{
		"apnic|VN|ipv4|14.160.0.0|2097152|20100816|allocated",
		"apnic|VN|ipv4|203.162.0.0|768|19960101|allocated",
		"apnic|VN|ipv6|2001:ee0::|32|20060901|allocated",
		"apnic|JP|ipv4|1.0.16.0|4096|20110412|allocated",
	}, "\n")
	prefixes, err := vietnamese(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, prefix := range prefixes {
		got = append(got, prefix.String())
	}
	want := "14.160.0.0/11 203.162.0.0/23 203.162.2.0/24 2001:ee0::/32"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}

func TestVietnameseRejectsMalformedLines(t *testing.T) {
	for name, line := range map[string]string{
		"ipv6 length above 128":       "apnic|VN|ipv6|2001:ee0::|200|20060901|allocated",
		"ipv6 length beyond int":      "apnic|VN|ipv6|2001:ee0::|18446744073709551615|20060901|allocated",
		"ipv4 count past 255.255.255": "apnic|VN|ipv4|255.255.255.0|512|20100816|allocated",
		"ipv4 count of zero":          "apnic|VN|ipv4|14.160.0.0|0|20100816|allocated",
		"ipv4 line with ipv6 address": "apnic|VN|ipv4|2001:ee0::|256|20100816|allocated",
		"ipv6 line with ipv4 address": "apnic|VN|ipv6|14.160.0.0|32|20100816|allocated",
	} {
		t.Run(name, func(t *testing.T) {
			if prefixes, err := vietnamese(strings.NewReader(line)); err == nil {
				t.Fatalf("accepted %q as %v", line, prefixes)
			}
		})
	}
}
