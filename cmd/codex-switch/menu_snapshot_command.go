package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/josh/codex-switch/internal/clientcapability"
	"github.com/josh/codex-switch/internal/codexstate"
	"github.com/josh/codex-switch/internal/menupb"
	"google.golang.org/protobuf/proto"
)

func menuSnapshotCommand(args []string) {
	flags := flag.NewFlagSet("menu-snapshot", flag.ExitOnError)
	cachedOnly := flags.Bool("cached-only", false, "read the last completed menu snapshot")
	address := flags.String("address", "", "Codex Switch loopback address (defaults to the effective Codex provider)")
	_ = flags.Parse(args)
	if flags.NArg() != 0 {
		log.Fatal("usage: codex-switch menu-snapshot [--cached-only] [--address 127.0.0.1:PORT]")
	}
	if *address != "" {
		if err := validateLocalListenAddress(*address); err != nil {
			log.Fatal(err)
		}
	}
	if err := writeMenuSnapshot(os.Stdout, dataDir(), *address, *cachedOnly); err != nil {
		log.Fatal(err)
	}
}

func writeMenuSnapshot(output io.Writer, directory, address string, cachedOnly bool) error {
	cachePath := filepath.Join(directory, "menu-snapshot.pb")
	if cachedOnly {
		encoded, err := os.ReadFile(cachePath)
		if err != nil {
			return err
		}
		if err := proto.Unmarshal(encoded, &menupb.MenuSnapshot{}); err != nil {
			return fmt.Errorf("read cached menu snapshot: %w", err)
		}
		_, err = output.Write(encoded)
		return err
	}
	snapshot, err := readMenuSnapshot(directory, address)
	if err != nil {
		return err
	}
	encoded, err := proto.Marshal(snapshot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".menu-snapshot-*")
	if err != nil {
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary.Name(), cachePath); err != nil {
		return err
	}
	_, err = output.Write(encoded)
	return err
}

func readMenuSnapshot(directory, address string) (*menupb.MenuSnapshot, error) {
	capability, err := clientcapability.Read(directory)
	if err != nil {
		return nil, err
	}
	config, problem := loadCodexModelConfig()
	if address == "" {
		if problem != "" {
			return nil, fmt.Errorf("read configured proxy: %s", problem)
		}
		if config.Provider != "openai" || config.OpenAIBaseURL == nil {
			return nil, fmt.Errorf("Codex is not configured to use the local proxy")
		}
		providerURL, err := url.Parse(*config.OpenAIBaseURL)
		if err != nil || providerURL.Scheme != "http" || providerURL.User != nil || providerURL.RawQuery != "" || providerURL.Fragment != "" || strings.TrimRight(providerURL.Path, "/") != clientcapability.PathPrefix(capability)+"/backend-api/codex" {
			return nil, fmt.Errorf("Codex provider URL does not match the local proxy")
		}
		address = providerURL.Host
	}
	if err := validateLocalListenAddress(address); err != nil {
		return nil, err
	}
	base := "http://" + address + clientcapability.PathPrefix(capability)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+menuSnapshotEndpoint+"?model="+url.QueryEscape(config.Model), nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		if requestError, ok := err.(*url.Error); ok {
			err = requestError.Err
		}
		return nil, fmt.Errorf("read local proxy at %s: %w", address, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("menu snapshot returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	snapshot := &menupb.MenuSnapshot{}
	if err := proto.Unmarshal(body, snapshot); err != nil {
		return nil, err
	}
	tasks := append([]*menupb.CompletedTask(nil), snapshot.CompletedTasks...)
	tasks = append(tasks, snapshot.GetProxyActivity().GetTasks()...)
	for _, period := range snapshot.ReportingPeriods {
		tasks = append(tasks, period.GetTaskUsage().GetTasks()...)
	}
	ids := make([]string, 0, len(tasks))
	seen := make(map[string]bool)
	for _, task := range tasks {
		if !seen[task.TaskId] && task.Title == nil {
			ids = append(ids, task.TaskId)
			seen[task.TaskId] = true
		}
	}
	reader, err := codexstate.DefaultReader()
	if err != nil {
		return nil, err
	}
	titles, err := reader.TaskTitles(ctx, ids)
	if err != nil {
		log.Printf("read Codex task titles: %v", err)
	}
	for _, task := range tasks {
		if task.Title != nil || task.TaskId == "" {
			continue
		}
		title := titles[task.TaskId]
		if title == "" {
			// Codex keeps no record of this task on this Mac: its history was
			// removed, or the run never saved one.
			title = "Codex task"
		}
		task.Title = &title
	}

	snapshot.Connection.NewTasksConfigured = problem == "" && config.Provider == "openai" && config.OpenAIBaseURL != nil && strings.TrimRight(*config.OpenAIBaseURL, "/") == base+"/backend-api/codex"
	return snapshot, nil
}
