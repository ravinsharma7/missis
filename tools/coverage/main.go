package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	patterns := []string{
		"testsuite/blackbox/*_test.go",
		"implementation/model/*_test.go",
		"implementation/store/*_test.go",
	}
	for _, pattern := range patterns {
		files, err := filepath.Glob(pattern)
		if err != nil {
			panic(err)
		}
		for _, file := range files {
			if err := scanFile(file); err != nil {
				panic(err)
			}
		}
	}
}

func scanFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var (
		currentTest string
		currentLine int
		ids         []string
		lineNo      int
	)
	flush := func() {
		if currentTest != "" && len(ids) > 0 {
			fmt.Printf("%s:%d:%s:%s\n", path, currentLine, currentTest, strings.Join(ids, " "))
		}
		currentTest = ""
		currentLine = 0
		ids = nil
	}

	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "func Test") {
			flush()
			currentTest = testName(trimmed)
			currentLine = lineNo
			continue
		}
		if currentTest != "" && strings.Contains(line, "// covers") {
			for _, token := range strings.Fields(line) {
				token = strings.TrimSuffix(token, ",")
				if strings.HasPrefix(token, "PH1-") || strings.HasPrefix(token, "N") {
					ids = append(ids, token)
				}
			}
		}
	}
	flush()
	return scanner.Err()
}

func testName(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return ""
	}
	name := fields[1]
	if idx := strings.Index(name, "("); idx >= 0 {
		name = name[:idx]
	}
	return name
}
