package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/rumpl/harness"
)

const serverReadyTimeout = 10 * time.Second

// Run streams opencode output through its local HTTP/SSE API. The opencode
// CLI's `run --format json` output only emits completed text parts, while the
// server event stream emits message.part.delta events as tokens arrive.
func (p *provider) Run(ctx context.Context, prompt string, fn func(harness.Event)) error {
	p.parser = &parser{}

	providerID, modelID, err := splitModel(p.model)
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}

	port, err := freePort(ctx)
	if err != nil {
		return err
	}
	baseURL := "http://127.0.0.1:" + strconv.Itoa(port)

	serverCtx, cancelServer := context.WithCancel(ctx)
	defer cancelServer()

	cmd := exec.CommandContext(serverCtx, "opencode", "serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.Env = withoutEnv(os.Environ(), "OPENCODE_SERVER_PASSWORD")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start opencode server: %w", err)
	}
	defer stopCommand(cmd, cancelServer)

	client := &http.Client{}
	if err := waitForServer(ctx, client, baseURL); err != nil {
		return err
	}

	sessionID, err := p.createSession(ctx, client, baseURL, cwd, providerID, modelID, prompt)
	if err != nil {
		return err
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	eventResp, err := openEventStream(runCtx, client, baseURL, cwd)
	if err != nil {
		return err
	}
	defer eventResp.Body.Close()

	postErr := make(chan error, 1)
	go func() {
		err := p.postPrompt(runCtx, client, baseURL, cwd, sessionID, providerID, modelID, prompt)
		if err != nil {
			cancelRun()
		}
		postErr <- err
	}()

	resultSeen, err := p.readEvents(eventResp.Body, sessionID, fn)
	if err != nil {
		select {
		case postErrValue := <-postErr:
			if postErrValue != nil {
				return postErrValue
			}
		default:
		}
		return err
	}
	if !resultSeen {
		select {
		case postErrValue := <-postErr:
			if postErrValue != nil {
				return postErrValue
			}
		default:
		}
		return errors.New("opencode stream ended before result")
	}

	select {
	case err := <-postErr:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		// The final SSE event is enough for callers; do not hang if the HTTP
		// request takes a little longer to close.
		return nil
	}
}

func (p *provider) createSession(ctx context.Context, client *http.Client, baseURL, cwd, providerID, modelID, prompt string) (string, error) {
	body := map[string]any{
		"title": titleFromPrompt(prompt),
		"model": map[string]any{
			"providerID": providerID,
			"id":         modelID,
		},
	}
	if p.agent != "" {
		body["agent"] = p.agent
	}

	var resp struct {
		ID string `json:"id"`
	}
	if err := doJSON(ctx, client, http.MethodPost, baseURL+"/session?"+directoryQuery(cwd), body, &resp); err != nil {
		return "", fmt.Errorf("create opencode session: %w", err)
	}
	if resp.ID == "" {
		return "", errors.New("create opencode session: empty session id")
	}
	return resp.ID, nil
}

func (p *provider) postPrompt(ctx context.Context, client *http.Client, baseURL, cwd, sessionID, providerID, modelID, prompt string) error {
	body := map[string]any{
		"model": map[string]any{
			"providerID": providerID,
			"modelID":    modelID,
		},
		"parts": []map[string]any{{
			"type": "text",
			"text": prompt,
		}},
	}
	if p.agent != "" {
		body["agent"] = p.agent
	}

	if err := doJSON(ctx, client, http.MethodPost, baseURL+"/session/"+url.PathEscape(sessionID)+"/message?"+directoryQuery(cwd), body, nil); err != nil {
		return fmt.Errorf("send opencode prompt: %w", err)
	}
	return nil
}

func (p *provider) readEvents(body io.Reader, sessionID string, fn func(harness.Event)) (bool, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}
		eventSID := eventSessionID(data)
		if eventSID != "" && eventSID != sessionID {
			continue
		}
		for _, ev := range p.ParseStreamLine(data) {
			fn(ev)
			if ev.Type == harness.EventResult {
				return true, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("read opencode event stream: %w", err)
	}
	return false, nil
}

func openEventStream(ctx context.Context, client *http.Client, baseURL, cwd string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/event?"+directoryQuery(cwd), http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open opencode event stream: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("open opencode event stream: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return resp, nil
}

func waitForServer(ctx context.Context, client *http.Client, baseURL string) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, serverReadyTimeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if serverHealthy(deadlineCtx, client, baseURL) {
			return nil
		}
		select {
		case <-deadlineCtx.Done():
			return fmt.Errorf("wait for opencode server: %w", deadlineCtx.Err())
		case <-ticker.C:
		}
	}
}

func serverHealthy(ctx context.Context, client *http.Client, baseURL string) bool {
	reqCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, baseURL+"/global/health", http.NoBody)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func doJSON(ctx context.Context, client *http.Client, method, endpoint string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func eventSessionID(data string) string {
	var obj struct {
		Properties struct {
			SessionID string `json:"sessionID"`
			Part      struct {
				SessionID string `json:"sessionID"`
			} `json:"part"`
			Info struct {
				SessionID string `json:"sessionID"`
			} `json:"info"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		return ""
	}
	if obj.Properties.SessionID != "" {
		return obj.Properties.SessionID
	}
	if obj.Properties.Part.SessionID != "" {
		return obj.Properties.Part.SessionID
	}
	return obj.Properties.Info.SessionID
}

func splitModel(model string) (string, string, error) {
	providerID, modelID, ok := strings.Cut(model, "/")
	if !ok || providerID == "" || modelID == "" {
		return "", "", fmt.Errorf("opencode model must be in provider/model form, got %q", model)
	}
	return providerID, modelID, nil
}

func titleFromPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "harness run"
	}
	const maxTitleRunes = 80
	runes := []rune(prompt)
	if len(runes) <= maxTitleRunes {
		return prompt
	}
	return string(runes[:maxTitleRunes])
}

func directoryQuery(cwd string) string {
	return url.Values{"directory": []string{cwd}}.Encode()
}

func withoutEnv(env []string, keys ...string) []string {
	blocked := make(map[string]bool, len(keys))
	for _, key := range keys {
		blocked[key] = true
	}
	out := env[:0]
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if blocked[key] {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func freePort(ctx context.Context) (int, error) {
	var listenConfig net.ListenConfig
	ln, err := listenConfig.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find free port: %w", err)
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("find free port: listener did not return TCP address")
	}
	return addr.Port, nil
}

func stopCommand(cmd *exec.Cmd, cancel context.CancelFunc) {
	cancel()
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
}
