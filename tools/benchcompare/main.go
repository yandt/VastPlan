// benchcompare 比较同一 runner 上 base/head 的 Go benchmark 中位数。
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type sample struct{ ns, bytes, allocs float64 }

var linePattern = regexp.MustCompile(`^(BenchmarkBackend_[^\s-]+(?:_[^\s-]+)*)(?:-\d+)?\s+\d+\s+([0-9.]+) ns/op(?:\s+([0-9.]+) B/op\s+([0-9.]+) allocs/op)?`)

func read(path string) (map[string][]sample, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	out := map[string][]sample{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		match := linePattern.FindStringSubmatch(strings.TrimSpace(scanner.Text()))
		if match == nil {
			continue
		}
		ns, _ := strconv.ParseFloat(match[2], 64)
		bytes, _ := strconv.ParseFloat(match[3], 64)
		allocs, _ := strconv.ParseFloat(match[4], 64)
		out[match[1]] = append(out[match[1]], sample{ns, bytes, allocs})
	}
	return out, errors.Join(scanner.Err(), file.Close())
}
func median(values []float64) float64 {
	sort.Float64s(values)
	if len(values)%2 == 1 {
		return values[len(values)/2]
	}
	return (values[len(values)/2-1] + values[len(values)/2]) / 2
}
func summarize(samples []sample) sample {
	ns := make([]float64, len(samples))
	bytes := make([]float64, len(samples))
	allocs := make([]float64, len(samples))
	for i, s := range samples {
		ns[i] = s.ns
		bytes[i] = s.bytes
		allocs[i] = s.allocs
	}
	return sample{median(ns), median(bytes), median(allocs)}
}

func main() {
	basePath := flag.String("base", "", "base benchmark output")
	currentPath := flag.String("current", "", "current benchmark output")
	flag.Parse()
	base, err := read(*basePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	current, err := read(*currentPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	failed, compared := false, 0
	for name, baseSamples := range base {
		currentSamples := current[name]
		if len(currentSamples) == 0 || len(baseSamples) == 0 {
			continue
		}
		compared++
		before, after := summarize(baseSamples), summarize(currentSamples)
		nsRatio := after.ns / before.ns
		bytesRatio, allocRatio := ratio(after.bytes, before.bytes), ratio(after.allocs, before.allocs)
		status := "PASS"
		if (nsRatio > 1.5 && after.ns-before.ns > 100) || bytesRatio > 1.25 || allocRatio > 1.25 {
			status = "FAIL"
			failed = true
		}
		fmt.Printf("%-58s %s ns %.0f→%.0f (%.2fx) B %.0f→%.0f alloc %.0f→%.0f\n", name, status, before.ns, after.ns, nsRatio, before.bytes, after.bytes, before.allocs, after.allocs)
	}
	if compared == 0 {
		fmt.Fprintln(os.Stderr, "没有可比较的 Backend benchmark")
		os.Exit(2)
	}
	if failed {
		os.Exit(1)
	}
}
func ratio(current, base float64) float64 {
	if base == 0 {
		if current == 0 {
			return 1
		}
		return 999
	}
	return current / base
}
