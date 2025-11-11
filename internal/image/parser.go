package image

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func ParseCRfile(path string) ([]Instruction, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open CRfile: %w", err)
	}
	defer file.Close()

	var instructions []Instruction
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 1 {
			continue
		}

		command := strings.ToUpper(parts[0])
		args := parts[1:]

		if command == "RUN" || command == "CMD" {
			args = []string{strings.TrimPrefix(line, parts[0])}
			args[0] = strings.TrimSpace(args[0])
		}

		instruction := Instruction{
			Command: command,
			Args:    args,
			Raw:     line,
		}

		if err := validateInstruction(instruction); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}

		instructions = append(instructions, instruction)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading CRfile: %w", err)
	}

	if len(instructions) == 0 {
		return nil, fmt.Errorf("CRfile is empty")
	}
	if instructions[0].Command != "FROM" {
		return nil, fmt.Errorf("CRfile must start with FROM instruction")
	}

	return instructions, nil
}

func validateInstruction(inst Instruction) error {
	validCommands := map[string]bool{
		"FROM":    true,
		"RUN":     true,
		"CMD":     true,
		"ENV":     true,
		"WORKDIR": true,
		"LABEL":   true,
		"COPY":    true,
	}

	if !validCommands[inst.Command] {
		return fmt.Errorf("unknown instruction: %s", inst.Command)
	}

	if len(inst.Args) == 0 {
		return fmt.Errorf("instruction %s requires arguments", inst.Command)
	}

	return nil
}
