package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type work struct {
	SessionID  string   `json:"sessionId"`
	Threads    int      `json:"threads"`
	Hash       int      `json:"hash"`
	MultiPV    int      `json:"multiPv"`
	Variant    string   `json:"variant"`
	InitialFEN string   `json:"initialFen"`
	Moves      []string `json:"moves"`
	Movetime   *uint64  `json:"movetime"`
	Depth      *uint64  `json:"depth"`
	Nodes      *uint64  `json:"nodes"`
}

type job struct {
	ID   string `json:"id"`
	Work work   `json:"work"`
}

func provide(ctx context.Context, cfg Config, log func(string, ...any), storeEngineName func(string, string) error, ready func()) error {
	engine, err := newEngine(cfg, log)
	if err != nil {
		return err
	}
	client := &http.Client{}
	secret := randomString(32)
	if err := registerEngine(ctx, client, cfg, engine, secret); err != nil {
		engine.terminate()
		return err
	}
	if err := storeEngineName(cfg.Engine, engine.name); err != nil {
		engine.terminate()
		return fmt.Errorf("save engine name: %w", err)
	}
	cfg.Name = engine.name
	log("Engine host ready")
	ready()

	type runningJob struct {
		cancel context.CancelFunc
		done   <-chan struct{}
	}
	var current *runningJob
	stopCurrent := func() {
		if current != nil {
			current.cancel()
			<-current.done
			current = nil
		}
	}
	defer func() {
		stopCurrent()
		engine.terminate()
	}()

	backoff := time.Second
	for {
		currentJob, ok, err := acquire(ctx, client, cfg.Broker, secret)
		if err != nil {
			log("Acquiring work: %v", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff = min(backoff+backoff/2, 10*time.Second)
			continue
		}
		backoff = time.Second
		if !ok {
			if current != nil {
				select {
				case <-current.done:
					current = nil
				default:
				}
			}
			if current == nil && engine.idleTime() > time.Duration(cfg.KeepAlive)*time.Second && engine.alive() {
				log("Terminating idle engine")
				engine.terminate()
			}
			continue
		}

		stopCurrent()
		if !engine.alive() {
			engine, err = newEngine(cfg, log)
			if err != nil {
				return err
			}
		}
		if err := engine.start(currentJob.Work); err != nil {
			return err
		}
		jobCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		current = &runningJob{cancel: cancel, done: done}
		go func(job job) {
			defer close(done)
			log("Handling job %s", job.ID)
			if err := submit(jobCtx, client, cfg.Broker, job.ID, engine); err != nil && !errors.Is(err, context.Canceled) {
				log("Submitting work: %v", err)
			}
		}(currentJob)
	}
}

func submit(ctx context.Context, client *http.Client, broker, jobID string, engine *engine) error {
	reader, writer := io.Pipe()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, broker+"/api/external-engine/work/"+jobID, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-ndjson")
	streamDone := make(chan error, 1)
	go func() {
		err := engine.stream(ctx, writer)
		writer.CloseWithError(err)
		streamDone <- err
	}()

	response, requestErr := client.Do(request)
	var responseErr error
	if response != nil {
		responseErr = checkResponse(response)
		response.Body.Close()
	}
	reader.CloseWithError(requestErr)
	streamErr := <-streamDone
	if requestErr != nil {
		return requestErr
	}
	if responseErr != nil {
		return responseErr
	}
	return streamErr
}

func registerEngine(ctx context.Context, client *http.Client, cfg Config, engine *engine, secret string) error {
	doJSON := func(method, endpoint string, body, result any) error {
		var input io.Reader
		if body != nil {
			data, err := json.Marshal(body)
			if err != nil {
				return err
			}
			input = bytes.NewReader(data)
		}
		request, err := http.NewRequestWithContext(ctx, method, endpoint, input)
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+cfg.Token)
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if err := checkResponse(response); err != nil {
			return err
		}
		if result != nil {
			return json.NewDecoder(response.Body).Decode(result)
		}
		return nil
	}
	endpoint := cfg.Host + "/api/external-engine"
	var registered []struct{ ID, Name string }
	if err := doJSON(http.MethodGet, endpoint, nil, &registered); err != nil {
		return err
	}

	if cfg.Name != "" && cfg.Name != engine.name {
		for _, candidate := range registered {
			if candidate.Name != cfg.Name {
				continue
			}
			if err := doJSON(http.MethodDelete, endpoint+"/"+candidate.ID, nil, nil); err != nil {
				return err
			}
			break
		}
	}

	allowed := map[string]bool{"chess": true, "antichess": true, "atomic": true, "crazyhouse": true, "horde": true, "kingofthehill": true, "racingkings": true, "3check": true}
	variants := make([]string, 0, len(engine.variants))
	if len(engine.variants) == 0 {
		variants = append(variants, "chess")
	} else {
		for _, variant := range engine.variants {
			if allowed[variant] {
				variants = append(variants, variant)
			}
		}
	}
	body := map[string]any{"name": engine.name, "maxThreads": cfg.MaxThreads, "maxHash": cfg.MaxHash, "variants": variants, "providerSecret": secret}
	method, registrationEndpoint := http.MethodPost, endpoint
	for _, candidate := range registered {
		if candidate.Name == engine.name {
			method, registrationEndpoint = http.MethodPut, endpoint+"/"+candidate.ID
			break
		}
	}
	return doJSON(method, registrationEndpoint, body, nil)
}

func acquire(ctx context.Context, client *http.Client, broker, secret string) (job, bool, error) {
	body, _ := json.Marshal(map[string]string{"providerSecret": secret})
	requestCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(requestCtx, http.MethodPost, broker+"/api/external-engine/work", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return job{}, false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return job{}, false, nil
	}
	if err := checkResponse(response); err != nil {
		return job{}, false, err
	}
	var acquired job
	err = json.NewDecoder(response.Body).Decode(&acquired)
	return acquired, err == nil, err
}

func checkResponse(response *http.Response) error {
	if response.StatusCode < 400 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	return fmt.Errorf("%s: %s", response.Status, strings.TrimSpace(string(body)))
}
