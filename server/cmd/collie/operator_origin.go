package main

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
)

// operatorOrigin reads OPERATOR_ORIGIN, the address of the operator console on its own host
// (docs/self-host/operator-console.md). Empty serves the console at APP_ORIGIN/operator/. A value that
// cannot work is an error; the console then stays at the path (serve logs it, the doctor reports it).
func operatorOrigin(appOrigin string) (string, error) {
	value := strings.TrimRight(strings.TrimSpace(os.Getenv("OPERATOR_ORIGIN")), "/")
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("OPERATOR_ORIGIN must be a scheme and a host, such as https://ops.example.com")
	}
	host := parsed.Hostname()
	local := host == "localhost" || strings.HasSuffix(host, ".localhost") || net.ParseIP(host) != nil
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && local) {
		return "", errors.New("OPERATOR_ORIGIN must use https (http only for localhost or an IP address)")
	}
	if app, err := url.Parse(appOrigin); err == nil && strings.EqualFold(app.Host, parsed.Host) {
		return "", errors.New("OPERATOR_ORIGIN must be another host than APP_ORIGIN; leave it empty to serve the console at APP_ORIGIN/operator/")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

// consoleBase is where the console's links point: OPERATOR_ORIGIN, else APP_ORIGIN/operator.
func consoleBase(appOrigin string) string {
	if origin, err := operatorOrigin(appOrigin); err == nil && origin != "" {
		return origin
	}
	return strings.TrimRight(appOrigin, "/") + "/operator"
}
