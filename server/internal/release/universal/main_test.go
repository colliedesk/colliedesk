package main

import (
	"bytes"
	"debug/macho"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// The universal file holds each thin executable byte for byte, which debug/macho (and the macOS
// loader) find by CPU.
func TestJoinMakesAUniversalFileOfTheThinOnes(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	if err := os.WriteFile(source, []byte("package main\n\nfunc main() { println(\"hi\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	thin := map[string]string{}
	for _, arch := range []string{"arm64", "amd64"} {
		thin[arch] = filepath.Join(dir, "hi-"+arch)
		build := exec.Command("go", "build", "-o", thin[arch], source)
		build.Env = append(os.Environ(), "GOOS=darwin", "GOARCH="+arch, "CGO_ENABLED=0")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v %s", arch, err, out)
		}
	}
	output := filepath.Join(dir, "hi")
	if err := join(output, []string{thin["arm64"], thin["amd64"]}); err != nil {
		t.Fatal(err)
	}
	fat, err := macho.OpenFat(output)
	if err != nil {
		t.Fatal(err)
	}
	defer fat.Close()
	data, _ := os.ReadFile(output)
	want := map[macho.Cpu]string{macho.CpuArm64: thin["arm64"], macho.CpuAmd64: thin["amd64"]}
	if len(fat.Arches) != 2 {
		t.Fatalf("%d slices", len(fat.Arches))
	}
	for _, arch := range fat.Arches {
		original, _ := os.ReadFile(want[arch.Cpu])
		if arch.Offset%(1<<alignment) != 0 || !bytes.Equal(data[arch.Offset:arch.Offset+arch.Size], original) {
			t.Errorf("slice %v is not the thin file at an aligned offset", arch.Cpu)
		}
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command(output).CombinedOutput(); err != nil || string(out) != "hi\n" {
			t.Errorf("the universal file does not run here: %v %q", err, out)
		}
	}
	if err := join(output, []string{thin["arm64"], thin["arm64"]}); err == nil {
		t.Error("two slices of one CPU must be refused")
	}
	if err := join(output, []string{source}); err == nil {
		t.Error("a file that is not Mach-O must be refused")
	}
}
