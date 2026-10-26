package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// promptPrints pins what each Version of the frame writes (the golden system and user turns): an
// evaluation run counts only for the Version it ran, so a change of what the model reads without a new
// Version would credit a run of the old prompt to the new one.
var promptPrints = map[string]string{
	"intake-2026-10-26.2": "63835f30370b6aebf36fa490bb7f013a82ae70d1e750c33ce0631e442b957fbb 26f7e29b24ed43ce999a16750fe5e1d19d342708fa8df46e8f2fe45820e3a69e",
}

// When the prompt changes, bump Version and pin the new print here; never change a print in place.
func TestThePromptVersionNamesWhatTheModelReads(t *testing.T) {
	print := ""
	for _, name := range []string{"group.system.txt", "group.user.txt"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if print != "" {
			print += " "
		}
		print += hex.EncodeToString(sum[:])
	}
	if pinned, ok := promptPrints[Version]; !ok || pinned != print {
		t.Fatalf("the prompt of %s is %s, pinned %q: bump Version and pin the new print", Version, print, pinned)
	}
}
