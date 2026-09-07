package host

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type engineLine struct {
	command string
	params  string
	err     error
}

var engineNamePattern = regexp.MustCompile(`\w+ [\d.]+`)

type engine struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	output chan engineLine
	log    func(string, ...any)
	done   chan struct{}
	name   string

	sessionID, variant     string
	threads, hash, multiPV int
	variants               []string
	lastUsedMu             sync.Mutex
	lastUsed               time.Time
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
		log: log, done: make(chan struct{}), lastUsed: time.Now(),
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	go func() {
		command.Wait()
		close(engine.done)
	}()
	go engine.readOutput(scanner)
	if err := engine.uci(); err != nil {
		engine.terminate()
		return nil, err
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

func (engine *engine) touch() {
	engine.lastUsedMu.Lock()
	engine.lastUsed = time.Now()
	engine.lastUsedMu.Unlock()
}

func (engine *engine) idleTime() time.Duration {
	engine.lastUsedMu.Lock()
	defer engine.lastUsedMu.Unlock()
	return time.Since(engine.lastUsed)
}

func (engine *engine) send(command string) error {
	if !engine.alive() {
		return io.EOF
	}
	engine.log("%d << %s", engine.cmd.Process.Pid, command)
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
		engine.log("%d >> %s", engine.cmd.Process.Pid, line)
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

func (engine *engine) recv() (string, string, error) {
	line, ok := <-engine.output
	if !ok {
		return "", "", io.EOF
	}
	return line.command, line.params, line.err
}

func (engine *engine) uci() error {
	if err := engine.send("uci"); err != nil {
		return err
	}
	for {
		command, params, err := engine.recv()
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
			name := strings.TrimSpace(strings.TrimPrefix(params, "name "))
			if match := engineNamePattern.FindString(name); match != "" {
				name = match
			}
			engine.name = "Native " + name
		}
		if command == "uciok" {
			if engine.name == "" {
				return errors.New("engine did not report a UCI id name")
			}
			return nil
		}
	}
}

func (engine *engine) isready() error {
	if err := engine.send("isready"); err != nil {
		return err
	}
	for {
		command, _, err := engine.recv()
		if err != nil || command == "readyok" {
			return err
		}
	}
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

func (engine *engine) start(work work) error {
	if work.SessionID != engine.sessionID {
		engine.sessionID = work.SessionID
		if err := engine.send("ucinewgame"); err != nil {
			return err
		}
		if err := engine.isready(); err != nil {
			return err
		}
	}
	changed := false
	for _, option := range []struct {
		name    string
		value   int
		current *int
	}{{"Threads", work.Threads, &engine.threads}, {"Hash", work.Hash, &engine.hash}, {"MultiPV", work.MultiPV, &engine.multiPV}} {
		if *option.current != option.value {
			if err := engine.send("setoption name " + option.name + " value " + strconv.Itoa(option.value)); err != nil {
				return err
			}
			*option.current, changed = option.value, true
		}
	}
	if engine.variant != work.Variant {
		if err := engine.send("setoption name UCI_Variant value " + work.Variant); err != nil {
			return err
		}
		engine.variant, changed = work.Variant, true
	}
	if changed {
		if err := engine.isready(); err != nil {
			return err
		}
	}
	if err := engine.send("position fen " + work.InitialFEN + " moves " + strings.Join(work.Moves, " ")); err != nil {
		return err
	}
	var search string
	switch {
	case work.Movetime != nil:
		search = "movetime " + strconv.FormatUint(*work.Movetime, 10)
	case work.Depth != nil:
		search = "depth " + strconv.FormatUint(*work.Depth, 10)
	case work.Nodes != nil:
		search = "nodes " + strconv.FormatUint(*work.Nodes, 10)
	default:
		return errors.New("work has no search limit")
	}
	return engine.send("go " + search)
}

func (engine *engine) stream(ctx context.Context, output io.Writer) error {
	defer engine.touch()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	var stopTimer *time.Timer
	var stopDeadline <-chan time.Time
	var stopCause error
	requestStop := func(cause error) error {
		if stopDeadline != nil {
			return nil
		}
		stopCause = cause
		if err := engine.send("stop"); err != nil {
			return err
		}
		stopTimer = time.NewTimer(2 * time.Second)
		stopDeadline = stopTimer.C
		return nil
	}
	defer func() {
		if stopTimer != nil {
			stopTimer.Stop()
		}
	}()
	contextDone := ctx.Done()
	for {
		if contextDone != nil && ctx.Err() != nil {
			contextDone = nil
			if err := requestStop(ctx.Err()); err != nil {
				return err
			}
		}
		select {
		case <-contextDone:
			contextDone = nil
			if err := requestStop(ctx.Err()); err != nil {
				return err
			}
		case <-stopDeadline:
			engine.terminate()
			return stopCause
		case <-ticker.C:
			engine.touch()
			if stopDeadline == nil && ctx.Err() == nil {
				if _, err := io.WriteString(output, "{\"keepalive\":true}\n"); err != nil {
					if err := requestStop(err); err != nil {
						return err
					}
				}
			}
		case got, ok := <-engine.output:
			engine.touch()
			if !ok {
				return io.EOF
			}
			if got.err != nil {
				return got.err
			}
			if got.command == "bestmove" {
				return stopCause
			}
			if stopDeadline == nil && ctx.Err() == nil && got.command == "info" && strings.Contains(got.params, "score") {
				if _, err := fmt.Fprintf(output, "info %s\n", got.params); err != nil {
					if err := requestStop(err); err != nil {
						return err
					}
				}
			}
		}
	}
}
