package routing

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/joshp123/codex-switch/internal/accounts"
	"github.com/joshp123/codex-switch/internal/store"
)

// ErrNoAvailableAccount means new work must wait for account capacity.
var ErrNoAvailableAccount = errors.New("no account can take this work")

// RouteDecision carries credentials with the account identity so the relay cannot mix accounts.
type RouteDecision struct {
	AccountName store.AccountName
	Credentials accounts.Credentials
	// Assignment is empty when model work stays on its pinned account. Otherwise it
	// says, in product terms, which account now serves the work and why; the relay
	// logs it with the client that sent the request.
	Assignment string
}

// TaskRouter reads every pin decision from SQLite so external repairs are visible without restart.
type TaskRouter struct {
	storeDatabase *store.StoreDatabase
	accountFacts  AccountRoutingFactsSource
	currentTime   func() time.Time
}

// CreateTaskRouter keeps allowance refresh ownership outside routing's single selection rule.
func CreateTaskRouter(storeDatabase *store.StoreDatabase, accountFacts AccountRoutingFactsSource) *TaskRouter {
	return &TaskRouter{storeDatabase: storeDatabase, accountFacts: accountFacts, currentTime: time.Now}
}

// RouteForTask keeps model work on its task's account while that account is
// available and gives new work the account with the most weekly allowance left.
// Work without a task ID gets the same choice as a new task but is never pinned.
// Metadata requests never create or move a pin.
func (router *TaskRouter) RouteForTask(codexTaskID store.CodexTaskID, workKind store.WorkKind, modelSlug string) (RouteDecision, error) {
	facts, err := router.accountFacts.ReadAccountRoutingFacts()
	if err != nil {
		return RouteDecision{}, fmt.Errorf("read account routing facts: %w", err)
	}
	var pin store.TaskAccountPin
	pinned := false
	if codexTaskID != "" {
		if pin, pinned, err = router.storeDatabase.ReadTaskAccountPin(codexTaskID); err != nil {
			return RouteDecision{}, err
		}
	}
	pinnedAccountProblem := ""
	if pinned {
		fact, inPool := factForAccount(facts, pin.AccountName)
		if !inPool {
			pinnedAccountProblem = "no longer in the account pool"
		} else if pinnedAccountProblem, err = router.accountUnavailableReason(fact, modelSlug); err != nil {
			return RouteDecision{}, err
		} else if pinnedAccountProblem == "" {
			return RouteDecision{AccountName: pin.AccountName, Credentials: fact.Credentials}, nil
		}
	}

	selected, found, err := router.selectAccountWithMostWeeklyCapacity(facts, codexTaskID, "", modelSlug)
	if err != nil {
		return RouteDecision{}, err
	}
	if !found {
		router.accountFacts.RequestAccountRoutingFactsRefresh()
		return RouteDecision{}, fmt.Errorf("%w: %s", ErrNoAvailableAccount, router.describeUnavailableAccounts(facts, modelSlug))
	}

	decision := RouteDecision{AccountName: selected.AccountName, Credentials: selected.Credentials}
	if workKind != store.ModelTurnWorkKind {
		return decision, nil
	}
	choice := router.describeChoice(selected)
	switch {
	case codexTaskID == "":
		decision.Assignment = "served by " + choice + "; not pinned to a task"
		return decision, nil
	case pinned:
		moved, err := router.storeDatabase.CompareAndSwapTaskAccountPin(codexTaskID, pin.AccountName, selected.AccountName, router.currentTime().UTC())
		if err != nil {
			return RouteDecision{}, err
		}
		if !moved {
			return router.RouteForTask(codexTaskID, workKind, modelSlug)
		}
		decision.Assignment = fmt.Sprintf("moved from %s (%s) to %s", pin.AccountName, pinnedAccountProblem, choice)
		return decision, nil
	}
	winner, err := router.storeDatabase.PinTaskAccountIfAbsent(store.TaskAccountPin{
		CodexTaskID: codexTaskID, AccountName: selected.AccountName, PinnedAt: router.currentTime().UTC(),
	})
	if err != nil {
		return RouteDecision{}, err
	}
	if winner.AccountName != selected.AccountName {
		return router.RouteForTask(codexTaskID, workKind, modelSlug)
	}
	decision.Assignment = "new task assigned to " + choice
	return decision, nil
}

// ReportExhaustedTurn changes the account for the next client request; routing never resends work.
// It returns what happened to the task, for the proxy log.
func (router *TaskRouter) ReportExhaustedTurn(codexTaskID store.CodexTaskID, servedAccountName store.AccountName, resetsAt time.Time) string {
	outcome, err := router.reportExhaustedTaskPin(codexTaskID, servedAccountName, resetsAt)
	if err != nil {
		return fmt.Sprintf("%s hit its usage limit%s; could not move the task: %v", servedAccountName, resetSuffix(resetsAt), err)
	}
	return outcome
}

// ReportUnauthorizedTurn refreshes rejected credentials for Codex's next request without replaying the failed turn.
func (router *TaskRouter) ReportUnauthorizedTurn(servedAccountName store.AccountName, rejectedCredentials accounts.Credentials) {
	if err := router.accountFacts.RefreshAccountCredentialsAfterUnauthorized(servedAccountName, rejectedCredentials); err != nil {
		log.Printf("%s: OpenAI rejected its login and refreshing it failed: %v", servedAccountName, err)
	}
}

func (router *TaskRouter) reportExhaustedTaskPin(codexTaskID store.CodexTaskID, servedAccountName store.AccountName, resetsAt time.Time) (string, error) {
	limitReached := fmt.Sprintf("%s hit its usage limit%s", servedAccountName, resetSuffix(resetsAt))
	if err := router.storeDatabase.RecordAccountReportedReset(servedAccountName, resetsAt, router.currentTime().UTC()); err != nil {
		return "", err
	}
	facts, err := router.accountFacts.ReadAccountRoutingFacts()
	if err != nil {
		return "", fmt.Errorf("read account routing facts: %w", err)
	}
	selected, found, err := router.selectAccountWithMostWeeklyCapacity(facts, codexTaskID, servedAccountName, "")
	if err != nil {
		return "", err
	}
	router.accountFacts.RequestAccountRoutingFactsRefresh()
	if !found {
		return limitReached + "; no other account is available, so the next request waits for one", nil
	}
	if codexTaskID == "" {
		return limitReached + "; the next request goes to " + router.describeChoice(selected), nil
	}
	moved, err := router.storeDatabase.CompareAndSwapTaskAccountPin(
		codexTaskID, servedAccountName, selected.AccountName, router.currentTime().UTC())
	if err != nil || !moved {
		return limitReached, err
	}
	return limitReached + "; task moved to " + router.describeChoice(selected) + " for its next request", nil
}

func (router *TaskRouter) selectAccountWithMostWeeklyCapacity(facts []accounts.AccountRoutingFact, codexTaskID store.CodexTaskID, excludedAccountName store.AccountName, modelSlug string) (accounts.AccountRoutingFact, bool, error) {
	var selected accounts.AccountRoutingFact
	selectedCapacity := -1.0
	for _, fact := range facts {
		if fact.AccountName == excludedAccountName {
			continue
		}
		problem, err := router.accountUnavailableReason(fact, modelSlug)
		if err != nil {
			return accounts.AccountRoutingFact{}, false, err
		}
		if problem != "" {
			continue
		}
		capacity, _ := router.weeklyAllowanceLeft(fact)
		if selectedCapacity < capacity || (selectedCapacity == capacity && codexTaskID != "" && accountWinsEqualCapacityTie(codexTaskID, fact.AccountName, selected.AccountName)) {
			selected = fact
			selectedCapacity = capacity
		}
	}
	return selected, selectedCapacity >= 0, nil
}

// describeChoice explains why selectAccountWithMostWeeklyCapacity chose an account.
func (router *TaskRouter) describeChoice(selected accounts.AccountRoutingFact) string {
	weeklyFreePercent, reported := router.weeklyAllowanceLeft(selected)
	if !reported {
		return fmt.Sprintf("%s, which reports no weekly allowance", selected.AccountName)
	}
	return fmt.Sprintf("%s, which has the most weekly allowance left (%g%%)", selected.AccountName, weeklyFreePercent)
}

func (router *TaskRouter) describeUnavailableAccounts(facts []accounts.AccountRoutingFact, modelSlug string) string {
	if len(facts) == 0 {
		return "no account has been read yet"
	}
	problems := make([]string, 0, len(facts))
	for _, fact := range facts {
		problem, err := router.accountUnavailableReason(fact, modelSlug)
		if err != nil {
			problem = err.Error()
		}
		problems = append(problems, string(fact.AccountName)+" "+problem)
	}
	return strings.Join(problems, "; ")
}

func accountWinsEqualCapacityTie(taskID store.CodexTaskID, candidateAccountName, selectedAccountName store.AccountName) bool {
	candidateRank := sha256.Sum256([]byte(string(taskID) + "\x00" + string(candidateAccountName)))
	selectedRank := sha256.Sum256([]byte(string(taskID) + "\x00" + string(selectedAccountName)))
	return bytes.Compare(candidateRank[:], selectedRank[:]) > 0
}

// ReportUnsupportedModel prepares the next client request without resending rejected work.
func (router *TaskRouter) ReportUnsupportedModel(taskID store.CodexTaskID, accountName store.AccountName, modelSlug string) bool {
	router.accountFacts.ExcludeUnsupportedAccountModel(accountName, modelSlug)
	facts, err := router.accountFacts.ReadAccountRoutingFacts()
	if err != nil {
		log.Printf("read model routing facts: %v", err)
		return false
	}
	selected, found, err := router.selectAccountWithMostWeeklyCapacity(accountsWithKnownModels(facts), taskID, accountName, modelSlug)
	if err != nil {
		log.Printf("select supported model account: %v", err)
		return false
	}
	if !found {
		// The next model-aware request will skip the rejected account and use the
		// existing capacity wait until a supported account becomes available.
		rejectedAccountExcluded := false
		supportedAccountExists := false
		for _, fact := range facts {
			if fact.AccountName == accountName {
				rejectedAccountExcluded = fact.SupportedModelSlugs != nil && !accountSupportsModel(fact, modelSlug)
			} else if accountCredentialsUsable(fact.Credentials) && fact.SupportedModelSlugs != nil && accountSupportsModel(fact, modelSlug) {
				supportedAccountExists = true
			}
		}
		return rejectedAccountExcluded && supportedAccountExists
	}
	moved, err := router.storeDatabase.CompareAndSwapTaskAccountPin(taskID, accountName, selected.AccountName, router.currentTime().UTC())
	if err != nil {
		log.Printf("move unsupported model task pin: %v", err)
		return false
	}
	return moved
}

func accountsWithKnownModels(facts []accounts.AccountRoutingFact) []accounts.AccountRoutingFact {
	known := make([]accounts.AccountRoutingFact, 0, len(facts))
	for _, fact := range facts {
		if fact.SupportedModelSlugs != nil {
			known = append(known, fact)
		}
	}
	return known
}

// RouteForModelCatalogue reads model metadata from the widest observed account catalogue.
// Inference allowance does not restrict this read, and task pins are untouched.
func (router *TaskRouter) RouteForModelCatalogue() (RouteDecision, error) {
	facts, err := router.accountFacts.ReadAccountRoutingFacts()
	if err != nil {
		return RouteDecision{}, fmt.Errorf("read model catalogue routing facts: %w", err)
	}
	var selected *accounts.AccountRoutingFact
	for index := range facts {
		fact := &facts[index]
		if !accountCredentialsUsable(fact.Credentials) || fact.SupportedModelSlugs == nil {
			continue
		}
		if selected == nil || len(fact.SupportedModelSlugs) > len(selected.SupportedModelSlugs) {
			selected = fact
		}
	}
	if selected == nil {
		router.accountFacts.RequestAccountRoutingFactsRefresh()
		return RouteDecision{}, ErrNoAvailableAccount
	}
	return RouteDecision{AccountName: selected.AccountName, Credentials: selected.Credentials}, nil
}

// ReadAccountAvailability uses the same rule as admission. An empty reason means
// the account can take new work for modelSlug.
func (router *TaskRouter) ReadAccountAvailability(modelSlug string) ([]accounts.AccountRoutingFact, map[store.AccountName]string, error) {
	facts, err := router.accountFacts.ReadAccountRoutingFacts()
	if err != nil {
		return nil, nil, err
	}
	unavailableReasons := make(map[store.AccountName]string, len(facts))
	for _, fact := range facts {
		if unavailableReasons[fact.AccountName], err = router.accountUnavailableReason(fact, modelSlug); err != nil {
			return nil, nil, err
		}
	}
	return facts, unavailableReasons, nil
}
