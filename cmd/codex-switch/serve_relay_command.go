package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	accountapi "github.com/joshp123/codex-switch/internal/accounts"
	"github.com/joshp123/codex-switch/internal/clientcapability"
	"github.com/joshp123/codex-switch/internal/codexstate"
	"github.com/joshp123/codex-switch/internal/relay"
	"github.com/joshp123/codex-switch/internal/routing"
	"github.com/joshp123/codex-switch/internal/store"
	"github.com/joshp123/codex-switch/internal/usage"
)

const defaultUpstreamOrigin = "https://chatgpt.com"

// deployStatusEndpoint lets deploy.sh distinguish generations of the same
// revision by pid, per the deployment contract.
const deployStatusEndpoint = "/codex-switch/deploy-status"

// Explicit stamps support build tools that cannot discover Git worktrees.
var sourceRevision string
var sourceModified string

// serveRelay runs the recovered serving path: SQLite-backed pins, the one
// routing rule, and the byte-preserving relay. The command layer owns the
// listener and signals; the relay owns admitted work during a drain.
func serveRelay(tcpListenAddress string) {
	// Every generation appends to one log in the data directory, so a blue-green
	// update keeps one timeline and the status command can name the file.
	proxyLog, err := os.OpenFile(proxyLogPath(dataDir()), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		log.Fatalf("open proxy log: %v", err)
	}
	defer proxyLog.Close()
	log.SetOutput(proxyLog)
	log.SetFlags(log.LstdFlags)
	capability, err := clientcapability.Create(dataDir())
	if err != nil {
		log.Fatal(err)
	}

	upstreamOriginValue := strings.TrimSpace(os.Getenv("CODEX_SWITCH_UPSTREAM_ORIGIN"))
	if upstreamOriginValue == "" {
		upstreamOriginValue = defaultUpstreamOrigin
	}
	upstreamOrigin, err := url.Parse(upstreamOriginValue)
	if err != nil {
		log.Fatal(fmt.Errorf("upstream origin: %w", err))
	}

	storeDatabase, err := store.OpenStoreDatabase(filepath.Join(dataDir(), "codex-switch.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer storeDatabase.CloseStoreDatabase()
	if err := storeDatabase.StartModelRequestObservation(time.Now().UTC()); err != nil {
		log.Fatal(err)
	}

	accountMonitor := accountapi.NewMonitor(dataDir(), upstreamOrigin, codexstate.FindCodexExecutable(), &http.Client{Timeout: 30 * time.Second}, storeDatabase)
	usageLedger, err := usage.OpenUsageLedger(storeDatabase, dataDir(), &http.Client{Timeout: 15 * time.Second})
	if err != nil {
		log.Fatal(err)
	}
	factsContext, stopFacts := context.WithCancel(context.Background())
	var background sync.WaitGroup
	for _, run := range []func(context.Context){accountMonitor.Run, usageLedger.Run} {
		background.Add(1)
		go func() { defer background.Done(); run(factsContext) }()
	}
	defer func() { stopFacts(); background.Wait() }()
	taskRouter := routing.CreateTaskRouter(storeDatabase, accountMonitor)
	relayHandler, err := relay.CreateCodexTrafficRelayHandler(relay.RelayHandlerOptions{
		UpstreamOrigin:   upstreamOrigin,
		ClientCapability: capability,
		TaskRouter:       taskRouter,
		CompletionLedger: storeDatabase,
		RequestCounters:  storeDatabase,
	})
	if err != nil {
		log.Fatal(err)
	}

	// A generation listens only once it can route: during an update, new
	// connections reach it as soon as it listens.
	accountMonitor.WaitForFirstRead()
	listener, err := openRelayListener(tcpListenAddress)
	if err != nil {
		log.Fatal(err)
	}
	revision, dirty := buildRevision()
	revision = revision[:min(12, len(revision))]
	if dirty {
		revision += " (modified)"
	}
	serving := fmt.Sprintf("codex-switch %s serving on %s, process %d", revision, "http://"+listener.Addr().String(), os.Getpid())
	log.Print(serving)
	fmt.Printf("codex-switch listening on %s\ncodex-switch log: %s\n", "http://"+listener.Addr().String(), proxyLogPath(dataDir()))

	codexReader, err := codexstate.DefaultReader()
	if err != nil {
		log.Fatal(err)
	}
	clientURL := "http://" + listener.Addr().String() + clientcapability.PathPrefix(capability)
	snapshots := &menuSnapshots{dataDirectory: dataDir(), codexBaseURL: clientURL + "/backend-api/codex", router: taskRouter, usageLedger: usageLedger, codex: codexReader}
	background.Add(1)
	go func() { defer background.Done(); snapshots.Run(factsContext) }()
	if err := writeMenuSnapshotURL(dataDir(), clientURL+menuSnapshotEndpoint); err != nil {
		log.Fatalf("publish the menu snapshot URL: %v", err)
	}

	server := &http.Server{Handler: snapshots.serveOrRelay(capability, deployStatusOrRelay(capability, relayHandler))}
	serveFailed := make(chan error, 1)
	go func() { serveFailed <- server.Serve(listener) }()

	stopping := make(chan os.Signal, 1)
	signal.Notify(stopping, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stopping)

	select {
	case <-stopping:
		// Drain order matters. WebSockets start closing first, so their next
		// requests reach the new process while Shutdown closes the listener
		// and idle keep-alive connections and waits for HTTP and SSE handlers.
		// Only then does the relay wait for the remaining WebSockets. A
		// response in progress has no deadline: the product promise is that
		// it finishes.
		drainStarted := time.Now()
		log.Printf("process %d stopping: it accepts no new requests and finishes the requests it admitted", os.Getpid())
		relayHandler.CloseWebSocketsOnceIdle()
		if err := server.Shutdown(context.Background()); err != nil {
			log.Printf("process %d: closing the listener failed: %v", os.Getpid(), err)
		}
		relayHandler.BeginRelayDrain()
		if err := relayHandler.WaitForRelayDrain(context.Background()); err != nil {
			log.Printf("process %d: waiting for admitted WebSocket work failed: %v", os.Getpid(), err)
		}
		log.Printf("process %d stopped after %s: all admitted requests finished", os.Getpid(), time.Since(drainStarted).Round(time.Second))
	case err := <-serveFailed:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}
}

// deployStatusResponse identifies the running generation. Revision and pid
// are deploy-time evidence, deliberately absent from the menu contract.
type deployStatusResponse struct {
	Revision string `json:"revision"`
	Dirty    bool   `json:"dirty"`
	PID      int    `json:"pid"`
}

func deployStatusOrRelay(capability string, relayHandler http.Handler) http.Handler {
	statusPath := clientcapability.PathPrefix(capability) + deployStatusEndpoint
	predecessorStatusPath := clientcapability.PathPrefix(capability) + "/status"
	revision, dirty := buildRevision()
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == statusPath {
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(deployStatusResponse{Revision: revision, Dirty: dirty, PID: os.Getpid()})
			return
		}
		// Reject the retired control route without forwarding it to OpenAI.
		if request.URL.Path == predecessorStatusPath {
			http.Error(writer, "this codex-switch generation does not serve the predecessor status snapshot", http.StatusNotImplemented)
			return
		}
		relayHandler.ServeHTTP(writer, request)
	})
}

func buildRevision() (string, bool) {
	if sourceRevision != "" {
		return sourceRevision, sourceModified == "true"
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown", false
	}
	revision, dirty := "unknown", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	return revision, dirty
}

// printBinaryRevision lets deploy.sh learn the target revision without
// starting a server.
func printBinaryRevision() {
	revision, _ := buildRevision()
	fmt.Println(revision)
}

// proxyLogPath is the serving proxy's one log file.
func proxyLogPath(dataDirectory string) string {
	return filepath.Join(dataDirectory, "proxy.log")
}
