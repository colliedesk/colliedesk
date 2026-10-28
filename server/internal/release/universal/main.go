// Command universal joins thin macOS executables into one universal (fat) Mach-O file, as lipo
// -create does. The release builds the client collie on Linux, where lipo is missing, and
// Claude Desktop's extension format chooses a command per platform, not per CPU, so its macOS
// binary must run on Apple silicon and Intel alike.
//
//	go run ./internal/release/universal -o collie collie-arm64 collie-amd64
package main

import (
	"debug/macho"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"os"
)

func main() {
	output := flag.String("o", "", "output file")
	flag.Parse()
	if *output == "" || flag.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: universal -o OUTPUT THIN THIN...")
		os.Exit(2)
	}
	if err := join(*output, flag.Args()); err != nil {
		fmt.Fprintf(os.Stderr, "universal: %v\n", err)
		os.Exit(1)
	}
}

// alignment of each slice, as a power of two: 2^14, the arm64 page size lipo also uses.
const alignment = 14

func join(output string, inputs []string) error {
	type slice struct {
		cpu    macho.Cpu
		subCPU uint32
		data   []byte
	}
	var slices []slice
	seen := map[macho.Cpu]bool{}
	for _, path := range inputs {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := macho.Open(path)
		if err != nil {
			return fmt.Errorf("%s is not a thin Mach-O file: %w", path, err)
		}
		cpu, subCPU, kind := file.Cpu, file.SubCpu, file.Type
		_ = file.Close()
		if kind != macho.TypeExec {
			return fmt.Errorf("%s is not an executable", path)
		}
		if seen[cpu] {
			return fmt.Errorf("%s repeats CPU %v", path, cpu)
		}
		seen[cpu] = true
		slices = append(slices, slice{cpu, subCPU, data})
	}
	if len(slices) == 0 {
		return errors.New("no input")
	}

	// fat_header and fat_arch are big-endian, whatever the slices are.
	header := make([]byte, 8+20*len(slices))
	binary.BigEndian.PutUint32(header[0:], macho.MagicFat)
	binary.BigEndian.PutUint32(header[4:], uint32(len(slices)))
	offset := alignUp(uint64(len(header)))
	body := []byte{}
	for i, s := range slices {
		if offset+uint64(len(s.data)) > 1<<32-1 {
			return errors.New("the slices exceed 4 GiB")
		}
		entry := header[8+20*i:]
		binary.BigEndian.PutUint32(entry[0:], uint32(s.cpu))
		binary.BigEndian.PutUint32(entry[4:], s.subCPU)
		binary.BigEndian.PutUint32(entry[8:], uint32(offset))
		binary.BigEndian.PutUint32(entry[12:], uint32(len(s.data)))
		binary.BigEndian.PutUint32(entry[16:], alignment)
		padding := offset - uint64(len(header)) - uint64(len(body))
		body = append(body, make([]byte, padding)...)
		body = append(body, s.data...)
		offset = alignUp(offset + uint64(len(s.data)))
	}
	return os.WriteFile(output, append(header, body...), 0o755)
}

func alignUp(n uint64) uint64 {
	const size = 1 << alignment
	return (n + size - 1) &^ (size - 1)
}
