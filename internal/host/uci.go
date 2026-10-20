package host

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/mem"
)

type engineLine struct {
	command string
	params  string
	err     error
}

type engine struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	output chan engineLine
	log    func(string, ...any)
	done   chan struct{}
	name   string

	variants                        []string
	minThreads, maxThreads, maxHash int
}

func newEngine(cfg Config, log func(string, ...any)) (*engine, error) {
	command := exec.Command(cfg.Engine)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	engine := &engine{
		cmd: command, stdin: stdin, output: make(chan engineLine, 64),
		log: log, done: make(chan struct{}), minThreads: 1, maxThreads: 1,
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	go func() {
		command.Wait()
		close(engine.done)
	}()
	go engine.readOutput(scanner)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.uci(ctx); err != nil {
		engine.terminate()
		return nil, fmt.Errorf("UCI handshake: %w", err)
	}
	for _, command := range []string{
		"setoption name UCI_AnalyseMode value true",
		"setoption name UCI_Chess960 value true",
	} {
		if err := engine.send(command); err != nil {
			engine.terminate()
			return nil, err
		}
	}
	for _, option := range cfg.SetOption {
		if err := engine.send("setoption name " + option[0] + " value " + option[1]); err != nil {
			engine.terminate()
			return nil, err
		}
	}
	return engine, nil
}

func (engine *engine) alive() bool {
	select {
	case <-engine.done:
		return false
	default:
		return true
	}
}

func (engine *engine) send(command string) error {
	if !engine.alive() {
		return io.EOF
	}
	engine.log("<< %s", command)
	_, err := fmt.Fprintln(engine.stdin, command)
	return err
}

func (engine *engine) readOutput(scanner *bufio.Scanner) {
	defer close(engine.output)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		engine.log(">> %s", line)
		command, params, _ := strings.Cut(line, " ")
		select {
		case engine.output <- engineLine{command: command, params: params}:
		case <-engine.done:
			return
		}
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	select {
	case engine.output <- engineLine{err: err}:
	case <-engine.done:
	}
}

func (engine *engine) recv(ctx context.Context) (string, string, error) {
	select {
	case line, ok := <-engine.output:
		if !ok {
			return "", "", io.EOF
		}
		return line.command, line.params, line.err
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
}

func (engine *engine) uci(ctx context.Context) error {
	if err := engine.send("uci"); err != nil {
		return err
	}
	for {
		command, params, err := engine.recv(ctx)
		if err != nil {
			return err
		}
		if command == "option" && strings.HasPrefix(params, "name UCI_Variant ") {
			parts := strings.Fields(params)
			for index := range parts[:len(parts)-1] {
				if parts[index] == "var" {
					engine.variants = append(engine.variants, parts[index+1])
				}
			}
		}
		if command == "id" && strings.HasPrefix(params, "name ") {
			engine.name = "Native " + strings.TrimSpace(strings.TrimPrefix(params, "name "))
		}
		if command == "option" {
			name, definition, _ := strings.Cut(strings.TrimPrefix(params, "name "), " type spin ")
			if name == "Threads" || name == "Hash" {
				parts := strings.Fields(definition)
				for index := range parts[:max(0, len(parts)-1)] {
					value, err := strconv.Atoi(parts[index+1])
					if err != nil {
						continue
					}
					if name == "Threads" && parts[index] == "min" {
						engine.minThreads = value
					}
					if name == "Threads" && parts[index] == "max" {
						engine.maxThreads = value
					}
					if name == "Hash" && parts[index] == "max" {
						engine.maxHash = value
					}
				}
			}
		}
		if command == "uciok" {
			if engine.name == "" {
				return errors.New("engine did not report a UCI id name")
			}
			engine.maxThreads = min(engine.maxThreads, runtime.NumCPU())
			if engine.minThreads > engine.maxThreads {
				return errors.New("what sort of engine is this?")
			}
			if engine.maxHash > 0 {
				memory, err := mem.VirtualMemory()
				if err != nil {
					return fmt.Errorf("reading system memory: %w", err)
				}
				engine.maxHash = hashLimit(engine.maxHash, memory.Available)
			}
			return nil
		}
	}
}

func hashLimit(engineMax int, availableBytes uint64) int {
	availableMB := int(availableBytes / (1024 * 1024))
	return min(engineMax, max(availableMB-256, 16))
}

func (engine *engine) terminate() {
	if !engine.alive() {
		return
	}
	engine.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-engine.done:
	case <-time.After(2 * time.Second):
		engine.cmd.Process.Kill()
		<-engine.done
	}
}
