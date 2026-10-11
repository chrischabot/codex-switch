package routing

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshp123/codex-switch/internal/accounts"
	"github.com/joshp123/codex-switch/internal/store"
)

func TestEqualCapacityTasksUseBothAccountsAndKeepPinsWhenCapacityCrosses(t *testing.T) {
	database, err := store.OpenStoreDatabase(filepath.Join(t.TempDir(), "routing.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.CloseStoreDatabase()

	now := time.Now()
	facts := capacityWaitFacts{}
	for _, name := range []store.AccountName{"account-1", "account-2"} {
		facts = append(facts, accounts.AccountRoutingFact{
			AccountName: name, Credentials: accounts.Credentials{AccessToken: "test", AccountID: string(name)},
			AccountState: accounts.AccountReady, SupportedModelSlugs: []string{"luna"}, RefreshedAt: now,
			AllowanceSample: accounts.AllowanceSample{Allowed: true, ObservedAt: now, Windows: []accounts.ReportedWindow{
				{UsedPercent: 50, DurationSeconds: int64((7 * 24 * time.Hour).Seconds()), ResetsAt: now.Add(time.Hour)},
			}},
		})
	}
	router := CreateTaskRouter(database, facts)
	pins := map[store.CodexTaskID]store.AccountName{}
	counts := map[store.AccountName]int{}
	for index := range 32 {
		taskID := store.CodexTaskID(fmt.Sprintf("00000000-0000-4000-8000-%012d", index))
		decision, err := router.RouteForTask(taskID, store.ModelTurnWorkKind, "luna")
		if err != nil {
			t.Fatal(err)
		}
		pins[taskID] = decision.AccountName
		counts[decision.AccountName]++
	}
	if counts["account-1"] == 0 || counts["account-2"] == 0 {
		t.Fatalf("equal-capacity work concentrated on one account: %v", counts)
	}

	facts[0].AllowanceSample.Windows[0].UsedPercent = 51
	for taskID, accountName := range pins {
		decision, err := router.RouteForTask(taskID, store.ModelTurnWorkKind, "luna")
		if err != nil || decision.AccountName != accountName {
			t.Fatalf("continuation moved task %s from %s to %s: %v", taskID, accountName, decision.AccountName, err)
		}
		fresh, err := router.RouteForTask(taskID+"-new", store.ModelTurnWorkKind, "luna")
		if err != nil || fresh.AccountName != "account-2" {
			t.Fatalf("fresh work did not prefer greater capacity: %s, %v", fresh.AccountName, err)
		}
	}
	untagged, err := router.RouteForTask("", store.ModelTurnWorkKind, "luna")
	if err != nil || untagged.AccountName != "account-2" {
		t.Fatalf("model work without a task ID did not get the new-task account: %s, %v", untagged.AccountName, err)
	}
	if _, pinned, err := database.ReadTaskAccountPin(""); err != nil || pinned {
		t.Fatalf("model work without a task ID was pinned: pinned=%v, error=%v", pinned, err)
	}
	if _, err := router.RouteForTask("metadata", store.TaskMetadataWorkKind, "luna"); err != nil {
		t.Fatal(err)
	}
	if _, pinned, err := database.ReadTaskAccountPin("metadata"); err != nil || pinned {
		t.Fatalf("metadata created a task pin: pinned=%v, error=%v", pinned, err)
	}
}
