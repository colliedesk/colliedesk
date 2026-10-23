package ai

import (
	"slices"
	"testing"
)

// A word in an alphabet the conversation never used is flagged; links, email addresses and Latin
// product names in a reply to a customer who writes in another alphabet are not.
func TestForeignWordsAreWordsOfAnotherAlphabet(t *testing.T) {
	for _, c := range []struct {
		reply, known string
		want         []string
	}{
		{"Chào anh, đơn هنوز ở trạng thái Đã xác nhận.", "Đơn của tôi đâu rồi?", []string{"هنوز"}},
		{"您好，请按照 https://help.example.com/printer 的步骤，在 Settings 中重置 iPhone。", "我的打印机坏了\n无法打印", nil},
		{"Здравствуйте, см. https://help.example.com/printer или напишите support@example.com.", "Принтер не печатает", nil},
		{"Hola, reinicie la impresora.", "Mi impresora no imprime", nil},
		{"こんにちは、プリンターを再起動してください。", "プリンターが動きません", nil},
		{"プリンターを再起動してください。", "ぷりんたーがうごきません", nil},
		{"Hi Ada, Привет and 你好.", "My printer jams", []string{"Привет", "你好"}},
	} {
		if got := foreignWords(c.reply, c.known); !slices.Equal(got, c.want) && !(len(got) == 0 && len(c.want) == 0) {
			t.Errorf("foreignWords(%q) = %q, want %q", c.reply, got, c.want)
		}
	}
}

func TestFillPlaceholdersNamesThisCustomerOrNobody(t *testing.T) {
	for _, c := range []struct{ reply, name, want string }{
		{"Chào chị [customer],", "Kiều Thị Dung", "Chào chị Kiều Thị Dung,"},
		{"Chào chị [customer],", "", "Chào chị,"},
		{"[customer], xin chào", "", "xin chào"},
		{"Hi [company] team", "Ada", "Hi [company] team"},
	} {
		if got := fillPlaceholders(c.reply, c.name); got != c.want {
			t.Errorf("fillPlaceholders(%q, %q) = %q, want %q", c.reply, c.name, got, c.want)
		}
	}
}
