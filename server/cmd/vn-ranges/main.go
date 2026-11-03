// Command vn-ranges rewrites internal/vnaddr/ranges.txt from APNIC's delegation file: every IPv4 and
// IPv6 block delegated to Vietnam, as CIDR prefixes. Run it from server/ to refresh the list:
//
//	go run ./cmd/vn-ranges            # downloads the file
//	go run ./cmd/vn-ranges apnic.txt  # reads a copy
package main

import (
	"bufio"
	"fmt"
	"io"
	"math/bits"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

const source = "https://ftp.apnic.net/stats/apnic/delegated-apnic-latest"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "vn-ranges:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	var input io.Reader
	if len(args) > 0 {
		file, err := os.Open(args[0])
		if err != nil {
			return err
		}
		defer file.Close()
		input = file
	} else {
		client := http.Client{Timeout: time.Minute}
		response, err := client.Get(source)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("%s answered %s", source, response.Status)
		}
		input = response.Body
	}
	prefixes, err := vietnamese(input)
	if err != nil {
		return err
	}
	if len(prefixes) < 1000 {
		return fmt.Errorf("only %d Vietnamese blocks read; the file is not APNIC's delegation list", len(prefixes))
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# Address blocks APNIC delegates to Vietnam (%s), as of %s.\n", source, time.Now().UTC().Format("2006-01-02"))
	out.WriteString("# Written by `go run ./cmd/vn-ranges`; do not edit by hand.\n")
	for _, prefix := range prefixes {
		out.WriteString(prefix.String() + "\n")
	}
	return os.WriteFile("internal/vnaddr/ranges.txt", []byte(out.String()), 0o644)
}

// vietnamese reads lines like apnic|VN|ipv4|14.160.0.0|2097152|20100816|allocated: an IPv4 line counts
// addresses (not always a power of two, so it may span several prefixes), an IPv6 line gives a length.
func vietnamese(input io.Reader) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "|")
		if len(fields) < 7 || fields[1] != "VN" || (fields[2] != "ipv4" && fields[2] != "ipv6") ||
			(fields[6] != "allocated" && fields[6] != "assigned") {
			continue
		}
		start, err := netip.ParseAddr(fields[3])
		if err != nil {
			return nil, fmt.Errorf("line %q: %w", scanner.Text(), err)
		}
		size, err := strconv.ParseUint(fields[4], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("line %q: %w", scanner.Text(), err)
		}
		switch fields[2] {
		case "ipv4":
			prefixes = append(prefixes, ipv4Prefixes(start, size)...)
		case "ipv6":
			prefixes = append(prefixes, netip.PrefixFrom(start, int(size)).Masked())
		}
	}
	return prefixes, scanner.Err()
}

// ipv4Prefixes covers count addresses from start with the fewest prefixes.
func ipv4Prefixes(start netip.Addr, count uint64) []netip.Prefix {
	bytes := start.As4()
	next := uint64(bytes[0])<<24 | uint64(bytes[1])<<16 | uint64(bytes[2])<<8 | uint64(bytes[3])
	end := next + count
	var prefixes []netip.Prefix
	for next < end {
		size := uint64(1) << bits.TrailingZeros64(next|1<<32)
		for size > end-next {
			size >>= 1
		}
		address := netip.AddrFrom4([4]byte{byte(next >> 24), byte(next >> 16), byte(next >> 8), byte(next)})
		prefixes = append(prefixes, netip.PrefixFrom(address, 32-bits.TrailingZeros64(size)))
		next += size
	}
	return prefixes
}
