package prompt

import "testing"

// A password in a link's or a connection string's user part is hidden, as is one after a label written
// with a dash, a full-width colon or a no-break space; an email, a port or a time stays.
func TestRedactPasswordsInUserPartsAndAfterOtherSeparators(t *testing.T) {
	for in, want := range map[string]string{
		"login at https://admin:Secret123@host.example.com/x": "login at https://admin:" + hidden + "@host.example.com/x",
		"mysql://app:Pw9xQ2zz@db:3306/x":                      "mysql://app:" + hidden + "@db:3306/x",
		"mysql://app:123456@db/x":                             "mysql://app:" + hidden + "@db/x",
		"ssh root:Hunter22!@10.0.0.1":                         "ssh root:" + hidden + "@10.0.0.1",
		"ssh deploy:123456@build.example.com":                 "ssh deploy:" + hidden + "@build.example.com",
		"mysql://app:p@ssw0rd@db:3306/x":                      "mysql://app:" + hidden + "@db:3306/x",
		"ssh root:p@ssw0rd@10.0.0.1":                          "ssh root:" + hidden + "@10.0.0.1",
		"redis://:Hunter22@cache:6379":                        "redis://:" + hidden + "@cache:6379",
		"password - Hunter22":                                 "password - " + hidden,
		"password – Hunter22":                                 "password – " + hidden,
		"password：Hunter22":                                   "password：" + hidden,
		"password Hunter22":                                   "password " + hidden,
		"password - đổi giúp em":                              "password - đổi giúp em",
		"email me at lan@example.com":                         "email me at lan@example.com",
		"https://host:8443/x":                                 "https://host:8443/x",
		"họp 10:30 nhé":                                       "họp 10:30 nhé",
	} {
		out, changed := newRedactor(nil).sequence([]string{in})
		if out[0] != want || changed[0] != (in != want) {
			t.Errorf("%q = %q (changed %v), want %q", in, out[0], changed[0], want)
		}
	}
}
